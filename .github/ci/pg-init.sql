-- CI 临时库初始化（ci.yml db job，以 postgres 超级用户执行）：模拟生产 roles.sql + grants.sql 的角色 / schema / 默认权限，
-- 之后以 qubar_owner 从零回放 migrations/。密码仅用于 CI 一次性容器。
\set ON_ERROR_STOP on

CREATE ROLE qubar_owner   LOGIN PASSWORD 'ci';
CREATE ROLE qubar_web_app LOGIN PASSWORD 'ci';
CREATE ROLE debezium      LOGIN REPLICATION PASSWORD 'ci';
ALTER ROLE qubar_owner SET lock_timeout = '5s';

ALTER DATABASE qubar OWNER TO qubar_owner;
REVOKE ALL ON DATABASE qubar FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE qubar TO qubar_web_app;
GRANT CONNECT ON DATABASE qubar TO debezium;

CREATE SCHEMA domains AUTHORIZATION qubar_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA domains TO qubar_web_app, debezium;

ALTER DEFAULT PRIVILEGES FOR ROLE qubar_owner IN SCHEMA domains
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO qubar_web_app;
ALTER DEFAULT PRIVILEGES FOR ROLE qubar_owner IN SCHEMA domains
  GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO qubar_web_app;
ALTER DEFAULT PRIVILEGES FOR ROLE qubar_owner IN SCHEMA domains
  GRANT SELECT ON TABLES TO debezium;
