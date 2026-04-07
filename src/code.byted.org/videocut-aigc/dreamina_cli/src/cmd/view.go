package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

type taskResultOutput struct{}
type taskListOutput struct{}
type queryResultImageOutput struct {
	ImageURL string `json:"image_url,omitempty"`
	Path     string `json:"path,omitempty"`
	Width    any    `json:"width,omitempty"`
	Height   any    `json:"height,omitempty"`
}

type queryResultVideoOutput struct {
	VideoURL string `json:"video_url,omitempty"`
	Path     string `json:"path,omitempty"`
	FPS      any    `json:"fps,omitempty"`
	Width    any    `json:"width,omitempty"`
	Height   any    `json:"height,omitempty"`
	Format   string `json:"format,omitempty"`
	Duration any    `json:"duration,omitempty"`
}

type remoteQueryImage struct{}
type remoteQueryVideo struct{}

type commerceTripletOutput struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	BenefitType  string `json:"benefit_type"`
}

type commerceInfoOutput struct {
	CreditCount any                     `json:"credit_count,omitempty"`
	Triplet     commerceTripletOutput   `json:"triplet"`
	Triplets    []commerceTripletOutput `json:"triplets,omitempty"`
}

type originalTaskListItem struct {
	SubmitID     string `json:"submit_id"`
	Prompt       any    `json:"prompt,omitempty"`
	GenTaskType  string `json:"gen_task_type"`
	GenStatus    string `json:"gen_status"`
	FailReason   string `json:"fail_reason"`
	ResultJSON   any    `json:"result_json,omitempty"`
	CommerceInfo any    `json:"commerce_info,omitempty"`
}

// taskListView 把任务列表转换成适合命令行 JSON 输出的简化视图。
func taskListView(v ...any) any {
	var tasks []*task.AIGCTask
	for _, arg := range v {
		switch value := arg.(type) {
		case []*task.AIGCTask:
			tasks = value
		case []task.AIGCTask:
			tasks = make([]*task.AIGCTask, 0, len(value))
			for i := range value {
				item := value[i]
				tasks = append(tasks, &item)
			}
		}
	}
	items := make([]originalTaskListItem, 0, len(tasks))
	for _, item := range tasks {
		if item == nil {
			continue
		}
		row := originalTaskListItem{
			SubmitID:    item.SubmitID,
			GenTaskType: item.GenTaskType,
			GenStatus:   item.GenStatus,
			FailReason:  strings.TrimSpace(item.FailReason),
			ResultJSON:  listTaskResultJSONView(item),
		}
		if prompt := item.ListPrompt(); prompt != nil {
			row.Prompt = prompt
		}
		if commerce := taskCommerceInfoView(item); commerce != nil {
			row.CommerceInfo = commerce
		}
		items = append(items, row)
	}
	return items
}

func listTaskResultJSONView(item *task.AIGCTask) any {
	if item == nil {
		return nil
	}
	if cleanViewString(item.ResultJSON) == "" {
		return nil
	}
	return rawResultJSON(item.ResultJSON)
}

// viewResultJSON 尝试把结果 JSON 字符串解析成可继续浏览的结构化对象。
func viewResultJSON(v ...any) any {
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			s = strings.TrimSpace(s)
			if s == "" {
				return ""
			}
			var payload any
			if json.Valid([]byte(s)) && json.Unmarshal([]byte(s), &payload) == nil {
				return payload
			}
			return s
		}
	}
	return nil
}

// rawResultJSON 尽量保留 result_json 原始字节顺序，避免二次反序列化改写字段顺序。
func rawResultJSON(v ...any) any {
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			s = strings.TrimSpace(s)
			if s == "" {
				return ""
			}
			if json.Valid([]byte(s)) {
				return json.RawMessage([]byte(s))
			}
			return s
		}
	}
	return nil
}

// taskPrompt 从任务请求、输入或结果 JSON 中尽量提取可展示的 prompt。
func taskPrompt(v ...any) any {
	for _, arg := range v {
		switch value := arg.(type) {
		case *task.AIGCTask:
			if value != nil && value.Request != nil {
				if prompt := promptValue(value.Request.Values["prompt"]); prompt != nil {
					return prompt
				}
			}
			root, _ := viewResultJSON(value.ResultJSON).(map[string]any)
			if input, ok := root["input"].(map[string]any); ok {
				if prompt := promptValue(input["prompt"]); prompt != nil {
					return prompt
				}
			}
			if req, ok := root["request"].(map[string]any); ok {
				if prompt := promptValue(req["Prompt"]); prompt != nil {
					return prompt
				}
				if prompt := promptValue(req["prompt"]); prompt != nil {
					return prompt
				}
			}
		case map[string]any:
			if prompt := promptValue(value["prompt"]); prompt != nil {
				return prompt
			}
		}
	}
	return nil
}

// promptValue 归一化 prompt 字段的单值或数组形态，返回最适合展示的值。
func promptValue(v ...any) any {
	for _, arg := range v {
		switch value := arg.(type) {
		case string:
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		case []string:
			out := make([]string, 0, len(value))
			for _, item := range value {
				item = strings.TrimSpace(item)
				if item != "" {
					out = append(out, item)
				}
			}
			if len(out) > 0 {
				return out
			}
		case []any:
			out := make([]string, 0, len(value))
			for _, item := range value {
				if s, ok := promptValue(item).(string); ok && s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

// printJSON 把任意结构化对象格式化成缩进 JSON 并写到输出流。
func printJSON(v ...any) error {
	out := io.Writer(os.Stdout)
	var payload any
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
			continue
		}
		payload = arg
	}
	if payload == nil {
		return fmt.Errorf("json payload is nil")
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(payload)
}

// taskQueueView 从结果 JSON 中提取队列状态摘要。
func taskQueueView(resultJSON string) any {
	root, ok := viewResultJSON(resultJSON).(map[string]any)
	if !ok {
		return nil
	}
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		return nil
	}
	return map[string]any{
		"history_id":   cleanViewString(queueInfo["history_id"]),
		"queue_status": strings.TrimSpace(fmt.Sprint(queueInfo["queue_status"])),
		"progress":     queueInfo["progress"],
		"query_count":  queueInfo["query_count"],
	}
}

// taskResultPreview 生成结果媒体或任务类型的轻量预览，供任务列表输出使用。
func taskResultPreview(resultJSON string) any {
	payload := viewResultJSON(resultJSON)
	if payload == nil {
		return nil
	}
	if media, ok := normalizeMediaPayload(payload); ok {
		return map[string]any{
			"images": len(media["images"].([]map[string]any)),
			"videos": len(media["videos"].([]map[string]any)),
		}
	}
	root, ok := payload.(map[string]any)
	if !ok {
		return payload
	}
	if genTaskType := strings.TrimSpace(fmt.Sprint(root["gen_task_type"])); genTaskType != "" {
		return map[string]any{"gen_task_type": genTaskType}
	}
	return nil
}

// taskResultJSONView 把任务结果收敛成更贴近原程序的 result_json 输出形态。
func taskResultJSONView(resultJSON string) any {
	payload := viewResultJSON(resultJSON)
	root, ok := payload.(map[string]any)
	if !ok {
		return payload
	}
	return map[string]any{
		"images": outputMediaList(root, "images"),
		"videos": outputMediaList(root, "videos"),
	}
}

// taskCommerceInfoView 从任务或结果 JSON 中提取 commerce_info。
func taskCommerceInfoView(item *task.AIGCTask) any {
	if item == nil {
		return nil
	}
	if commerce, ok := item.CommerceInfo.(map[string]any); ok && len(commerce) > 0 {
		return normalizeCommerceInfoView(commerce)
	}
	root, ok := viewResultJSON(item.ResultJSON).(map[string]any)
	if !ok {
		return nil
	}
	response, _ := root["response"].(map[string]any)
	data, _ := response["data"].(map[string]any)
	commerce, _ := data["commerce_info"].(map[string]any)
	return normalizeCommerceInfoView(commerce)
}

// outputMediaList 保留媒体条目的原始字段风格，避免把 image_url/video_url 强行改写成通用 url。
func outputMediaList(root map[string]any, key string) []any {
	if len(root) == 0 {
		return []any{}
	}
	raw, ok := root[key]
	if !ok {
		return []any{}
	}
	items := mediaItemsForOutput(raw)
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, cloneOutputMediaItem(item, key))
	}
	return out
}

// cloneOutputMediaItem 把媒体项规范成稳定字段顺序的展示结构。
func cloneOutputMediaItem(item map[string]any, key string) any {
	if len(item) == 0 {
		if key == "images" {
			return queryResultImageOutput{}
		}
		return queryResultVideoOutput{}
	}
	if key == "images" {
		return queryResultImageOutput{
			ImageURL: firstNonEmptyViewString(item["image_url"], item["url"]),
			Path:     cleanViewString(item["path"]),
			Width:    normalizeViewScalar(item["width"]),
			Height:   normalizeViewScalar(item["height"]),
		}
	}
	return queryResultVideoOutput{
		VideoURL: firstNonEmptyViewString(item["video_url"], item["url"]),
		Path:     cleanViewString(item["path"]),
		FPS:      normalizeViewScalar(item["fps"]),
		Width:    normalizeViewScalar(item["width"]),
		Height:   normalizeViewScalar(item["height"]),
		Format:   cleanViewString(item["format"]),
		Duration: normalizeViewScalar(item["duration"]),
	}
}

// normalizeCommerceInfoView 把 commerce_info 收敛到更接近原程序的字段形态。
func normalizeCommerceInfoView(commerce map[string]any) any {
	out := commerceInfoOutput{
		CreditCount: 0,
		Triplet: commerceTripletOutput{
			ResourceType: "",
			ResourceID:   "",
			BenefitType:  "",
		},
	}
	if len(commerce) == 0 {
		return out
	}
	if value := normalizeViewScalar(commerce["credit_count"]); value != nil {
		out.CreditCount = value
	} else if value := normalizeViewScalar(commerce["creditCount"]); value != nil {
		out.CreditCount = value
	}
	if triplet := normalizeCommerceTriplet(firstMapValue(commerce["triplet"])); triplet != nil {
		out.Triplet = *triplet
	}
	if triplets := normalizeCommerceTripletList(commerce["triplets"]); len(triplets) > 0 {
		out.Triplets = triplets
	}
	return out
}

// cleanViewString 清理展示层里的空字符串、<nil> 和 null 文本噪音。
func cleanViewString(v any) string {
	value := strings.TrimSpace(fmt.Sprint(v))
	if value == "" || value == "<nil>" || strings.EqualFold(value, "null") {
		return ""
	}
	return value
}

func firstNonEmptyViewString(v ...any) string {
	for _, arg := range v {
		if value := cleanViewString(arg); value != "" {
			return value
		}
	}
	return ""
}

func normalizeViewScalar(v any) any {
	if cleanViewString(v) == "" {
		return nil
	}
	return v
}

func firstMapValue(v any) map[string]any {
	root, _ := v.(map[string]any)
	return root
}

func normalizeCommerceTriplet(root map[string]any) *commerceTripletOutput {
	if len(root) == 0 {
		return nil
	}
	return &commerceTripletOutput{
		ResourceType: firstNonEmptyViewString(root["resource_type"], root["resourceType"], root["ResourceType"]),
		ResourceID:   firstNonEmptyViewString(root["resource_id"], root["resourceId"], root["ResourceID"]),
		BenefitType:  firstNonEmptyViewString(root["benefit_type"], root["benefitType"], root["BenefitType"]),
	}
}

func normalizeCommerceTripletList(v any) []commerceTripletOutput {
	list, _ := v.([]any)
	if len(list) == 0 {
		if typed, ok := v.([]map[string]any); ok {
			out := make([]commerceTripletOutput, 0, len(typed))
			for _, item := range typed {
				if triplet := normalizeCommerceTriplet(item); triplet != nil {
					out = append(out, *triplet)
				}
			}
			return out
		}
		return nil
	}
	out := make([]commerceTripletOutput, 0, len(list))
	for _, item := range list {
		if triplet := normalizeCommerceTriplet(firstMapValue(item)); triplet != nil {
			out = append(out, *triplet)
		}
	}
	return out
}
