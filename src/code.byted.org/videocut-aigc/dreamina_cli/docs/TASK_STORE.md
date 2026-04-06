# 本地任务库说明

本项目的任务记录默认保存在：

- `~/.dreamina_cli/tasks.db`

底层使用 SQLite。

## 核心对象

主要类型位于 `src/components/task/store.go`：

- `Store`
- `AIGCTask`
- `TaskRequestPayload`
- `UpdateTaskInput`
- `ListTaskFilter`

## 任务主要字段

- `SubmitID`
- `UID`
- `GenTaskType`
- `GenStatus`
- `FailReason`
- `ResultJSON`
- `CreateTime`
- `UpdateTime`
- `LogID`
- `CommerceInfo`

## 主要行为

### 创建任务

- `CreateTask`
- 保持 `submit_id` 唯一
- 不会把重复创建偷偷变成更新

### 更新任务

- `UpdateTask`
- 只覆盖显式传入的字段
- 避免空字符串误清理已有状态

### 查询任务

- `GetTask`：按 `submit_id` 查询单条任务
- `ListTasks`：按 `uid`、状态、任务类型分页筛选

## 迁移与兼容

`NewStore` 会优先对齐到 SQLite 任务库，并尝试兼容旧 JSON 存储或坏库文件恢复场景。

这意味着当前仓库已经把任务存储语义收拢到 SQLite，而不是继续沿用恢复阶段的 JSON 文件。
