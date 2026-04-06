# dreaminacli 仓库说明

本仓库的 Git 根目录固定为 `/opt/tiger`，这样可以保留原始工程引用中的路径层级：

```text
/opt/tiger
└── src/code.byted.org/videocut-aigc/dreamina_cli
```

## 目录约定

- 项目目录：`src/code.byted.org/videocut-aigc/dreamina_cli`
- Go 模块目录：`src/code.byted.org/videocut-aigc/dreamina_cli/src`
- 项目文档目录：`src/code.byted.org/videocut-aigc/dreamina_cli/docs`
- 构建脚本目录：`src/code.byted.org/videocut-aigc/dreamina_cli/scripts`
- 示例资源目录：`src/code.byted.org/videocut-aigc/dreamina_cli/testdata/smoke`

## 快速开始

构建：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/build_release.sh
```

测试：

```bash
cd /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src
go test ./...
```

更具体的项目说明见：

- `/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/README.md`
