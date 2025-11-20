# Gochen Shared - 质量改进行动计划

**基于**: [AUDIT_REPORT.md](./AUDIT_REPORT.md) 审核报告  
**制定日期**: 2025年  
**目标**: 在3个月内将代码质量从 4/5 提升到 5/5

---

## 🎯 总体目标

- ✅ 修复所有 P0 和 P1 问题
- ✅ 集成工业级静态分析工具链
- ✅ 将测试覆盖率提升到 80%+
- ✅ 建立 CI/CD 自动化流程
- ✅ 完善文档和 API 稳定性保证

---

## Phase 1: 立即修复 (Week 1-2) 🚨

### P0 - 关键问题修复

#### 任务 1.1: 移除库代码中的 panic ⚠️

**问题文件**:
- `di/container.go:156`
- `eventing/registry/registry.go:78`
- `storage/database/sql/insert.go:89`
- `storage/database/sql/update.go`

**执行步骤**:
```bash
# 1. 查找所有 panic 使用
grep -rn "panic(" --include="*.go" --exclude-dir="examples" .

# 2. 逐个文件修改 (示例)
# di/container.go
# 修改前:
func (c *Container) Get(name string) interface{} {
    if !c.Has(name) {
        panic(fmt.Sprintf("service not found: %s", name))
    }
    return c.services[name]
}

# 修改后:
func (c *Container) Get(name string) (any, error) {
    if !c.Has(name) {
        return nil, fmt.Errorf("service not found: %s", name)
    }
    return c.services[name], nil
}

# 3. 更新所有调用方
# 4. 运行测试验证
go test ./di -v
```

**验收标准**:
- ✅ 所有库代码中的 panic 已替换为 error 返回
- ✅ 相关测试通过
- ✅ 文档已更新

**负责人**: 核心开发团队  
**截止日期**: Week 1

---

#### 任务 1.2: 修复错误包装 (%v → %w) 🔧

**问题文件**:
- `bridge/http_bridge.go:123`
- `bridge/serializer.go`
- `di/container.go:87`
- `eventing/projection/checkpoint_sql.go`
- `eventing/store/helpers.go:45`
- `saga/orchestrator.go`

**执行步骤**:
```bash
# 使用自动化脚本
./scripts/fix_error_wrapping.sh

# 手动验证关键修改
git diff

# 运行测试
go test ./...

# 验证错误链
go test -v ./bridge -run TestErrorWrapping
```

**验收标准**:
- ✅ 所有 `fmt.Errorf("%v")` 已替换为 `fmt.Errorf("%w")`
- ✅ 错误链测试通过
- ✅ 所有包测试通过

**负责人**: 任何开发者  
**截止日期**: Week 1  
**工时估算**: 2 小时

---

#### 任务 1.3: 集成 golangci-lint 🛠️

**执行步骤**:
```bash
# 1. 安装工具
make install-tools

# 或手动安装
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# 2. 运行初次检查
golangci-lint run ./...

# 3. 修复关键问题
# (根据输出逐项修复)

# 4. 更新 .golangci.yml 配置
# (已提供配置文件)

# 5. 集成到 Makefile
make lint
```

**验收标准**:
- ✅ golangci-lint 安装完成
- ✅ 配置文件 `.golangci.yml` 已添加
- ✅ 所有 P0/P1 linter 错误已修复
- ✅ `make lint` 执行通过

**负责人**: DevOps/CI 团队  
**截止日期**: Week 1  
**工时估算**: 4 小时

---

### P1 - 高优先级问题

#### 任务 1.4: 建立 CI/CD 流程 🔄

**执行步骤**:
```bash
# 1. 添加 GitHub Actions 配置
# (已提供 .github/workflows/ci.yml)

# 2. 启用 Actions
# - 在 GitHub 仓库设置中启用 Actions
# - 推送代码触发首次构建

# 3. 配置徽章
# - 在 README.md 添加 CI 状态徽章
# - 添加覆盖率徽章 (Codecov)

# 4. 配置保护规则
# - 要求 CI 通过才能合并
# - 要求代码审查
```

**验收标准**:
- ✅ CI 流程已运行并通过
- ✅ 测试覆盖率报告已上传
- ✅ 分支保护规则已配置

**负责人**: DevOps 团队  
**截止日期**: Week 2  
**工时估算**: 3 小时

---

#### 任务 1.5: 添加核心包测试 ✅

**目标包**:
- `domain/repository` - 仓储接口契约测试
- `app/application` - CRUD 和批量操作测试

**执行步骤**:
```bash
# 1. 创建测试文件
touch domain/repository/contract_test.go
touch app/application_test.go

# 2. 编写接口契约测试
# (参考 AUDIT_REPORT.md 中的示例)

# 3. 运行测试
go test ./domain/repository -v
go test ./app -v

# 4. 查看覆盖率
go test -cover ./domain/repository ./app
```

**测试模板**:
```go
// domain/repository/contract_test.go
package repository_test

import (
    "context"
    "testing"
    "gochen/domain/entity"
    "gochen/domain/repository"
)

// TestIRepositoryContract 接口契约测试
func TestIRepositoryContract(t *testing.T) {
    // 使用 mock 实现测试所有接口方法
    // ...
}
```

**验收标准**:
- ✅ 测试文件已创建
- ✅ 接口契约测试通过
- ✅ 代码覆盖率 > 60%

**负责人**: 后端开发团队  
**截止日期**: Week 2  
**工时估算**: 8 小时

---

## Phase 2: 短期改进 (Week 3-6) 📈

### 任务 2.1: 提升测试覆盖率

**目标**: 核心包覆盖率达到 80%

**优先级包**:
1. `eventing/upgrader` - 事件升级器
2. `saga` - Saga 编排器
3. `bridge` - 远程桥接
4. `httpx/basic` - HTTP 适配器

**执行步骤**:
```bash
# 1. 查看当前覆盖率
go test -cover ./...

# 2. 识别未覆盖代码
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 3. 补充测试
# (针对每个包)

# 4. 验证提升
go test -cover ./... | grep -E "coverage: [0-9]+%"
```

**验收标准**:
- ✅ 核心包覆盖率 > 80%
- ✅ 关键路径（快乐路径 + 错误路径）全覆盖
- ✅ 边界条件测试完整

**负责人**: 全体开发人员  
**截止日期**: Week 6  
**工时估算**: 20 小时

---

### 任务 2.2: 迁移 interface{} 到 any

**影响范围**: 88 处

**执行步骤**:
```bash
# 1. 使用自动化脚本
./scripts/migrate_interface_to_any.sh

# 2. 验证语法
gofmt -w .
go build ./...

# 3. 运行测试
go test ./...

# 4. 提交更改
git add .
git commit -m "refactor: migrate interface{} to any (Go 1.18+)"
```

**验收标准**:
- ✅ 所有 `interface{}` 已替换为 `any`
- ✅ 代码格式正确
- ✅ 所有测试通过

**负责人**: 任何开发者  
**截止日期**: Week 3  
**工时估算**: 1 小时

---

### 任务 2.3: 改进错误消息

**目标**: 移除中文硬编码，使用英文 + 错误码

**执行步骤**:
```bash
# 1. 查找中文错误消息
grep -rn "fmt.Errorf.*[\u4e00-\u9fa5]" --include="*.go" .

# 2. 定义错误码常量
# errors/codes.go (新建)
var (
    ErrInvalidAggregateID = NewError(
        "INVALID_AGGREGATE_ID",
        "aggregate ID must be positive",
    )
)

# 3. 替换错误消息
# eventing/event.go
// 修改前:
return fmt.Errorf("聚合ID必须大于0")

// 修改后:
return ErrInvalidAggregateID.WithDetails(map[string]any{
    "aggregate_id": e.AggregateID,
})

# 4. 更新测试
# (验证新的错误码)
```

**验收标准**:
- ✅ 核心错误消息已英文化
- ✅ 错误码文档已更新
- ✅ 测试已适配

**负责人**: 国际化团队  
**截止日期**: Week 5  
**工时估算**: 6 小时

---

### 任务 2.4: 添加性能基准测试

**目标包**:
- `eventing/store` - 事件存储性能
- `cache` - 缓存性能
- `messaging` - 消息总线吞吐量

**执行步骤**:
```bash
# 1. 添加 benchmark 测试
# eventing/store/benchmark_test.go (新建)
func BenchmarkSQLEventStore_AppendEvents(b *testing.B) {
    // ...
}

# 2. 运行 benchmark
go test -bench=. -benchmem ./eventing/store

# 3. 保存基线
go test -bench=. -benchmem ./eventing/store > benchmark.txt

# 4. 持续监控
# (集成到 CI)
```

**验收标准**:
- ✅ 关键路径有 benchmark 测试
- ✅ 性能基线已记录
- ✅ CI 中集成性能回归检查

**负责人**: 性能团队  
**截止日期**: Week 6  
**工时估算**: 8 小时

---

## Phase 3: 中期优化 (Week 7-12) 🚀

### 任务 3.1: 完善文档

**目标文档**:
1. ✅ **AUDIT_REPORT.md** - 代码质量审核报告 (已完成)
2. ✅ **STABILITY.md** - API 稳定性承诺 (已完成)
3. ⏳ **ARCHITECTURE.md** - 架构决策记录
4. ⏳ **TROUBLESHOOTING.md** - 故障排查手册
5. ⏳ **CONTRIBUTING.md** - 贡献指南
6. ⏳ **SECURITY.md** - 安全政策

**执行步骤**:
```bash
# 1. 编写架构文档
# docs/ARCHITECTURE.md

# 2. 编写故障排查手册
# docs/TROUBLESHOOTING.md

# 3. 编写贡献指南
# CONTRIBUTING.md

# 4. 编写安全政策
# SECURITY.md
```

**验收标准**:
- ✅ 所有文档已编写
- ✅ 文档已审查
- ✅ 文档已发布

**负责人**: 技术写作团队  
**截止日期**: Week 10  
**工时估算**: 16 小时

---

### 任务 3.2: 代码清理

**清理项**:
1. 清理 TODO/FIXME (16处)
2. 移除死代码
3. 优化复杂函数

**执行步骤**:
```bash
# 1. 查找 TODO
grep -rn "TODO\|FIXME\|XXX\|HACK" --include="*.go" .

# 2. 处理每个 TODO
# - 完成实现，或
# - 创建 Issue 追踪，或
# - 移除过期 TODO

# 3. 查找死代码
golangci-lint run --enable deadcode,unused,structcheck

# 4. 移除未使用代码
```

**验收标准**:
- ✅ 所有 TODO 已处理
- ✅ 死代码已移除
- ✅ 复杂函数已重构

**负责人**: 全体开发人员  
**截止日期**: Week 12  
**工时估算**: 10 小时

---

### 任务 3.3: 安全审计

**审计项**:
1. 输入验证
2. SQL 注入防护
3. 并发安全
4. 敏感信息保护

**执行步骤**:
```bash
# 1. 运行安全扫描
golangci-lint run --enable gosec

# 2. 手动代码审查
# (重点检查)

# 3. 修复安全问题
# (根据扫描结果)

# 4. 编写安全测试
```

**验收标准**:
- ✅ 安全扫描通过
- ✅ 关键漏洞已修复
- ✅ 安全文档已更新

**负责人**: 安全团队  
**截止日期**: Week 11  
**工时估算**: 12 小时

---

## 📊 进度跟踪

### Week 1-2 检查点

- [ ] P0 问题全部修复
- [ ] golangci-lint 集成完成
- [ ] CI/CD 流程运行正常
- [ ] 核心包测试覆盖率 > 60%

### Week 3-6 检查点

- [ ] 测试覆盖率达到 80%
- [ ] interface{} 迁移完成
- [ ] 错误消息英文化
- [ ] 性能基准建立

### Week 7-12 检查点

- [ ] 文档完善
- [ ] 代码清理完成
- [ ] 安全审计通过
- [ ] 最终验收

---

## 🎯 验收标准

### 代码质量

- ✅ golangci-lint 零警告
- ✅ go vet 零警告
- ✅ 测试覆盖率 > 80%
- ✅ 所有测试通过 (包括 race 检测)

### 工程实践

- ✅ CI/CD 流程完整
- ✅ 代码审查流程建立
- ✅ 自动化测试完善
- ✅ 文档齐全

### 可维护性

- ✅ API 稳定性承诺明确
- ✅ 架构文档完整
- ✅ 故障排查手册可用
- ✅ 贡献指南清晰

---

## 📝 资源分配

### 人力投入

- 核心开发: 2人 × 12周 = 24 人周
- DevOps: 1人 × 4周 = 4 人周
- 测试: 1人 × 6周 = 6 人周
- 技术写作: 0.5人 × 8周 = 4 人周

**总计**: 约 38 人周 (~9.5 人月)

### 时间轴

```
Week 1-2  |████████░░░░░░░░░░░░░░| Phase 1: 立即修复
Week 3-6  |████████████████░░░░░░| Phase 2: 短期改进
Week 7-12 |████████████████████████| Phase 3: 中期优化
```

---

## 🚦 风险管理

### 高风险项

1. **panic 移除影响面广**
   - 缓解: 充分测试，灰度发布
   - 回滚: 保留旧版本分支

2. **测试覆盖率提升工作量大**
   - 缓解: 优先核心包，分批完成
   - 应对: 调整时间表，增加人力

### 中风险项

1. **CI/CD 集成可能遇到环境问题**
   - 缓解: 提前准备环境，测试流程
   - 应对: 准备备用方案 (GitLab CI)

2. **性能基准可能发现性能问题**
   - 缓解: 提前分析瓶颈
   - 应对: 制定优化计划

---

## 📞 联系方式

**项目负责人**: [填写姓名]  
**技术顾问**: [填写姓名]  
**周会时间**: 每周五 14:00  
**问题反馈**: [GitHub Issues](https://github.com/xxx/gochen/issues)

---

## 📚 相关文档

- [AUDIT_REPORT.md](./AUDIT_REPORT.md) - 详细审核报告
- [STABILITY.md](./STABILITY.md) - API 稳定性承诺
- [README.md](./README.md) - 项目介绍
- [CONTRIBUTING.md](./CONTRIBUTING.md) - 贡献指南 (待编写)

---

**最后更新**: 2025年  
**文档版本**: 1.0  
**下次审查**: Phase 1 完成后
