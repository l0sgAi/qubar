"""从站点根路径读取实际响应，与此次部署的 tar 逐字节比较（Pages 深链可能返回 404）。"""
import sys
import tarfile
import time
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


def smoke(site, archive):
    with tarfile.open(archive) as package:
        files = {m.name.removeprefix("./"): m for m in package if m.isfile()}
        routes = {"/": "index.html", "/landing/": "landing/index.html",
                  "/.well-known/microsoft-identity-association.json": ".well-known/microsoft-identity-association.json"}
        index = package.extractfile(files["index.html"]).read().decode()
        routes.update({"/" + p: p for p in files if p.startswith("assets/") and f'"/{p}"' in index})
        deep = ["/success", "/messages/smoke", "/post/smoke", "/invite/smoke"]
        routes.update(dict.fromkeys(deep, "404.html"))
        for url, name in routes.items():
            wanted = package.extractfile(files[name]).read()
            req = Request(site.rstrip("/") + url, headers={"Cache-Control": "no-cache"})
            try:
                response = urlopen(req, timeout=10)
            except HTTPError as exc:
                if exc.code != 404 or url not in deep:
                    raise
                response = exc
            with response:
                if response.read() != wanted:
                    raise ValueError(f"线上内容与候选包不符 {url}")


if __name__ == "__main__":
    for attempt in range(6):
        try:
            smoke(sys.argv[1], Path(sys.argv[2]))
            break
        except (URLError, ValueError):
            if attempt == 5:
                raise
            time.sleep(10)
