# Gochen Shared - 质量检查清单

**用途**: 在提交代码前运行此检查清单，确保代码质量  
**更新日期**: 2025年

---

## 🚀 快速检查（5分钟）

```bash
# 1. 格式检查
make fmt

# 2. 静态检查
make vet

# 3. 快速测试
go test -short ./...

# 4. 构建验证
go build ./...
```

**通过标准**: 所有命令返回 0，无错误输出

---

## 📋 提交前完整检查（15分钟）

### Step 1: 代码格式化 ✅

```bash
# 运行 gofmt
make fmt

# 验证无格式问题
if [ -n "$(gofmt -l .)" ]; then
    echo "❌ 代码格式不符合规范"
    gofmt -l .
    exit 1
fi
echo "✅ 代码格式正确"
```

**期望结果**: 无输出

---

### Step 2: 静态分析 ✅

```bash
# go vet
make vet

# golangci-lint (如已安装)
if command -v golangci-lint &> /dev/null; then
    make lint
else
    echo "⚠️  golangci-lint 未安装，运行: make install-tools"
fi
```

**期望结果**: 无警告或错误

**常见问题修复**:
- `errcheck` 警告: 检查是否遗漏错误处理
- `unused` 警告: 删除未使用的代码
- `errorlint` 警告: 使用 `%w` 而非 `%v`

---

### Step 3: 单元测试 ✅

```bash
# 运行所有测试
go test ./... -v

# 检查覆盖率
go test ./... -cover

# 竞态检测
go test -race ./...
```

**期望结果**: 
- ✅ 所有测试通过
- ✅ 核心包覆盖率 > 60%
- ✅ 无竞态条件

**如果测试失败**:
1. 查看错误信息
2. 检查最近的更改
3. 运行单个包测试: `go test ./path/to/package -v`

---

### Step 4: 构建验证 ✅

```bash
# 验证所有包可编译
go build ./...

# 验证 go.mod
go mod tidy
git diff go.mod go.sum
```

**期望结果**: 
- ✅ 编译成功
- ✅ go.mod/go.sum 无变化

---

### Step 5: 文档检查 📚

```bash
# 检查公共 API 是否有文档
go doc -all ./domain/entity | grep "MISSING"
go doc -all ./domain/repository | grep "MISSING"
```

**期望结果**: 所有导出的类型和函数有注释

**文档规范**:
- 包注释放在 package 声明前
- 导出类型和函数必须有注释
- 注释以类型/函数名开头

---

## 🔍 代码审查清单

### 通用检查 ✅

- [ ] 代码格式化（gofmt）
- [ ] 无 go vet 警告
- [ ] 无 golangci-lint 错误
- [ ] 所有测试通过
- [ ] 无竞态条件

### 错误处理 ⚠️

- [ ] 所有错误都被检查（无 `_ = err`，除非注释说明）
- [ ] 使用 `fmt.Errorf("%w")` 包装错误
- [ ] 库代码不使用 `panic`（除非文档明确说明）
- [ ] 错误消息清晰且可操作

### 并发安全 🔒

- [ ] 共享状态使用 mutex 保护
- [ ] 正确使用 `defer mu.Unlock()`
- [ ] channel 关闭前检查是否已关闭
- [ ] goroutine 有明确的退出机制

### Context 使用 📡

- [ ] 所有 I/O 操作传递 context
- [ ] context 作为第一个参数
- [ ] 不在结构体中存储 context
- [ ] context.WithTimeout 用于外部调用

### 测试质量 🧪

- [ ] 关键路径有测试覆盖
- [ ] 测试名称清晰（TestXxx_WhenYyy_ThenZzz）
- [ ] 使用 table-driven tests
- [ ] 错误场景有测试

### 文档 📖

- [ ] 公共 API 有文档注释
- [ ] 复杂逻辑有行内注释
- [ ] README 更新（如有 API 变更）
- [ ] 示例代码可运行

---

## 🎯 特定场景检查

### 添加新接口

- [ ] 接口名使用 `I` 前缀（IRepository）
- [ ] 方法数量 < 5（遵循 ISP）
- [ ] 有接口契约测试
- [ ] 文档说明使用场景

### 添加新错误类型

- [ ] 定义错误码常量
- [ ] 提供错误创建函数
- [ ] 实现 `Unwrap()` 方法
- [ ] 添加 `IsXxxError()` 辅助函数

### 修改公共 API

- [ ] 检查是否破坏兼容性
- [ ] 更新 STABILITY.md
- [ ] 更新 CHANGELOG
- [ ] 提供迁移指南（如有破坏性变更）

---

## 🐛 常见问题排查

### 问题 1: golangci-lint 报告大量错误

**原因**: 首次运行，历史代码有问题

**解决方案**:
```bash
# 仅检查新修改的文件
golangci-lint run --new-from-rev=HEAD~1

# 或临时禁用某些检查
golangci-lint run --disable=errcheck,unused
```

---

### 问题 2: 测试随机失败

**原因**: 可能存在并发竞态或依赖外部状态

**排查步骤**:
```bash
# 1. 运行竞态检测
go test -race ./path/to/package

# 2. 多次运行测试
go test -count=10 ./path/to/package

# 3. 检查测试隔离性
# 确保测试间无共享状态
```

---

### 问题 3: 覆盖率报告不准确

**原因**: 测试文件或 mock 代码被计入

**解决方案**:
```bash
# 排除测试文件
go test -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -func=coverage.out | grep -v "_test.go" | grep -v "mock"
```

---

## 📊 质量指标

### 当前基线

| 指标 | 目标 | 当前 | 状态 |
|------|------|------|------|
| gofmt 合规率 | 100% | 100% | ✅ |
| go vet 通过 | 0 警告 | 0 警告 | ✅ |
| 测试覆盖率 | > 80% | ~50% | ⚠️ |
| golangci-lint | 0 错误 | 未运行 | ⚠️ |

### 改进目标（3个月）

| 指标 | 3个月目标 | 行动项 |
|------|----------|--------|
| 测试覆盖率 | 80% | Phase 2: 补充测试 |
| Lint 通过率 | 100% | Phase 1: 集成 lint |
| 文档完整性 | 100% | Phase 3: 补充文档 |

---

## 🔄 自动化检查

### Pre-commit Hook

创建 `.git/hooks/pre-commit`:

```bash
#!/bin/bash
set -e

echo "🔍 运行 pre-commit 检查..."

# 格式检查
if [ -n "$(gofmt -l .)" ]; then
    echo "❌ 代码格式不符合规范，运行: make fmt"
    exit 1
fi

# 快速测试
go test -short ./...

echo "✅ Pre-commit 检查通过"
```

```bash
chmod +x .git/hooks/pre-commit
```

---

### CI/CD 集成

GitHub Actions 已配置（`.github/workflows/ci.yml`）:

- ✅ Lint 检查
- ✅ 单元测试
- ✅ 覆盖率报告
- ✅ 构建验证
- ✅ 格式检查

**查看 CI 状态**: 
```bash
# 推送代码后，在 GitHub Actions 页面查看
# 或在 README 中添加徽章
```

---

## 📚 参考资源

### Go 官方指南

- [Effective Go](https://go.dev/doc/effective_go)
- [Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Go Proverbs](https://go-proverbs.github.io/)

### 项目文档

- [AUDIT_REPORT.md](./AUDIT_REPORT.md) - 详细审核报告
- [ACTION_PLAN.md](./ACTION_PLAN.md) - 改进行动计划
- [NAMING.md](./NAMING.md) - 命名规范
- [STABILITY.md](./STABILITY.md) - API 稳定性承诺

### 工具

- [golangci-lint](https://golangci-lint.run/)
- [staticcheck](https://staticcheck.io/)
- [go-critic](https://go-critic.com/)

---

**最后更新**: 2025年  
**维护者**: Gochen Core Team  
**版本**: 1.0
