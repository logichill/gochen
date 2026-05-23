# errors

gochen 的统一错误模型：错误码 + `*AppError` + 边界层 `Normalize` + HTTP 映射。

`gochen/errors` 是 Go 标准库 `errors` 的超集：`New`、`Is`、`As`、`AsType`、`Join`、`Unwrap`、`ErrUnsupported` 保持标准库语义，框架能力只做叠加。

- 标准库兼容 API 跟随当前 `go.mod` 的 Go 版本补齐，避免替换标准库后缺符号。
- gochen 扩展能力统一围绕 `ErrorCode`、`AppError`、`Normalize`、HTTP 映射与结构化上下文展开。
- `errors.ErrUnsupported` 会归一为 `errors.Unsupported`；它适合表达“调用方请求了当前不支持的能力”，不应用来掩盖服务端装配缺失或内部故障。

[docs/architecture/framework-design.md 第 8 章](../docs/architecture/framework-design.md#8-错误与日志)。本文档聚焦于 `errors` 包本身怎么用。

## 1. 快速使用

### 1.1 创建与包装

`NewCodeWithCause` 用于已经明确存在的 cause；`Wrap` 用于错误早返回，`cause == nil` 时直接返回 `nil`。

```go
// 创建（无 cause）
err := errors.NewCode(errors.NotFound, "user not found").
    WithContext("user_id", userID)

// 创建（带 cause）
err := errors.NewCodeWithCause(errors.Database, "query user failed", dbErr).
    WithContext("user_id", userID)

// 包装（nil-safe：cause 为 nil 时返回 nil）
if err := repo.Save(ctx, u); err != nil {
    return errors.Wrap(err, errors.Database, "save user failed").
        WithContext("user_id", userID)
}
```

### 1.2 判断与提取

```go
// 判断错误码
if errors.Is(err, errors.NotFound) {
    // ...
}

// 提取错误码（未知错误默认归为 Internal；nil 返回 ""）
code := errors.Code(err)

// 提取结构化上下文
var appErr *errors.AppError
if errors.As(err, &appErr) && appErr != nil {
    details := appErr.Details()
    _ = details["user_id"]
}

// 标准库兼容：泛型提取
if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
    _ = pathErr.Path
}
```

## 2. 边界层规范化

第三方库错误或业务领域错误想纳入错误码体系，只需实现 `IErrorCoder`：

```go
type IErrorCoder interface {
    ErrorCode() errors.ErrorCode
}
```

在 HTTP / RPC / MQ 等边界层调用 `errors.Normalize(err)`：

1. `nil` → `nil`
2. 错误链上已包含 `*AppError` → 原样返回（视为已规范化）
3. 实现 `IErrorCoder` → 包装为 `*AppError`（保留原始错误为 cause）
4. `errors.ErrUnsupported` → 包装为 `errors.Unsupported`
5. 未识别错误 → 原样返回（由调用方决定是否再 `Wrap`）

内部层尽量保留原始错误语义，避免过早包装——`Normalize` 只应在跨进程边界使用。

## 3. HTTP 映射

错误码到 HTTP 状态码的映射以 `ToHTTPStatus` / `ErrorCodeToHTTPStatus` 为准。`api/rest.DefaultErrorHandler` 会先 `Normalize`，再按 `ErrorCode` 映射状态码；5xx 响应会使用安全 message，避免泄露内部细节。

## 4. 调用栈（仅 5xx）

为避免可预期的业务错误（4xx）产生大量日志噪音，`errors.NewCode` / `NewCodeWithCause` / `Wrap` 仅在 `ErrorCodeToHTTPStatus(code) >= 500` 时捕获调用栈并写入 `details["stack"]`。

- 4xx（如 `InvalidInput`、`NotFound`、`Conflict`、`Unsupported`）不会携带 stack。
- stack 仅用于日志，不会回传到 HTTP 响应；`logging.StdLogger` 会从 `logging.Error(err)` 自动提取。

## 5. 常见陷阱

**Details / Context 只放不可变值**。`WithContext` / `WithDetails` 建议只放 string / number / bool / time / `[]string` 这类可序列化、不可变的简单值；避免放 `map`、`slice`、指针对象，防止错误对象的 details 随时间变化。
