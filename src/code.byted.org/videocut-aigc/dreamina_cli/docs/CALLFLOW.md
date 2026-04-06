# 主调用链路

本文描述程序从入口启动，到命令执行完成的主流程。

## 启动链路

调用关系从 `src/main.go` 开始：

1. `main()`
2. `envsetup.Apply()`
3. `cmd.ExecuteArgs(os.Args[1:])`
4. `cmd.NewRootCommand()`
5. 根据参数匹配具体子命令
6. 执行对应命令的 `RunE`

## 根命令挂载

根命令 `dreamina` 在 `src/cmd/root.go` 中统一挂载：

- 帮助与版本命令
- 登录命令
- 账号命令
- 查询与任务命令
- 所有生成类命令

## 公共命令执行过程

大多数命令都会经过以下几个阶段：

1. 解析 CLI 参数。
2. 通过 `app.NewContext()` 构造运行上下文。
3. 视情况校验登录状态。
4. 调用对应 service 或 client。
5. 输出 JSON 结果。

## 典型分支

登录类：

- 进入浏览器或导入凭证
- 更新 `~/.dreamina_cli/credential.json`

生成类：

- 校验输入
- 必要时上传本地资源
- 调用 MCP 生成接口
- 写入本地任务库
- 视 `--poll` 决定是否继续轮询

查询类：

- 读取本地任务或远端任务状态
- 必要时下载图片或视频结果

## 为什么需要保留路径层级

代码和文档里都保留了 `code.byted.org/videocut-aigc/dreamina_cli` 的模块与路径语义。把 Git 根提升到 `/opt/tiger`，但保留项目层级，可以同时满足：

- 原始模块路径不变
- 工程化仓库根存在统一 README、AGENTS 和 Git 元数据
