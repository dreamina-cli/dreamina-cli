package task

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// sqliteStoreMarker 仅用于标记当前 Store 已切到 SQLite 主路径。
type sqliteStoreMarker struct{}

const sqliteTaskSelectColumns = "submit_id, gen_task_type, CAST(uid AS TEXT) AS uid, create_time, update_time, logid, request, gen_status, fail_reason, result_json, commerce_info"

// ensureSQLiteTaskStore 初始化 tasks.db 以及 aigc_task schema。
func ensureSQLiteTaskStore(path string) error {
	schema := `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS aigc_task (
	submit_id TEXT PRIMARY KEY,
	gen_task_type TEXT NOT NULL,
	uid INTEGER NOT NULL,
	create_time INTEGER NOT NULL,
	update_time INTEGER NOT NULL,
	logid TEXT NOT NULL DEFAULT "",
	request TEXT NOT NULL,
	gen_status TEXT NOT NULL,
	fail_reason TEXT NOT NULL DEFAULT "",
	result_json TEXT NOT NULL DEFAULT "",
	commerce_info TEXT NOT NULL DEFAULT ""
);
CREATE INDEX IF NOT EXISTS idx_aigc_task_gen_status ON aigc_task(gen_status);
CREATE INDEX IF NOT EXISTS idx_aigc_task_update_time ON aigc_task(update_time);
CREATE INDEX IF NOT EXISTS idx_aigc_task_gen_task_type ON aigc_task(gen_task_type);
`
	return sqliteExec(path, schema)
}

// collectLegacyMigrationRecords 收集 legacy JSON 任务库里的任务，供启动时导入 SQLite。
func collectLegacyMigrationRecords(paths ...string) ([]taskRecord, error) {
	seen := make(map[string]struct{})
	out := make([]taskRecord, 0)
	for _, path := range paths {
		records, err := readLegacyTaskRecordsFromPath(path)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			record = normalizeTaskRecordForSQLite(record)
			if strings.TrimSpace(record.SubmitID) == "" {
				continue
			}
			if _, ok := seen[record.SubmitID]; ok {
				continue
			}
			seen[record.SubmitID] = struct{}{}
			out = append(out, record)
		}
	}
	return out, nil
}

// readLegacyTaskRecordsFromPath 从 JSON 兼容路径读取任务记录。
func readLegacyTaskRecordsFromPath(path string) ([]taskRecord, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 || !looksLikeJSONTaskStore(body) {
		return nil, nil
	}
	return parseLegacyTaskRecords(body)
}

// migrateTaskRecordsToSQLite 把 legacy JSON 任务合并写入 SQLite，重复 submit_id 会跳过。
func migrateTaskRecordsToSQLite(path string, records []taskRecord) error {
	if len(records) == 0 {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("BEGIN IMMEDIATE;\n")
	for _, record := range records {
		builder.WriteString(taskRecordInsertSQL(record, true))
		builder.WriteByte('\n')
	}
	builder.WriteString("COMMIT;\n")
	return sqliteExec(path, builder.String())
}

// createTaskSQLite 在 SQLite 中创建一条新任务。
func createTaskSQLite(path string, record *taskRecord) error {
	if record == nil {
		return fmt.Errorf("task is required")
	}
	if err := sqliteExec(path, taskRecordInsertSQL(normalizeTaskRecordForSQLite(*record), false)); err != nil {
		if isSQLiteUniqueError(err) {
			return fmt.Errorf("task already exists: %s", record.SubmitID)
		}
		return err
	}
	return nil
}

// updateTaskSQLite 更新 SQLite 中已存在的任务。
func updateTaskSQLite(path string, in UpdateTaskInput) error {
	record, err := getTaskRecordSQLite(path, strings.TrimSpace(in.SubmitID))
	if err != nil {
		return err
	}
	if in.Request != nil {
		body, err := json.Marshal(in.Request)
		if err != nil {
			return err
		}
		record.Request = string(body)
	}
	if strings.TrimSpace(in.GenStatus) != "" {
		record.GenStatus = in.GenStatus
	}
	if in.FailReason != nil {
		record.FailReason = *in.FailReason
	}
	if in.ResultJSON != nil {
		record.ResultJSON = *in.ResultJSON
	}
	if in.UpdateTime > 0 {
		record.UpdateTime = in.UpdateTime
	}
	if in.LogID != nil {
		record.LogID = *in.LogID
	}
	if in.CommerceInfo != nil {
		body, err := json.Marshal(in.CommerceInfo)
		if err != nil {
			return err
		}
		record.CommerceInfo = string(body)
	}
	normalized := normalizeTaskRecordForSQLite(*record)
	return sqliteExec(path, taskRecordUpdateSQL(normalized, normalized.SubmitID))
}

// renameTaskSubmitIDSQLite 把任务主键从旧 submit_id 改成新 submit_id。
func renameTaskSubmitIDSQLite(path string, oldSubmitID string, newSubmitID string) error {
	if _, err := getTaskRecordSQLite(path, newSubmitID); err == nil {
		return fmt.Errorf("task already exists: %s", newSubmitID)
	} else if !isTaskNotFoundError(err) {
		return err
	}
	if _, err := getTaskRecordSQLite(path, oldSubmitID); err != nil {
		return err
	}
	return sqliteExec(path, fmt.Sprintf(
		"UPDATE aigc_task SET submit_id = %s WHERE submit_id = %s;",
		sqliteQuote(newSubmitID),
		sqliteQuote(oldSubmitID),
	))
}

// getTaskRecordSQLite 读取单条任务记录。
func getTaskRecordSQLite(path string, submitID string) (*taskRecord, error) {
	rows, err := sqliteJSONQuery(path, fmt.Sprintf(
		"SELECT %s FROM aigc_task WHERE submit_id = %s LIMIT 1;",
		sqliteTaskSelectColumns,
		sqliteQuote(submitID),
	))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("task not found: %s", strings.TrimSpace(submitID))
	}
	record, err := taskRecordFromSQLiteMap(rows[0])
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// listTaskRecordsSQLite 按筛选和排序语义列出任务记录。
func listTaskRecordsSQLite(path string, filter ListTaskFilter) ([]taskRecord, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	where := make([]string, 0, 4)
	if uid := strings.TrimSpace(filter.UID); uid != "" {
		where = append(where, "CAST(uid AS TEXT) = "+sqliteQuote(uid))
	}
	if submitID := strings.TrimSpace(filter.SubmitID); submitID != "" {
		where = append(where, "submit_id = "+sqliteQuote(submitID))
	}
	if genTaskType := strings.TrimSpace(filter.GenTaskType); genTaskType != "" {
		where = append(where, "gen_task_type = "+sqliteQuote(genTaskType))
	}
	if genStatus := strings.TrimSpace(filter.GenStatus); genStatus != "" {
		where = append(where, "gen_status = "+sqliteQuote(genStatus))
	}
	query := fmt.Sprintf("SELECT %s FROM aigc_task", sqliteTaskSelectColumns)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY CASE WHEN update_time > 0 THEN update_time ELSE create_time END DESC, rowid ASC LIMIT %d OFFSET %d;", limit, offset)
	rows, err := sqliteJSONQuery(path, query)
	if err != nil {
		return nil, err
	}
	out := make([]taskRecord, 0, len(rows))
	for _, row := range rows {
		record, err := taskRecordFromSQLiteMap(row)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

// taskRecordFromSQLiteMap 把 sqlite3 -json 的结果行转回 taskRecord。
func taskRecordFromSQLiteMap(root map[string]any) (taskRecord, error) {
	record := taskRecord{
		SubmitID:    firstTaskString(root, "submit_id", "submitId", "SubmitID"),
		UID:         firstTaskString(root, "uid", "UID"),
		GenTaskType: firstTaskString(root, "gen_task_type", "genTaskType", "GenTaskType"),
		GenStatus:   firstTaskString(root, "gen_status", "genStatus", "GenStatus"),
		FailReason:  firstTaskString(root, "fail_reason", "failReason", "FailReason"),
		LogID:       firstTaskString(root, "logid", "log_id", "logId", "LogID"),
		CreateTime:  firstTaskInt64(root, "create_time", "createTime", "CreateTime"),
		UpdateTime:  firstTaskInt64(root, "update_time", "updateTime", "UpdateTime"),
	}
	var err error
	record.Request, err = normalizeTaskJSONField(root, "request", "Request")
	if err != nil {
		return taskRecord{}, err
	}
	record.ResultJSON, err = normalizeTaskJSONField(root, "result_json", "resultJson", "ResultJSON")
	if err != nil {
		return taskRecord{}, err
	}
	record.CommerceInfo, err = normalizeTaskJSONField(root, "commerce_info", "commerceInfo", "CommerceInfo")
	if err != nil {
		return taskRecord{}, err
	}
	return normalizeTaskRecordForSQLite(record), nil
}

// normalizeTaskRecordForSQLite 补齐 SQLite NOT NULL 字段的默认值。
func normalizeTaskRecordForSQLite(record taskRecord) taskRecord {
	if strings.TrimSpace(record.Request) == "" {
		record.Request = "null"
	}
	if strings.TrimSpace(record.FailReason) == "" {
		record.FailReason = ""
	}
	if strings.TrimSpace(record.ResultJSON) == "" {
		record.ResultJSON = ""
	}
	if strings.TrimSpace(record.LogID) == "" {
		record.LogID = ""
	}
	if strings.TrimSpace(record.CommerceInfo) == "" {
		record.CommerceInfo = ""
	}
	if strings.TrimSpace(record.GenStatus) == "" {
		record.GenStatus = "querying"
	}
	return record
}

// taskRecordInsertSQL 生成插入语句；ignoreDuplicate 为 true 时使用 INSERT OR IGNORE。
func taskRecordInsertSQL(record taskRecord, ignoreDuplicate bool) string {
	record = normalizeTaskRecordForSQLite(record)
	verb := "INSERT"
	if ignoreDuplicate {
		verb = "INSERT OR IGNORE"
	}
	return fmt.Sprintf(
		"%s INTO aigc_task (submit_id, gen_task_type, uid, create_time, update_time, logid, request, gen_status, fail_reason, result_json, commerce_info) VALUES (%s, %s, %s, %d, %d, %s, %s, %s, %s, %s, %s);",
		verb,
		sqliteQuote(record.SubmitID),
		sqliteQuote(record.GenTaskType),
		sqliteQuote(record.UID),
		record.CreateTime,
		record.UpdateTime,
		sqliteQuote(record.LogID),
		sqliteQuote(record.Request),
		sqliteQuote(record.GenStatus),
		sqliteQuote(record.FailReason),
		sqliteQuote(record.ResultJSON),
		sqliteQuote(record.CommerceInfo),
	)
}

// taskRecordUpdateSQL 生成覆盖式更新语句。
func taskRecordUpdateSQL(record taskRecord, oldSubmitID string) string {
	record = normalizeTaskRecordForSQLite(record)
	return fmt.Sprintf(
		"UPDATE aigc_task SET submit_id = %s, gen_task_type = %s, uid = %s, create_time = %d, update_time = %d, logid = %s, request = %s, gen_status = %s, fail_reason = %s, result_json = %s, commerce_info = %s WHERE submit_id = %s;",
		sqliteQuote(record.SubmitID),
		sqliteQuote(record.GenTaskType),
		sqliteQuote(record.UID),
		record.CreateTime,
		record.UpdateTime,
		sqliteQuote(record.LogID),
		sqliteQuote(record.Request),
		sqliteQuote(record.GenStatus),
		sqliteQuote(record.FailReason),
		sqliteQuote(record.ResultJSON),
		sqliteQuote(record.CommerceInfo),
		sqliteQuote(oldSubmitID),
	)
}

// sqliteJSONQuery 用 sqlite3 -json 执行查询并解析输出。
func sqliteJSONQuery(path string, query string) ([]map[string]any, error) {
	cmd := exec.Command("sqlite3", "-json", path, query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, normalizeSQLiteError(err, output)
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return []map[string]any{}, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// sqliteExec 执行一段不返回结果集的 SQLite 语句。
func sqliteExec(path string, query string) error {
	cmd := exec.Command("sqlite3", path, query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return normalizeSQLiteError(err, output)
	}
	return nil
}

// normalizeSQLiteError 把 sqlite3 命令错误收敛成稳定的字符串。
func normalizeSQLiteError(err error, output []byte) error {
	text := strings.TrimSpace(string(output))
	if text == "" {
		text = strings.TrimSpace(err.Error())
	}
	return fmt.Errorf("%s", text)
}

// isSQLiteUniqueError 判断 sqlite3 是否返回了主键冲突。
func isSQLiteUniqueError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// isTaskNotFoundError 判断错误是否为 task not found。
func isTaskNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "task not found:")
}

// sqliteQuote 转义 SQLite 文本字面量。
func sqliteQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// sqliteIntLiteral 把动态数值转成整数字面量；当前保留给后续 schema 对齐使用。
func sqliteIntLiteral(value any) string {
	switch typed := value.(type) {
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return "0"
	}
}
