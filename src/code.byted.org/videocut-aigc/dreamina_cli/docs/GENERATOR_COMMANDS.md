# 生成类命令清单

本文整理当前仓库支持的生成类命令、关键输入和适用场景。

## 图片生成

### `text2image`

- 任务类型：`text2image`
- 主要输入：`prompt`、`ratio`、`resolution_type`、`model_version`
- 用途：纯文本生成图片

### `image2image`

- 任务类型：`image2image`
- 主要输入：`image_paths`、`prompt`、`ratio`、`resolution_type`、`model_version`
- 用途：上传参考图后做编辑或重绘

### `image_upscale`

- 任务类型：`image_upscale`
- 主要输入：`image_path`、`resolution_type`
- 用途：单图超分

## 视频生成

### `text2video`

- 任务类型：`text2video`
- 主要输入：`prompt`、`duration`、`ratio`、`video_resolution`、`model_version`

### `image2video`

- 任务类型：`image2video`
- 主要输入：`image_path`、`prompt`、`duration`、`video_resolution`、`model_version`
- 特点：只要显式传入高级参数，就会走带配置的提交分支

### `frames2video`

- 任务类型：`frames2video`
- 主要输入：`first_path`、`last_path`、`prompt`、`duration`

### `multiframe2video`

- 任务类型：`multiframe2video`
- 主要输入：`image_paths`、`transition_prompts`、`transition_durations`
- 约束：至少 2 张，最多 20 张图片

### `ref2video`

- 本质上是 `multiframe2video` 的兼容别名
- 输入约束与行为一致

### `multimodal2video`

- 任务类型：`multimodal2video`
- 主要输入：`image_paths`、`video_paths`、`audio_paths`、`prompt`
- 约束：不能只有音频，至少要有图片或视频输入

## 共同特征

- 所有生成命令最终都汇总到 `runGeneratorSubmit`
- 都会尝试从登录态里提取用户信息
- 结果统一输出 JSON
- `--poll` 大于 0 时会进入轮询查询逻辑

## 推荐阅读

- `GEN_FLOW.md`
- `命令请求流程与Curl示例.md`
