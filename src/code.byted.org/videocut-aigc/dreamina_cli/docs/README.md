# 文档索引

本目录集中放置项目级 Markdown 文档，统一使用中文描述。

## 建议阅读顺序

1. `命令速查与Curl速览.md`
2. `GENERATOR_COMMANDS.md`
3. `GEN_FLOW.md`
4. `LOGIN_FLOW.md`
5. `TASK_STORE.md`
6. `命令请求流程与Curl示例.md`
7. `旧新二进制对比脚本.md`
8. `发布前回归.md`

## 文档清单

- `ACCOUNT_FLOW.md`：账号与凭证相关命令、持久化和状态流转
- `BUILDINFO.md`：版本信息、构建变量与注入方式
- `CALLFLOW.md`：程序启动到命令执行的主调用链
- `GENERATOR_COMMANDS.md`：生成类命令清单、主要参数与适用场景
- `GEN_FLOW.md`：生成任务从参数解析到提交、轮询、落库的流程
- `HANDLER_MAPPING.md`：命令入口、服务对象与源码文件的映射关系
- `LOGIN_FLOW.md`：登录、回调、凭证导入与会话整理流程
- `MCP_API.md`：与 MCP / commerce / resource 相关的接口整理
- `SOURCE_TREE_COVERAGE.md`：源码树覆盖范围与当前还原状态
- `SYMBOL_INDEX.md`：主要包、类型、函数的索引
- `TASK_STORE.md`：本地任务库存储结构和查询语义
- `命令速查与Curl速览.md`：适合日常使用的精简命令入口与 curl 速览
- `命令请求流程与Curl示例.md`：完整请求链路、完整 curl 对齐示例与更细的验证说明
- `还原计划.md`：本仓库从恢复态到工程化整理态的处理说明
- `旧新二进制对比脚本.md`：旧版与当前构建产物的 `query_result` 对比脚本说明，含 JSON 输出
- `发布前回归.md`：发布前一键构建与批量回归检查入口，含 JSON 汇总与失败判定
