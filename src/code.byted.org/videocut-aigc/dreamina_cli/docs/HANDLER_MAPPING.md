# 命令与处理器映射

本文用“命令 -> 入口函数 -> 主要依赖”的形式快速定位源码。

## 根命令

- `dreamina` -> `cmd.NewRootCommand` -> `src/cmd/root.go`

## 登录与账号

- `login` -> `newLoginCommand` -> `src/cmd/login.go`
- `relogin` -> `newReloginCommand` -> `src/cmd/login.go`
- `import_login_response` -> `newImportLoginResponseCommand` -> `src/cmd/import_login_response.go`
- `validate-auth-token` -> `newValidateAuthCommand` -> `src/cmd/account.go`
- `user_credit` -> `newUserCreditCommand` -> `src/cmd/account.go`
- `logout` -> `newLogoutCommand` -> `src/cmd/account.go`
- `version` -> `newVersionCommand` -> `src/cmd/version.go`

## 任务查询

- `query_result` -> `newQueryResultCommand` -> `src/cmd/tasks.go`
- `list_task` -> `newListTaskCommand` -> `src/cmd/tasks.go`

## 生成类

- `text2image` -> `newText2ImageCommand` -> `src/cmd/generators.go`
- `image2image` -> `newImage2ImageCommand` -> `src/cmd/generators.go`
- `image_upscale` -> `newImageUpscaleCommand` -> `src/cmd/generators.go`
- `text2video` -> `newText2VideoCommand` -> `src/cmd/generators.go`
- `image2video` -> `newImage2VideoCommand` -> `src/cmd/generators.go`
- `frames2video` -> `newFrames2VideoCommand` -> `src/cmd/generators.go`
- `multiframe2video` -> `newMultiFrame2VideoCommandWithUse` -> `src/cmd/generators.go`
- `ref2video` -> `newMultiFrame2VideoCommandWithUse` -> `src/cmd/generators.go`
- `multimodal2video` -> `newMultiModal2VideoCommand` -> `src/cmd/generators.go`

## 主要服务依赖

- 登录能力：`src/components/login/*`
- 生成聚合服务：`src/components/gen/*`
- MCP HTTP 客户端：`src/components/client/dreamina/mcp/client.go`
- 资源上传客户端：`src/components/client/dreamina/resource/client.go`
- 账号权益客户端：`src/components/client/dreamina/commerce/client.go`
- 本地任务存储：`src/components/task/*`
