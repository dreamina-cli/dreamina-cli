# Dreamina CLI 项目说明

这是 `dreamina` 命令行工具的可编译源码整理版本，项目路径保持为：

```text
/opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli
```

这样可以兼容源码里已有的路径语义和还原文档中的引用关系。

## 当前工程结构

- `src/`：Go 模块源码，模块名为 `code.byted.org/videocut-aigc/dreamina_cli`
- `docs/`：项目分析、流程、接口和恢复说明文档
- `scripts/`：辅助脚本，包括 smoke 与跨平台构建脚本
- `testdata/smoke/`：保留的稳定 smoke 示例资源

## 主要命令

账号与状态：

- `login`
- `relogin`
- `import_login_response`
- `validate-auth-token`
- `user_credit`
- `logout`
- `version`

任务与结果：

- `query_result`
- `list_task`

生成类：

- `text2image`
- `image2image`
- `image_upscale`
- `text2video`
- `image2video`
- `frames2video`
- `multiframe2video`
- `ref2video`
- `multimodal2video`

## 构建与测试

进入模块目录后执行：

```bash
cd /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/src
go test ./...
```

跨平台构建：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/build_release.sh
```

Windows PowerShell：

```powershell
pwsh /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/build_release.ps1
```

默认产物输出到项目目录下的 `dist/`。

输出结构：

- `dist/<goos>-<goarch>/`：裸二进制
- `dist/packages/`：可分发归档包
- `dist/packages/SHA256SUMS.txt`：归档包校验和

默认发布目标：

- `darwin/arm64`
- `darwin/amd64`
- `linux/amd64`
- `linux/arm64`
- `windows/amd64`
- `windows/arm64`

归档规则：

- Unix 目标：`dreamina-<version>-<goos>-<goarch>.tar.gz`
- Windows 目标：`dreamina-<version>-<goos>-<goarch>.zip`

## 文档入口

文档统一放在 `docs/`，建议先看：

- `docs/README.md`
- `docs/命令速查与Curl速览.md`
- `docs/GENERATOR_COMMANDS.md`
- `docs/TASK_STORE.md`
- `docs/旧新二进制对比脚本.md`

旧/新二进制 `query_result` 对比：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/compare_legacy_query_result.sh \
  --submit_id b8cd2dbbcd84b21f
```

批量样本对比：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/compare_legacy_query_result.sh \
  --submit_id_file /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/testdata/legacy/query_result_submit_ids.txt
```

批量样本对比并写出 JSON：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/compare_legacy_query_result.sh \
  --submit_id_file /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/testdata/legacy/query_result_submit_ids.txt \
  --json-output /tmp/query_result_compare.json
```

发布前一键回归：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/preflight_release.sh
```

发布前回归并导出机器可读结果：

```bash
bash /opt/tiger/src/code.byted.org/videocut-aigc/dreamina_cli/scripts/preflight_release.sh \
  --skip-build \
  --json-output /tmp/dreamina_preflight.json
```

## 说明

- 项目中的接口、流程和数据结构来自已有源码与恢复整理结果
- 文档以“当前仓库状态”为准，不再沿用旧的恢复阶段目录摆放
- 运行期数据默认落在 `~/.dreamina_cli/`
