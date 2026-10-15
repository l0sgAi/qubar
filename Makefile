# 本机数据库迁移（golang-migrate），规范见 migrations/README.md。
# 依赖：brew install golang-migrate squawk
#
#   make migrate-new name=add_post_pin_until   # 生成 migrations/<ts>_add_post_pin_until.up.sql
#   make migrate-up                            # 执行到最新
#   make migrate-check                         # 规范检查 + Squawk（与 CI 一致）
#   make migrate-version / migrate-force v=<version>

MIGRATIONS := migrations
# 本机 Homebrew PG 默认以当前系统用户连接；需要 qubar_owner 时：make migrate-up MIGRATE_USER=qubar_owner
MIGRATE_USER ?= $(USER)
MIGRATE_DB ?= postgres://$(MIGRATE_USER)@localhost:5432/qubar?sslmode=disable&x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1
MIGRATE := migrate -path $(MIGRATIONS) -database '$(MIGRATE_DB)'

.PHONY: migrate-new migrate-up migrate-version migrate-force migrate-check

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
