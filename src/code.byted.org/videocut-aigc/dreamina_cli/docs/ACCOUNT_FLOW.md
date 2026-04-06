# 账号与凭证流程

本文聚焦账号相关命令，以及本地凭证如何落地和被后续命令复用。

## 本地持久化

默认配置目录位于：

- `~/.dreamina_cli/`

账号凭证文件：

- `~/.dreamina_cli/credential.json`

核心字段：

- `auth_token`
- `auto_token_md5_sign`
- `random_secret_key`
- `sign_key_pair_name`

这些字段由 `src/components/login/store.go` 中的 `Credential` 定义。

## 相关命令

- `login`：启动浏览器登录流程，生成本地随机密钥并等待回调写入会话
- `relogin`：清空旧状态后重新进入登录流程
- `import_login_response`：把外部复制的登录响应导入本地凭证
- `validate-auth-token`：读取本地凭证并输出整理后的会话信息
- `user_credit`：携带登录态请求账户权益额度
- `logout`：清理本地登录信息

## 导入登录响应的关键行为

`ImportLoginResponseJSON` 的核心约束可以概括为：

1. 输入必须是合法 JSON。
2. 必须先存在本地 `random_secret_key`。
3. 只把登录响应里的核心认证字段合并回本地凭证。
4. 保存成功后把登录状态标记为已完成。

常见失败原因：

- 登录响应为空
- `auth_token` 缺失
- 本地没有 `random_secret_key`

## 登录状态管理

`Manager` 在内存中维护登录过程状态：

- `ResetLoginState`：清理失败信息并置为未完成
- `LastLoginFailure`：读取最近一次失败
- `markLoginCompleted`：登录成功后清空失败并置完成
- `LoginCompleted`：读取当前完成标记

这部分状态只影响进程内判断，真正跨进程复用的是 `credential.json`。

## 授权地址生成

`AuthorizationURL` 会做三件事：

1. 确保本地存在随机密钥。
2. 生成回调地址 `http://127.0.0.1:<port>/dreamina/callback/save_session`。
3. 拼接登录页 URL，并附带 `callback` 与 `random_secret_key` 参数。

因此登录流程不是单纯打开一个固定网页，而是带着本地回调地址进入浏览器认证。
