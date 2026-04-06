# 登录流程

本文描述 Dreamina CLI 当前的登录、回调与会话整理方式。

## 组件划分

登录相关代码集中在：

- `src/components/login/auth.go`
- `src/components/login/browser.go`
- `src/components/login/callback.go`
- `src/components/login/login.go`
- `src/components/login/presenter.go`
- `src/components/login/service.go`
- `src/components/login/store.go`

## 标准登录流程

1. 执行 `dreamina login`。
2. 本地生成或复用 `random_secret_key`。
3. 启动本地回调监听。
4. 打开浏览器进入登录页。
5. 登录成功后回调到本地地址。
6. 回调内容写入 `credential.json`。
7. CLI 重新读取凭证，标记登录完成。

## 重新登录

`relogin` 的差异只是先清理当前状态，再重新走标准登录流程。

## 导入式登录

`import_login_response` 适用于不能直接完成浏览器回调的场景：

1. 先通过 `login` 准备本地随机密钥。
2. 从外部获取登录响应 JSON。
3. 执行 `import_login_response` 导入。
4. CLI 将认证字段写回本地凭证。

## 会话输出

`validate-auth-token` 会读取本地凭证，并输出整理后的：

- `cookie`
- 转发头
- `uid`

这部分输出也是 smoke 与 curl 对齐脚本复用登录态的重要来源。
