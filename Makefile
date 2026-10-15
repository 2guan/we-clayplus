GO ?= go
PYTHON ?= python3
EXE := $(shell $(GO) env GOEXE)
.PHONY: help build example test vet generate check-secrets
help:
	@printf '%s\n' 'make run            启动真实微信与控制台' 'make mock           启动完全脱机的控制台' 'make dashboard      构建内嵌前端资源' 'make build          检查库编译' 'make example        编译示例插件' 'make test           运行离线竞态测试' 'make vet            静态检查' 'make generate       重新生成 protobuf/gRPC 代码' 'make check-secrets  检查 Git 暂存内容'
build:
	$(GO) build ./gateway/... ./sdk/...
example:
	mkdir -p bin
	$(GO) build -trimpath -o bin/we-clay-plugin-example$(EXE) ./examples/plugin
test:
	$(GO) test -race ./...
vet:
	$(GO) vet ./...
generate:
	$(PYTHON) scripts/generate-proto.py
check-secrets:
	$(PYTHON) scripts/check-secrets.py

# 脱机预览只运行显式测试入口，不启动 core 或真实模型。
.PHONY: dashboard mock
dashboard:
	npm --prefix plugins/dashboard/web ci
	npm --prefix plugins/dashboard/web run build
mock:
	WE_CLAY_MOCK_PREVIEW=1 $(GO) test ./gateway -run ^TestMockPreview$$ -count=1 -timeout=0 -v

# 真实验收入口复用 Run；默认 Mac 模式，运行数据与 Mock 完全隔离。
.PHONY: run
run:
	WE_CLAY_REAL_PREVIEW=1 $(GO) test ./gateway -run ^TestRealPreview$$ -count=1 -timeout=0 -v

# 连接已登录的独立 core；重启后台不影响 core 进程。
.PHONY: attach
attach:
	WE_CLAY_ATTACH_CORE=1 WE_CLAY_REAL_PREVIEW=1 $(GO) test ./gateway -run ^TestRealPreview$$ -count=1 -timeout=0 -v
