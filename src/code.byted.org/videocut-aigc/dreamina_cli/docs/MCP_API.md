# MCP 与相关接口整理

项目中的远端请求大致分为三类：

- MCP 生成与历史查询接口
- commerce 账户权益接口
- resource 资源上传接口

## MCP 类接口

常见查询参数：

- `aid=513695`
- `from=dreamina_cli`
- `cli_version=<当前版本>`

常见请求头：

- `Accept: application/json`
- `Content-Type: application/json`
- `Cookie: <登录态>`
- `Appid: 513695`
- `Pf: 7`
- `X-Tt-Logid: <动态生成>`

常见路径：

- 图片生成：`/dreamina/cli/v1/image_generate`
- 视频生成：`/dreamina/cli/v1/video_generate`
- 历史查询：`/mweb/v1/get_history_by_ids`

## commerce 接口

当前主要看到的账号接口是：

- `POST /commerce/v1/benefits/user_credit`

它除了 cookie 外，还依赖若干签名和客户端头，具体对齐方式已经体现在 smoke 脚本里。

## resource 上传接口

本地文件不会直接上传到生成接口，而是先走资源上传。

资源上传负责：

- 判断文件类型
- 上传图片、视频、音频
- 产出可供生成接口使用的 `resource_id`

相关实现：

- `src/components/client/dreamina/resource/client.go`

## 参考文档

- `命令请求流程与Curl示例.md`
- `GEN_FLOW.md`
