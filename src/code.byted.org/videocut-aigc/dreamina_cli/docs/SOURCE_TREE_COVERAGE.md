# 源码树覆盖情况

当前仓库已经不是单纯的“恢复报告目录”，而是一个可编译、可测试、可继续整理的工程目录。

## 已覆盖的主要模块

- `app/`
- `buildinfo/`
- `cmd/`
- `components/client/dreamina/*`
- `components/gen/`
- `components/login/`
- `components/task/`
- `config/`
- `infra/`
- `server/`
- `util/`

## 当前状态

- 已具备独立 Go 模块结构
- 已包含单元测试
- 已补齐本地任务存储的 SQLite 语义
- 已有 smoke 脚本可辅助验证关键命令

## 仍需注意的边界

- 某些接口仍属于基于现有行为整理的实现，而非完整原始源码
- 远端服务协议仍以当前代码和验证日志为准
- 运行时依赖真实登录态与线上可用接口
