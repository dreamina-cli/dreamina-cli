# 仓库协作约定

## 根目录与路径

- Git 根目录固定为 `/opt/tiger`
- 项目根目录固定为 `/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli`
- 不要为了“看起来更简洁”移动或压平 `src/code.byted.org/videocut-aigc/dreamina_cli` 这层路径

## 文档约定

- 项目级 Markdown 统一使用中文
- 项目说明放在项目根 `README.md`
- 其他分析、流程、接口、恢复说明统一放在 `docs/`

## 构建与验证

- Go 模块目录是 `.../dreamina_cli/src`
- 默认测试命令：
  - `cd /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src && go test ./...`
- 默认发布脚本：
  - `bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/build_release.sh`
  - `pwsh /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/build_release.ps1`

## 文件清理规则

- `dist/`、`bin/`、`logs/`、`tmp_smoke_*` 属于构建或运行产物，不提交
- `testdata/smoke/` 仅保留必要的稳定示例资源
- 不要把下载结果、临时日志、编译缓存重新加入仓库
