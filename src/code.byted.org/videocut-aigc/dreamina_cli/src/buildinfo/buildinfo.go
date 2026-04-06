package buildinfo

// 这些变量对应二进制里嵌入的构建信息，`version` 命令会直接读取这里的值输出。

var (
	// Version 是包含工作区脏标记的构建版本字符串。
	Version = "4946b9d-dirty"
	// Commit 是构建时写入的提交哈希。
	Commit = "4946b9d"
	// BuildTime 是构建产物生成时间，使用 UTC RFC3339 形态。
	BuildTime = "2026-03-31T07:24:44Z"
)
