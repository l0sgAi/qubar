package password

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	plain := "hunter2-correct-horse"

	hash, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("Hash returned non-PHC string: %q", hash)
	}

	ok, needsUpgrade, err := Verify(plain, hash)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned ok=false for correct password")
	}
	if needsUpgrade {
		t.Fatal("Verify returned needsUpgrade=true for fresh Argon2id hash")
	}
}

func TestVerifyWrongPassword(t *testing.T) {
	hash, _ := Hash("correct-password")
	ok, _, err := Verify("wrong-password", hash)
	if err != nil {
		t.Fatalf("Verify on wrong password should not error, got: %v", err)
	}
	if ok {
		t.Fatal("Verify returned ok=true for wrong password")
	}
}

func TestVerifyLegacySHA256(t *testing.T) {
	plain := "old-password"
	legacy := fmt.Sprintf("%x", sha256.Sum256([]byte(plain)))

	ok, needsUpgrade, err := Verify(plain, legacy)
	if err != nil {
		t.Fatalf("Verify failed on legacy SHA256: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned ok=false for correct legacy SHA256")
	}
	if !needsUpgrade {
		t.Fatal("Verify should return needsUpgrade=true for legacy SHA256")
	}

	// 错误密码也应返回 needsUpgrade=true（仍是旧格式哈希）
	ok2, needsUpgrade2, err := Verify("wrong", legacy)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if ok2 {
		t.Fatal("Verify returned ok=true for wrong password against legacy hash")
	}
	if !needsUpgrade2 {
		t.Fatal("needsUpgrade should still be true (format is still legacy) even when password is wrong")
	}

	// 大写十六进制的 legacy 哈希（如外部系统迁移）也应能校验通过
	upper := strings.ToUpper(legacy)
	ok3, needsUpgrade3, err := Verify(plain, upper)
	if err != nil {
		t.Fatalf("Verify failed on uppercase legacy SHA256: %v", err)
	}
	if !ok3 {
		t.Fatal("Verify returned ok=false for correct uppercase legacy SHA256")
	}
	if !needsUpgrade3 {
		t.Fatal("needsUpgrade should be true for uppercase legacy SHA256")
	}
}

func TestVerifyMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$", // 段数不够（2 段）
		"$argon2id$v=19$m=65536,t=3,p=4$onlysalt",      // 段数不够（5 段，缺 hash）
		"$argon2id$v=19$m=65536,t=3,p=4$!!!$@@@",       // base64 损坏
		"$argon2id$v=19$m=65536,t=0,p=4$dGVzdA$dGVzdA", // time=0 → 参数守卫拦截（防 panic）
		"$argon2id$v=19$m=65536,t=3,p=0$dGVzdA$dGVzdA", // threads=0 → 参数守卫拦截（防 panic）
		strings.Repeat("g", 64),                        // 64 字符但不是十六进制
	}
	for _, c := range cases {
		ok, _, err := Verify("any", c)
		if err == nil {
			t.Errorf("Verify(%q) should error on malformed hash", c)
		}
		if ok {
			t.Errorf("Verify(%q) returned ok=true on malformed hash", c)
		}
	}
}

func TestHashRandomness(t *testing.T) {
	plain := "same-input"
	h1, _ := Hash(plain)
	h2, _ := Hash(plain)
	if h1 == h2 {
		t.Fatal("Two Hash() calls on same input produced identical output (salt is not random)")
	}
	// 两个不同的哈希都应能校验通过
	for _, h := range []string{h1, h2} {
		ok, _, err := Verify(plain, h)
		if err != nil || !ok {
			t.Fatalf("Verify failed on Hash output %q: ok=%v err=%v", h, ok, err)
		}
	}
}

func TestIsLegacyHash(t *testing.T) {
	cases := map[string]bool{
		"":                                       false,
		strings.Repeat("a", 64):                  true,
		strings.Repeat("F", 64):                  true,
		strings.Repeat("0", 64):                  true,
		strings.Repeat("a", 63):                  false,
		strings.Repeat("a", 65):                  false,
		strings.Repeat("g", 64):                  false, // 非十六进制
		"$argon2id$v=19$m=65536,t=3,p=4$abc$def": false,
	}
	for input, want := range cases {
		if got := IsLegacyHash(input); got != want {
			t.Errorf("IsLegacyHash(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestSetParams(t *testing.T) {
	// 备份/恢复，避免污染其他测试
	orig := GetParams()
	t.Cleanup(func() { activeParams.Store(orig) })

	// 部分字段为 0 → 应使用默认值
	SetParams(Params{Time: 2})
	p := GetParams()
	if p.Time != 2 {
		t.Errorf("Time = %d, want 2", p.Time)
	}
	if p.Memory != defaultMemory {
		t.Errorf("Memory = %d, want default %d", p.Memory, defaultMemory)
	}
	if p.Threads != defaultThreads {
		t.Errorf("Threads = %d, want default %d", p.Threads, defaultThreads)
	}

	// 验证新参数生效：用较弱参数生成的哈希应能正常校验
	hash, err := Hash("test")
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	ok, _, err := Verify("test", hash)
	if err != nil || !ok {
		t.Fatalf("Verify after SetParams failed: ok=%v err=%v", ok, err)
	}
}
