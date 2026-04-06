package gen

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
	resourceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/resource"
	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

// 该文件收敛生成服务的通用注册、提交流程和查询更新骨架。

type Registry struct {
	handlers map[string]*HandlerEntry
}

type HandlerEntry struct {
	GenTaskType   string
	PrepareSubmit func(ctx context.Context, uid string, input any) (*PreparedSubmit, error)
	Query         func(ctx context.Context, localTask any) (*RemoteQueryResult, error)
}

type Service struct {
	registry  *Registry
	taskStore *task.Store
}

type PreparedSubmit struct {
	// PrepareSubmit 先返回本地任务初始化信息和后续提交动作，
	// 这样可以先落库，再执行远端提交。
	InitialTask func(submitID string) any
	Commit      func(ctx context.Context) (*SubmitOutcome, error)
	BuildUpdate func(submitID string, out *SubmitOutcome, logID string, err error) any
}

type SubmitOutcome struct {
	ResultJSON   string
	FailReason   string
	LogID        string
	CommerceInfo any
}

type RemoteQueryResult struct {
	// Status 约定为：1=查询中，2=成功，3=失败。
	Status         int
	ResultJSON     string
	FallbackResult string
	FailReason     string
	CommerceInfo   any
}

type sessionContextKey struct{}

func NewRegistry() *Registry     { return &Registry{handlers: map[string]*HandlerEntry{}} }
func DefaultRegistry() *Registry { return NewRegistry() }

func NewService(v ...any) (*Service, error) {
	var (
		store     *task.Store
		registry  *Registry
		mcpClient *mcpclient.HTTPClient
		resources *resourceclient.ByteDanceUploadClient
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case *task.Store:
			store = value
		case *Registry:
			registry = value
		case *mcpclient.HTTPClient:
			mcpClient = value
		case *resourceclient.ByteDanceUploadClient:
			resources = value
		}
	}
	var err error
	if store == nil {
		store, err = task.NewStore()
		if err != nil {
			return nil, err
		}
	}
	if registry == nil {
		registry = DefaultRegistry()
	}
	if len(registry.handlers) == 0 {
		if err := RegisterDreaminaHandlers(registry, mcpClient, resources); err != nil {
			return nil, err
		}
	}
	return &Service{
		registry:  registry,
		taskStore: store,
	}, nil
}

func (r *Registry) Register(v ...any) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	var entry *HandlerEntry
	for _, arg := range v {
		if value, ok := arg.(*HandlerEntry); ok {
			entry = value
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("handler entry is required")
	}
	key := strings.TrimSpace(entry.GenTaskType)
	if key == "" {
		return fmt.Errorf("gen_task_type is required")
	}
	r.handlers[key] = entry
	return nil
}

func (r *Registry) Lookup(genTaskType string) (*HandlerEntry, bool) {
	if r == nil {
		return nil, false
	}
	entry, ok := r.handlers[strings.TrimSpace(genTaskType)]
	return entry, ok
}

// LookupQuery 优先按任务类型取查询处理器；取不到时回退到任意一个可用查询处理器。
// 这样即使本地 tasks.db 缺失，或旧任务没有可靠的 gen_task_type，也还能继续按 submit_id 远端补查。
func (r *Registry) LookupQuery(genTaskType string) (*HandlerEntry, bool) {
	if r == nil {
		return nil, false
	}
	if entry, ok := r.Lookup(genTaskType); ok && entry != nil && entry.Query != nil {
		return entry, true
	}
	for _, entry := range r.handlers {
		if entry != nil && entry.Query != nil {
			return entry, true
		}
	}
	return nil, false
}

func (s *Service) SubmitTask(ctx context.Context, uid string, genTaskType string, input any) (any, error) {
	// 当前流程是先校验输入并解析处理器，再生成本地 submit_id 和初始任务落库，
	// 随后执行远端提交，并把结果、日志号和失败原因回写到任务存储。
	genTaskType = strings.TrimSpace(genTaskType)
	if genTaskType == "" {
		return nil, fmt.Errorf("gen_task_type is required")
	}
	if s == nil || s.taskStore == nil || s.registry == nil {
		return nil, fmt.Errorf("generator service is not initialized")
	}
	if _, ok := s.registry.Lookup(genTaskType); !ok {
		return nil, fmt.Errorf("unknown gen_task_type %q", genTaskType)
	}
	handler, _ := s.registry.Lookup(genTaskType)

	submitID, err := newSubmitID()
	if err != nil {
		return nil, err
	}
	var prepared *PreparedSubmit
	if handler != nil && handler.PrepareSubmit != nil {
		prepared, err = handler.PrepareSubmit(ctx, uid, input)
		if err != nil {
			return nil, err
		}
	}
	initialTask := normalizeInitialTask(prepared, submitID, uid, genTaskType, input)
	if err := s.taskStore.CreateTask(ctx, initialTask); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.Commit == nil {
		return s.taskStore.GetTask(ctx, submitID)
	}

	outcome, commitErr := prepared.Commit(ctx)
	logID := extractLogID(commitErr)
	if outcome != nil && strings.TrimSpace(outcome.LogID) != "" {
		logID = strings.TrimSpace(outcome.LogID)
	}
	if commitErr == nil {
		if remoteSubmitID := remoteSubmitIDFromOutcome(outcome); remoteSubmitID != "" && remoteSubmitID != submitID {
			if err := s.taskStore.RenameTaskSubmitID(ctx, submitID, remoteSubmitID); err != nil {
				return nil, err
			}
			submitID = remoteSubmitID
		}
	}
	if commitErr != nil {
		if remoteSubmitID := extractRemoteSubmitID(commitErr); remoteSubmitID != "" && remoteSubmitID != submitID {
			if err := s.taskStore.RenameTaskSubmitID(ctx, submitID, remoteSubmitID); err != nil {
				return nil, err
			}
			submitID = remoteSubmitID
		}
	}

	update := task.UpdateTaskInput{
		SubmitID:   submitID,
		UpdateTime: time.Now().Unix(),
	}
	if prepared.BuildUpdate != nil {
		switch value := prepared.BuildUpdate(submitID, outcome, logID, commitErr).(type) {
		case task.UpdateTaskInput:
			update = value
		case *task.UpdateTaskInput:
			if value != nil {
				update = *value
			}
		}
		if strings.TrimSpace(update.SubmitID) == "" {
			update.SubmitID = submitID
		}
		if update.UpdateTime <= 0 {
			update.UpdateTime = time.Now().Unix()
		}
	}
	if commitErr != nil {
		reason := dreaminaFailureReason(commitErr)
		update.GenStatus = "failed"
		update.FailReason = &reason
		if outcome != nil {
			if strings.TrimSpace(outcome.ResultJSON) != "" {
				update.ResultJSON = &outcome.ResultJSON
			}
			if outcome.CommerceInfo != nil {
				update.CommerceInfo = outcome.CommerceInfo
			}
		}
		if strings.TrimSpace(logID) != "" {
			update.LogID = &logID
		}
		if err := s.taskStore.UpdateTask(ctx, update); err != nil {
			return nil, err
		}
		return s.taskStore.GetTask(ctx, submitID)
	}

	update.GenStatus = "querying"
	if outcome != nil {
		if strings.TrimSpace(outcome.ResultJSON) != "" {
			update.ResultJSON = &outcome.ResultJSON
		}
		if outcome.CommerceInfo != nil {
			update.CommerceInfo = outcome.CommerceInfo
		}
	}
	if update.ResultJSON == nil {
		update.ResultJSON = &initialTask.ResultJSON
	}
	if strings.TrimSpace(logID) != "" {
		update.LogID = &logID
	}
	if err := s.taskStore.UpdateTask(ctx, update); err != nil {
		return nil, err
	}
	return s.taskStore.GetTask(ctx, submitID)
}

func extractLogID(err error) string {
	// 从错误对象里提取可能携带的日志号。
	if err == nil {
		return ""
	}
	if apiErr, ok := err.(*mcpclient.APIError); ok {
		return strings.TrimSpace(apiErr.LogID)
	}
	type logIDCarrier interface {
		LogID() string
	}
	var target logIDCarrier
	if value, ok := any(err).(logIDCarrier); ok {
		target = value
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.LogID())
}

func extractRemoteSubmitID(err error) string {
	if err == nil {
		return ""
	}
	if apiErr, ok := err.(*mcpclient.APIError); ok {
		return strings.TrimSpace(apiErr.SubmitID)
	}
	type submitIDCarrier interface {
		SubmitID() string
	}
	var target submitIDCarrier
	if value, ok := any(err).(submitIDCarrier); ok {
		target = value
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.SubmitID())
}

func remoteSubmitIDFromOutcome(outcome *SubmitOutcome) string {
	if outcome == nil || strings.TrimSpace(outcome.ResultJSON) == "" {
		return ""
	}
	root := parseRecoveredResultRoot(outcome.ResultJSON)
	if len(root) == 0 {
		return ""
	}
	response := mapValue(root, "response")
	for _, candidate := range []map[string]any{
		root,
		response,
		mapValue(response, "data"),
		mapValue(response, "recovered"),
		mapValue(root, "queue_info"),
	} {
		if submitID := firstCleanRecoveredValue(candidate, "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID"); submitID != "" {
			return submitID
		}
	}
	for _, candidate := range []map[string]any{
		root,
		response,
		mapValue(response, "recovered"),
		mapValue(root, "queue_info"),
	} {
		if historyID := firstCleanRecoveredValue(candidate, "history_id", "historyId", "HistoryID"); historyID != "" {
			return historyID
		}
	}
	return ""
}

func mapValue(root map[string]any, key string) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	value, _ := root[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func (s *Service) QueryResult(ctx context.Context, submitID string) (any, error) {
	// 查询时先读取本地任务，再按任务类型调用远端查询，
	// 最后把状态、结果和失败原因同步回本地任务存储。
	if strings.TrimSpace(submitID) == "" {
		return nil, fmt.Errorf("submit_id is required")
	}
	if s == nil || s.taskStore == nil || s.registry == nil {
		return nil, fmt.Errorf("generator service is not initialized")
	}

	localStore := s.taskStore
	localTask, err := s.taskStore.GetTask(ctx, submitID)
	hasLocalTask := err == nil
	if err != nil {
		if !strings.Contains(err.Error(), "task not found:") {
			return nil, err
		}
		if fallbackStore, fallbackTask, fallbackErr := lookupTaskFromDefaultPath(ctx, submitID); fallbackErr == nil && fallbackTask != nil {
			localStore = fallbackStore
			localTask = fallbackTask
			hasLocalTask = true
		} else {
			return nil, fmt.Errorf("task %q not found", submitID)
		}
	}
	handler, ok := s.registry.LookupQuery(strings.TrimSpace(localTask.GenTaskType))
	if !ok {
		return nil, fmt.Errorf("query handler is not configured")
	}

	resultJSON := strings.TrimSpace(localTask.ResultJSON)
	if resultJSON == "" {
		resultJSON = buildSkeletonResultJSON(localTask.GenTaskType, nil)
	}
	remote := &RemoteQueryResult{
		Status:       2,
		ResultJSON:   resultJSON,
		CommerceInfo: localTask.CommerceInfo,
	}
	if handler != nil && handler.Query != nil {
		remote, err = handler.Query(ctx, localTask)
		if err != nil {
			return nil, err
		}
		if remote == nil {
			remote = &RemoteQueryResult{
				Status:       2,
				ResultJSON:   resultJSON,
				CommerceInfo: localTask.CommerceInfo,
			}
		}
	}
	if strings.TrimSpace(remote.ResultJSON) != "" {
		resultJSON = strings.TrimSpace(remote.ResultJSON)
	} else if strings.TrimSpace(remote.FallbackResult) != "" {
		resultJSON = strings.TrimSpace(remote.FallbackResult)
	}
	genStatus := "success"
	switch remote.Status {
	case 1:
		genStatus = "querying"
	case 3:
		genStatus = "failed"
	}

	patch := task.UpdateTaskInput{
		SubmitID:   submitID,
		UpdateTime: time.Now().Unix(),
		GenStatus:  genStatus,
		ResultJSON: &resultJSON,
	}
	if genStatus == "failed" {
		reason := strings.TrimSpace(remote.FailReason)
		if reason == "" {
			reason = strings.TrimSpace(localTask.FailReason)
		}
		patch.FailReason = &reason
	}
	if remote.CommerceInfo != nil {
		patch.CommerceInfo = remote.CommerceInfo
	}
	if !hasLocalTask {
		localTask.GenStatus = genStatus
		localTask.ResultJSON = resultJSON
		localTask.CommerceInfo = remote.CommerceInfo
		if genStatus == "failed" {
			localTask.FailReason = strings.TrimSpace(remote.FailReason)
		}
		localTask.UpdateTime = patch.UpdateTime
		return localTask, nil
	}
	if err := localStore.UpdateTask(ctx, patch); err != nil {
		return nil, err
	}
	return localStore.GetTask(ctx, submitID)
}

// lookupTaskFromDefaultPath 在显式配置目录未命中时，再回退一次默认 ~/.dreamina_cli/tasks.db。
// 这样可以对齐原程序在自定义配置目录下仍能命中默认任务库历史记录的兼容行为。
func lookupTaskFromDefaultPath(ctx context.Context, submitID string) (*task.Store, *task.AIGCTask, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	defaultPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	store, err := task.NewStore(defaultPath)
	if err != nil {
		return nil, nil, err
	}
	taskValue, err := store.GetTask(ctx, submitID)
	if err != nil {
		return nil, nil, err
	}
	return store, taskValue, nil
}

func normalizeInitialTask(prepared *PreparedSubmit, submitID string, uid string, genTaskType string, input any) *task.AIGCTask {
	now := time.Now().Unix()
	request := &task.TaskRequestPayload{
		Values: cloneInputMap(input),
	}
	initialTask := &task.AIGCTask{}
	if prepared != nil && prepared.InitialTask != nil {
		switch value := prepared.InitialTask(submitID).(type) {
		case *task.AIGCTask:
			if value != nil {
				initialTask = value
			}
		case task.AIGCTask:
			initialTask = &value
		}
	}
	if initialTask.Request == nil {
		initialTask.Request = request
	}
	if initialTask.Request != nil && len(initialTask.Request.Values) == 0 {
		initialTask.Request.Values = cloneInputMap(input)
	}
	if strings.TrimSpace(initialTask.SubmitID) == "" {
		initialTask.SubmitID = submitID
	}
	if strings.TrimSpace(initialTask.UID) == "" {
		initialTask.UID = strings.TrimSpace(uid)
	}
	if strings.TrimSpace(initialTask.GenTaskType) == "" {
		initialTask.GenTaskType = genTaskType
	}
	if strings.TrimSpace(initialTask.GenStatus) == "" {
		initialTask.GenStatus = "querying"
	}
	if initialTask.CreateTime <= 0 {
		initialTask.CreateTime = now
	}
	if initialTask.UpdateTime <= 0 {
		initialTask.UpdateTime = now
	}
	if strings.TrimSpace(initialTask.ResultJSON) == "" {
		initialTask.ResultJSON = buildSkeletonResultJSON(genTaskType, input)
	}
	if strings.TrimSpace(initialTask.LogID) == "" {
		initialTask.LogID = ""
	}
	return initialTask
}

// 以下辅助函数负责资源上传、队列信息组装和结果 JSON 构建。

func newSubmitID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "submit-" + hex.EncodeToString(buf), nil
}

func buildSkeletonResultJSON(genTaskType string, input any) string {
	payload := map[string]any{
		"gen_task_type": genTaskType,
		"recovered":     true,
		"input":         input,
		"gen_status":    "querying",
		"queue_info": map[string]any{
			"queue_status": "submitted",
			"progress":     0,
			"query_count":  0,
			"recovered":    true,
		},
	}
	inputMap := cloneInputMap(input)
	switch genTaskType {
	case "text2image", "image2image", "image_upscale":
		payload["images"] = []map[string]any{}
	case "text2video", "image2video", "frames2video", "multiframe2video", "multimodal2video":
		payload["videos"] = []map[string]any{}
	default:
		payload["result"] = map[string]any{}
	}
	applyRecoveredTaskDetails(payload, genTaskType, inputMap)
	body, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(body)
}

func cloneInputMap(input any) map[string]any {
	switch value := input.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = v
		}
		return out
	case nil:
		return map[string]any{}
	default:
		body, err := json.Marshal(value)
		if err == nil {
			var out map[string]any
			if json.Unmarshal(body, &out) == nil {
				return out
			}
		}
		return map[string]any{"value": value}
	}
}

func applyRecoveredTaskDetails(payload map[string]any, genTaskType string, input map[string]any) {
	switch genTaskType {
	case "image2image":
		payload["source_images"] = buildResourceRefs(anyStringSlice(input["image_paths"]), "image")
	case "image_upscale":
		payload["source_image"] = buildResourceRefs([]string{anyString(input["image_path"])}, "image")
	case "image2video":
		payload["first_frame"] = buildResourceRefs([]string{anyString(input["image_path"])}, "image")
		payload["use_by_config"] = anyString(input["use_by_config"])
	case "frames2video":
		payload["first_frame"] = buildResourceRefs([]string{anyString(input["first_path"])}, "image")
		payload["last_frame"] = buildResourceRefs([]string{anyString(input["last_path"])}, "image")
	case "multiframe2video":
		payload["frames"] = buildResourceRefs(anyStringSlice(input["image_paths"]), "image")
		payload["transition_prompts"] = anyStringSlice(input["transition_prompts"])
		payload["transition_durations"] = anyStringSlice(input["transition_durations"])
	case "multimodal2video":
		payload["image_inputs"] = buildResourceRefs(anyStringSlice(input["image_paths"]), "image")
		payload["video_inputs"] = buildResourceRefs(anyStringSlice(input["video_paths"]), "video")
		payload["audio_inputs"] = buildResourceRefs(anyStringSlice(input["audio_paths"]), "audio")
	}
}

func buildResourceRefs(paths []string, resourceType string) []map[string]any {
	out := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		out = append(out, map[string]any{
			"path":        path,
			"resource_id": localRecoveredResourceID(resourceType, path),
			"type":        resourceType,
			"name":        filepath.Base(path),
		})
	}
	return out
}

func localRecoveredResourceID(resourceType string, path string) string {
	sum := md5.Sum([]byte(resourceType + ":" + filepath.Base(path)))
	return fmt.Sprintf("res_%s_%x", resourceType, sum[:6])
}

func anyString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func anyStringSlice(v any) []string {
	switch value := v.(type) {
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if s := strings.TrimSpace(value); s != "" {
			return []string{s}
		}
	}
	return nil
}

func ContextWithSession(ctx context.Context, payload any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	session := buildClientSession(payload)
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, session)
}

func sessionFromContext(ctx context.Context) *mcpclient.Session {
	if ctx == nil {
		return &mcpclient.Session{}
	}
	if value, ok := ctx.Value(sessionContextKey{}).(*mcpclient.Session); ok && value != nil {
		return value
	}
	return &mcpclient.Session{}
}

func buildClientSession(payload any) *mcpclient.Session {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	session := &mcpclient.Session{
		Headers: map[string]string{},
	}
	if cookie := anyString(root["cookie"]); cookie != "" {
		session.Cookie = cookie
	}
	if rawHeaders, ok := root["headers"].(map[string]any); ok {
		for key, value := range rawHeaders {
			key = strings.TrimSpace(key)
			text := strings.TrimSpace(fmt.Sprint(value))
			if key != "" && text != "" && text != "<nil>" {
				session.Headers[key] = text
			}
		}
	}
	// gen/mcp 客户端最终只认 Session.UserID。
	// 这里把登录 payload 里常见的 user_id/uid/UserID/UID 一次性统一，避免同一份会话在 query 链路里再次丢用户信息。
	if userID := recursiveSessionString(root, "user_id", "uid", "userId", "UserId", "UserID", "UID"); userID != "" {
		session.UserID = normalizeSessionIDString(userID)
	}
	return session
}

func scalarString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case int:
		return fmt.Sprintf("%d", value)
	case int64:
		return fmt.Sprintf("%d", value)
	case float64:
		return fmt.Sprintf("%.0f", value)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func recursiveSessionString(node any, keys ...string) string {
	lookup := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return findRecursiveSessionString(node, lookup)
}

func findRecursiveSessionString(node any, lookup map[string]struct{}) string {
	switch value := node.(type) {
	case map[string]any:
		for key, item := range value {
			if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok {
				if text := scalarString(item); text != "" && text != "<nil>" {
					return normalizeSessionIDString(text)
				}
			}
		}
		for _, item := range value {
			if text := findRecursiveSessionString(item, lookup); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range value {
			if text := findRecursiveSessionString(item, lookup); text != "" {
				return text
			}
		}
	}
	return ""
}

func normalizeSessionIDString(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if !strings.ContainsAny(text, "eE") {
		return text
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return text
	}
	return fmt.Sprintf("%.0f", parsed)
}
