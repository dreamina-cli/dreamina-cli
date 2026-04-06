# 符号索引

本文给出当前工程里最值得优先关注的包和函数入口。

## 启动入口

- `main.main`
- `cmd.ExecuteArgs`
- `cmd.NewRootCommand`

## 命令入口

- `newLoginCommand`
- `newReloginCommand`
- `newImportLoginResponseCommand`
- `newQueryResultCommand`
- `newListTaskCommand`
- `newUserCreditCommand`
- `newVersionCommand`
- `newText2ImageCommand`
- `newImage2ImageCommand`
- `newImageUpscaleCommand`
- `newText2VideoCommand`
- `newImage2VideoCommand`
- `newFrames2VideoCommand`
- `newMultiFrame2VideoCommandWithUse`
- `newMultiModal2VideoCommand`

## 关键服务

- `app.NewContext`
- `gen.Service.SubmitTask`
- `gen.Service.QueryResult`
- `login.Manager.AuthorizationURL`
- `login.Manager.ImportLoginResponseJSON`
- `task.NewStore`
- `task.Store.CreateTask`
- `task.Store.UpdateTask`
- `task.Store.GetTask`
- `task.Store.ListTasks`

## 值得关注的辅助逻辑

- `downloadQueryResultMedia`
- `parseRemoteQueryResult`
- `printGeneratorQueryResultOutput`
- `FormatSessionPayload`
