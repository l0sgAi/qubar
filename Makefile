# Qubar 常用命令。agent / 新人从这里开始（l0sgAi/qubar#67）：
#
#   make verify       # 提交 / 开 PR 前必须全绿：格式 + vet + lint + 单测 + 迁移检查与空库回放（与 CI 一致）
#   make dev-up       # 起本机开发依赖栈（PG18 / Redis / ES+IK / Redpanda）并执行迁移，只需要 Docker
#   make dev-run      # 用 configs/config.dev.yaml 启动应用（:8888）
#   make dev-down     # 停栈保留数据；make dev-reset 连数据卷一起删
#
# 工具（golangci-lint / squawk）按固定版本自动下载到 bin/（已 git-ignore），无需 brew。
#
# 数据库迁移（golang-migrate），规范见 migrations/README.md：
#   make migrate-new name=add_post_pin_until   # 生成 migrations/<ts>_add_post_pin_until.up.sql
#   make dev-migrate                           # 对开发栈执行到最新（只需要 Docker）
#   make migrate-check                         # 规范检查 + Squawk（与 CI 一致）
#   make migrate-test                          # 一次性 PG18 容器从空库回放全部迁移（与 CI 一致）
#   make migrate-up / migrate-version / migrate-force v=<version>   # 本机 Homebrew PG（brew install golang-migrate）

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

# 固定版本的工具，下载到 bin/（文件名带版本号，升级版本即重新下载）。SQUAWK_VERSION 与 ci.yml 一致。
BIN := $(CURDIR)/bin
GOLANGCI_LINT_VERSION := v2.14.0
GOLANGCI_LINT := $(BIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
SQUAWK_VERSION := 2.69.0
SQUAWK := $(BIN)/squawk-$(SQUAWK_VERSION)

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

migrate-check: $(SQUAWK)
	scripts/check-migrations.sh
	@files=$$(ls $(MIGRATIONS)/*.up.sql | grep -v '_baseline\.up\.sql$$' || true); \
	if [ -z "$$files" ]; then echo "squawk: 没有基线以外的迁移"; else $(SQUAWK) $$files; fi

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

# ---------- 验证（与 .github/workflows/ci.yml 一致）----------
# lint 默认全量检查；可选 make lint LINT_BASE=origin/develop 只看相对该分支 merge-base 的新增改动（本地提速用）
LINT_BASE ?=

.PHONY: verify fmt-check vet lint test

verify: fmt-check vet lint test migrate-check migrate-test
	@echo "verify ok"

fmt-check:
	@out=$$(gofmt -l cmd pkg); [ -z "$$out" ] || { echo "未 gofmt 的文件（gofmt -w cmd pkg）："; echo "$$out"; exit 1; }

vet:
	go vet ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run $(if $(LINT_BASE),--new-from-merge-base=$(LINT_BASE)) ./...

test:
	go test ./pkg/...

$(GOLANGCI_LINT):
	@mkdir -p $(BIN)
	GOBIN=$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(BIN)/golangci-lint $@

$(SQUAWK):
	@mkdir -p $(BIN)
	@os=$$(uname -s | tr A-Z a-z); arch=$$(uname -m); \
	case $$arch in x86_64|amd64) arch=x64 ;; aarch64|arm64) arch=arm64 ;; *) echo "不支持的架构 $$arch"; exit 1 ;; esac; \
	curl -fsSL -o $@.tmp "https://github.com/sbdchd/squawk/releases/download/v$(SQUAWK_VERSION)/squawk-$$os-$$arch" && chmod +x $@.tmp && mv $@.tmp $@

# ---------- 本机 / agent 开发栈（deploy/compose/dev）----------
# 端口：PG 25432 / Redis 26379 / ES 29200 / Redpanda 29092（避开 15432：ssh -L 到生产库的本地端口）
DEV_COMPOSE := docker compose -f deploy/compose/dev/compose.yml
DEV_CONFIG := configs/config.dev.yaml

.PHONY: dev-up dev-migrate dev-run dev-down dev-reset dev-ps dev-logs dev-psql

dev-up:
	$(DEV_COMPOSE) up -d --build --wait
	$(MAKE) dev-migrate

# 以 qubar_owner 在 compose 网络内执行，不依赖本机 migrate
dev-migrate:
	docker run --rm --network qubar-dev_default -v "$(CURDIR)/$(MIGRATIONS):/migrations:ro" $(MIGRATE_IMAGE) \
	  -path /migrations -database 'postgres://qubar_owner:ci@postgres:5432/qubar?sslmode=disable&$(MIGRATIONS_TABLE)' up

dev-run:
	go run cmd/main.go -c $(DEV_CONFIG) -b ""

dev-down:
	$(DEV_COMPOSE) down

dev-reset:
	$(DEV_COMPOSE) down -v

dev-ps:
	$(DEV_COMPOSE) ps

dev-logs:
	$(DEV_COMPOSE) logs -f --tail=100

# 以超级用户进开发库：make dev-psql，或 make dev-psql q="select ..."
dev-psql:
	$(DEV_COMPOSE) exec postgres psql -U postgres -d qubar $(if $(q),-c "$(q)")
