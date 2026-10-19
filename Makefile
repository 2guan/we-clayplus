GO ?= go
PYTHON ?= python3
EXE := $(shell $(GO) env GOEXE)
.PHONY: help build example dashboard ai run mock test vet generate check-secrets
help:
	@printf '%s\n' 'make build          检查库编译' 'make example        编译示例插件' 'make dashboard      编译控制台插件' 'make ai             编译AI智能体插件' 'make run            启动网关并装载插件' 'make mock           启动脱机Mock预览模式(免扫码免Core)' 'make test           运行离线竞态测试' 'make vet            静态检查' 'make generate       重新生成 protobuf/gRPC 代码' 'make check-secrets  检查 Git 暂存内容'
build:
	$(GO) build ./gateway/... ./sdk/...
example:
	mkdir -p bin
	$(GO) build -trimpath -o bin/we-clay-plugin-example$(EXE) ./examples/plugin
dashboard:
	mkdir -p bin
	$(GO) build -trimpath -o bin/we-clay-plugin-dashboard$(EXE) ./plugins/dashboard
ai:
	mkdir -p bin
	$(GO) build -trimpath -o bin/we-clay-plugin-ai$(EXE) ./plugins/ai
run: dashboard ai
	$(GO) run ./examples/bot
mock: dashboard ai
	WE_CLAY_LOGIN_PORT=5668 WE_CLAY_CORE_PORT=5666 WE_CLAY_DASHBOARD_PORT=5699 $(GO) run ./examples/bot -mock
test:

	$(GO) test -race ./...
vet:
	$(GO) vet ./...
generate:
	$(PYTHON) scripts/generate-proto.py
check-secrets:
	$(PYTHON) scripts/check-secrets.py
