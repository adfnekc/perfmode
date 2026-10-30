# perfmode — 构建配置（目标 Windows amd64）

APP     := perfmode
VERSION := 2.0.0
GO      := go
GOOS    := windows
GOARCH  := amd64
LDFLAGS := -s -w

.PHONY: all build vet clean info help

all: build

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o $(APP).exe .

vet:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 $(GO) vet ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -f $(APP).exe $(APP)

info:
	@echo "$(APP) v$(VERSION)  ->  $(GOOS)/$(GOARCH)"

help:
	@echo "用法: make [target]"
	@echo ""
	@echo "  build   编译 Windows amd64 二进制 (默认)"
	@echo "  vet     代码检查"
	@echo "  fmt     格式化代码"
	@echo "  clean   清理构建产物"
	@echo "  info    项目信息"
	@echo "  help    显示此帮助"
