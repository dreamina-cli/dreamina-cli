# 构建信息说明

项目构建信息位于：

- `src/buildinfo/buildinfo.go`

当前核心变量：

- `Version`
- `Commit`
- `BuildTime`

## 这些字段如何使用

`version` 命令会直接读取上述变量，并输出如下 JSON：

```json
{
  "version": "4946b9d-dirty",
  "commit": "4946b9d",
  "build_time": "2026-03-31T07:24:44Z"
}
```

因此，这三个字段既是构建元数据，也是 CLI 对外暴露的版本接口。

## 推荐注入方式

跨平台构建脚本会通过 `go build -ldflags` 注入版本值，对应包路径为：

```text
code.byted.org/videocut-aigc/dreamina_cli/buildinfo
```

注入键：

- `Version`
- `Commit`
- `BuildTime`

## 约束

- 不要把发布版本长期写死在源码中
- 生成发布包时优先使用 Git 提交号和构建时间
- 本地开发构建可以接受 `-dirty` 标记
