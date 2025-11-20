.PHONY: help test coverage test-all clean docs

# 默认目标
help:
    @echo "Gochen Shared - 质量改进工具"
    @echo ""
    @echo "可用命令:"
    @echo "  make test          - 运行核心模块测试"
    @echo "  make test-all      - 运行所有测试"
    @echo "  make coverage      - 生成覆盖率报告"
    @echo "  make clean         - 清理临时文件"
    @echo "  make docs          - 查看文档列表"
    @echo "  make stats         - 显示项目统计"
    @echo ""

# 运行核心模块测试
test:
    @echo "运行核心模块测试..."
    @go test ./domain/eventsourced ./bridge ./eventing/projection ./eventing/outbox -v

# 运行所有测试
test-all:
    @echo "运行所有测试..."
    @go test ./... -v

# 生成覆盖率报告
coverage:
    @echo "生成覆盖率报告..."
    @go test ./domain/eventsourced ./bridge ./eventing/projection ./eventing/outbox \
        -coverprofile=coverage.out -covermode=atomic
    @go tool cover -html=coverage.out -o coverage.html
    @echo "✅ 覆盖率报告已生成: coverage.html"
    @go tool cover -func=coverage.out | tail -1

# 竞态检测
race:
    @echo "运行竞态检测..."
    @go test ./domain/eventsourced ./bridge ./eventing/projection ./eventing/outbox -race

# 性能测试
bench:
    @echo "运行性能测试..."
    @go test ./domain/eventsourced ./bridge ./eventing/projection -bench=. -benchmem

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
    @go test ./domain/eventsourced ./bridge ./eventing/projection ./eventing/outbox -cover 2>&1 | \
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

# Lint 检查
lint:
    @echo "运行 golangci-lint..."
    @golangci-lint run ./...
    @echo "✅ Lint 检查通过"

# 安装开发工具
install-tools:
    @echo "安装开发工具..."
    @go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    @echo "✅ 工具安装完成"

# 完整检查 (格式化 + vet + lint + 测试 + 覆盖率)
check: fmt vet lint test coverage
    @echo "✅ 完整检查通过！"
