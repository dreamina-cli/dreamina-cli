# auth_token 解析与 Cookie 流程说明

本文说明当前 `dreamina` 在本地如何把 `credential.json` 中的 `auth_token` 解析成可用会话，以及请求发送时 `Cookie` 头从哪里来。

## 结论

- 请求侧发送的 `Cookie`，不是运行时重新计算出来的。
- 当前实现会先解密 `auth_token`，得到一份 JSON payload。
- 之后直接从 payload 根层的 `cookie` 字段取值，写入 HTTP 请求头 `Cookie`。
- 如果 payload 中只有嵌套结构，没有根层 `cookie` / `headers`，解析阶段会先把这些字段回填到根层，再交给各客户端使用。

## 本地凭证字段

`~/.dreamina_cli/credential.json` 当前使用下面四个字段：

- `auth_token`
- `auto_token_md5_sign`
- `random_secret_key`
- `sign_key_pair_name`

其中：

- `auth_token` 是密文
- `random_secret_key` 是本地解密所需密钥材料
- `auto_token_md5_sign` 与 `sign_key_pair_name` 用于签名校验

## 解析流程

### 1. 读取本地凭证

入口在：

- [store.go](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src/components/login/store.go)

`loadUsableCredential()` 会先检查四个字段是否齐全，然后按顺序做：

1. 校验 `auth_token` 签名
2. 用 `random_secret_key` 解密 `auth_token`
3. 检查解密后的 payload 是否可用

### 2. 解密 auth_token

入口在：

- [auth.go](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src/components/login/auth.go)

当前实现对齐原程序，解密路径是：

1. `base64` 解码 `auth_token`
2. 对 `random_secret_key` 做 `sha256`
3. 取 `sha256(random_secret_key)` 作为 AES key
4. 取 key 的前 `16` 字节作为 AES-CBC 的 IV
5. AES-CBC 解密
6. 做 PKCS7 去 padding
7. 解析为 JSON

也就是说，`auth_token` 本身承载的是一份加密后的 session payload，而不是单独的 cookie 编码。

### 3. 回填根层 cookie 和 headers

仍在：

- [auth.go](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src/components/login/auth.go)

`backfillParsedSessionRootFields()` 会把嵌套结构中的字段回填到根层，重点包括：

- `cookie`
- `headers`
- `request_headers`
- 若干身份字段别名

这样做的目的，是保证后续请求侧统一只读一套稳定键名。

### 4. 请求侧如何使用 cookie

不同客户端都会从解析后的 session 根层读取 `cookie`。

相关位置：

- [auth client](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src/components/client/dreamina/auth/client.go)
- [commerce client](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src/components/client/dreamina/commerce/client.go)
- [mcp client](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src/components/client/dreamina/mcp/client.go)

行为是一致的：

- 读 `root["cookie"]`
- 若不为空，则写入请求头 `Cookie`
- 再结合 `headers` 中的其余可转发头发起请求

所以从代码语义上，实际发出的 `Cookie` 就是解密 payload 中的 cookie。

## 当前观察文件

为了方便观察，我已把当前本地凭证解析后的实际 payload 落盘到：

- [auth_token_session_payload.json](/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/logs/auth_token_session_payload.json)

这个文件包含真实会话数据，里面有敏感信息，不应提交到仓库，也不建议发到聊天渠道。

## 这件事不能推出什么

虽然可以从 `auth_token` 解出 `cookie`，但反过来不能简单地只靠 `cookie` 还原一个可用的 `auth_token`。

原因有三点：

- `auth_token` 对应的是整个 JSON payload，而不是只有 cookie
- 本地加载可用凭证时还要求签名校验通过
- `cookie` 本身不足以恢复完整 payload，更不足以得到一个通过签名校验的新 token

所以当前能确认的是：

- `auth_token -> payload -> cookie` 成立
- `cookie -> 原始 auth_token` 不成立
- `cookie -> 新的可用 auth_token` 也不成立
