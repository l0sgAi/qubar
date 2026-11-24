"""校验 Pages API 接收的 tar 与浏览器入口，输出可记录到发布清单的摘要。"""
import hashlib
import json
import sys
import tarfile
from html.parser import HTMLParser
from pathlib import Path, PurePosixPath


class Assets(HTMLParser):
    def __init__(self):
        super().__init__()
        self.urls = []

    def handle_starttag(self, tag, attrs):
        url = dict(attrs).get("src" if tag == "script" else "href", "")
        if tag in {"script", "link"} and "assets/" in url:
            self.urls.append(url)


def verify(path):
    files = {}
    with tarfile.open(path, "r:") as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not (member.isdir() or member.isfile()):
                raise ValueError(f"不支持的 tar 条目 {name}")
            if member.isfile():
                if name.as_posix() in files:
                    raise ValueError(f"重复路径 {name}")
                files[name.as_posix()] = member
        def read(name):
            return archive.extractfile(files[name]).read()
        index = read("index.html")
        if not index or index != read("404.html") or not read("landing/index.html"):
            raise ValueError("SPA 回退或 landing 入口无效")
        identity = json.loads(read(".well-known/microsoft-identity-association.json"))
        if not identity.get("associatedApplications"):
            raise ValueError("缺少微软身份关联")
        assets = Assets()
        assets.feed(index.decode())
        if not assets.urls or any(not u.startswith("/assets/") or u[1:] not in files for u in assets.urls):
            raise ValueError(f"入口资源路径无效 {assets.urls}")
    print(f"Pages tar SHA-256 `{hashlib.sha256(path.read_bytes()).hexdigest()}`")
    print(f"\n{len(files)} 个文件，{len(assets.urls)} 个入口资源。已验证 index.html、404.html、landing、assets 和身份 JSON。")


if __name__ == "__main__":
    verify(Path(sys.argv[1]))
