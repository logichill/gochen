# 工作区验证与架构文档生成

## 完整验证

在 Core 仓库根目录执行：

```sh
bash scripts/validate-workspace.sh
```

入口按九个相邻仓库分别执行 `GOWORK=off` 的 build、vet 和非缓存测试，核对 Core 零依赖，再运行 Runtime 示例、ALife 单元测试脚本（含 race、70% 覆盖率和 benchmark）及 ALife、ERP、EMS 的真实 API 脚本。任一阶段失败，最终退出非零；各阶段日志存放在独立临时目录，也可通过第一个参数指定日志目录。需要 Go、make、Bash、curl、jq、python3、sqlite3，以及模块声明的相邻仓库；API 测试只使用自身临时数据库。

## 架构文档生成

在 Core 仓库根目录执行：

```sh
GOWORK=off go run scripts/dependency_graph.go -out /tmp/gochen-dependencies
```

生成 `DEPENDENCIES.md`（含来源文件与 import 行号）、`dependency-graph.json`（含生产 / 测试 / 示例角色及标准库导入）和 `dependencies.mmd`（内部生产包依赖）。输出目录中同名文件会被替换。

工具使用独立模块模式的 `go list` 确定当前构建环境启用的文件，再由 Go AST 读取 import；不会把自身或未启用的构建标签文件算作生产依赖。更换 GOOS、GOARCH 或构建标签后应重新生成。它用于文档与评审，不承担架构门禁。

调用链由人工审查维护：静态 import 无法自动推导接口运行时装配关系。
