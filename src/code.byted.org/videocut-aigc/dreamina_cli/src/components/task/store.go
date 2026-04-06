package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

// This file implements the task storage shape inferred from disassembly and
// runtime behavior.

type Store struct {
	db   any
	path string
}

type AIGCTask struct {
	SubmitID     string
	UID          string
	GenTaskType  string
	Request      *TaskRequestPayload
	GenStatus    string
	FailReason   string
	ResultJSON   string
	CreateTime   int64
	UpdateTime   int64
	LogID        string
	CommerceInfo any
	RequestRaw   string
}

type TaskRequestPayload struct {
	Values map[string]any `json:"values,omitempty"`
}
type QueueInfo struct{}

type UpdateTaskInput struct {
	SubmitID     string
	Request      *TaskRequestPayload
	GenStatus    string
	FailReason   *string
	ResultJSON   *string
	UpdateTime   int64
	LogID        *string
	CommerceInfo any
}

type ListTaskFilter struct {
	UID         string
	SubmitID    string
	GenTaskType string
	GenStatus   string
	Offset      int
	Limit       int
}

type taskRecord struct {
	SubmitID     string `json:"submit_id"`
	UID          string `json:"uid"`
	GenTaskType  string `json:"gen_task_type"`
	Request      string `json:"request"`
	GenStatus    string `json:"gen_status"`
	FailReason   string `json:"fail_reason"`
	ResultJSON   string `json:"result_json"`
	CreateTime   int64  `json:"create_time"`
	UpdateTime   int64  `json:"update_time"`
	LogID        string `json:"logid"`
	CommerceInfo string `json:"commerce_info"`
}

// NewStore 创建任务存储，并把默认主路径对齐回原程序使用的 tasks.db SQLite。
func NewStore(v ...any) (*Store, error) {
	// 当前流程：
	// 1. dbPath := config.TaskDBPath()
	// 2. os.MkdirAll(filepath.Dir(dbPath), 0700)
	// 3. open sqlite DB
	// 4. configureSQLite(...)
	// 5. ensureTaskSchema(...)
	// 6. return &Store{...}
	dbPath := config.TaskDBPath()
	for _, arg := range v {
		if path, ok := arg.(string); ok && strings.TrimSpace(path) != "" {
			dbPath = filepath.Clean(path)
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	store := &Store{path: dbPath}
	jsonPath := filepath.Join(filepath.Dir(dbPath), "tasks.json")
	legacyRecoveredPath := filepath.Join(filepath.Dir(dbPath), "tasks.recovered.json")
	if err := store.withFileLock(func() error {
		records, err := collectLegacyMigrationRecords(dbPath, jsonPath, legacyRecoveredPath)
		if err != nil {
			return err
		}
		if body, err := os.ReadFile(dbPath); err == nil && looksLikeJSONTaskStore(body) {
			if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := ensureSQLiteTaskStore(dbPath); err != nil {
			if strings.Contains(err.Error(), "file is not a database") {
				if removeErr := os.Remove(dbPath); removeErr != nil && !os.IsNotExist(removeErr) {
					return err
				}
				if retryErr := ensureSQLiteTaskStore(dbPath); retryErr != nil {
					return retryErr
				}
			} else {
				return err
			}
		}
		return migrateTaskRecordsToSQLite(dbPath, records)
	}); err != nil {
		return nil, err
	}
	store.db = sqliteStoreMarker{}
	return store, nil
}

// ensureMigratedJSONTaskStore 在目标任务库不存在时，从旧 JSON 路径迁移出新的 tasks.json。
func ensureMigratedJSONTaskStore(targetPath string, sourcePaths ...string) error {
	if strings.TrimSpace(targetPath) == "" {
		return nil
	}
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}
	for _, sourcePath := range sourcePaths {
		sourcePath = strings.TrimSpace(sourcePath)
		if sourcePath == "" || sourcePath == targetPath {
			continue
		}
		// 迁移顺序本身也是兼容策略的一部分：
		// 优先使用更接近当前路径对齐目标的 JSON 源，只有前一个源不存在时才继续回退。
		body, err := os.ReadFile(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := os.WriteFile(targetPath, body, 0o600); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// CreateTask 创建一条新任务记录，并保持 submit_id 的唯一性约束。
func (s *Store) CreateTask(ctx context.Context, task *AIGCTask) error {
	_ = ctx
	// 当前流程：
	// 1. rec := newTaskRecord(task)
	// 2. withSQLiteBusyRetry(func() { db.Create(rec) })
	// submit_id 必须保持唯一，当前实现不能把重复写入悄悄覆盖成“更新”。
	return s.withFileLock(func() error {
		rec, err := newTaskRecord(task)
		if err != nil {
			return err
		}
		return createTaskSQLite(s.path, rec)
	})
}

// UpdateTask 更新已有任务；只有显式传入的字段才会覆盖当前记录。
func (s *Store) UpdateTask(ctx context.Context, in UpdateTaskInput) error {
	_ = ctx
	// 当前流程：
	// 1. validate submit_id and update_time
	// 2. optionally validate request/gen_status
	// 3. build updates map with keys like:
	//    update_time, logid, gen_status, fail_reason, result_json, commerce_info
	// 4. withSQLiteBusyRetry(func() { db.Where("submit_id = ?", ...).Updates(map) })
	// 5. if rows affected == 0, return not-found style error
	// 这里只做“显式传入才覆盖”，避免空字符串把已有状态误清空。
	if strings.TrimSpace(in.SubmitID) == "" {
		return fmt.Errorf("submit_id is required")
	}
	return s.withFileLock(func() error {
		return updateTaskSQLite(s.path, in)
	})
}

// RenameTaskSubmitID 把本地任务主键从旧 submit_id 切到远端真实 submit_id。
func (s *Store) RenameTaskSubmitID(ctx context.Context, oldSubmitID string, newSubmitID string) error {
	_ = ctx
	oldSubmitID = strings.TrimSpace(oldSubmitID)
	newSubmitID = strings.TrimSpace(newSubmitID)
	if oldSubmitID == "" {
		return fmt.Errorf("old submit_id is required")
	}
	if newSubmitID == "" {
		return fmt.Errorf("new submit_id is required")
	}
	if oldSubmitID == newSubmitID {
		return nil
	}
	return s.withFileLock(func() error {
		return renameTaskSubmitIDSQLite(s.path, oldSubmitID, newSubmitID)
	})
}

// GetTask 按 submit_id 读取单条任务，并把底层记录转换回 AIGCTask。
func (s *Store) GetTask(ctx context.Context, submitID string) (*AIGCTask, error) {
	_ = ctx
	// 当前流程：
	// 1. db.Where("submit_id = ?", submitID).First(...)
	// 2. convert not-found to explicit formatted error
	// 3. return record.toAIGCTask()
	var out *AIGCTask
	err := s.withFileLock(func() error {
		record, err := getTaskRecordSQLite(s.path, strings.TrimSpace(submitID))
		if err != nil {
			return err
		}
		out, err = record.toAIGCTask()
		return err
	})
	return out, err
}

// ListTasks 按过滤条件列出任务，并稳定执行筛选、排序和分页语义。
func (s *Store) ListTasks(ctx context.Context, filter ListTaskFilter) ([]*AIGCTask, error) {
	_ = ctx
	// 当前流程：
	// 1. default limit to 20, clamp offset to 0
	// 2. start query with uid = ?
	// 3. optionally filter by submit_id / gen_task_type / gen_status
	// 4. order by update_time DESC
	// 5. limit + offset + find
	// 6. convert each taskRecord to AIGCTask
	// 先把筛选、排序、分页语义固定下来，方便后续替换底层存储时保持外部行为一致。
	var out []*AIGCTask
	err := s.withFileLock(func() error {
		records, err := listTaskRecordsSQLite(s.path, filter)
		if err != nil {
			return err
		}
		out = make([]*AIGCTask, 0, len(records))
		for _, rec := range records {
			task, err := rec.toAIGCTask()
			if err != nil {
				return err
			}
			out = append(out, task)
		}
		return nil
	})
	return out, err
}

// newTaskRecord 把业务层 AIGCTask 序列化成底层可落盘的 taskRecord。
func newTaskRecord(task *AIGCTask) (*taskRecord, error) {
	// 当前流程：
	// - validate submit_id / gen_task_type / timestamps
	// - validate request and gen_status
	// - marshal Request to JSON
	// - optionally marshal CommerceInfo
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	if strings.TrimSpace(task.SubmitID) == "" {
		return nil, fmt.Errorf("submit_id is required")
	}
	if strings.TrimSpace(task.GenTaskType) == "" {
		return nil, fmt.Errorf("gen_task_type is required")
	}
	requestBody, err := json.Marshal(task.Request)
	if err != nil {
		return nil, err
	}
	commerceBody := []byte("null")
	if task.CommerceInfo != nil {
		commerceBody, err = json.Marshal(task.CommerceInfo)
		if err != nil {
			return nil, err
		}
	}
	return &taskRecord{
		SubmitID:     task.SubmitID,
		UID:          task.UID,
		GenTaskType:  task.GenTaskType,
		Request:      string(requestBody),
		GenStatus:    task.GenStatus,
		FailReason:   task.FailReason,
		ResultJSON:   task.ResultJSON,
		CreateTime:   task.CreateTime,
		UpdateTime:   task.UpdateTime,
		LogID:        task.LogID,
		CommerceInfo: string(commerceBody),
	}, nil
}

// toAIGCTask 把底层 taskRecord 反序列化回业务层任务对象。
func (r *taskRecord) toAIGCTask() (*AIGCTask, error) {
	// 当前流程：
	// - unmarshal Request JSON
	// - copy scalar fields
	// - optionally unmarshal CommerceInfo JSON
	var req TaskRequestPayload
	if strings.TrimSpace(r.Request) != "" && strings.TrimSpace(r.Request) != "null" {
		if err := json.Unmarshal([]byte(r.Request), &req); err != nil {
			return nil, err
		}
	}

	var commerce any
	if trimmed := strings.TrimSpace(r.CommerceInfo); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &commerce); err != nil {
			return nil, err
		}
	}

	return &AIGCTask{
		SubmitID:     r.SubmitID,
		UID:          r.UID,
		GenTaskType:  r.GenTaskType,
		Request:      &req,
		GenStatus:    r.GenStatus,
		FailReason:   r.FailReason,
		ResultJSON:   r.ResultJSON,
		CreateTime:   r.CreateTime,
		UpdateTime:   r.UpdateTime,
		LogID:        r.LogID,
		CommerceInfo: commerce,
		RequestRaw:   r.Request,
	}, nil
}

// ListPrompt 从旧版任务库 request 包装里提取 prompt，仅用于对齐 list_task 顶层展示。
func (t *AIGCTask) ListPrompt() any {
	if t == nil {
		return nil
	}
	return extractLegacyRequestPrompt(t.RequestRaw)
}

func extractLegacyRequestPrompt(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var root map[string]any
	if !json.Valid([]byte(raw)) || json.Unmarshal([]byte(raw), &root) != nil {
		return nil
	}
	body := strings.TrimSpace(fmt.Sprint(root["body"]))
	if body == "" || body == "<nil>" || strings.EqualFold(body, "null") {
		return nil
	}
	var payload map[string]any
	if !json.Valid([]byte(body)) || json.Unmarshal([]byte(body), &payload) != nil {
		return nil
	}
	return legacyPromptValue(payload["prompt"])
}

func legacyPromptValue(v any) any {
	switch value := v.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := legacyPromptValue(item).(string); ok && text != "" {
				out = append(out, text)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// readAllRecords 读取整个任务库，并兼容旧版 JSON 容器与字段别名。
func (s *Store) readAllRecords() ([]taskRecord, error) {
	body, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return []taskRecord{}, nil
	}
	if !looksLikeJSONTaskStore(body) {
		return []taskRecord{}, nil
	}
	var records []taskRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return parseLegacyTaskRecords(body)
	}
	if legacyRecords, legacyErr := parseLegacyTaskRecords(body); legacyErr == nil && len(legacyRecords) == len(records) {
		// 一部分旧 JSON 可以直接按当前 struct tag 反序列化成功，但 user_id/log_id 等别名会被静默漏掉。
		// 这里再走一遍 legacy 解析做 alias merge，避免“能读但字段不完整”的半兼容状态。
		for i := range records {
			records[i] = mergeTaskRecordAliases(records[i], legacyRecords[i])
		}
	}
	return records, nil
}

// writeAllRecords 把当前任务集合完整写回 JSON 任务库。
func (s *Store) writeAllRecords(records []taskRecord) error {
	body, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(s.path, body, 0o600)
}

// looksLikeJSONTaskStore 粗略判断任务库文件是否仍是当前恢复版可接管的 JSON 形态。
func looksLikeJSONTaskStore(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return true
	}
	return strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{")
}

// parseLegacyTaskRecords 解析旧版 JSON 任务库，兼容数组根、wrapper 根和 keyed-object 根形态。
func parseLegacyTaskRecords(body []byte) ([]taskRecord, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	items := legacyTaskRecordItems(root)
	if len(items) == 0 {
		return []taskRecord{}, nil
	}
	out := make([]taskRecord, 0, len(items))
	for _, item := range items {
		record, ok, err := parseLegacyTaskRecord(item)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, record)
		}
	}
	return out, nil
}

// legacyTaskRecordItems 从任意旧任务库 JSON 根节点中抽出候选任务项列表。
func legacyTaskRecordItems(root any) []any {
	switch typed := root.(type) {
	case []any:
		return typed
	case map[string]any:
		// 老任务库既出现过数组根，也出现过 tasks/items/list/data 多层 wrapper。
		// 这里优先递归拆壳，再回收 keyed-object 形态，尽量把历史落盘格式都收进同一条兼容链路。
		for _, key := range []string{"tasks", "Tasks", "items", "Items", "list", "List", "records", "Records", "data", "Data"} {
			if items := legacyTaskRecordItems(typed[key]); len(items) > 0 {
				return items
			}
		}
		if looksLikeLegacyTaskRecordMap(typed) {
			return []any{typed}
		}
		out := make([]any, 0, len(typed))
		for key, value := range typed {
			child, ok := value.(map[string]any)
			if !ok || len(child) == 0 {
				continue
			}
			if firstTaskString(child, "submit_id", "submitId", "SubmitID") == "" {
				child = cloneTaskAnyMap(child)
				child["submit_id"] = strings.TrimSpace(key)
			}
			if looksLikeLegacyTaskRecordMap(child) {
				out = append(out, child)
			}
		}
		if len(out) > 0 {
			return out
		}
		if len(typed) > 0 {
			return []any{typed}
		}
	}
	return nil
}

// looksLikeLegacyTaskRecordMap 判断一个 map 节点是否像旧版任务记录，而不是普通 wrapper 容器。
func looksLikeLegacyTaskRecordMap(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	// 只要命中一组核心任务字段，就把它当成候选 record；
	// 真正是否可用由 parseLegacyTaskRecord 再做 submit_id 等关键字段校验。
	for _, key := range []string{
		"submit_id", "submitId", "SubmitID",
		"gen_task_type", "genTaskType", "GenTaskType",
		"gen_status", "genStatus", "GenStatus",
		"result_json", "resultJson", "ResultJSON",
		"request", "Request",
	} {
		if _, ok := root[key]; ok {
			return true
		}
	}
	return false
}

// parseLegacyTaskRecord 把单个旧版任务节点解析成当前 taskRecord，并回收常见字段别名。
func parseLegacyTaskRecord(value any) (taskRecord, bool, error) {
	root, ok := value.(map[string]any)
	if !ok || len(root) == 0 {
		return taskRecord{}, false, nil
	}
	record := taskRecord{
		SubmitID: firstTaskString(root, "submit_id", "submitId", "SubmitID"),
		// 历史任务库对用户字段的命名不稳定：
		// 有的版本写 uid，有的版本直接落 user_id/UserID。
		// 这里统一回收到 UID，保证 list/filter/view 都继续走同一套字段。
		UID:         firstTaskString(root, "uid", "UID", "user_id", "userId", "UserId", "UserID"),
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
		return taskRecord{}, false, err
	}
	record.ResultJSON, err = normalizeTaskJSONField(root, "result_json", "resultJson", "ResultJSON")
	if err != nil {
		return taskRecord{}, false, err
	}
	record.CommerceInfo, err = normalizeTaskJSONField(root, "commerce_info", "commerceInfo", "CommerceInfo")
	if err != nil {
		return taskRecord{}, false, err
	}
	if strings.TrimSpace(record.SubmitID) == "" {
		return taskRecord{}, false, nil
	}
	return record, true, nil
}

// mergeTaskRecordAliases 用 legacy 解析结果补齐直接反序列化后缺失的别名字段。
func mergeTaskRecordAliases(record taskRecord, alias taskRecord) taskRecord {
	if strings.TrimSpace(record.SubmitID) == "" {
		record.SubmitID = alias.SubmitID
	}
	if strings.TrimSpace(record.UID) == "" {
		record.UID = alias.UID
	}
	if strings.TrimSpace(record.GenTaskType) == "" {
		record.GenTaskType = alias.GenTaskType
	}
	if strings.TrimSpace(record.GenStatus) == "" {
		record.GenStatus = alias.GenStatus
	}
	if strings.TrimSpace(record.FailReason) == "" {
		record.FailReason = alias.FailReason
	}
	if strings.TrimSpace(record.ResultJSON) == "" {
		record.ResultJSON = alias.ResultJSON
	}
	if record.CreateTime == 0 {
		record.CreateTime = alias.CreateTime
	}
	if record.UpdateTime == 0 {
		record.UpdateTime = alias.UpdateTime
	}
	if strings.TrimSpace(record.LogID) == "" {
		record.LogID = alias.LogID
	}
	if strings.TrimSpace(record.CommerceInfo) == "" {
		record.CommerceInfo = alias.CommerceInfo
	}
	if strings.TrimSpace(record.Request) == "" {
		record.Request = alias.Request
	}
	return record
}

// taskSortTimestamp 返回任务排序时使用的主时间戳，优先 update_time，缺失时回落 create_time。
func taskSortTimestamp(record taskRecord) int64 {
	if record.UpdateTime > 0 {
		return record.UpdateTime
	}
	return record.CreateTime
}

// normalizeTaskJSONField 把旧任务库中的 request/result/commerce 字段统一收敛成 JSON 字符串。
func normalizeTaskJSONField(root map[string]any, keys ...string) (string, error) {
	for _, key := range keys {
		value, ok := root[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed, nil
		default:
			body, err := json.Marshal(typed)
			if err != nil {
				return "", err
			}
			return string(body), nil
		}
	}
	return "", nil
}

// cloneTaskAnyMap 浅拷贝一份任务节点 map，避免兼容解析时直接改写原始根对象。
func cloneTaskAnyMap(root map[string]any) map[string]any {
	if len(root) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(root))
	for key, value := range root {
		out[key] = value
	}
	return out
}

// firstTaskString 在当前任务节点层读取首个非空字符串字段。
func firstTaskString(root map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := root[key]
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

// firstTaskInt64 在当前任务节点层读取首个可解析的整数时间或数值字段。
func firstTaskInt64(root map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := root[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case int64:
			return typed
		case int:
			return int64(typed)
		case float64:
			return int64(typed)
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed
			}
		case string:
			typed = strings.TrimSpace(typed)
			if typed == "" {
				continue
			}
			if parsed, err := json.Number(typed).Int64(); err == nil {
				return parsed
			}
		}
	}
	return 0
}

// withFileLock 为 JSON 任务库存取加上进程级文件锁，避免并发写坏文件。
func (s *Store) withFileLock(fn func() error) error {
	// 原始工程存在 filelock_unix.go，说明任务存储至少需要进程级串行保护。
	// 当前实现仍保留这一层，避免 JSON 文件在并发写入时被破坏。
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	return fn()
}

// Recovered migration/storage helpers:
// - configureSQLite
// - ensureTaskSchema
// - migrateTaskSchemaV0ToV1
// - migrateTaskSchemaV1ToV2
// - migrateLegacyGenStatuses
// - getTaskSchemaVersion
// - setTaskSchemaVersion
// - withSQLiteBusyRetry
