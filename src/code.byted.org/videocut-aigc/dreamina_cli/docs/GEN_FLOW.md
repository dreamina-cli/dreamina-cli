# 生成任务流程

本文说明生成类命令从 CLI 输入到任务落库、结果查询的整体路径。

## 总体流程

1. 命令入口解析参数。
2. 组装 `GenerateInput`。
3. 校验输入合法性。
4. 创建应用上下文并要求登录。
5. 必要时上传本地图片、视频、音频，换取远端 `resource_id`。
6. 调用生成服务提交任务。
7. 提交结果写入本地 `tasks.db`。
8. 如果指定了轮询，则继续查询到终态或超时。
9. 输出统一 JSON。

## 核心入口

主要代码位于：

- `src/cmd/generators.go`
- `src/components/gen/gen.go`
- `src/components/client/dreamina/mcp/client.go`
- `src/components/client/dreamina/resource/client.go`

## 输入校验

不同命令在进入提交前会做不同校验，例如：

- `multiframe2video` 必须是 2 到 20 张图片
- `multiframe2video` 的过渡提示数量必须等于图片数减一
- `multimodal2video` 至少需要图片或视频
- `image_upscale` 需要有效图片路径

## 资源上传

只要命令参数里出现本地文件路径，CLI 会先走资源上传，而不是直接把文件塞进生成接口。

上传职责主要由：

- `src/components/client/dreamina/resource/client.go`

完成，返回值里的 `resource_id` 会再传给生成接口。

## 轮询行为

`runGeneratorSubmit` 在 `--poll > 0` 且状态仍为 `querying` 时，会每秒查询一次，直到：

- 任务进入终态
- 达到超时
- 上下文被取消

终态查询结果会切换到统一的 `query_result` 视图输出。
