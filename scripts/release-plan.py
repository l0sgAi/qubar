"""在完整 Git 树上预演发布。首次出现 web/ 时以已上线的前端树为比较基线。"""
import os
import subprocess
import sys

FRONTEND = "b8f551dc60f23a26835412fbd6a6008bab23d54a"
# 未知的前端构建输入默认会发布。排除源码树中已知的文档、测试、开发工具。
WEB_INPUTS = [".", ":(exclude,glob)**/*.md", ":(exclude)docs", ":(exclude)tests",
              ":(exclude).github", ":(exclude).agents", ":(exclude).claude", ":(exclude).gitignore",
              ":(exclude)LICENSE", ":(exclude)vitest.config.js", ":(exclude)scripts/check-build.sh"]


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def changed(base, head, paths):
    status = subprocess.run(["git", "diff", "--quiet", base, head, "--", *paths]).returncode
    if status not in (0, 1):
        raise RuntimeError(f"无法比较 {base} 与 {head}")
    return status == 1


def plan(base, head, product_base=None):
    product_base = product_base or base
    has_web = bool(git("ls-tree", base, "--", "web"))
    contract = changed(product_base, head, ["docs/api", ":(glob)docs/**/*frontend-api.md"])
    app = contract or changed(product_base, head, ["cmd", "pkg", "go.mod", "go.sum", "Dockerfile", ".dockerignore"])
    web = contract or changed(f"{product_base}:web" if git("ls-tree", product_base, "--", "web") else FRONTEND, f"{head}:web", WEB_INPUTS)
    rollback = os.getenv("ROLLBACK") == "true"
    return {
        "app": (app or os.getenv("FORCE_APP") == "true") and not rollback,
        "web": (web or os.getenv("FORCE_WEB") == "true") and not rollback,
        "imported": not has_web and not rollback,
        "db": (changed(base, head, ["migrations"]) or os.getenv("FORCE_DB") == "true") and not rollback,
        "infra": changed(base, head, ["deploy", ".github/actions", ".github/workflows/release.yml"]) or os.getenv("FORCE_INFRA") == "true",
    }


if __name__ == "__main__":
    for key, value in plan(*sys.argv[1:]).items():
        print(f"{key}={str(value).lower()}")
