package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

type originalQueryResultOutput struct {
	SubmitID    string `json:"submit_id"`
	Prompt      any    `json:"prompt,omitempty"`
	LogID       string `json:"logid,omitempty"`
	GenStatus   string `json:"gen_status"`
	FailReason  string `json:"fail_reason,omitempty"`
	ResultJSON  any    `json:"result_json,omitempty"`
	CreditCount *int   `json:"credit_count,omitempty"`
	QueueInfo   any    `json:"queue_info,omitempty"`
}

type originalQueryQueueInfo struct {
	QueueIdx    any    `json:"queue_idx,omitempty"`
	Priority    any    `json:"priority,omitempty"`
	QueueStatus string `json:"queue_status,omitempty"`
	QueueLength any    `json:"queue_length,omitempty"`
	DebugInfo   string `json:"debug_info,omitempty"`
}

// buildQueryResultOutput 组装 query_result 命令的最终输出结构，并按需附带下载结果。
func buildQueryResultOutput(v ...any) any {
	// 统一组装 query_result 的任务、结果和下载输出结构。
	var (
		taskValue  any
		parsed     any
		downloaded any
	)
	downloadDir := ""
	for _, arg := range v {
		switch value := arg.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				downloadDir = value
			}
		default:
			switch {
			case taskValue == nil:
				taskValue = value
			case parsed == nil:
				parsed = value
			case downloaded == nil:
				downloaded = value
			}
		}
	}

	localTask, ok := taskValue.(*task.AIGCTask)
	if !ok || localTask == nil {
		out := map[string]any{"task": taskValue}
		if parsed != nil {
			out["result"] = buildQueryResultMediaView(parsed)
		}
		if downloadDir != "" {
			out["download_dir"] = downloadDir
		}
		if downloaded != nil {
			out["downloaded"] = downloaded
		}
		return out
	}

	out := &originalQueryResultOutput{
		SubmitID:  localTask.SubmitID,
		GenStatus: normalizeQueryResultGenStatus(strings.TrimSpace(localTask.GenStatus)),
	}
	if prompt := localTask.ListPrompt(); prompt != nil {
		out.Prompt = prompt
	}
	if out.GenStatus != "success" {
		out.LogID = taskQueryResultLogID(localTask)
	}
	resultJSONView := compactQueryResultJSONView(localTask, parsed, downloaded, out.GenStatus)
	includeResultJSON := resultJSONView != nil
	if includeResultJSON {
		out.ResultJSON = resultJSONView
	}
	if reason := strings.TrimSpace(localTask.FailReason); reason != "" {
		out.FailReason = reason
	}
	if creditCount, ok := taskCreditCount(localTask); ok {
		out.CreditCount = &creditCount
	}
	if queue := queryResultQueueInfo(localTask.ResultJSON); queue != nil && !(out.GenStatus == "querying" && includeResultJSON) {
		out.QueueInfo = queue
	}
	return out
}

func normalizeQueryResultGenStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "failed":
		return "fail"
	default:
		return strings.TrimSpace(status)
	}
}

func compactQueryResultJSONView(localTask *task.AIGCTask, parsed any, downloaded any, genStatus string) any {
	if localTask == nil {
		return nil
	}
	if downloadedView := downloadedTaskResultJSONView(localTask.ResultJSON, downloaded); downloadedView != nil {
		return downloadedView
	}
	if mediaView := compactQueryResultMediaJSONView(localTask.ResultJSON, parsed); mediaView != nil {
		return mediaView
	}
	root, ok := viewResultJSON(localTask.ResultJSON).(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	if strings.TrimSpace(genStatus) == "querying" && shouldPreserveVerboseQueryingResultJSON(root) {
		return root
	}
	if strings.TrimSpace(genStatus) == "querying" && queryResultRootLooksFailed(root) {
		if failedLikeQueryResultIsOnlyPlaceholder(root) {
			return taskResultJSONView(localTask.ResultJSON)
		}
		return root
	}
	if strings.TrimSpace(genStatus) == "querying" && isQueryingResultJSONSkeleton(root) {
		return nil
	}
	out := map[string]any{}
	if failedItems, exists := root["failed_item_list"]; exists {
		out["failed_item_list"] = failedItems
	}
	if queueInfo := queryResultQueueInfo(localTask.ResultJSON); queueInfo != nil {
		out["queue_info"] = queueInfo
	}
	if len(out) > 0 {
		return out
	}
	if strings.TrimSpace(genStatus) == "querying" {
		return nil
	}
	return viewResultJSON(localTask.ResultJSON)
}

func queryResultRootLooksFailed(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	if strings.TrimSpace(fmt.Sprint(root["gen_status"])) == "failed" {
		return true
	}
	queueInfo, _ := root["queue_info"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(queueInfo["queue_status"])) == "failed" {
		return true
	}
	return false
}

func failedLikeQueryResultIsOnlyPlaceholder(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	allowedKeys := map[string]struct{}{
		"gen_status":    {},
		"gen_task_type": {},
		"images":        {},
		"input":         {},
		"queue_info":    {},
		"recovered":     {},
		"result":        {},
		"use_by_config": {},
		"videos":        {},
	}
	for key := range root {
		if _, ok := allowedKeys[key]; !ok {
			return false
		}
	}
	return true
}

func shouldPreserveVerboseQueryingResultJSON(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	queueInfo, _ := root["queue_info"].(map[string]any)
	for _, key := range []string{"history_id", "history_query", "debug_info", "priority", "queue_idx", "queue_length"} {
		if _, ok := queueInfo[key]; ok {
			return true
		}
	}
	for _, key := range []string{"first_frame", "last_frame", "source_image", "uploaded_images", "use_by_config"} {
		if _, ok := root[key]; ok {
			return true
		}
	}
	return false
}

func compactQueryResultMediaJSONView(resultJSON string, parsed any) any {
	if mediaView, ok := taskResultJSONView(resultJSON).(map[string]any); ok && compactMediaViewHasItems(mediaView) {
		return mediaView
	}
	if media, ok := normalizeMediaPayload(parsed); ok && compactMediaViewHasItems(media) {
		return media
	}
	return nil
}

func compactMediaViewHasItems(media map[string]any) bool {
	if len(media) == 0 {
		return false
	}
	if images, ok := media["images"].([]map[string]any); ok && len(images) > 0 {
		return true
	}
	if images, ok := media["images"].([]any); ok && len(images) > 0 {
		return true
	}
	if videos, ok := media["videos"].([]map[string]any); ok && len(videos) > 0 {
		return true
	}
	if videos, ok := media["videos"].([]any); ok && len(videos) > 0 {
		return true
	}
	return false
}

// isQueryingResultJSONSkeleton 判断 querying 态 result_json 是否仍是“仅本地排队骨架”。
func isQueryingResultJSONSkeleton(root map[string]any) bool {
	if len(root) == 0 {
		return true
	}
	allowedKeys := map[string]struct{}{
		"gen_status":    {},
		"gen_task_type": {},
		"images":        {},
		"input":         {},
		"queue_info":    {},
		"recovered":     {},
		"result":        {},
		"videos":        {},
	}
	for key := range root {
		if _, ok := allowedKeys[key]; !ok {
			return false
		}
	}
	if media, ok := normalizeMediaPayload(root); ok {
		if len(media["images"].([]map[string]any)) > 0 || len(media["videos"].([]map[string]any)) > 0 {
			return false
		}
	}
	return true
}

// buildQueryResultMediaView 把解析后的结果 payload 转换成适合输出的媒体视图。
func buildQueryResultMediaView(v ...any) any {
	// 把远端媒体结果整理成更适合命令行输出的视图。
	payload := firstNonWriter(v...)
	if payload == nil {
		return nil
	}
	if media, ok := normalizeMediaPayload(payload); ok {
		return media
	}
	return payload
}

func downloadedTaskResultJSONView(resultJSON string, downloaded any) any {
	if downloaded == nil {
		return nil
	}
	root, ok := taskResultJSONView(resultJSON).(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	out := map[string]any{
		"images": replaceDownloadedMediaPaths(mediaItemsForOutput(root["images"]), downloadedMediaPaths(downloaded, "images"), "images"),
		"videos": replaceDownloadedMediaPaths(mediaItemsForOutput(root["videos"]), downloadedMediaPaths(downloaded, "videos"), "videos"),
	}
	return out
}

func mediaItemsForOutput(v any) []map[string]any {
	switch items := v.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			cloned := map[string]any{}
			for key, value := range item {
				cloned[key] = value
			}
			out = append(out, cloned)
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if view, ok := item.(map[string]any); ok {
				cloned := map[string]any{}
				for key, value := range view {
					cloned[key] = value
				}
				out = append(out, cloned)
				continue
			}
			body, err := json.Marshal(item)
			if err != nil {
				continue
			}
			decoded := map[string]any{}
			if err := json.Unmarshal(body, &decoded); err != nil || len(decoded) == 0 {
				continue
			}
			out = append(out, decoded)
		}
		return out
	default:
		return []map[string]any{}
	}
}

func downloadedMediaPaths(downloaded any, key string) []string {
	root, ok := downloaded.(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	raw, exists := root[key]
	if !exists {
		return nil
	}
	switch items := raw.(type) {
	case []map[string]any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if path := cleanViewString(item["path"]); path != "" {
				out = append(out, path)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if view, ok := item.(map[string]any); ok {
				if path := cleanViewString(view["path"]); path != "" {
					out = append(out, path)
				}
			}
		}
		return out
	default:
		body, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var decoded []map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil
		}
		out := make([]string, 0, len(decoded))
		for _, item := range decoded {
			if path := cleanViewString(item["path"]); path != "" {
				out = append(out, path)
			}
		}
		return out
	}
}

func replaceDownloadedMediaPaths(items []map[string]any, paths []string, listKey string) []any {
	if len(items) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(items))
	for idx, item := range items {
		if idx < len(paths) && strings.TrimSpace(paths[idx]) != "" {
			delete(item, "url")
			delete(item, "image_url")
			delete(item, "video_url")
			item["path"] = paths[idx]
		}
		out = append(out, cloneOutputMediaItem(item, listKey))
	}
	return out
}

// taskCreditCount 从 commerce_info 中提取剩余积分，贴近原程序 query_result 输出。
func taskCreditCount(item *task.AIGCTask) (int, bool) {
	if item == nil {
		return 0, false
	}
	commerce, ok := item.CommerceInfo.(map[string]any)
	if ok {
		if creditCount, ok := firstCompactInt(commerce["credit_count"], commerce["creditCount"]); ok && creditCount > 0 {
			return creditCount, true
		}
	}
	root, _ := viewResultJSON(item.ResultJSON).(map[string]any)
	response, _ := root["response"].(map[string]any)
	responseData, _ := response["data"].(map[string]any)
	queueInfo, _ := root["queue_info"].(map[string]any)
	queueHistory, _ := queueInfo["history"].(map[string]any)
	history, _ := responseData["history"].(map[string]any)
	if creditCount, ok := firstCompactInt(
		root["credit_count"],
		root["creditCount"],
		responseData["credit_count"],
		responseData["creditCount"],
		nestedCompactValue(responseData, "commerce_info", "credit_count"),
		nestedCompactValue(responseData, "commerce_info", "creditCount"),
		queueHistory["credit_count"],
		queueHistory["creditCount"],
		nestedCompactValue(queueHistory, "commerce_info", "credit_count"),
		nestedCompactValue(queueHistory, "commerce_info", "creditCount"),
		history["credit_count"],
		history["creditCount"],
		nestedCompactValue(history, "commerce_info", "credit_count"),
		nestedCompactValue(history, "commerce_info", "creditCount"),
	); ok && creditCount > 0 {
		return creditCount, true
	}
	return 0, false
}

// taskQueryResultLogID 优先读任务本身 logid；本地缺失时再从 result_json 里回补。
func taskQueryResultLogID(item *task.AIGCTask) string {
	if item == nil {
		return ""
	}
	root, _ := viewResultJSON(item.ResultJSON).(map[string]any)
	response, _ := root["response"].(map[string]any)
	responseData, _ := response["data"].(map[string]any)
	queueInfo, _ := root["queue_info"].(map[string]any)
	queueHistory, _ := queueInfo["history"].(map[string]any)
	history, _ := responseData["history"].(map[string]any)
	return firstCompactString(
		item.LogID,
		root["logid"],
		root["log_id"],
		root["logId"],
		root["LogID"],
		response["log_id"],
		response["logId"],
		response["LogID"],
		responseData["log_id"],
		responseData["logId"],
		responseData["LogID"],
		queueHistory["generate_id"],
		queueHistory["generateId"],
		queueHistory["GenerateID"],
		queueHistory["log_id"],
		queueHistory["logId"],
		queueHistory["LogID"],
		history["generate_id"],
		history["generateId"],
		history["GenerateID"],
		history["log_id"],
		history["logId"],
		history["LogID"],
		nestedCompactValue(queueInfo, "history_query", "log_id"),
		nestedCompactValue(queueInfo, "history_query", "logId"),
	)
}

// queryResultQueueInfo 过滤掉恢复期诊断字段，只保留 query_result 需要展示的队列摘要。
func queryResultQueueInfo(resultJSON string) any {
	root, ok := viewResultJSON(resultJSON).(map[string]any)
	if !ok {
		return nil
	}
	queue, _ := root["queue_info"].(map[string]any)
	if len(queue) == 0 {
		return nil
	}
	out := &originalQueryQueueInfo{}
	hasValue := false
	if value, ok := firstCompactInt(queue["queue_idx"]); ok {
		out.QueueIdx = value
		hasValue = true
	}
	if value, ok := firstCompactInt(queue["priority"]); ok {
		out.Priority = value
		hasValue = true
	}
	if status := normalizeQueryQueueStatus(queue["queue_status"]); status != "" {
		out.QueueStatus = status
		hasValue = true
	}
	if value, ok := firstCompactInt(queue["queue_length"]); ok {
		out.QueueLength = value
		hasValue = true
	}
	if debugInfo := cleanViewString(queue["debug_info"]); debugInfo != "" {
		out.DebugInfo = debugInfo
		hasValue = true
	}
	if !hasValue {
		return nil
	}
	return out
}

func normalizeQueryQueueStatus(v any) string {
	status := cleanViewString(v)
	switch strings.ToLower(status) {
	case "":
		return ""
	case "0":
		return "Pending"
	case "1", "waiting", "queued":
		return "Queueing"
	case "2":
		return "Generating"
	case "3":
		return "Finish"
	case "4":
		return "Failed"
	default:
		return status
	}
}

// parseRemoteQueryResult 解析任务里保存的 result_json，并尽量提取标准 images/videos 结构。
func parseRemoteQueryResult(v ...any) (any, error) {
	// 尝试把任务保存的 result_json 解析成标准图片和视频结果结构。
	raw := ""
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			raw = s
			break
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !json.Valid([]byte(raw)) {
		return raw, nil
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	if media, ok := normalizeMediaPayload(payload); ok {
		return media, nil
	}
	return payload, nil
}

// downloadQueryResultMedia 把 query_result 解析出的图片和视频下载到指定目录。
func downloadQueryResultMedia(v ...any) (any, error) {
	// 按 query_result 的解析结果把媒体下载到指定目录。
	var (
		taskValue *task.AIGCTask
		payload   any
		dir       string
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case *task.AIGCTask:
			if value != nil {
				taskValue = value
			}
		case string:
			if dir == "" {
				dir = value
			}
		default:
			if payload == nil {
				payload = value
			}
		}
	}
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	media, ok := normalizeMediaPayload(payload)
	if !ok {
		return nil, fmt.Errorf("result_json is not a media result payload")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	type downloadedItem struct {
		URL  string `json:"url"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	out := map[string]any{
		"images": []downloadedItem{},
		"videos": []downloadedItem{},
	}

	imageItems := media["images"].([]map[string]any)
	videoItems := media["videos"].([]map[string]any)

	imageOut := make([]downloadedItem, 0, len(imageItems))
	for i, item := range imageItems {
		url := strings.TrimSpace(fmt.Sprint(item["url"]))
		dst := filepath.Join(dir, buildDownloadedMediaFilename(taskValue, "image", i+1, url, ".png"))
		if err := downloadFile(url, dst); err != nil {
			return nil, fmt.Errorf("download image %d: %w", i+1, err)
		}
		imageOut = append(imageOut, downloadedItem{URL: url, Path: dst, Type: "image"})
	}

	videoOut := make([]downloadedItem, 0, len(videoItems))
	for i, item := range videoItems {
		url := strings.TrimSpace(fmt.Sprint(item["url"]))
		dst := filepath.Join(dir, buildDownloadedMediaFilename(taskValue, "video", i+1, url, ".mp4"))
		if err := downloadFile(url, dst); err != nil {
			return nil, fmt.Errorf("download video %d: %w", i+1, err)
		}
		videoOut = append(videoOut, downloadedItem{URL: url, Path: dst, Type: "video"})
	}

	out["images"] = imageOut
	out["videos"] = videoOut
	return out, nil
}

// buildDownloadedMediaFilename 生成 query_result 下载文件名；优先对齐原程序的 submit_id 前缀命名。
func buildDownloadedMediaFilename(taskValue *task.AIGCTask, mediaType string, index int, url string, fallbackExt string) string {
	ext := normalizeDownloadedMediaExtension(mediaType, inferMediaExtension(url, fallbackExt), fallbackExt)
	submitID := ""
	if taskValue != nil {
		submitID = strings.TrimSpace(taskValue.SubmitID)
	}
	if submitID != "" {
		return fmt.Sprintf("%s_%s_%d%s", submitID, mediaType, index, ext)
	}
	return fmt.Sprintf("%s-%d%s", mediaType, index, ext)
}

func normalizeDownloadedMediaExtension(mediaType string, inferredExt string, fallbackExt string) string {
	inferredExt = sanitizeExtension(inferredExt)
	fallbackExt = sanitizeExtension(fallbackExt)
	switch strings.TrimSpace(strings.ToLower(mediaType)) {
	case "image":
		if isKnownImageExtension(inferredExt) {
			return inferredExt
		}
	case "video":
		if isKnownVideoExtension(inferredExt) {
			return inferredExt
		}
	default:
		if inferredExt != "" {
			return inferredExt
		}
	}
	if fallbackExt != "" {
		return fallbackExt
	}
	return inferredExt
}

func isKnownImageExtension(ext string) bool {
	switch strings.TrimSpace(strings.ToLower(ext)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	default:
		return false
	}
}

func isKnownVideoExtension(ext string) bool {
	switch strings.TrimSpace(strings.ToLower(ext)) {
	case ".mp4", ".mov", ".webm", ".mkv", ".avi", ".m4v":
		return true
	default:
		return false
	}
}

// inferMediaExtension 从 URL 或给定 fallback 中推断下载文件扩展名。
func inferMediaExtension(v ...any) string {
	fallback := ""
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			if fallback == "" && strings.HasPrefix(s, ".") {
				fallback = sanitizeExtension(s)
				continue
			}
			if parsed, err := url.Parse(s); err == nil && parsed.Path != "" {
				ext := filepath.Ext(parsed.Path)
				if ext != "" {
					return sanitizeExtension(ext)
				}
				if parsed.Scheme == "http" || parsed.Scheme == "https" {
					if mimeExt := inferMediaExtensionFromMIME(parsed.Query().Get("mime_type")); mimeExt != "" {
						return mimeExt
					}
				}
				if parsed.Scheme == "http" || parsed.Scheme == "https" {
					return ".bin"
				}
				continue
			}
			if idx := strings.IndexAny(s, "?#"); idx >= 0 {
				s = s[:idx]
			}
			ext := filepath.Ext(s)
			if ext != "" {
				return sanitizeExtension(ext)
			}
		}
	}
	return fallback
}

func inferMediaExtensionFromMIME(v ...any) string {
	for _, arg := range v {
		s, ok := arg.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(strings.ToLower(s))
		switch s {
		case "video_mp4", "video/mp4":
			return ".mp4"
		case "image_png", "image/png":
			return ".png"
		case "image_jpeg", "image/jpeg", "image_jpg", "image/jpg":
			return ".jpg"
		case "image_webp", "image/webp":
			return ".webp"
		}
	}
	return ""
}

// sanitizeExtension 把扩展名归一成以点开头的形式。
func sanitizeExtension(v ...any) string {
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			s = strings.TrimSpace(s)
			if s == "" {
				return ""
			}
			if !strings.HasPrefix(s, ".") {
				s = "." + s
			}
			return s
		}
	}
	return ""
}

// downloadFile 下载或复制单个媒体文件到目标路径。
func downloadFile(v ...any) error {
	var (
		url string
		dst string
	)
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			if url == "" {
				url = s
			} else if dst == "" {
				dst = s
			}
		}
	}
	if strings.TrimSpace(url) == "" || strings.TrimSpace(dst) == "" {
		return fmt.Errorf("download file arguments are incomplete")
	}

	if strings.HasPrefix(url, "file://") {
		srcPath := strings.TrimPrefix(url, "file://")
		return copyLocalFile(srcPath, dst)
	}
	if strings.HasPrefix(url, "/") {
		return copyLocalFile(url, dst)
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	file, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

// firstNonWriter 返回参数里首个不是 io.Writer 的值，供输出辅助函数抽取 payload 使用。
func firstNonWriter(v ...any) any {
	for _, arg := range v {
		if _, ok := arg.(io.Writer); ok {
			continue
		}
		return arg
	}
	return nil
}

// normalizeMediaPayload 从任意结果 payload 中提取标准化的 images/videos 列表。
func normalizeMediaPayload(payload any) (map[string]any, bool) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, false
	}
	images := extractMediaList(root, "images")
	videos := extractMediaList(root, "videos")
	if len(images) == 0 && len(videos) == 0 {
		return nil, false
	}
	return map[string]any{
		"images": images,
		"videos": videos,
	}, true
}

// extractMediaList 从结果 payload 的 images/videos 字段中提取统一的媒体项列表。
func extractMediaList(root map[string]any, key string) []map[string]any {
	raw, ok := root[key]
	if !ok {
		return []map[string]any{}
	}
	switch list := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			switch value := item.(type) {
			case map[string]any:
				if strings.TrimSpace(fmt.Sprint(value["url"])) != "" {
					out = append(out, value)
				}
			case string:
				if strings.TrimSpace(value) != "" {
					out = append(out, map[string]any{"url": value, "type": strings.TrimSuffix(key, "s")})
				}
			}
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if strings.TrimSpace(fmt.Sprint(item["url"])) != "" {
				out = append(out, item)
			}
		}
		return out
	case []string:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if strings.TrimSpace(item) != "" {
				out = append(out, map[string]any{"url": item, "type": strings.TrimSuffix(key, "s")})
			}
		}
		return out
	}
	return []map[string]any{}
}

// copyLocalFile 把本地文件原样复制到目标路径，供 file:// 和绝对路径下载场景复用。
func copyLocalFile(srcPath string, dst string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	_, err = io.Copy(dstFile, src)
	return err
}
