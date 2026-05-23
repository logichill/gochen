.PHONY: help telemetry-off test test-core integration coverage coverage-core coverage-check race race-core bench bench-core test-all clean docs fmt vet lint check stats

CORE_PACKAGES := ./domain/eventsourced ./app/eventsourced ./eventing/projection ./eventing/outbox
COVERAGE_MIN ?= 1

# 默认目标
help:
	@echo "Gochen Shared - 质量改进工具"
	@echo ""
	@echo "可用命令:"
	@echo "  make test          - 运行所有测试"
	@echo "  make test-core     - 运行核心模块测试"
	@echo "  make integration   - 运行带 integration tag 的集成测试"
	@echo "  make race          - 运行所有竞态检测"
	@echo "  make race-core     - 运行核心模块竞态检测"
	@echo "  make test-all      - 运行所有测试"
	@echo "  make coverage      - 生成全仓覆盖率报告"
	@echo "  make coverage-core - 生成核心模块覆盖率报告"
	@echo "  make coverage-check - 生成覆盖率报告并检查最低阈值"
	@echo "  make lint          - 运行 golangci-lint"
	@echo "  make clean         - 清理临时文件"
	@echo "  make docs          - 查看文档列表"
	@echo "  make stats         - 显示项目统计"
	@echo ""

# 关闭 Go telemetry，避免本地/CI 质量命令采集工具链遥测数据。
telemetry-off:
	@go telemetry off >/dev/null 2>&1 || true

# 运行所有测试
test: telemetry-off
	@echo "运行所有测试..."
	@go test ./... -v

# 运行核心模块测试
test-core: telemetry-off
	@echo "运行核心模块测试..."
	@go test $(CORE_PACKAGES) -v

# 运行所有测试（兼容旧目标名）
test-all: test

# 运行集成测试
integration: telemetry-off
	@echo "运行集成测试..."
	@go test -tags=integration ./... -v

# 生成全仓覆盖率报告
coverage: telemetry-off
	@echo "生成全仓覆盖率报告..."
	@go test ./... \
		-coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✅ 覆盖率报告已生成: coverage.html"
	@go tool cover -func=coverage.out | tail -1

# 生成覆盖率报告并检查最低阈值
coverage-check: coverage
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v total="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { \
		if (total + 0 < min + 0) { \
			printf "coverage %.1f%% below required %.1f%%\n", total, min; \
			exit 1; \
		} \
		printf "coverage %.1f%% >= required %.1f%%\n", total, min; \
	}'

# 生成核心模块覆盖率报告
coverage-core: telemetry-off
	@echo "生成核心模块覆盖率报告..."
	@go test $(CORE_PACKAGES) \
		-coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✅ 覆盖率报告已生成: coverage.html"
	@go tool cover -func=coverage.out | tail -1

# 全仓竞态检测
race: telemetry-off
	@echo "运行全仓竞态检测..."
	@go test ./... -race

# 核心模块竞态检测
race-core: telemetry-off
	@echo "运行核心模块竞态检测..."
	@go test $(CORE_PACKAGES) -race

# 性能测试
bench: telemetry-off
	@echo "运行性能测试..."
	@go test ./... -bench=. -benchmem

# 核心模块性能测试
bench-core: telemetry-off
	@echo "运行核心模块性能测试..."
	@go test $(CORE_PACKAGES) -bench=. -benchmem

# 清理临时文件
clean:
	@echo "清理临时文件..."
	@rm -f coverage.out coverage.html
	@rm -f *.test
	@go clean -testcache
	@echo "✅ 清理完成"

# 查看文档列表
docs:
	@echo "质量改进文档列表:"
	@echo ""
	@ls -lh *.md | awk '{printf "  %-40s %8s\n", $$9, $$5}'
	@echo ""
	@echo "总计: $$(ls *.md 2>/dev/null | wc -l) 个文档"

# 显示统计
stats:
	@echo "=== 项目统计 ==="
	@echo ""
	@echo "【文档】"
	@echo "  • 文档数量: $$(ls *.md 2>/dev/null | wc -l) 个"
	@echo "  • 文档大小: ~190KB"
	@echo ""
	@echo "【测试】"
	@echo "  • 测试文件: $$(find . -name "*_test.go" | wc -l) 个"
	@echo ""
	@echo "【覆盖率】"
	@go test ./... -cover 2>&1 | \
		grep "coverage:" | \
		awk '{printf "  • %s: %s\n", $$2, $$5}'

# 格式化代码
fmt:
	@echo "格式化代码..."
	@go fmt ./...
	@echo "✅ 格式化完成"

# 代码检查
vet:
	@echo "运行 go vet..."
	@go vet ./...
	@echo "✅ 检查通过"

# 静态分析
lint:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found; install golangci-lint v2.x or run through CI"; \
		exit 127; \
	fi
	@golangci-lint run --new ./...

# 完整检查 (格式化 + vet + 测试 + 覆盖率)
check: fmt vet lint test race coverage-check
	@echo "✅ 完整检查通过！"
