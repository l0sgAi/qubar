"""用冻结前端的真实父历史预演变更选择和语义化版本，无需部署权限。"""
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = "b8f551dc60f23a26835412fbd6a6008bab23d54a"
PREV = "v0.9.0"


def rehearse(repo):
    def run(*args):
        return subprocess.check_output(args, cwd=repo, text=True).strip()

    def git(*args):
        return run("git", *args)

    git("config", "user.email", "rehearsal@example.invalid")
    git("config", "user.name", "Release rehearsal")
    git("checkout", "-q", "--detach", PREV)
    git("read-tree", "--prefix=web/", "-u", SOURCE)
    imported = git("commit-tree", git("write-tree"), "-p", PREV, "-p", SOURCE, "-m", "chore: import history")
    git("reset", "-q", "--hard", imported)

    def check(base, expected, next_version=None):
        lines = run("python3", ROOT / "scripts/release-plan.py", base, "HEAD").splitlines()
        selected = {line.split("=")[0] for line in lines if line.endswith("=true")}
        assert selected == expected, (selected, expected)
        if next_version is not None:
            version = run(ROOT / "scripts/next-version.sh", "HEAD")
            assert f"next={next_version}" == version.splitlines()[-1], version
        print(f"{git('log','-1','--format=%s')} → {','.join(sorted(selected)) or '无组件变更'}；版本 {next_version or '无自动发布'}")

    check(PREV, {"imported"}, "")
    cases = [
        ("fix(web): first frontend fix", ["web/src/probe.js"], {"web"}, "v0.9.1"),
        ("fix(api): first API fix", ["pkg/probe.go"], {"app"}, "v0.9.1"),
        ("feat: mixed product change", ["pkg/probe.go", "web/src/probe.js"], {"web", "app"}, "v0.10.0"),
        ("fix: cross-component contract", ["docs/api/probe.md"], {"web", "app"}, "v0.9.1"),
        ("chore: database migration", ["migrations/probe.sql"], {"db"}, None),
        ("chore: release workflow", [".github/workflows/release.yml"], {"infra"}, None),
        ("docs: contributor notes", ["web/README.md", "docs/guide.md"], set(), None),
    ]
    for title, files, selected, version in cases:
        git("reset", "-q", "--hard", imported)
        for name in files:
            with (repo / name).open("a") as stream:
                stream.write("\nrelease rehearsal\n")
        git("add", "--", *files)
        git("commit", "-qm", title)
        check(imported, selected, version)
        assert run(ROOT / "scripts/product-log.sh", PREV, "HEAD", "%s") == title


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as temp:
        repo = Path(temp) / "repo"
        subprocess.run(["git", "clone", "--quiet", "--shared", "--no-checkout", ROOT, repo], check=True)
        rehearse(repo)
