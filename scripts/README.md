# 架构文档生成

在 Core 仓库根目录执行：

```sh
GOWORK=off go run scripts/dependency_graph.go -out /tmp/gochen-dependencies
```

生成 `DEPENDENCIES.md`（含来源文件与 import 行号）、`dependency-graph.json`（含生产 / 测试 / 示例角色及标准库导入）和 `dependencies.mmd`（内部生产包依赖）。输出目录中同名文件会被替换。

工具使用独立模块模式的 `go list` 确定当前构建环境启用的文件，再由 Go AST 读取 import；不会把自身或未启用的构建标签文件算作生产依赖。更换 GOOS、GOARCH 或构建标签后应重新生成。它用于文档与评审，不承担架构门禁。

调用链由人工审查维护：静态 import 无法自动推导接口运行时装配关系。
