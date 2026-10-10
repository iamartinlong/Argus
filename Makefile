.PHONY: build test test-integration frontend dist clean-dist clean-data docker up down agent install

BINARY=argus
CONFIG?=

# 构建完整二进制（先构建前端产物并嵌入）
build:
	cd web && npm install && npm run build
	go build -o build/$(BINARY) ./cmd/argus

# 构建 Agent 二进制
agent:
	go build -o build/argus-agent ./cmd/argus-agent

# 仅后端二进制（无前端 UI，webassets 走 noui 分支）
build-noui:
	go build -tags noui -o build/$(BINARY)-noui ./cmd/argus

# 文档站（VitePress）
docs:
	cd site-docs && npm install && npm run build

docs-dev:
	cd site-docs && npm run dev

frontend:
	cd web && npm install && npm run build

test:
	go test ./...

test-integration:
	go install github.com/letsencrypt/pebble/v2/cmd/pebble@v2.6.0
	PEBBLE_BIN="$$(go env GOPATH)/bin/pebble" PEBBLE_TESTDATA="$(CURDIR)/build/testdata" go test -tags integration ./internal/infrastructure/providers/acme/
	cd web && npm run build
	go test -tags integration ./internal/infrastructure/deploy/
	go test -tags integration ./internal/tests/e2e/

docker:
	cd deploy/docker && docker compose up -d --build

up: docker

down:
	cd deploy/docker && docker compose down

# 一键部署（Linux systemd 服务，需 root）
install: build
	sudo deploy/systemd/install.sh build/argus

# 清理构建产物。data/ 是运行时数据库（含证书、私钥、账号），不在此清理；
# 确需清空业务数据请手动执行: make clean-dist clean-data
clean-dist:
	rm -rf build webassets/dist

clean-data:
	rm -rf data
	@echo "警告: 已删除 data/ 目录（全部业务数据不可恢复）"
