# Dreamina CLI 命令请求流程与 Curl 示例

说明：

- 本文是完整深度版，保留了更细的请求链路、字段说明和验证记录。
- 如果你只是想快速找命令和 curl 样例，先看 `命令速查与Curl速览.md`。

本文基于当前源码梳理 `dreamina` 各命令的请求链路，目标是回答三件事：

1. 这个命令是否真的会发远端 HTTP 请求。
2. 如果会，请求流程是什么。
3. 如果要脱离 CLI 直接模拟，请怎么写 `curl`。

说明：

- 文档中的 `curl` 以“对齐源码请求形态”为目标，不保证你在外部环境直接可用。
- 涉及登录态的命令都依赖浏览器登录后落地的 `cookie` / 转发头。
- `commerce` 相关接口额外依赖签名头，文档只给出占位示例。
- 生成类命令里，凡是传本地文件的，CLI 实际会先走“上传资源”子流程，再拿到 `resource_id` 调生成接口。
- 默认站点来源于登录域名，当前默认可视作 `https://jimeng.jianying.com`。

---

## 1. 公共约定

### 1.1 基础环境变量

```bash
export BASE_URL="https://jimeng.jianying.com"
export CLI_VERSION="__YOUR_CLI_VERSION__"
export COOKIE="__YOUR_COOKIE__"
export LOGID="__YOUR_LOGID__"
export RESOURCE_ID="__YOUR_RESOURCE_ID__"
export VIDEO_RESOURCE_ID="__YOUR_VIDEO_RESOURCE_ID__"
export AUDIO_RESOURCE_ID="__YOUR_AUDIO_RESOURCE_ID__"
export MULTIMODAL_SUBMIT_ID="__YOUR_MULTIMODAL_SUBMIT_ID__"
export SUBMIT_ID="__YOUR_SUBMIT_ID__"
```

### 1.2 MCP 类接口公共 Query

生成类接口、历史查询接口，都会自动附带：

```text
aid=513695
from=dreamina_cli
cli_version=${CLI_VERSION}
```

### 1.3 MCP 类接口公共 Header

大多数生成/查询请求都会带：

```http
Accept: application/json
Appid: 513695
Pf: 7
Cookie: ${COOKIE}
Content-Type: application/json
X-Tt-Logid: ${LOGID}
X-Use-Ppe: 1
```

说明：

- `Cookie` 由登录态提供。
- `X-Tt-Logid` 是 CLI 每次请求动态生成的。
- 部分会话透传头也会从本地登录态继续转发。

### 1.4 命令总览矩阵

| 命令 | 是否发远端请求 | 主要远端接口 |
| --- | --- | --- |
| `help` | 否 | 无 |
| `version` | 否 | 无 |
| `logout` | 否 | 无 |
| `validate-auth-token` | 否 | 无 |
| `list_task` | 否 | 无 |
| `import_login_response` | 否 | 无 |
| `login` | 是，浏览器流程 | 打开登录页，回调写本地凭证 |
| `relogin` | 是，浏览器流程 | 同 `login`，先清本地凭证 |
| `user_credit` | 是 | `POST /commerce/v1/benefits/user_credit` |
| `query_result` | 可能 | `POST /mweb/v1/get_history_by_ids` |
| `query_result --download_dir` | 可能 + 直链下载 | 历史查询 + 结果 URL 直接 `GET` |
| `text2image` | 是 | `POST /dreamina/cli/v1/image_generate/` |
| `image2image` | 是 | 上传 + `POST /dreamina/cli/v1/image_generate` |
| `image_upscale` | 是 | 上传 + `POST /dreamina/cli/v1/image_generate` |
| `text2video` | 是 | `POST /dreamina/cli/v1/video_generate` |
| `image2video` | 是 | 上传 + `POST /dreamina/cli/v1/video_generate` |
| `frames2video` | 是 | 上传 + `POST /dreamina/cli/v1/video_generate` |
| `multiframe2video` | 是 | 多图上传 + `POST /dreamina/cli/v1/video_generate` |
| `ref2video` | 是 | 同 `multiframe2video` |
| `multimodal2video` | 是 | 多资源上传 + `POST /dreamina/cli/v1/video_generate` |

---

## 2. 无远端请求的命令

这几类命令只读写本地状态，不发 HTTP，请求流里没有可模拟的 `curl`。

### 2.1 `help`

流程：

1. 读取根命令树。
2. 输出帮助文本。

`curl`：

```text
不适用
```

### 2.2 `version`

流程：

1. 读取本地构建版本信息。
2. 输出版本。

`curl`：

```text
不适用
```

### 2.3 `logout`

流程：

1. 读取本地 `credential.json`。
2. 删除或清理本地登录态。
3. 输出“已清除本地登录态”或“当前没有本地登录态”。

`curl`：

```text
不适用
```

### 2.4 `validate-auth-token`

流程：

1. 读取本地 `auth_token`。
2. 仅做本地解析。
3. 输出整理后的会话 JSON。

说明：

- 当前命令实现没有调用远端 `/auth/v1/token/validate`。
- 源码里虽然存在认证客户端，但这个命令没有走它。

`curl`：

```text
不适用
```

### 2.5 `list_task`

流程：

1. 读取本地 `tasks.db`。
2. 按 `submit_id / gen_task_type / gen_status / offset / limit` 过滤。
3. 输出本地任务列表。

`curl`：

```text
不适用
```

### 2.6 `import_login_response`

流程：

1. 从 `--file` 或标准输入读取完整登录响应 JSON。
2. 本地解析并落地到凭证文件。
3. 后续命令复用这份登录态。

说明：

- 这是本地导入动作，不直接请求后端。
- 如果你已经从浏览器拿到了完整 JSON，这个命令是最稳定的“非交互登录”接入点。

CLI 示例：

```bash
dreamina import_login_response --file login_response.json
cat login_response.json | dreamina import_login_response
```

`curl`：

```text
不适用
```

---

## 3. 登录相关命令

### 3.1 `login`

源码流程：

1. CLI 启动本地回调服务，默认监听 `60713` 端口。
2. 打开浏览器访问登录页。
3. 浏览器完成扫码/授权。
4. 登录页把会话结果回调到本地：
   `http://127.0.0.1:60713/dreamina/callback/save_session`
5. CLI 落地本地凭证，随后可继续调用后续接口。

浏览器打开的 URL 形态：

```text
https://jimeng.jianying.com/ai-tool/login?callback=http://127.0.0.1:60713/dreamina/callback/save_session&from=cli&random_secret_key=__RANDOM_SECRET_KEY__
```

结论：

- 这不是一个“单一稳定 API”。
- 更准确地说，这是“浏览器登录页 + 本地回调服务”的组合流程。
- 因此严格意义上没有一个可以完全替代 CLI 的单条 `curl`。

可模拟部分：

```bash
curl "${BASE_URL}/ai-tool/login?callback=http://127.0.0.1:60713/dreamina/callback/save_session&from=cli&random_secret_key=__RANDOM_SECRET_KEY__"
```

说明：

- 上面的 `curl` 只能访问登录页，不会完成扫码授权，也不会产生最终登录态。
- 真正可落地的替代方案仍然是“手工拿到登录响应 JSON + `import_login_response`”。

### 3.2 `relogin`

流程：

1. 先清理本地凭证。
2. 之后完全复用 `login` 的浏览器授权流程。

`curl`：

```text
与 login 相同，本质仍然不是单条稳定 API
```

### 3.3 手工导入页面

CLI 还会生成一个手工导入页面：

```text
https://jimeng.jianying.com/dreamina/cli/v1/dreamina_cli_login?aid=513695&random_secret_key=__RANDOM_SECRET_KEY__&web_version=7.5.0
```

它的用途更接近：

1. 在浏览器侧拿登录结果。
2. 再把完整 JSON 交给 `dreamina import_login_response`。

可模拟访问：

```bash
curl "${BASE_URL}/dreamina/cli/v1/dreamina_cli_login?aid=513695&random_secret_key=__RANDOM_SECRET_KEY__&web_version=7.5.0"
```

---

## 4. 账号相关命令

### 4.1 `user_credit`

流程：

1. 读取本地登录态。
2. 组装 `commerce` 请求头。
3. 追加 `commerce` 签名头。
4. 发起 `POST /commerce/v1/benefits/user_credit?aid=513695`。
5. 请求体固定为 `{}`。
6. 输出额度信息。

接口：

```text
POST /commerce/v1/benefits/user_credit?aid=513695
```

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/commerce/v1/benefits/user_credit?aid=513695" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Appvr: __APP_VERSION__" \
  -H "App-Sdk-Version: __APP_SDK_VERSION__" \
  -H "Device-Time: __UNIX_TS__" \
  -H "Sign: __SIGN__" \
  -H "Sign-Ver: __SIGN_VER__" \
  -H "X-Client-Scheme: https" \
  -H "X-Use-Ppe: 1" \
  --data '{}'
```

注意：

- 这里最关键的是 `--data '{}'`，不能是空 body。
- `Sign / Sign-Ver / Device-Time` 等头由源码里的签名逻辑动态生成，外部环境通常需要自己实现。

---

## 5. 查询类命令

### 5.1 `query_result`

流程分成两段：

1. 先查本地任务库。
2. 如果本地任务仍处于 `querying`，则远端补查历史接口：
   `POST /mweb/v1/get_history_by_ids`

接口：

```text
POST /mweb/v1/get_history_by_ids
```

请求 Query：

```text
aid=513695
from=dreamina_cli
cli_version=${CLI_VERSION}
```

请求体核心结构：

```json
{
  "submit_ids": ["${SUBMIT_ID}"],
  "history_ids": [],
  "need_batch": false
}
```

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/mweb/v1/get_history_by_ids?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "submit_ids": ["'"${SUBMIT_ID}"'"],
    "history_ids": [],
    "need_batch": false
  }'
```

### 5.2 `query_result --download_dir`

在上面的查询流程之外，CLI 还会：

1. 从查询结果中拿到图片/视频直链。
2. 直接对媒体 URL 发 `GET`。
3. 下载到 `--download_dir`。

这一步不是再调用 Dreamina 业务接口，而是直接下载资源 URL。

模拟 `curl`：

```bash
curl -L "__MEDIA_URL_FROM_QUERY_RESULT__" -o ./downloads/result.bin
```

---

## 6. 上传资源子流程

凡是命令参数里带本地文件，CLI 都不是直接把文件塞进生成接口，而是先上传，再拿 `resource_id` 提交生成任务。

涉及命令：

- `image2image`
- `image_upscale`
- `image2video`
- `frames2video`
- `multiframe2video`
- `ref2video`
- `multimodal2video`

统一流程：

1. `POST /mweb/v1/get_upload_token`
2. 根据资源类型走不同上传链路
3. `POST /dreamina/mcp/v1/resource_store`
4. 拿到最终 `resource_id`
5. 再调用图片/视频生成接口

### 6.1 获取上传令牌

接口：

```text
POST /mweb/v1/get_upload_token
```

请求体：

```json
{
  "scene": 2,
  "agent_scene": 2,
  "resource_type": "image"
}
```

说明：

- `scene` / `agent_scene` 会按资源类型推导。
- `resource_type` 常见值有 `image`、`video`、`audio`。

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/mweb/v1/get_upload_token" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "scene": 2,
    "agent_scene": 2,
    "resource_type": "image"
  }'
```

### 6.2 文件上传

这里不是单一固定 URL，取决于 token 返回结果：

- 图片：走 ImageX 的 `apply / upload / commit` 链路。
- 视频：走 VOD OpenAPI 的 `ApplyUploadInner -> 直传 -> CommitUploadInner`。
- 音频：当前也可能复用类似直传/确认链路。

因此这里无法给出“一条固定 curl 覆盖所有上传”的通用模板。

如果只想理解流程，可以把它简化为：

```text
get_upload_token -> 按 token 指示上传二进制 -> resource_store
```

### 6.3 资源确认 `resource_store`

接口：

```text
POST /dreamina/mcp/v1/resource_store
```

请求体是上传结果的归一化结构，核心可理解为：

```json
{
  "resource_items": [
    {
      "resource_type": "image",
      "resource_value": "__STORE_URI_OR_UPLOAD_RESULT__"
    }
  ]
}
```

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/mcp/v1/resource_store" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "X-Tt-Logid: ${LOGID}" \
  --data '{
    "resource_items": [
      {
        "resource_type": "image",
        "resource_value": "__STORE_URI_OR_UPLOAD_RESULT__"
      }
    ]
  }'
```

---

## 7. 图片生成命令

### 7.1 `text2image`

CLI 请求流程：

1. 校验 `--prompt`。
2. 直接调用图片生成接口。

接口：

```text
POST /dreamina/cli/v1/image_generate/
```

说明：

- 源码里的 `Text2Image` 调用路径是带尾斜杠的 `/dreamina/cli/v1/image_generate/`。
- 但直接手写 `curl` 时，服务端当前会先返回 `307` 跳转到无尾斜杠路径。
- 因此手工重放最稳妥的写法是直接请求 `/dreamina/cli/v1/image_generate`，或者显式加 `-L` 跟随跳转。

默认 payload 关键字段：

```json
{
  "agent_scene": "workbench",
  "creation_agent_version": "3.0.0",
  "generate_type": "text2imageByConfig",
  "prompt": "一只白色机械猫，电影感光影",
  "ratio": "16:9",
  "submit_id": "__AUTO_GENERATED__",
  "subject_id": "__AUTO_GENERATED__"
}
```

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "generate_type": "text2imageByConfig",
    "prompt": "一只白色机械猫，电影感光影",
    "ratio": "16:9",
    "model_key": "__OPTIONAL_MODEL_KEY__",
    "resolution_type": "__OPTIONAL_RESOLUTION_TYPE__",
    "submit_id": "__AUTO_GENERATED__",
    "subject_id": "__AUTO_GENERATED__"
  }'
```

实测结论：

- 2026-04-05 已验证：无尾斜杠 `curl` 可直接拿到业务成功 JSON。
- 同日验证到：带尾斜杠路径会先收到 `HTTP 307`，如果不加 `-L`，`stdout` 可能为空。

### 7.2 `image2image`

CLI 请求流程：

1. 校验 `--image/--images` 与 `--prompt`。
2. 上传图片，拿到 `resource_id_list`。
3. 调 `image_generate`。

接口：

```text
POST /dreamina/cli/v1/image_generate
```

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "generate_type": "editImageByConfig",
    "prompt": "把画面改成夜景霓虹风格",
    "ratio": "16:9",
    "resource_id_list": ["'"${RESOURCE_ID}"'"],
    "model_key": "__OPTIONAL_MODEL_KEY__",
    "resolution_type": "__OPTIONAL_RESOLUTION_TYPE__",
    "submit_id": "__AUTO_GENERATED__",
    "subject_id": "__AUTO_GENERATED__"
  }'
```

### 7.3 `image_upscale`

CLI 请求流程：

1. 校验 `--image` 与 `--resolution_type`。
2. 上传图片，拿到 `resource_id`。
3. 调 `image_generate`，但 `generate_type` 固定为 `imageSuperResolution`。

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "generate_type": "imageSuperResolution",
    "resource_id": "'"${RESOURCE_ID}"'",
    "resolution_type": "__REQUIRED_RESOLUTION_TYPE__",
    "submit_id": "__AUTO_GENERATED__",
    "subject_id": "__AUTO_GENERATED__"
  }'
```

---

## 8. 视频生成命令

### 8.1 `text2video`

流程：

1. 校验 `--prompt`。
2. 直接调用视频生成接口。

接口：

```text
POST /dreamina/cli/v1/video_generate
```

默认字段：

```json
{
  "generate_type": "text2VideoByConfig",
  "agent_scene": "workbench",
  "prompt": "未来城市清晨航拍",
  "ratio": "16:9",
  "duration": 5,
  "creation_agent_version": "3.0.0",
  "model_key": "seedance2.0fast",
  "submit_id": "__AUTO_GENERATED__"
}
```

说明：

- 当前支持的 `model_key` 额外包含 `seedance2.0_vip`、`seedance2.0fast_vip`
- 默认值仍然是 `seedance2.0fast`

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "text2VideoByConfig",
    "agent_scene": "workbench",
    "prompt": "未来城市清晨航拍",
    "ratio": "16:9",
    "duration": 5,
    "creation_agent_version": "3.0.0",
    "model_key": "seedance2.0fast",
    "video_resolution": "__OPTIONAL_VIDEO_RESOLUTION__",
    "submit_id": "__AUTO_GENERATED__"
  }'
```

### 8.2 `image2video`

有两条提交形态。

#### 8.2.1 默认路径

当没有显式高级参数时：

```json
{
  "generate_type": "image2video",
  "first_frame_resource_id": "__RESOURCE_ID__",
  "prompt": "镜头缓慢推进",
  "duration": 5
}
```

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "image2video",
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "first_frame_resource_id": "'"${RESOURCE_ID}"'",
    "prompt": "镜头缓慢推进",
    "duration": 5,
    "submit_id": "__AUTO_GENERATED__"
  }'
```

#### 8.2.2 高级配置路径

当显式传了 `duration / video_resolution / model_version`，CLI 会切到：

```json
{
  "generate_type": "firstFrameVideoByConfig",
  "model_key": "seedance2.0fast",
  "first_frame_resource_id": "__RESOURCE_ID__"
}
```

说明：

- 高级配置路径的 `model_key` 现在也支持 `seedance2.0_vip`、`seedance2.0fast_vip`
- 端点和 `generate_type` 没有变化，仍然是 `/dreamina/cli/v1/video_generate` + `firstFrameVideoByConfig`

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "firstFrameVideoByConfig",
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "first_frame_resource_id": "'"${RESOURCE_ID}"'",
    "prompt": "镜头缓慢推进",
    "duration": 5,
    "model_key": "seedance2.0fast",
    "video_resolution": "720p",
    "submit_id": "__AUTO_GENERATED__"
  }'
```

### 8.3 `frames2video`

流程：

1. 上传首帧和尾帧。
2. 拿到两个 `resource_id`。
3. 调视频生成接口。

补充：

- `model_key` 可选值除了原来的 `3.0`、`3.5pro`、`seedance2.0`、`seedance2.0fast`，还包括 `seedance2.0_vip`、`seedance2.0fast_vip`
- 仅模型集合变更，接口和 `generate_type` 不变

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "startEndFrameVideoByConfig",
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "first_frame_resource_id": "__FIRST_RESOURCE_ID__",
    "last_frame_resource_id": "__LAST_RESOURCE_ID__",
    "prompt": "__OPTIONAL_PROMPT__",
    "duration": 5,
    "video_resolution": "__OPTIONAL_VIDEO_RESOLUTION__",
    "model_key": "__OPTIONAL_MODEL_KEY__",
    "submit_id": "__AUTO_GENERATED__"
  }'
```

### 8.4 `multiframe2video` / `ref2video`

这两个命令在实现上共用同一条链路。

流程：

1. 上传 2 到 20 张图。
2. 拿到 `media_resource_id_list`。
3. 组装 `media_type_list / prompt_list / duration_list`。
4. 调视频生成接口。

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "multiFrame2video",
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "submit_id": "__AUTO_GENERATED__",
    "media_resource_id_list": ["__RID1__", "__RID2__", "__RID3__"],
    "media_type_list": ["图片", "图片", "图片"],
    "prompt_list": ["从图1过渡到图2", "从图2过渡到图3"],
    "duration_list": [2.0, 2.0]
  }'
```

### 8.5 `multimodal2video`

流程：

1. 上传图片、视频、音频资源。
2. 拿到三类 `resource_id_list`。
3. 调视频生成接口。

注意：

- 不能只有音频，至少要有图片或视频。
- 2026-04-05 实测确认：最小可行重放可以只传 `image_resource_id_list`，`video_resource_id_list` 和 `audio_resource_id_list` 可为空数组。
- 同日也已验证 CLI 端可以走“图片 + 视频 + 音频”完整上传链路并成功提交。
- 当前 `model_key` 还支持 `seedance2.0_vip`、`seedance2.0fast_vip`，端点和 `generate_type` 保持不变。
- 当前 smoke 脚本中的 `curl:multimodal2video` 会优先从最近一次 `cli:multimodal2video` 对应的 `tasks.db.result_json` 读取 `request.*_resource_id_list` / `uploaded_*`，自动补全图片、视频、音频三类列表；若本地库拿不到，再回退到 `history/query` 兜底，最后才退回 image-only 最小 payload。
- 从 2026-04-05 起，`curl:multimodal2video` 的标准 smoke 结果页还会额外写出 `payload_mode`、`image_resource_id`、`video_resource_id`、`audio_resource_id` 摘要，方便直接判断本次重放用了哪组资源。

模拟 `curl`：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "multiModal2VideoByConfig",
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "submit_id": "__AUTO_GENERATED__",
    "prompt": "以图像为主视觉生成统一短片",
    "ratio": "16:9",
    "duration": 5,
    "model_key": "seedance2.0fast",
    "video_resolution": "__OPTIONAL_VIDEO_RESOLUTION__",
    "image_resource_id_list": ["__IMAGE_RID__"],
    "video_resource_id_list": [],
    "audio_resource_id_list": []
  }'
```

如果要模拟 CLI 已完成三类资源上传后的完整提交，可以直接把三类 `resource_id` 都带上：

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "Cookie: ${COOKIE}" \
  -H "Content-Type: application/json" \
  -H "X-Tt-Logid: ${LOGID}" \
  -H "X-Use-Ppe: 1" \
  --data '{
    "generate_type": "multiModal2VideoByConfig",
    "agent_scene": "workbench",
    "creation_agent_version": "3.0.0",
    "submit_id": "__AUTO_GENERATED__",
    "prompt": "以图像为主视觉生成统一短片",
    "ratio": "16:9",
    "duration": 5,
    "model_key": "seedance2.0fast",
    "image_resource_id_list": ["__IMAGE_RID__"],
    "video_resource_id_list": ["__VIDEO_RID__"],
    "audio_resource_id_list": ["__AUDIO_RID__"]
  }'
```

---

## 9. 一条命令的完整链路怎么读

如果要从“用户输入一个 dreamina 命令”追到“最后发了哪些 HTTP”，可以用下面这个思路：

### 9.1 纯本地命令

```text
CLI 参数解析 -> 读取本地配置/凭证/任务库 -> 直接输出
```

适用：

- `help`
- `version`
- `logout`
- `validate-auth-token`
- `list_task`
- `import_login_response`

### 9.2 登录命令

```text
CLI 参数解析 -> 启动本地回调服务 -> 打开浏览器登录页 -> 浏览器授权 -> 回调保存凭证
```

适用：

- `login`
- `relogin`

### 9.3 直接生成命令

```text
CLI 参数解析 -> 读取登录态 -> 直接 POST 到 image_generate/video_generate -> 本地记录 submit_id
```

适用：

- `text2image`
- `text2video`

### 9.4 带本地文件的生成命令

```text
CLI 参数解析 -> 校验本地文件 -> get_upload_token -> 文件直传 -> resource_store -> 拿 resource_id -> 调生成接口
```

适用：

- `image2image`
- `image_upscale`
- `image2video`
- `frames2video`
- `multiframe2video`
- `ref2video`
- `multimodal2video`

### 9.5 查询命令

```text
CLI 参数解析 -> 查本地任务库 -> 如仍 querying 则调 get_history_by_ids -> 输出结果
```

适用：

- `query_result`

如果开启下载：

```text
query_result -> 解析媒体 URL -> 直接 GET 媒体直链
```

---

## 10. 补充：源码里存在但当前命令未直接暴露的接口

### 10.1 远端校验 auth token

源码里有：

```text
GET /auth/v1/token/validate?nonce=...
```

但当前 `dreamina validate-auth-token` 命令没有调用它。

### 10.2 用户信息接口

登录后汇总信息时，内部还可能调用：

```text
POST /commerce/v1/subscription/user_info?aid=513695&platform_app_id=14942&subscription_env=prod
```

这不是一个独立 CLI 一级命令。

---

## 11. 最简对照表

如果只关心“命令最终打到哪个接口”，可以直接看这张表：

| 命令 | 最终关键接口 |
| --- | --- |
| `user_credit` | `POST /commerce/v1/benefits/user_credit` |
| `query_result` | `POST /mweb/v1/get_history_by_ids` |
| `text2image` | `POST /dreamina/cli/v1/image_generate/` |
| `image2image` | `POST /dreamina/cli/v1/image_generate` |
| `image_upscale` | `POST /dreamina/cli/v1/image_generate` |
| `text2video` | `POST /dreamina/cli/v1/video_generate` |
| `image2video` | `POST /dreamina/cli/v1/video_generate` |
| `frames2video` | `POST /dreamina/cli/v1/video_generate` |
| `multiframe2video` | `POST /dreamina/cli/v1/video_generate` |
| `ref2video` | `POST /dreamina/cli/v1/video_generate` |
| `multimodal2video` | `POST /dreamina/cli/v1/video_generate` |

---

## 12. CLI 调用对照

这一节补的是“用户实际在终端里怎么敲”以及“它最终对应哪一类请求”。

### 12.1 本地命令

```bash
dreamina help
dreamina version
dreamina logout
dreamina validate-auth-token
dreamina list_task --limit 20
dreamina import_login_response --file login_response.json
```

对应链路：

```text
命令解析 -> 本地配置 / 凭证 / tasks.db -> 输出结果
```

### 12.2 登录命令

```bash
dreamina login
dreamina login --headless
dreamina login --debug
dreamina relogin
```

对应链路：

```text
命令解析 -> 启动本地回调服务 -> 打开浏览器或无头浏览器 -> 登录授权 -> 回调写 credential.json
```

### 12.3 账号命令

```bash
dreamina user_credit
```

对应链路：

```text
命令解析 -> 读取本地登录态 -> commerce 签名 -> POST /commerce/v1/benefits/user_credit
```

### 12.4 查询命令

```bash
dreamina query_result --submit_id "${SUBMIT_ID}"
dreamina query_result --submit_id "${SUBMIT_ID}" --download_dir ./downloads
```

对应链路：

```text
命令解析 -> 本地 tasks.db -> 如仍 querying 则 POST /mweb/v1/get_history_by_ids -> 可选下载媒体直链
```

### 12.5 图片生成命令

```bash
dreamina text2image --prompt "一只白色机械猫，电影感光影"
dreamina text2image --prompt "一只白色机械猫，电影感光影" --ratio 1:1 --resolution_type 2k

dreamina image2image --image ./input.png --prompt "改成夜景霓虹风格"
dreamina image_upscale --image ./input.png --resolution_type 4k
```

对应链路：

```text
text2image:
命令解析 -> 读取登录态 -> POST /dreamina/cli/v1/image_generate/

image2image / image_upscale:
命令解析 -> 校验本地文件 -> get_upload_token -> 上传 -> resource_store -> POST /dreamina/cli/v1/image_generate
```

### 12.6 视频生成命令

```bash
dreamina text2video --prompt "未来城市清晨航拍"

dreamina image2video --image ./cover.png --prompt "镜头缓慢推进"
dreamina image2video --image ./cover.png --prompt "镜头缓慢推进" --duration 5 --video_resolution 720p

dreamina frames2video --first ./start.png --last ./end.png --prompt "人物从静止转身离开"

dreamina multiframe2video \
  --images ./1.png \
  --images ./2.png \
  --images ./3.png \
  --transition-prompt "从图1过渡到图2" \
  --transition-prompt "从图2过渡到图3" \
  --transition-duration 2 \
  --transition-duration 2

dreamina ref2video \
  --images ./1.png \
  --images ./2.png \
  --prompt "从图1自然过渡到图2" \
  --duration 3

dreamina multimodal2video \
  --image ./cover.png \
  --video ./bg.mp4 \
  --audio ./music.mp3 \
  --prompt "生成统一短片"
```

对应链路：

```text
text2video:
命令解析 -> 读取登录态 -> POST /dreamina/cli/v1/video_generate

其余视频类:
命令解析 -> 校验本地文件 -> get_upload_token -> 上传 -> resource_store -> POST /dreamina/cli/v1/video_generate
```

---

## 13. 源码定位

如果后续要继续做“对齐原版请求”，建议直接从下面这些入口看。

### 13.1 命令入口

| 类型 | 文件 |
| --- | --- |
| 根命令树 | `src/cmd/root.go` |
| 登录命令 | `src/cmd/login.go` |
| 登录导入 | `src/cmd/import_login_response.go` |
| 账号命令 | `src/cmd/account.go` |
| 查询命令 | `src/cmd/tasks.go` |
| 生成命令 | `src/cmd/generators.go` |

### 13.2 远端客户端

| 类型 | 文件 |
| --- | --- |
| MCP 生成 / 历史查询 | `src/components/client/dreamina/mcp/client.go` |
| 资源上传 | `src/components/client/dreamina/resource/client.go` |
| Commerce | `src/components/client/dreamina/commerce/client.go` |
| Auth 校验客户端 | `src/components/client/dreamina/auth/client.go` |
| 登录 URL / 手工导入 URL | `src/components/login/store.go` |
| 登录主流程 | `src/components/login/login.go` |
| 默认 baseURL / 公共后端头 | `src/infra/httpclient/client.go` |

### 13.3 常看函数

| 关注点 | 函数 / 位置 |
| --- | --- |
| 根命令挂载 | `NewRootCommand()` |
| MCP 公共 Query | `defaultMCPQuery()` |
| MCP 发请求 | `doPost()` |
| 文生图 payload | `buildText2ImagePayload()` |
| 图生图 payload | `buildImage2ImagePayload()` |
| 超分 payload | `buildUpscalePayload()` |
| 文生视频 payload | `buildText2VideoPayload()` |
| 图生视频 payload | `buildImage2VideoPayload()` |
| 首尾帧 payload | `buildFrames2VideoPayload()` |
| 多帧视频 payload | `buildRef2VideoPayload()` |
| 多模态视频 payload | `buildMultiModal2VideoPayload()` |
| 上传 token | `getUploadToken()` |
| 资源确认 | `resourceStore()` |
| 登录授权 URL | `AuthorizationURL()` |
| 手工导入 URL | `ManualImportURL()` |

---

## 14. 结论

当前 CLI 可以粗分为三层：

1. 纯本地命令：没有 `curl` 可模拟。
2. 浏览器登录命令：不是单一稳定 API，建议通过“手工拿登录 JSON + `import_login_response`”接入。
3. 业务请求命令：核心就是 `commerce`、`history`、`image_generate`、`video_generate`、`get_upload_token`、`resource_store` 这几组接口。

如果后面要继续做“命令和原版请求完全对齐”，最实用的做法是：

1. 先固定登录态。
2. 再按本文模板逐条比对请求头、query、payload。
3. 带文件的命令优先验证“上传链路 + resource_store”是否一致，因为这部分最容易产生偏差。

---

## 15. 常见输出视图

这一节补的是“命令执行完后，CLI 大致会输出什么结构”。这里不追求逐字段穷举，而是给后续对齐一个稳定参照。

### 15.1 生成提交成功但仍在排队

生成类命令在未开启轮询，或者开启轮询但超时后仍处于 `querying` 时，常见输出会收敛成类似：

```json
{
  "submit_id": "__SUBMIT_ID__",
  "prompt": "镜头缓慢推进",
  "logid": "__LOG_ID__",
  "gen_status": "querying",
  "credit_count": 1,
  "queue_info": {
    "queue_position": 3
  }
}
```

适用：

- `text2image`
- `image2image`
- `image_upscale`
- `text2video`
- `image2video`
- `frames2video`
- `multiframe2video`
- `ref2video`
- `multimodal2video`

### 15.2 生成失败

如果后端明确返回失败，CLI 常见输出会收敛成：

```json
{
  "submit_id": "__SUBMIT_ID__",
  "prompt": "镜头缓慢推进",
  "logid": "__LOG_ID__",
  "gen_status": "fail",
  "fail_reason": "__FAIL_REASON__"
}
```

### 15.3 查询终态结果

`query_result` 或生成命令轮询到终态后，输出会更接近统一任务视图：

```json
{
  "task": {
    "submit_id": "__SUBMIT_ID__",
    "gen_status": "success",
    "gen_task_type": "text2video",
    "result": {
      "items": [
        {
          "resource_id": "__RESOURCE_ID__",
          "url": "__MEDIA_URL__"
        }
      ]
    }
  }
}
```

说明：

- 实际字段会比这里多。
- 但在做链路验收时，通常先看 `submit_id / gen_status / result.items[].url` 就足够判断主流程是否跑通。

### 15.4 账号额度输出

`user_credit` 的公开输出会收敛成：

```json
{
  "vip_credit": 0,
  "gift_credit": 0,
  "purchase_credit": 0,
  "total_credit": 0
}
```

### 15.5 本地登录态解析输出

`validate-auth-token` 输出的是本地解析后的会话视图，通常类似：

```json
{
  "cookie": "__COOKIE__",
  "headers": {
    "Accept": "application/json",
    "Pf": "7",
    "User-Agent": "__UA__"
  },
  "uid": "__UID__"
}
```

---

## 16. 命令级对齐检查清单

这一节可以直接当验收 checklist 用。后续继续对齐时，建议按命令逐条打勾。

### 16.1 本地命令

| 命令 | 参数行为 | 本地状态读写 | 远端请求 | 当前检查点 |
| --- | --- | --- | --- | --- |
| `help` | 无 | 无 | 无 | 确认只输出帮助 |
| `version` | 无 | 无 | 无 | 确认只输出版本 |
| `logout` | 无 | 删除凭证 | 无 | 确认不误发远端请求 |
| `validate-auth-token` | 无 | 读凭证 | 无 | 确认不调用 `/auth/v1/token/validate` |
| `list_task` | `submit_id/gen_task_type/gen_status/offset/limit` | 读 `tasks.db` | 无 | 确认分页与筛选行为 |
| `import_login_response` | `--file` 或 stdin | 写凭证 | 无 | 确认空 stdin/空文件报错 |

### 16.2 登录命令

| 命令 | 登录页 URL | 本地回调 | 手工导入 URL | 当前检查点 |
| --- | --- | --- | --- | --- |
| `login` | `/ai-tool/login?...` | `/dreamina/callback/save_session` | 支持 | 确认普通/无头/调试模式 |
| `relogin` | 同 `login` | 同 `login` | 支持 | 确认先清本地凭证再登录 |

### 16.3 账号与查询命令

| 命令 | 核心接口 | 请求体 | 输出视图 | 当前检查点 |
| --- | --- | --- | --- | --- |
| `user_credit` | `/commerce/v1/benefits/user_credit` | `{}` | 四个 credit 字段 | 确认空 body 不被发送 |
| `query_result` | `/mweb/v1/get_history_by_ids` | `submit_ids/history_ids/need_batch` | 统一 task 视图 | 确认先查本地再远端补查 |
| `query_result --download_dir` | 同上 + 媒体直链 GET | 无额外业务 body | task + 下载文件 | 确认下载命名与错误处理 |

### 16.4 图片生成命令

| 命令 | 上传资源 | 最终接口 | generate_type | 当前检查点 |
| --- | --- | --- | --- | --- |
| `text2image` | 否 | `/dreamina/cli/v1/image_generate/` | `text2imageByConfig` | 确认源码路径带尾 `/`，手工 curl 需改成无尾 `/` 或 `-L` |
| `image2image` | 是 | `/dreamina/cli/v1/image_generate` | `editImageByConfig` | 确认字段名是 `resource_id_list` |
| `image_upscale` | 是 | `/dreamina/cli/v1/image_generate` | `imageSuperResolution` | 确认不是旧 `/mcp/v1/upscale` |

### 16.5 视频生成命令

| 命令 | 上传资源 | 最终接口 | generate_type | 当前检查点 |
| --- | --- | --- | --- | --- |
| `text2video` | 否 | `/dreamina/cli/v1/video_generate` | `text2VideoByConfig` | 确认默认模型 `seedance2.0fast`，且 `_vip` 变体仍走同端点/同 payload 结构 |
| `image2video` | 是 | `/dreamina/cli/v1/video_generate` | `image2video` 或 `firstFrameVideoByConfig` | 确认高级参数触发 by-config |
| `frames2video` | 是 | `/dreamina/cli/v1/video_generate` | `startEndFrameVideoByConfig` | 确认首尾帧字段名 |
| `multiframe2video` | 是 | `/dreamina/cli/v1/video_generate` | `multiFrame2video` | 确认 `prompt_list/duration_list` 数量 |
| `ref2video` | 是 | `/dreamina/cli/v1/video_generate` | `multiFrame2video` | 确认只是别名，不是独立接口 |
| `multimodal2video` | 是 | `/dreamina/cli/v1/video_generate` | `multiModal2VideoByConfig` | 确认禁止纯音频输入 |

### 16.6 上传链路

| 阶段 | 接口/动作 | 当前检查点 |
| --- | --- | --- |
| 获取 token | `/mweb/v1/get_upload_token` | 确认 `scene/agent_scene/resource_type` |
| 图片上传 | ImageX apply/upload/commit | 确认不是旧 phase 上传 |
| 视频上传 | `ApplyUploadInner -> 直传 -> CommitUploadInner` | 确认 token 驱动，不写死上传 URL |
| 资源确认 | `/dreamina/mcp/v1/resource_store` | 确认最终必须回填 `resource_id` |

---

## 17. 建议的后续验收顺序

如果接下来要继续做“接口与原版 100% 对齐”，建议按下面顺序推进，收益最高。

1. `login / import_login_response`
   原因：登录态稳定了，后面所有命令才能稳定复现。

2. `user_credit`
   原因：它链路最短，可以先验证 commerce 签名与登录态是否正确。

3. `text2image / text2video`
   原因：没有文件上传，最容易验证 MCP 头、query、payload 是否对齐。

4. `query_result`
   原因：可以校验本地任务状态与远端历史查询之间的衔接。

5. `image2image / image_upscale / image2video`
   原因：开始覆盖资源上传 + resource_store + generate 的完整链路。

6. `frames2video / multiframe2video / ref2video / multimodal2video`
   原因：这些命令 payload 更复杂，更适合在基础链路稳定后再做精修。

---

## 18. 当前状态判定

这一节把前面的说明收束成“当前代码状态台账”。这里的判定标准不是“线上真实环境 100% 已验证”，而是：

1. 是否已在当前源码中实现成目标链路。
2. 是否已有明确的本地构建/测试支撑。
3. 是否仍依赖真实账号、真实资源或真实服务端进一步验收。

状态说明：

- `已对齐（源码）`：从命令入口到请求链路看，当前实现已经落到目标接口和目标字段形态。
- `已验证（实测）`：除源码对齐外，已经有真实 smoke、调试日志或批量回归结果支撑。
- `待人工验收`：链路已基本明确，但仍需要人工登录、账号权限或特定场景补验。

### 18.1 一级命令状态

| 命令 | 当前状态 | 说明 |
| --- | --- | --- |
| `help` | `已对齐（源码）` | 纯本地命令，无远端请求面。 |
| `version` | `已对齐（源码）` | 纯本地命令。 |
| `logout` | `已对齐（源码）` | 纯本地凭证清理。 |
| `validate-auth-token` | `已对齐（源码）` | 当前明确只做本地解析，不调远端校验接口。 |
| `list_task` | `已对齐（源码）` | 当前明确只读本地 `tasks.db`。 |
| `import_login_response` | `已对齐（源码）` | 当前明确是本地导入链路。 |
| `login` | `待人工验收` | 浏览器登录、回调落凭证的链路已明确，但仍需真实扫码/授权验证。 |
| `relogin` | `待人工验收` | 与 `login` 相同，另需确认“先清凭证再登录”的实际行为。 |
| `user_credit` | `已验证（实测）` | CLI 与 curl 都已跑通；当前主要剩真实签名策略和多账号环境的人工验收。 |
| `query_result` | `已验证（实测）` | 本地查库、历史补查、图片查询/下载链路都已有 smoke 结果支撑。 |
| `text2image` | `已验证（实测）` | 接口与 payload 已落源码；2026-04-05 已实测 curl 直连无尾 `/` 路径成功，源码尾 `/` 路径通过跳转到达同一接口。 |
| `image2image` | `已验证（实测）` | 请求面、上传链路和 curl/CLI 两条提交流程都已有 smoke 支撑。 |
| `image_upscale` | `已验证（实测）` | 已切到 `image_generate + imageSuperResolution`，并有标准 debug case 固定记录图片上传四段日志。 |
| `text2video` | `已验证（实测）` | 主接口、默认 payload 和 CLI/curl 提交链路都已实测通过。 |
| `image2video` | `已验证（实测）` | curl 复用已有图片 `resource_id` 的提交流程已通过；当前未单列 CLI 上传式 case。 |
| `frames2video` | `已验证（实测）` | curl 复用首尾帧资源的提交流程已通过；当前未单列 CLI 首尾帧上传式 case。 |
| `multiframe2video` | `已验证（实测）` | 参数校验和 payload 已收紧，curl 复用多图资源列表的提交流程已通过。 |
| `ref2video` | `已验证（实测）` | 作为 `multiframe2video` 别名的 curl 重放已通过。 |
| `multimodal2video` | `已验证（实测）` | curl 最小闭环、curl 自动补全资源重放，以及 CLI 图片+视频+音频完整链路都已通过。 |

### 18.2 上传链路状态

| 链路 | 当前状态 | 说明 |
| --- | --- | --- |
| `get_upload_token` | `已验证（实测）` | 已收敛到 `/mweb/v1/get_upload_token`；2026-04-05 已在真实图片/视频/音频链路里观察到 image/video/audio 三类 token 回包。 |
| 图片上传 | `已验证（实测）` | 已切到 ImageX 主链路；`ApplyImageUpload -> PUT -> CommitImageUpload -> resource_store` 已有标准 debug case 和标准 smoke 结果。 |
| 视频上传 | `已验证（实测）` | 已走 `ApplyUploadInner -> 直传 -> CommitUploadInner`，并已通过 multimodal CLI 完整链路验证。 |
| 音频上传 | `已验证（实测）` | 已纳入统一资源上传体系，并已通过 multimodal CLI 完整链路验证。 |
| `resource_store` | `已验证（实测）` | 已显式收紧为最终确认接口，并在图片链路与 multimodal 链路中观察到真实成功回包。 |

### 18.3 本轮已完成的确定性结果

这些是当前可以明确说“已经完成”的内容：

1. 命令面已经梳理成完整台账，覆盖所有一级命令。
2. 远端接口入口、公共 query/header、主要 payload 形态已经落文档。
3. 上传子流程已经单独拆出，不再把“本地文件命令”和“直接生成命令”混写。
4. `query_result` 的本地查库、历史补查、媒体下载三段链路已在文档中分开。
5. 文档里已经补上 CLI 调用示例、模拟 `curl`、源码入口、输出视图、检查清单、状态判定。
6. 本轮文档与脚本整理后，统一使用 `dist/<goos>-<goarch>/dreamina` 作为默认构建产物位置。
7. `curl:image2video`、`curl:frames2video`、`curl:multiframe2video` 已可直接复用已有图片资源跑通。
8. 2026-04-05 已完成最新一次 `batch:all` 全量回归，当前内置 CLI + curl case 全部通过。
9. `cli:multimodal2video` 已以“图片+视频+音频”形态成功进入 `querying`，音视频上传链路已有端到端样本。
10. 音频上传链路已通过：当前实现会在音频 token 带完整 STS 时复用 VOD OpenAPI 主链，而不是继续走会报 `InvalidAuthorization` 的旧 phase 路径。

### 18.4 仍需真实验收的高风险点

如果后面要继续精修，对齐风险最高的是下面几类：

1. `commerce` 签名头是否与真实服务端要求完全一致。
2. 浏览器登录回调拿到的完整会话字段，是否在不同账号环境下都稳定。
3. ImageX / VOD 上传 token 返回结构在不同资源类型下的差异。
4. `resource_store` 对多资源、乱序资源、缺字段资源的实际返回形态。
5. `query_result` 在失败态、空历史、部分历史、轮询超时场景下的输出一致性。

### 18.5 下一步最实用的推进方式

到这一步，文档已经足够当验收台账。继续往前推进，建议不要再扩写“大而全说明”，而是直接做下面其中一种：

1. 在这份文档里继续补“实测结果”列，逐条记录真实环境验证结论。
2. 直接按命令写回归脚本，把 `curl` 模板变成可执行 smoke case。
3. 优先做 `login -> user_credit -> text2video -> query_result` 的最短真实链路验证，先拿到第一条可闭环样本。

---

## 19. 实测记录模板

这一节开始不再讨论“应该怎样”，而是给后续真实验收留固定格式。建议每做完一条命令实测，就按下面模板补一条。

### 19.1 单条记录模板

```md
#### 命令：text2video

- 验证日期：YYYY-MM-DD
- 验证环境：dev / test / prod-like
- CLI 命令：
  `dreamina text2video --prompt "未来城市清晨航拍" --poll 10`
- 预期链路：
  `POST /dreamina/cli/v1/video_generate -> query_result`
- 实际结果：
  成功 / 失败 / 部分成功
- submit_id：
  `__SUBMIT_ID__`
- 关键 logid：
  `__LOG_ID__`
- 请求是否与文档一致：
  是 / 否
- 差异说明：
  例如 header 不一致、payload 字段缺失、响应字段命名不一致
- 结论：
  已通过 / 待修复 / 待复验
```

### 19.2 建议附加证据

每条实测记录至少建议保存下面一种证据：

- CLI 原始输出
- 对应 `curl` 重放命令
- 抓到的请求头 / 请求体摘要
- `submit_id` 对应的最终查询结果

---

## 20. 实测台账

下面这张表可以直接持续更新，先填状态，再补链接或备注。

### 20.1 一级命令实测台账

| 命令 | 是否已实测 | 结果 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| `login` | 否 | 待测 | - | 建议优先做普通模式和 `--headless` 两种 |
| `relogin` | 否 | 待测 | - | 重点看是否先清理旧凭证 |
| `import_login_response` | 否 | 待测 | - | 重点看文件导入和 stdin 导入两种 |
| `user_credit` | 是 | CLI 通过，curl 通过 | 2026-04-05 | CLI 已通过；curl 已通过，见 `logs/smoke/20260405-164206/results.md` 与 `logs/smoke/20260405-164219/results.md` |
| `query_result` | 是 | curl 通过，图片查询/下载通过 | 2026-04-05 | `batch:curl-core` 与 `batch:image-result-core` 已通过；最新 curl 批量结果见 `logs/smoke/20260405-200727/results.md`，图片下载结果见 `logs/smoke/20260405-165030/results.md` |
| `text2image` | 是 | curl 通过 | 2026-04-05 | 单项结果见 `logs/smoke/20260405-170445/results.md`；批量结果见 `logs/smoke/20260405-170456/results.md`；源码路径带尾 `/` 时会先收到 `307` |
| `image2image` | 是 | CLI 通过，curl 通过 | 2026-04-05 | CLI 已通过；curl 已通过，见 `logs/smoke/20260405-165624/results.md` |
| `image_upscale` | 是 | CLI 通过，curl 通过 | 2026-04-05 | CLI 已通过；curl 已通过，见 `logs/smoke/20260405-165624/results.md` |
| `text2video` | 是 | CLI 通过，curl 通过 | 2026-04-05 | CLI 与 curl 都已通过；最新 curl 结果见 `logs/smoke/20260405-164219/results.md` |
| `image2video` | 是 | curl 通过 | 2026-04-05 | 结果见 `logs/smoke/20260405-170937/results.md`；当前 smoke 默认复用最近图片资源 |
| `frames2video` | 是 | curl 通过 | 2026-04-05 | 结果见 `logs/smoke/20260405-170937/results.md`；当前 smoke 默认用最近 `image2image/upscale` 资源做首尾帧 |
| `multiframe2video` | 是 | curl 通过 | 2026-04-05 | 结果见 `logs/smoke/20260405-170937/results.md`；当前 smoke 默认复用已有图片资源列表 |
| `ref2video` | 是 | curl 通过 | 2026-04-05 | 结果见 `logs/smoke/20260405-171642/results.md`；当前 smoke 直接复用 `multiframe2video` 同一 payload 链路 |
| `multimodal2video` | 是 | CLI 通过，curl 通过 | 2026-04-05 | curl 历史最小闭环结果见 `logs/smoke/20260405-171536/results.md`；最新单项结果见 `logs/smoke/20260405-200544/results.md`，其中已直接写出 `payload_mode` 与三类资源 ID；CLI 单项完整链路结果见 `logs/smoke/20260405-173556/results.md`；最新批量结果见 `logs/smoke/20260405-174654/results.md`，已验证图片+视频+音频共同提交 |

### 20.2 上传链路实测台账

| 链路 | 是否已实测 | 结果 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| `get_upload_token(image)` | 是 | CLI 调试已观察 | 2026-04-05 | 开启 `DREAMINA_DEBUG=1` 后已观察到真实回包：`scene=1`、`resource_type=image`、`upload_domain=imagex.bytedanceapi.com`、`sts_present=true`；标准 smoke 结果见 `logs/smoke/20260405-192154/results.md` |
| `get_upload_token(video)` | 是 | CLI 调试已观察 | 2026-04-05 | 开启 `DREAMINA_DEBUG=1` 后已观察到真实回包：`scene=2`、`resource_type=video`、`upload_domain=vod.bytedanceapi.com`、`sts_present=true` |
| `get_upload_token(audio)` | 是 | CLI 调试已观察 | 2026-04-05 | 开启 `DREAMINA_DEBUG=1` 后已观察到真实回包：`scene=3`、`resource_type=audio`、`upload_domain=vod.bytedanceapi.com`、`sts_present=true` |
| 图片上传到 ImageX | 是 | CLI 通过 | 2026-04-05 | 已通过标准 case `cli:image_upscale_debug` 验证；结果见 `logs/smoke/20260405-192154/results.md`，其中 stderr 已包含 `ImageXUpload.apply`、`ImageXUpload.put`、`ImageXUpload.commit`、上传中间态 `resource_id` 与后续成功提交 |
| 视频上传到 VOD | 是 | CLI 通过 | 2026-04-05 | 已通过 `cli:multimodal2video` 验证；单项结果见 `logs/smoke/20260405-173556/results.md`，最新批量结果见 `logs/smoke/20260405-174654/results.md` |
| 音频上传 | 是 | CLI 通过 | 2026-04-05 | 已通过 `cli:multimodal2video` 验证；单项结果见 `logs/smoke/20260405-173556/results.md`，最新批量结果见 `logs/smoke/20260405-174654/results.md`；当前实现会在 audio token 带 STS 时切到 VOD OpenAPI 主链 |
| `resource_store` | 是 | CLI 通过 | 2026-04-05 | 已在 `cli:multimodal2video` 和 `cli:image_upscale_debug` 中观察到成功回包；multimodal 可结合 `logs/smoke/20260405-173556/results.md` 与 `logs/smoke/20260405-192908/results.md`，图片链路标准结果见 `logs/smoke/20260405-192154/results.md` |

### 20.3 批量回归台账

| 场景 | 当前状态 | 说明 |
| --- | --- | --- |
| `batch:core` | 已通过 | 2026-04-05 已跑通，结果见 `logs/smoke/20260405-162713/results.md` |
| `batch:image-core` | 已通过 | 2026-04-05 已跑通；当前仓库约定复用 `testdata/smoke/image-1.png` 作为默认样例图片 |
| `batch:image-upload-core` | 已通过 | 2026-04-05 已跑通，最新结果见 `logs/smoke/20260405-192851/results.md`；当前只执行 `cli:image_upscale_debug`，用于固定留存图片上传的 ImageX 四段明细日志 |
| `batch:video-upload-core` | 已通过 | 2026-04-05 已跑通，最新结果见 `logs/smoke/20260405-174834/results.md`；自动生成 5 秒参考视频和 5 秒音频样本，验证 `cli:multimodal2video` 图片+视频+音频链路 |
| `batch:image-result-core` | 已通过 | 2026-04-05 已跑通，结果见 `logs/smoke/20260405-165030/results.md`；已验证图片查询成功态和下载落盘 |
| `batch:curl-core` | 已通过 | 2026-04-05 已跑通，最新结果见 `logs/smoke/20260405-200727/results.md`；当前覆盖 `user_credit / text2image / text2video / query_result / multimodal2video`，脚本可自动解析本地 `COOKIE`、`CLI_VERSION`、`SUBMIT_ID`，并为 `multimodal2video` 自动补全可用资源 ID |
| `batch:all` | 已通过 | 2026-04-05 已跑通，最新结果见 `logs/smoke/20260405-192908/results.md`；当前内置 CLI + curl case 已整批验证通过，已包含 `cli:image_upscale_debug`、`cli:multimodal2video` 与 `curl:ref2video` |
| `cli:multimodal2video` 带音频 | 已解决 | 2026-04-05 已通过；当前 smoke 会自动生成 5 秒音频样本并完成完整提交，单项结果见 `logs/smoke/20260405-173556/results.md`，最新批量结果见 `logs/smoke/20260405-192908/results.md` |

---

## 21. 建议的第一批真实验证用例

如果从零开始做实测，建议先跑这一批，用最少样本覆盖最多风险点。

### 21.1 用例 A：登录闭环

目标：

- 验证 `login`
- 验证本地回调写凭证
- 验证 `validate-auth-token`

建议步骤：

```bash
dreamina login
dreamina validate-auth-token
```

通过标准：

- 能拿到本地凭证文件
- `validate-auth-token` 能输出可解析的 session 视图

### 21.2 用例 B：账号额度最短链路

目标：

- 验证登录态可被 commerce 接口接受
- 验证 `user_credit`

建议步骤：

```bash
dreamina user_credit
```

通过标准：

- 非 4xx / 5xx
- 输出结构包含 `vip_credit/gift_credit/purchase_credit/total_credit`

### 21.3 用例 C：无上传主链路

目标：

- 验证 `text2video`
- 验证 `query_result`

建议步骤：

```bash
dreamina text2video --prompt "未来城市清晨航拍" --poll 10
dreamina query_result --submit_id "__SUBMIT_ID__"
```

通过标准：

- 能拿到 `submit_id`
- 查询命令可以查到同一个任务
- 成功态下能拿到媒体 URL

### 21.4 用例 D：图片上传链路

目标：

- 验证 `get_upload_token(image)`
- 验证图片上传
- 验证 `resource_store`
- 验证 `image2image`

建议步骤：

```bash
dreamina image2image --image ./input.png --prompt "改成夜景霓虹风格" --poll 10
```

现成证据：

- 现在也有标准 smoke case：`bash scripts/dreamina_smoke.sh cli:image_upscale_debug`
- 最新标准结果见 `logs/smoke/20260405-192154/results.md`
- 其 stderr 中已连续出现 `ResourceUpload.getUploadToken`、`ImageXUpload.apply`、`ImageXUpload.put`、`ImageXUpload.commit`、`ResourceUpload.resourceStore`

通过标准：

- 本地图片能成功换成远端 `resource_id`
- 最终生成请求里字段名为 `resource_id_list`

### 21.5 用例 E：视频上传链路

目标：

- 验证视频资源上传
- 验证 `multimodal2video`

建议步骤：

```bash
dreamina multimodal2video \
  --image ./cover.png \
  --video ./bg.mp4 \
  --audio ./music.mp3 \
  --prompt "生成统一短片" \
  --poll 10
```

通过标准：

- 三类资源都能拿到有效 `resource_id`
- 最终提交进入 `multiModal2VideoByConfig`

---

## 22. 后续维护约定

为了让这份文档一直可用，建议后续更新时遵守下面三条：

1. 如果改了接口路径、公共 query、payload 关键字段，先改文档再改代码或同步改。
2. 如果新增一级命令，必须把“命令矩阵、curl 示例、状态判定、实测台账”四处都补齐。
3. 每完成一次真实环境验证，就把结果填进 `20. 实测台账`，不要只留在聊天记录里。

---

## 23. Smoke 脚本

为了把文档里的关键命令固化下来，仓库里已经补了一个脚本：

```text
scripts/dreamina_smoke.sh
```

用途：

1. 打印推荐验证顺序。
2. 直接执行少量 CLI smoke 命令。
3. 直接重放关键 `curl` 模板。

### 23.1 先看计划

```bash
bash scripts/dreamina_smoke.sh plan
```

### 23.2 直接跑 CLI smoke

```bash
bash scripts/dreamina_smoke.sh cli:user_credit
bash scripts/dreamina_smoke.sh cli:text2video
bash scripts/dreamina_smoke.sh cli:image_upscale_debug
bash scripts/dreamina_smoke.sh batch:core
```

### 23.3 直接重放 curl 模板

```bash
export BASE_URL="https://jimeng.jianying.com"
export CLI_VERSION="__YOUR_CLI_VERSION__"
export COOKIE="__YOUR_COOKIE__"
export LOGID="__YOUR_LOGID__"
export SUBMIT_ID="__YOUR_SUBMIT_ID__"
export RESOURCE_ID="__YOUR_RESOURCE_ID__"
export VIDEO_RESOURCE_ID="__YOUR_VIDEO_RESOURCE_ID__"
export AUDIO_RESOURCE_ID="__YOUR_AUDIO_RESOURCE_ID__"
export MULTIMODAL_SUBMIT_ID="__YOUR_MULTIMODAL_SUBMIT_ID__"
export FIRST_RESOURCE_ID="__YOUR_FIRST_RESOURCE_ID__"
export LAST_RESOURCE_ID="__YOUR_LAST_RESOURCE_ID__"

bash scripts/dreamina_smoke.sh curl:user_credit
bash scripts/dreamina_smoke.sh curl:text2image
bash scripts/dreamina_smoke.sh curl:text2video
bash scripts/dreamina_smoke.sh curl:query_result
bash scripts/dreamina_smoke.sh curl:image2image
bash scripts/dreamina_smoke.sh curl:image_upscale
bash scripts/dreamina_smoke.sh curl:image2video
bash scripts/dreamina_smoke.sh curl:frames2video
bash scripts/dreamina_smoke.sh curl:multiframe2video
bash scripts/dreamina_smoke.sh curl:ref2video
bash scripts/dreamina_smoke.sh curl:multimodal2video
bash scripts/dreamina_smoke.sh batch:curl-core
```

### 23.4 批量模式

脚本现在支持多种批量入口：

```bash
bash scripts/dreamina_smoke.sh batch:core
bash scripts/dreamina_smoke.sh batch:image-core
bash scripts/dreamina_smoke.sh batch:image-upload-core
bash scripts/dreamina_smoke.sh batch:video-upload-core
bash scripts/dreamina_smoke.sh batch:image-result-core
bash scripts/dreamina_smoke.sh batch:curl-core
bash scripts/dreamina_smoke.sh batch:all
```

说明：

- `batch:core`
  目前会顺序执行 `cli:user_credit`、`cli:text2video`
- `batch:image-core`
  目前会顺序执行 `cli:image_upscale`、`cli:image2image`
- `batch:image-upload-core`
  目前只执行 `cli:image_upscale_debug`，用于固定采集图片上传链路中的 `get_upload_token -> ImageX apply -> PUT -> commit -> resource_store` 明细日志
- `batch:video-upload-core`
  目前会执行 `cli:multimodal2video`，默认自动生成 5 秒本地参考视频和 5 秒本地音频，并验证图片+视频+音频上传链路
- `batch:image-result-core`
  目前会顺序执行 `cli:query_image_upscale`、`cli:download_image_upscale`、`cli:query_image2image`
- `batch:curl-core`
  目前会顺序执行 `curl:user_credit`、`curl:text2image`、`curl:text2video`、`curl:query_result`、`curl:multimodal2video`
- `batch:all`
  会把当前脚本内所有已内置 case 顺序跑一遍

补充：

- `curl:image2image`、`curl:image_upscale` 现在会自动从最近一次对应 CLI 图片任务的 `aigc_task.result_json` 中提取 `uploaded_images[].resource_id`
- 因此不需要手工再传 `RESOURCE_ID`
- `curl:image2video` 会优先复用最近一次 `cli:image2image` 的 `resource_id`，其次回退到 `cli:image_upscale`
- `curl:frames2video` 会默认用最近一次 `cli:image2image` 作为首帧、最近一次 `cli:image_upscale` 作为尾帧
- `curl:multiframe2video` 会默认复用上述两类图片资源拼出一个三帧列表
- `curl:ref2video` 直接复用 `curl:multiframe2video` 的同一请求链路
- `curl:multimodal2video` 会默认复用最近图片资源；如果最近一次 `cli:multimodal2video` 的 `tasks.db.result_json` 已包含 `request.*_resource_id_list` 或 `uploaded_*`，脚本会自动补全 `video_resource_id_list` 和 `audio_resource_id_list`，必要时再回退到 `history/query`
- 如果你想手工指定 multimodal 素材标识，可以直接传 `MULTIMODAL_SUBMIT_ID`、`VIDEO_RESOURCE_ID`、`AUDIO_RESOURCE_ID`

批量模式的行为：

- 不会在某一条 case 失败后立即中断
- 会继续把剩余 case 全部执行完
- 最终以“是否存在失败 case”决定批量返回码

### 23.5 结果落盘位置

除 `plan` 外，脚本每次执行都会新建一个运行目录：

```text
logs/smoke/<timestamp>/
```

目录内会生成：

- `results.md`
  汇总本次每个 case 的状态、返回码、stdout/stderr 文件名，以及输出预览。
- `*.stdout.log`
  对应 case 的标准输出。
- `*.stderr.log`
  对应 case 的标准错误。

执行结束后，脚本会直接把 `results_md/stdout_log/stderr_log` 路径打印到终端。

### 23.6 当前脚本覆盖范围

已内置：

- `cli:user_credit`
- `cli:text2video`
- `curl:user_credit`
- `curl:query_result`
- `curl:text2image`
- `curl:text2video`
- `curl:image2image`
- `curl:image_upscale`
- `curl:image2video`
- `curl:frames2video`
- `curl:multiframe2video`
- `curl:multimodal2video`

说明：

- 登录流程没有被脚本自动化，因为它本质上是浏览器授权 + 本地回调，不是单条稳定 API。
- 带本地文件的真实上传链路仍建议优先走 CLI 命令，而不是只靠 `curl` 模板。
