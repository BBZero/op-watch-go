.PHONY: build run clean tidy fmt vet test help

APP_NAME = opwatch
BUILD_DIR = ./bin
CMD_DIR = ./main
GO = go

.DEFAULT_GOAL := help

## build: 编译
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(APP_NAME) $(CMD_DIR)

## run: 编译并前台运行
run: build
	$(BUILD_DIR)/$(APP_NAME)

## tidy: 整理依赖
tidy:
	$(GO) mod tidy

## test
test:
	$(GO) test ./... -v

## fmt: 格式化代码
fmt:
	$(GO) fmt ./...

## vet: 代码检查
vet:
	$(GO) vet ./...

## clean: 清理编译产物
clean:
	rm -rf $(BUILD_DIR)

help:
	@grep -h '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
