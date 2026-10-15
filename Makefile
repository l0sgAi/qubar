# 本机数据库迁移（golang-migrate），规范见 migrations/README.md。
# 依赖：brew install golang-migrate squawk；migrate-test 只需要 Docker
#
#   make migrate-new name=add_post_pin_until   # 生成 migrations/<ts>_add_post_pin_until.up.sql
#   make migrate-up                            # 执行到最新
#   make migrate-check                         # 规范检查 + Squawk（与 CI 一致）
#   make migrate-test                          # 一次性 PG18 容器从空库回放全部迁移（与 CI 一致）
#   make migrate-version / migrate-force v=<version>

MIGRATIONS := migrations
MIGRATIONS_TABLE := x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1
# 以 qubar_owner 执行：新表归它所有，default privileges 才会给 qubar_web_app / debezium 授权
# （以超级用户建的表应用会 permission denied）。本机没有该角色时：make migrate-up MIGRATE_USER=$USER
MIGRATE_USER ?= qubar_owner
MIGRATE_DB ?= postgres://$(MIGRATE_USER)@localhost:5432/qubar?sslmode=disable&$(MIGRATIONS_TABLE)
MIGRATE := migrate -path $(MIGRATIONS) -database '$(MIGRATE_DB)'

# 与 .github/workflows/ci.yml db job 一致
MIGRATE_IMAGE ?= migrate/migrate:v4.20.1
PG_IMAGE ?= postgres:18.4

.PHONY: migrate-new migrate-up migrate-version migrate-force migrate-check migrate-test

migrate-new:
	@echo '$(name)' | grep -Eq '^[a-z0-9_]+$$' || { echo "用法: make migrate-new name=snake_case_desc"; exit 1; }
	@f=$(MIGRATIONS)/$$(date +%Y%m%d%H%M%S)_$(name).up.sql; \
	printf '%s\n' \
	  "-- $(name)" \
	  "-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。" \
	  "-- 新建 domains 表：进 CDC 则登记 deploy/ansible/group_vars/all.yml 的 cdc_tables；不进 CDC 则在文件里单独写一行 -- cdc: none" \
	  "SET lock_timeout = '5s';" \
	  "" > $$f; \
	echo "created $$f"

migrate-up:
	$(MIGRATE) up

migrate-version:
	$(MIGRATE) version

migrate-force:
	@[ -n "$(v)" ] || { echo "用法: make migrate-force v=<version>"; exit 1; }
	$(MIGRATE) force $(v)

migrate-check:
	scripts/check-migrations.sh
	squawk --exclude-path='*_baseline.up.sql' '$(MIGRATIONS)/*.up.sql'

# 空库回放：pg-init.sql 建角色 / schema / 默认权限 → qubar_owner migrate up → 二次 up 必须 no-op。
# 容器与网络用完即删（失败也删），不占用本机端口、不碰本机 dev 库。
migrate-test:
	@set -eu; \
	n=qubar-migrate-test-$$$$; \
	trap 'docker rm -f $$n >/dev/null 2>&1 || true; docker network rm $$n >/dev/null 2>&1 || true' EXIT; \
	docker network create $$n >/dev/null; \
	docker run -d --name $$n --network $$n -e POSTGRES_PASSWORD=ci -e POSTGRES_DB=qubar $(PG_IMAGE) >/dev/null; \
	i=0; until docker exec $$n pg_isready -q -h 127.0.0.1 -U postgres -d qubar; do \
	  i=$$((i + 1)); [ $$i -lt 60 ] || { echo "postgres 未就绪"; docker logs $$n; exit 1; }; sleep 1; \
	done; \
	docker exec -i $$n psql -q -U postgres -d qubar -v ON_ERROR_STOP=1 < .github/ci/pg-init.sql; \
	migrate() { docker run --rm --network $$n -e PGPASSWORD=ci -v "$(CURDIR)/$(MIGRATIONS):/migrations:ro" $(MIGRATE_IMAGE) \
	  -path /migrations -database 'postgres://qubar_owner@'"$$n"':5432/qubar?sslmode=disable&$(MIGRATIONS_TABLE)' "$$@"; }; \
	migrate up; \
	out=$$(migrate up 2>&1); echo "$$out"; \
	echo "$$out" | grep -q 'no change' || { echo "二次 up 不是 no-op"; exit 1; }; \
	migrate version; \
	docker exec $$n psql -tA -U postgres -d qubar -c "select count(*) || ' tables in domains' from pg_tables where schemaname = 'domains'"; \
	echo "migrate-test ok"
