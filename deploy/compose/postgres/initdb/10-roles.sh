#!/bin/bash
# 仅在数据目录为空（首次启动）时由官方镜像入口执行。
# 角色分工与 docs/pgsql-ddl/README.md 一致：owner 建表 / web_app 只 DML / debezium 只读+复制。
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -v owner_pw="$PG_OWNER_PASSWORD" -v app_pw="$PG_APP_PASSWORD" -v dbz_pw="$PG_DEBEZIUM_PASSWORD" <<'SQL'
CREATE ROLE qubar_owner    LOGIN PASSWORD :'owner_pw';
CREATE ROLE qubar_web_app  LOGIN PASSWORD :'app_pw';
CREATE ROLE debezium       LOGIN REPLICATION PASSWORD :'dbz_pw';

ALTER DATABASE qubar OWNER TO qubar_owner;
REVOKE ALL ON DATABASE qubar FROM PUBLIC;
GRANT CONNECT ON DATABASE qubar TO qubar_web_app, debezium;

CREATE SCHEMA IF NOT EXISTS domains AUTHORIZATION qubar_owner;
GRANT USAGE ON SCHEMA domains TO qubar_web_app, debezium;

-- qubar_owner 以后新建的表：应用角色自动获得 DML，debezium 自动获得 SELECT（不给 TRUNCATE/ALTER）
ALTER DEFAULT PRIVILEGES FOR ROLE qubar_owner IN SCHEMA domains
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO qubar_web_app;
ALTER DEFAULT PRIVILEGES FOR ROLE qubar_owner IN SCHEMA domains
  GRANT SELECT ON TABLES TO debezium;

-- Debezium（pgoutput）读取的发布；表由 owner 后续建出，FOR TABLES IN SCHEMA 自动覆盖新表（PG15+）。
CREATE PUBLICATION dbz_publication FOR TABLES IN SCHEMA domains;
SQL
