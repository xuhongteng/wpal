# Wpal Makefile
#
# 常用目标：
#   make build      编译当前平台的二进制到 bin/
#   make run        本地跑起来（默认 127.0.0.1:8891）
#   make test       跑单元测试
#   make verify     跑全部验收（A1/A2/A7）
#   make release    用 goreleaser 出全平台包
#   make cross      交叉编译全部目标平台

SHELL       := /bin/bash
BIN         := bin/wpal
PKG         := ./cmd/wpal
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X mon/internal/version.Version=$(VERSION) \
	-X mon/internal/version.Commit=$(COMMIT) \
	-X mon/internal/version.Date=$(DATE)

# Go 环境：/tmp 是内存盘（1.9G），缓存一律放 $HOME 下
export GOROOT  ?= $(HOME)/sdk/go
export GOPATH  ?= $(HOME)/go
export GOMODCACHE ?= $(GOPATH)/pkg/mod
export GOCACHE ?= $(GOPATH)/.cache/go-build
export GOTMPDIR ?= $(GOPATH)/tmp
export GOPROXY  ?= https://goproxy.cn,direct
export PATH := $(GOROOT)/bin:$(PATH)
export CGO_ENABLED = 0

.PHONY: all build run test vet lint fmt tidy verify verify-a1 verify-a2 verify-a7 \
        cross release clean docker help

all: build

## build: 编译当前平台二进制
build:
	@mkdir -p bin $(GOTMPDIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)
	@ls -lh $(BIN)

## run: 本地启动服务
run: build
	$(BIN) --data-dir ./data

## test: 单元测试
# race detector 需要 cgo，这里单独开启（产物本身仍是 CGO_ENABLED=0 的静态二进制）
test:
	CGO_ENABLED=1 go test -race ./...

## vet: go vet
vet:
	go vet ./...

## lint: golangci-lint（如未安装则跳过）
lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || \
		echo "golangci-lint 未安装，跳过（CI 中会安装）"

## fmt: 格式化
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

## tidy: 整理依赖
tidy:
	go mod tidy

## cross: 交叉编译全部目标平台
cross:
	@mkdir -p dist
	@for t in linux/amd64 linux/arm64 linux/386 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "  → $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/wpal_$${os}_$${arch}$$ext $(PKG) || exit 1; \
	done
	@ls -lh dist/

## verify: 跑全部验收
verify: verify-a1 verify-a2 verify-a7

## verify-a1: 字段一致性（Go vs Python 参考实现）
verify-a1:
	python3 tools/compare.py --go $(BIN) --samples 16

## verify-a2: 前端零回归（同一前端连两个后端，截图比对）
verify-a2:
	bash tools/a2/run.sh
	python3 tools/compare_shots.py --a .a2out/go.png --b .a2out/py.png

## verify-a7: i18n 完整度
verify-a7:
	python3 tools/verify_i18n.py --url http://127.0.0.1:8892

## release: goreleaser 出全平台包
release:
	goreleaser release --clean

## docker: 构建容器镜像
docker:
	docker build -t wpal:latest .

## clean: 清理构建产物
clean:
	rm -rf bin dist .a2out

## help: 显示帮助
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F': ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
