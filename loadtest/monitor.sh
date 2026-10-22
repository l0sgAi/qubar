#!/usr/bin/env bash
# 压测期间的临时监控采样（在服务器上运行，线上暂无 Prometheus）。每 INTERVAL 秒追加一行 CSV：
#   host.csv    负载、CPU 使用率、内存
#   docker.csv  各容器 CPU / 内存
#   pg.csv      连接数（按状态）、等锁数、CDC 复制槽 WAL 积压
#   redis.csv   客户端数、内存、ops/s
#   lag.csv     Redpanda 各消费组积压
# 用法：OUT=results/monitor-read-stress ./monitor.sh &   结束：kill %1
set -uo pipefail

INTERVAL="${INTERVAL:-5}"
OUT="${OUT:-results/monitor-$(date +%H%M%S)}"
PG="${PG_CONTAINER:-qubar-postgres}"
REDIS="${REDIS_CONTAINER:-qubar-redis}"
RPK="${RPK_CONTAINER:-qubar-redpanda}"
mkdir -p "$OUT"

echo "ts,load1,cpu_pct,mem_used_mb" >"$OUT/host.csv"
echo "ts,container,cpu_pct,mem_mb" >"$OUT/docker.csv"
echo "ts,active,idle,idle_in_tx,total,lock_waiting,slot_lag_mb" >"$OUT/pg.csv"
echo "ts,clients,blocked,used_mb,ops_per_sec" >"$OUT/redis.csv"
echo "ts,group,lag" >"$OUT/lag.csv"

read_cpu() { awk '/^cpu /{printf "%d %d\n", $2+$3+$4+$6+$7+$8, $5}' /proc/stat; }
read -r busy0 idle0 < <(read_cpu)
tick=0

while true; do
  ts="$(date +%s)"
  read -r busy1 idle1 < <(read_cpu)
  cpu="$(awk -v b="$((busy1 - busy0))" -v i="$((idle1 - idle0))" 'BEGIN{printf "%.1f", ((b+i) > 0 ? 100*b/(b+i) : 0)}')"
  busy0=$busy1 idle0=$idle1
  echo "$ts,$(cut -d' ' -f1 /proc/loadavg),$cpu,$(free -m | awk '/^Mem:/{print $3}')" >>"$OUT/host.csv"

  docker stats --no-stream --format '{{.Name}},{{.CPUPerc}},{{.MemUsage}}' |
    awk -F, -v ts="$ts" '{
      split($3, m, " / "); v = m[1]; mb = v + 0
      if (v ~ /GiB/) mb *= 1024; else if (v ~ /KiB/) mb /= 1024
      gsub("%", "", $2); printf "%s,%s,%s,%.0f\n", ts, $1, $2, mb
    }' >>"$OUT/docker.csv"

  docker exec "$PG" psql -U postgres -d qubar -AtF, -c "
    select
      count(*) filter (where state = 'active'),
      count(*) filter (where state = 'idle'),
      count(*) filter (where state like 'idle in transaction%'),
      count(*),
      count(*) filter (where wait_event_type = 'Lock'),
      (select coalesce(round(max(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)) / 1048576.0, 1), 0) from pg_replication_slots)
    from pg_stat_activity where datname = 'qubar'" | sed "s/^/$ts,/" >>"$OUT/pg.csv"

  docker exec "$REDIS" sh -c 'redis-cli ${REDIS_PASSWORD:+-a "$REDIS_PASSWORD"} --no-auth-warning info' |
    tr -d '\r' | awk -F: -v ts="$ts" '
      /^connected_clients:/ {c=$2} /^blocked_clients:/ {b=$2}
      /^used_memory:/ {m=$2} /^instantaneous_ops_per_sec:/ {o=$2}
      END {printf "%s,%s,%s,%.0f,%s\n", ts, c, b, m/1048576, o}' >>"$OUT/redis.csv"

  # 消费组积压较慢，每 3 个周期采一次
  if [ $((tick % 3)) -eq 0 ]; then
    for g in $(docker exec "$RPK" rpk group list 2>/dev/null | awk 'NR>1{print $2}'); do
      lag="$(docker exec "$RPK" rpk group describe "$g" 2>/dev/null | awk '/^TOTAL-LAG/{print $2}')"
      echo "$ts,$g,${lag:-0}" >>"$OUT/lag.csv"
    done
  fi
  tick=$((tick + 1))
  sleep "$INTERVAL"
done
