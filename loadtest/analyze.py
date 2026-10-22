#!/usr/bin/env python3
"""汇总一轮压测结果：k6 时序（k6.csv.gz）按时间窗聚合 RPS / 延迟分位 / 错误率，并对齐 monitor.sh 采样。

用法：python3 analyze.py results/read-stress-120000 [--window 60]
输出：
  1. 按时间窗（默认 60s，与阶梯加压的每档对齐）：实际 RPS、p50/p95/p99、错误率，以及同窗口主机 CPU、
     qubar / postgres / redis / elasticsearch 容器 CPU、PG 活跃连接、Redis ops/s、消费组积压峰值
  2. 按接口（name 标签）整体：请求数、p95、p99、错误率
"""
import argparse
import csv
import gzip
import os
from collections import defaultdict


def pct(vals, p):
    if not vals:
        return 0.0
    vals = sorted(vals)
    k = min(len(vals) - 1, max(0, int(round(p / 100.0 * len(vals))) - 1))
    return vals[k]


def load_k6(path):
    """返回 [(ts, name, kind, duration_ms, failed)]：取 http_req_duration 行，失败以 expected_response 判定（同 http_req_failed）。"""
    rows = []
    with gzip.open(path, "rt") as f:
        for row in csv.DictReader(f):
            if row["metric_name"] != "http_req_duration":
                continue
            kind = ""
            for kv in row.get("extra_tags", "").split("&"):
                if kv.startswith("kind="):
                    kind = kv[5:]
            failed = 0 if row.get("expected_response") == "true" else 1
            rows.append((int(row["timestamp"]), row.get("name", ""), kind, float(row["metric_value"]), failed))
    return rows


def load_series(path, key_cols, val_col):
    """读 monitor CSV：返回 {ts: {key: value}}。"""
    out = defaultdict(dict)
    if not os.path.exists(path):
        return out
    with open(path) as f:
        for row in csv.DictReader(f):
            try:
                v = float(row[val_col])
            except (ValueError, KeyError):
                continue
            k = "|".join(row[c] for c in key_cols) if key_cols else ""
            out[int(row["ts"])][k] = v
    return out


def window_max(series, start, end, key=None, agg=max):
    vals = []
    for ts, kv in series.items():
        if start <= ts < end:
            if key is None:
                vals.append(sum(kv.values()))
            elif key in kv:
                vals.append(kv[key])
    return agg(vals) if vals else 0.0


def avg(vals):
    vals = list(vals)
    return sum(vals) / len(vals) if vals else 0.0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("dir")
    ap.add_argument("--window", type=int, default=60)
    a = ap.parse_args()

    rows = load_k6(os.path.join(a.dir, "k6.csv.gz"))
    rows = [r for r in rows if r[2] != "sse"] or rows
    if not rows:
        print("无请求数据")
        return
    t0 = min(r[0] for r in rows)

    host = load_series(os.path.join(a.dir, "host.csv"), [], "cpu_pct")
    docker = load_series(os.path.join(a.dir, "docker.csv"), ["container"], "cpu_pct")
    dmem = load_series(os.path.join(a.dir, "docker.csv"), ["container"], "mem_mb")
    pg = load_series(os.path.join(a.dir, "pg.csv"), [], "active")
    pgtotal = load_series(os.path.join(a.dir, "pg.csv"), [], "total")
    slot = load_series(os.path.join(a.dir, "pg.csv"), [], "slot_lag_mb")
    redis = load_series(os.path.join(a.dir, "redis.csv"), [], "ops_per_sec")
    lag = load_series(os.path.join(a.dir, "lag.csv"), ["group"], "lag")

    buckets = defaultdict(list)
    for r in rows:
        buckets[(r[0] - t0) // a.window].append(r)

    print(f"## 时间窗汇总（{a.window}s）\n")
    print("| 窗口 | RPS | p50 | p95 | p99 | 错误率 | 主机CPU% | qubar CPU% | qubar MB | PG CPU% | Redis CPU% | ES CPU% | PG活跃/总连接 | Redis ops/s | 消费积压 | WAL积压MB |")
    print("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for b in sorted(buckets):
        rs = buckets[b]
        ds = [r[3] for r in rs]
        start, end = t0 + b * a.window, t0 + (b + 1) * a.window
        span = min(a.window, max(1, max(r[0] for r in rs) - start + 1))
        err = sum(r[4] for r in rs) / len(rs) * 100
        c = lambda n: window_max(docker, start, end, n, avg)  # noqa: E731
        print(
            f"| {b * a.window}-{(b + 1) * a.window}s | {len(rs) / span:.0f} | {pct(ds, 50):.0f} | {pct(ds, 95):.0f} | {pct(ds, 99):.0f} "
            f"| {err:.2f}% | {window_max(host, start, end, None, avg):.0f} | {c('qubar'):.0f} | {window_max(dmem, start, end, 'qubar'):.0f} "
            f"| {c('qubar-postgres'):.0f} | {c('qubar-redis'):.0f} | {c('qubar-elasticsearch'):.0f} "
            f"| {window_max(pg, start, end):.0f}/{window_max(pgtotal, start, end):.0f} | {window_max(redis, start, end):.0f} "
            f"| {window_max(lag, start, end):.0f} | {window_max(slot, start, end):.1f} |"
        )

    by_name = defaultdict(list)
    for r in rows:
        by_name[r[1]].append(r)
    print("\n## 按接口\n")
    print("| 接口 | 请求数 | p50 | p95 | p99 | 错误率 |")
    print("|---|---|---|---|---|---|")
    for n in sorted(by_name, key=lambda k: -len(by_name[k])):
        rs = by_name[n]
        ds = [r[3] for r in rs]
        err = sum(r[4] for r in rs) / len(rs) * 100
        print(f"| {n} | {len(rs)} | {pct(ds, 50):.0f} | {pct(ds, 95):.0f} | {pct(ds, 99):.0f} | {err:.2f}% |")

    pg_tables(a.dir)


def pg_tables(d):
    """pg_stat_user_tables 前后快照之差：本轮各表的扫描 / 写入量。"""
    def load(name):
        path = os.path.join(d, name)
        if not os.path.exists(path):
            return {}
        out = {}
        with open(path) as f:
            for row in csv.reader(f):
                if len(row) == 7:
                    out[row[0]] = [int(x) for x in row[1:]]
        return out

    before, after = load("pg_tables_before.csv"), load("pg_tables_after.csv")
    if not after:
        return
    diff = []
    for t, v in after.items():
        b = before.get(t, [0] * 6)
        dv = [x - y for x, y in zip(v, b)]
        if any(dv):
            diff.append((t, dv))
    diff.sort(key=lambda x: -(x[1][1] + x[1][2] + x[1][3] + x[1][4]))
    print("\n## PG 表访问（本轮增量）\n")
    print("| 表 | 顺序扫描 | 顺序扫描读行 | 索引扫描 | 插入 | 更新 | 删除 |")
    print("|---|---|---|---|---|---|---|")
    for t, dv in diff[:15]:
        print(f"| {t} | " + " | ".join(str(x) for x in dv) + " |")


if __name__ == "__main__":
    main()
