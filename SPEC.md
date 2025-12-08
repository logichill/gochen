# Gochen 项目开发规范 (SPEC)

## 1. 架构设计原则 (Architecture)
1. **接口优先**: 基础能力必须定义接口，接口命名统一为 `I` 前缀（如 `ILogger`, `IDatabase`）。
2. **依赖接口**: 跨包暴露的字段、参数和返回值，必须使用接口类型，禁止直接依赖具体 struct。
3. **依赖注入**: 功能模块只提供实现（如 `SQLEventStore`），不决定使用哪个实现。具体装配由组合根（main/server）负责。
4. **构造函数**: 默认返回具体 struct，由调用方在装配时转为接口变量。仅在需隐藏实现时直接返回接口。

## 2. 命名规范 (Naming)
1. **接口 (Interfaces)**
   - 必须以 `I` 开头（如 `IRepository`）。
   - 方法名必须以 **动词** 开头（如 `GetByID`, `Save`）。
   - 避免泛化命名（如 `IManager`, `IHandler`），应准确描述职责。

2. **结构体 (Structs)**
   - 使用名词，体现本质（如 `User`, `MemoryCache`）。
   - **禁止** `I` 前缀或 `Impl` 后缀。

3. **方法 (Methods)**
   - **CRUD**: `Create`/`Add`, `Get`/`Find`/`List`, `Update`, `Delete`。
   - **布尔值**: 必须使用 `Is`, `Has`, `Can`, `Should` 前缀。
   - **Getter/Setter**: Getter 直接使用字段名（如 `Name()`），Setter 使用 `Set` 前缀（如 `SetName()`）。

4. **变量与常量**
   - 变量名简短有意义（如 `ctx`, `err`, `userID`）。
   - 常量使用 **CamelCase**（如 `MaxRetries`），**禁止**蛇形全大写（`MAX_RETRIES`）。
   - 缩写词保持全大写（如 `ServeHTTP`, `UserID`, `JSONData`）。

5. **包 (Packages)**
   - 使用 **小写单数** 名词（如 `user`, `order`），禁止复数或下划线。

## 3. 文档与注释
1. **GoDoc**: 所有导出类型和方法必须包含注释。
2. **语言**: 统一使用 **简体中文**。
3. **格式**: 第一句为完整摘要，参数/返回值单独说明。

### 3.1 示例代码中的错误处理约定

1. **示例代码中的快速失败**
   - `examples/*` 目录下的示例代码使用 `must(err)` 或 `log.Fatal(err)` 来简化错误处理，让读者专注核心流程：

   ```go
   // 示例代码推荐写法：使用 must 快速失败
   agg, err := repo.GetByID(ctx, id)
   must(err)  // 失败时立即终止程序
   ```

   - `must(err)` 在示例中的作用：
     - 明确表达"这里失败就不应该继续"
     - 避免用 `_` 掩盖错误，让初学者理解每个操作都可能失败
     - 简化代码，让读者专注业务逻辑而非错误处理细节

2. **生产代码应返回错误**
   - 生产代码不应使用 `must(err)` 或 `panic`，而是应该返回错误，让调用方决定如何处理：

   ```go
   // 生产代码推荐写法：返回错误由上层处理
   if err := repo.Save(ctx, aggregate); err != nil {
       return fmt.Errorf("save aggregate %d: %w", aggregate.ID, err)
   }
   ```

   - 生产代码的错误处理策略：
     - 在业务边界统一通过 `errors.Normalize` 转换为 `AppError`
     - 根据错误码决定重试、降级或告警
     - 记录结构化日志（包含 error_code、aggregate_id 等关键字段）

3. **特殊场景的显式标注**
   - 若业务逻辑中存在"预期失败"的场景（如 Saga 补偿、幂等检测），必须用注释明确说明：

   ```go
   // 预期失败场景：Credit 在 false 参数下会故意失败，触发 Saga 补偿逻辑
   if err := orch.Credit(ctx, id, false); err != nil {
       log.Printf("Credit failed as expected (triggering compensation): %v", err)
   }
   ```

   - 避免用裸的 `_` 掩盖错误，即使在示例中也应该让意图清晰可见。
