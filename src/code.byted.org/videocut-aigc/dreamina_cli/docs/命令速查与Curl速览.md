# Dreamina CLI 命令速查与 Curl 速览

本文是面向日常使用的精简版入口，目标是让你快速回答三件事：

1. 这个命令是否会发远端请求。
2. 这个命令主要依赖什么输入。
3. 如果要模拟 HTTP，大致该怎么写 `curl`。

需要完整请求链路、完整字段解释、历史验证记录时，再看：

- `命令请求流程与Curl示例.md`

## 1. 公共约定

基础环境变量：

```bash
export BASE_URL="https://jimeng.jianying.com"
export CLI_VERSION="__YOUR_CLI_VERSION__"
export COOKIE="__YOUR_COOKIE__"
export LOGID="__YOUR_LOGID__"
export SUBMIT_ID="__YOUR_SUBMIT_ID__"
export RESOURCE_ID="__YOUR_RESOURCE_ID__"
export VIDEO_RESOURCE_ID="__YOUR_VIDEO_RESOURCE_ID__"
export AUDIO_RESOURCE_ID="__YOUR_AUDIO_RESOURCE_ID__"
```

生成类与查询类接口常见 Query：

```text
aid=513695
from=dreamina_cli
cli_version=${CLI_VERSION}
```

生成类与查询类接口常见 Header：

```http
Accept: application/json
Appid: 513695
Pf: 7
Cookie: ${COOKIE}
Content-Type: application/json
X-Tt-Logid: ${LOGID}
X-Use-Ppe: 1
```

## 2. 命令总览

| 命令 | 远端请求 | 说明 |
| --- | --- | --- |
| `help` | 否 | 输出帮助 |
| `version` | 否 | 输出本地构建信息 |
| `logout` | 否 | 清理本地登录态 |
| `validate-auth-token` | 否 | 本地解析会话 |
| `list_task` | 否 | 查询本地 `tasks.db` |
| `import_login_response` | 否 | 导入本地登录响应 |
| `login` | 是 | 浏览器登录 + 本地回调 |
| `relogin` | 是 | 重置后重新登录 |
| `user_credit` | 是 | 查询权益额度 |
| `query_result` | 可能 | 查询远端任务结果 |
| `text2image` | 是 | 文生图 |
| `image2image` | 是 | 图生图，先上传资源 |
| `image_upscale` | 是 | 图片超分，先上传资源 |
| `text2video` | 是 | 文生视频 |
| `image2video` | 是 | 图生视频，先上传资源 |
| `frames2video` | 是 | 首尾帧视频 |
| `multiframe2video` | 是 | 多图视频 |
| `ref2video` | 是 | `multiframe2video` 兼容别名 |
| `multimodal2video` | 是 | 图/视频/音频混合输入 |

## 3. 本地命令速查

### 登录相关

```bash
dreamina login
dreamina relogin
dreamina import_login_response --file login_response.json
dreamina validate-auth-token
dreamina logout
```

### 本地任务相关

```bash
dreamina list_task --gen_task_type text2video --limit 20
dreamina query_result --submit_id "${SUBMIT_ID}"
dreamina query_result --submit_id "${SUBMIT_ID}" --download_dir ./downloads
```

### 生成类

```bash
dreamina text2image --prompt "霓虹城市夜景" --ratio 16:9
dreamina image2image --image ./testdata/smoke/image-1.png --prompt "改成赛博朋克风格"
dreamina image_upscale --image ./testdata/smoke/image-1.png --resolution_type 4k
dreamina text2video --prompt "海边延时摄影" --duration 5 --model_version seedance2.0_vip
dreamina multimodal2video --image ./testdata/smoke/image-1.png --video ./testdata/smoke/ref5.mp4 --audio ./testdata/smoke/music5.mp3 --prompt "生成统一短片" --model_version seedance2.0fast_vip
```

说明：

- `text2video`、`image2video`、`frames2video`、`multimodal2video` 的 VIP 模型变体仍然走 `/dreamina/cli/v1/video_generate`
- 默认模型没有变，仍然是 `seedance2.0fast`

## 4. Curl 速览

### `user_credit`

```bash
curl -X POST \
  "${BASE_URL}/commerce/v1/benefits/user_credit?aid=513695" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695"
```

说明：

- 真实请求还会带签名头。
- 更完整的 commerce 头部对齐方式见深度版文档。

### `query_result`

```bash
curl -X POST \
  "${BASE_URL}/mweb/v1/get_history_by_ids?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "X-Tt-Logid: ${LOGID}" \
  --data "{\"submit_ids\":[\"${SUBMIT_ID}\"]}"
```

### `text2image`

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "X-Tt-Logid: ${LOGID}" \
  --data '{
    "prompt":"霓虹城市夜景",
    "ratio":"16:9",
    "generate_type":"text2image"
  }'
```

### `image2image`

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "X-Tt-Logid: ${LOGID}" \
  --data "{
    \"prompt\":\"改成赛博朋克风格\",
    \"ratio\":\"16:9\",
    \"resource_id_list\":[\"${RESOURCE_ID}\"],
    \"generate_type\":\"editImageByConfig\"
  }"
```

说明：

- CLI 真实行为是先上传本地图片，再把上传结果 `resource_id` 填进生成请求。

### `text2video`

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "X-Tt-Logid: ${LOGID}" \
  --data '{
    "prompt":"海边延时摄影",
    "duration":5
  }'
```

### `multimodal2video`

```bash
curl -X POST \
  "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${CLI_VERSION}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Cookie: ${COOKIE}" \
  -H "Appid: 513695" \
  -H "Pf: 7" \
  -H "X-Tt-Logid: ${LOGID}" \
  --data "{
    \"prompt\":\"生成统一短片\",
    \"image_resource_id_list\":[\"${RESOURCE_ID}\"],
    \"video_resource_id_list\":[\"${VIDEO_RESOURCE_ID}\"],
    \"audio_resource_id_list\":[\"${AUDIO_RESOURCE_ID}\"]
  }"
```

## 5. 推荐阅读路径

如果你只想跑命令：

1. 先看本文
2. 再看 `GENERATOR_COMMANDS.md`
3. 有问题时查 `命令请求流程与Curl示例.md`

如果你要追源码：

1. 先看 `HANDLER_MAPPING.md`
2. 再看 `GEN_FLOW.md` / `LOGIN_FLOW.md` / `TASK_STORE.md`
