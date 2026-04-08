# /Users/awei/.local/bin/dreamina 的 updater 实现分析

本文基于二进制 `/Users/awei/.local/bin/dreamina` 的反汇编与字符串分析，整理 `components/updater` 的真实行为与关键常量。所有结论均为静态分析所得，未进行动态网络探测。

## 关键结论

- updater 在 `main.main` 中被固定触发：启动时先 `CheckUpdateAsync()`，命令执行完成后再 `PrintUpdateResult()`，且 `PrintUpdateResult()` 为非阻塞读取。
- 远端版本拉取使用 `version.json`，本地缓存路径高置信度为 `~/.dreamina_cli/version.json`（由 `os.UserHomeDir` + `filepath.Join` 组装）。
- 版本缓存节流窗口为 **1 小时**（`time.Since(modtime) < 3600000000000ns`）。
- 远端探测带 30 秒超时（`context.WithTimeout(..., 30s)`）。
- 只有当 CDN `GetCurrentDir()` 的结果中包含 `version.json` 时才继续远端请求。
- 更新提示文案已内置：`[Update Available] A new version %s is available (current: %s).`
- 若无更新，会输出 `local [%v] is the latest version, pass`。

## 触发链路（主流程）

从 `main.main` 可见调用顺序：

1. 启动即调用 `components/updater.CheckUpdateAsync()`，以 goroutine 异步执行。
2. CLI 主命令执行完成后，调用 `components/updater.PrintUpdateResult()`。
3. `PrintUpdateResult()` 使用 `selectnbrecv` 非阻塞读取 `UpdateResultChan`，没有结果就直接返回。

结论：update 逻辑不会阻塞主命令，仅在主命令结束后尝试打印结果。

## UpdateResultChan 初始化

`components/updater.init` 中调用 `runtime.makechan` 初始化 `UpdateResultChan`，容量为 1。

## CheckUpdateAsync 细节

### 超时

`context.WithTimeout(ctx, 30s)`，常量值为 `30000000000ns`。

### 本地版本与节流

`getLocalVersion()` 会从本地缓存读取版本信息；如果本地 `version.json` 不存在，会尝试远端拉取后写入本地再读。

`CheckUpdateAsync` 随后调用 `os.Stat(localVersionFile)` 并检查 `time.Since(modtime)`。

- 若 `Since < 1h` 则直接退出，不做远端请求。
- 该 1 小时阈值来源于 `3600000000000ns` 常量。

### CDN 探测门禁

调用 `cdn-uploadx-go-sdk.(*DeliverClient).GetCurrentDir`，并对返回的目录字符串做 `bytes.Index` 检查。

门禁串被解出为 `version.json`（字节序列 `version.` + `json`），即：

- `bytes.Index(currentDir, []byte("version.json")) != -1` 才继续远端请求

## 远端版本获取

### URL 构造（已确认）

`downloadJSONFromCDN` 使用 `fmt.Sprintf`，格式串前缀已确认是 `%s/%s`。同时已从 `__DATA_CONST.__rodata` 的字符串头解析出 `<base>` 指针，确定 `base` 为固定常量：

- `https://lf3-static.bytednsdoc.com/obj/eden-cn/psj_hupthlyk/ljhwZthlaukjlkulzlp`

因此 URL 拼接可以确认为：

```go
fmt.Sprintf("%s/%s", "https://lf3-static.bytednsdoc.com/obj/eden-cn/psj_hupthlyk/ljhwZthlaukjlkulzlp", "version.json")
```

对应的完整地址为：

```text
https://lf3-static.bytednsdoc.com/obj/eden-cn/psj_hupthlyk/ljhwZthlaukjlkulzlp/version.json
```

### HTTP 请求行为

`downloadJSONFromCDN` 执行流程：

1. `http.NewRequestWithContext(ctx, "GET", url, nil)`（`GET` 字符串在 rodata 中已确认）
2. 使用默认 `http.Client` 执行
3. 若 `StatusCode != 200` 报错
4. `io.ReadAll(resp.Body)` 读返回体

### JSON 解析

`fetchLatestVersionFromCDN` 中将响应体 `json.Unmarshal` 到 `VersionInfo`。
解析失败时返回错误：

`parse remote version.json failed: %w`

此外，在 `downloadJSONFromCDN` 中可见错误格式串：

`unexpected status code: %d`

## 本地缓存路径与格式

`getLocalVersionFilePath()`：

- 调用 `os.UserHomeDir()`，失败则直接返回文件名 `version.json`
- 成功则 `filepath.Join(home, <13 字节目录名>, "version.json")`

结合路径长度与惯例，高置信度推断该目录名为 `.dreamina_cli`。因此本地缓存文件为：

`~/.dreamina_cli/version.json`

## PrintUpdateResult 输出行为

`PrintUpdateResult()`：

- 非阻塞读取 `UpdateResultChan`，无结果直接返回
- 若 `UpdateResult.Error != nil` 会输出错误
- 若 `HasUpdate == true` 输出：
  - `[Update Available] A new version %s is available (current: %s).`
- 若 `HasUpdate == false` 输出：
  - `local [%v] is the latest version, pass`
- 最后调用 `os.Chtimes(localVersionFile, now, now)` 更新本地 `version.json` 的 mtime，作为 1 小时节流窗口的基准

## UpdateResult 与 VersionInfo 结构字段（反汇编推断）

从 `type:.eq` 自动生成的比较函数可推断字段布局：

### VersionInfo

`type:.eq...VersionInfo` 依次比较 3 组 `string`（每组 16 字节），因此：

- `VersionInfo` 包含 **3 个 string 字段**（顺序未知）

### UpdateResult

`type:.eq...UpdateResult` 的比较顺序为：

1. 起始 1 字节 `bool`（`HasUpdate`）
2. 紧跟一个 `VersionInfo`（3 个 string 字段）
3. 再比较 2 个 `string` 字段

结合 `PrintUpdateResult` 对 `offset=80` 的 `len` 判断，可知最后一个 `string` 用作“可选输出信息”（可能是描述、提示或错误文本）。

因此 `UpdateResult` 高置信度结构为：

- `HasUpdate`（bool）
- `RemoteVersion`（VersionInfo，含 3 个 string 字段）
- `CurrentVersion`（string）
- `Message`（string，非空则输出）

字段命名以源码为准，此处仅用于解释打印逻辑。

## VersionInfo 字段语义（结合远端版本文件）

从远端 `version.json` 的实际内容可确认 `VersionInfo` 的 3 个字符串字段语义：

```json
{
    "version": "1.3.3",
    "release_date": "2026-04-07",
    "release_notes": "修复了超清图片任务一直处于排队的问题"
}
```

结合 `VersionInfo` 的 3 个 `string` 字段数量，可判断其字段对应：

- `version`
- `release_date`
- `release_notes`

## 备注：与旧结论的修正

先前版本分析曾误写节流窗口为 24 小时。此次重新校验后确认：

`time.Since(modtime) < 1 小时` 才会跳过远端探测。
