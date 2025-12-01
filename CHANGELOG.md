# Changelog

所有值得记录的变更都会出现在这个文件中。

本文件格式遵循 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)，  
并且本项目版本号遵循 [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html)。

> 说明：破坏性变更（Breaking Changes）会在对应版本下单独以 `### Breaking Changes` 小节标注。

## [Unreleased]

### Added
- server 包单元测试

### Changed
- 统一所有错误消息和日志为英文（validation, di, eventing, projection, saga）

### Fixed
- 缓存 `EnableStats` 配置逻辑与 `CleanExpired` 重复删除问题
- `MemoryEventStore` 类型断言安全性

### Improved
- `MessageBus` 并发控制粒度优化

### Docs
- 添加 di 全局容器使用边界说明
- 添加 `Version` 字段语义文档

## [1.0.0] - 2025-11-30

### Added
- 初始稳定版本发布。

