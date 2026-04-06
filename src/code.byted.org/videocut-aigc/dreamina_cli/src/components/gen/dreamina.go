package gen

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
	resourceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/resource"
	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

// 该文件负责 Dreamina 各类生成任务处理器的注册、提交和查询适配。

type SubmitInput map[string]any

// RegisterDreaminaHandlers 注册 Dreamina 相关的提交与查询处理器，并在缺少依赖时补默认客户端。
func RegisterDreaminaHandlers(v ...any) error {
	var (
		registry  *Registry
		client    *mcpclient.HTTPClient
		resources *resourceclient.ByteDanceUploadClient
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case *Registry:
			registry = value
		case *mcpclient.HTTPClient:
			client = value
		case *resourceclient.ByteDanceUploadClient:
			resources = value
		}
	}
	if registry == nil {
		return nil
	}
	if client == nil {
		client = mcpclient.New()
	}
	if resources == nil {
		resources = resourceclient.New()
	}

	handlers := []*HandlerEntry{
		newText2VideoHandler(client, resources),
		newImage2VideoHandler(client, resources),
		newFrames2VideoHandler(client, resources),
		newMultiFrame2VideoHandler(client, resources),
		newMultiModal2VideoHandler(client, resources),
		newText2ImageHandler(client, resources),
		newImage2ImageHandler(client, resources),
		newImageUpscaleHandler(client, resources),
	}
	for _, handler := range handlers {
		if err := registry.Register(handler); err != nil {
			return err
		}
	}
	return nil
}

func newText2VideoHandler(client *mcpclient.HTTPClient, _ *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "text2video",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			req := &mcpclient.Text2VideoRequest{
				Prompt:          anyString(inputMap["prompt"]),
				Duration:        anyInt(inputMap["duration"]),
				Ratio:           normalizeRatio(anyString(inputMap["ratio"])),
				VideoResolution: anyString(inputMap["video_resolution"]),
				ModelVersion:    anyString(inputMap["model_version"]),
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				resp, err := client.Text2Video(ctx, sessionFromContext(ctx), req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON:   buildDreaminaResultJSON("text2video", inputMap, req, nil, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newImage2VideoHandler(client *mcpclient.HTTPClient, resources *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "image2video",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			req := &mcpclient.Image2VideoRequest{
				Prompt:          anyString(inputMap["prompt"]),
				Duration:        anyInt(inputMap["duration"]),
				VideoResolution: anyString(inputMap["video_resolution"]),
				ModelVersion:    anyString(inputMap["model_version"]),
				UseByConfig:     strings.EqualFold(anyString(inputMap["use_by_config"]), "true"),
			}
			imagePath := anyString(inputMap["image_path"])
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				session := sessionFromContext(ctx)
				firstFrame, err := uploadSingleResource(ctx, session, resources, "image", imagePath)
				if err != nil {
					return nil, err
				}
				req.FirstFrameResourceID = firstFrame.ResourceID
				var resp *mcpclient.BaseResponse
				if req.UseByConfig {
					resp, err = client.Image2VideoByConfig(ctx, session, req)
				} else {
					resp, err = client.Image2Video(ctx, session, req)
				}
				if err != nil {
					return nil, err
				}
				uploaded := map[string]any{
					"uploaded_images": resourcesToView([]*resourceclient.Resource{firstFrame}),
				}
				return &SubmitOutcome{
					ResultJSON:   buildDreaminaResultJSON("image2video", inputMap, req, uploaded, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newFrames2VideoHandler(client *mcpclient.HTTPClient, resources *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "frames2video",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			firstPath := anyString(inputMap["first_path"])
			lastPath := anyString(inputMap["last_path"])
			req := &mcpclient.Frames2VideoRequest{
				Prompt:          anyString(inputMap["prompt"]),
				Duration:        anyInt(inputMap["duration"]),
				VideoResolution: anyString(inputMap["video_resolution"]),
				ModelVersion:    anyString(inputMap["model_version"]),
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				session := sessionFromContext(ctx)
				uploaded, err := uploadImageList(ctx, session, resources, []string{firstPath, lastPath})
				if err != nil {
					return nil, err
				}
				if len(uploaded) != 2 {
					return nil, fmt.Errorf("frames2video requires 2 uploaded frames")
				}
				req.FirstFrameResourceID = uploaded[0].ResourceID
				req.LastFrameResourceID = uploaded[1].ResourceID
				resp, err := client.Frames2Video(ctx, session, req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON: buildDreaminaResultJSON("frames2video", inputMap, req, map[string]any{
						"uploaded_images": resourcesToView(uploaded),
					}, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newMultiFrame2VideoHandler(client *mcpclient.HTTPClient, resources *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "multiframe2video",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			imagePaths := anyStringSlice(inputMap["image_paths"])
			promptList, durationList, err := resolveRef2VideoTransitions(
				imagePaths,
				anyString(inputMap["prompt"]),
				anyString(inputMap["duration"]),
				anyStringSlice(inputMap["transition_prompts"]),
				anyStringSlice(inputMap["transition_durations"]),
			)
			if err != nil {
				return nil, err
			}
			req := &mcpclient.Ref2VideoRequest{
				PromptList:   promptList,
				DurationList: durationList,
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				session := sessionFromContext(ctx)
				uploaded, err := uploadImageList(ctx, session, resources, imagePaths)
				if err != nil {
					return nil, err
				}
				req.MediaResourceIDList = resourceIDs(uploaded)
				resp, err := client.Ref2Video(ctx, session, req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON: buildDreaminaResultJSON("multiframe2video", inputMap, req, map[string]any{
						"uploaded_images": resourcesToView(uploaded),
					}, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newMultiModal2VideoHandler(client *mcpclient.HTTPClient, resources *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "multimodal2video",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			req := &mcpclient.MultiModal2VideoRequest{
				Prompt:          anyString(inputMap["prompt"]),
				Duration:        anyInt(inputMap["duration"]),
				Ratio:           normalizeRatio(anyString(inputMap["ratio"])),
				VideoResolution: anyString(inputMap["video_resolution"]),
				ModelVersion:    anyString(inputMap["model_version"]),
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				ctx = resourceclient.ContextWithUploadModelVersion(ctx, req.ModelVersion)
				session := sessionFromContext(ctx)
				images, err := uploadResourceList(ctx, session, resources, "image", anyStringSlice(inputMap["image_paths"]))
				if err != nil {
					return nil, err
				}
				videos, err := uploadResourceList(ctx, session, resources, "video", anyStringSlice(inputMap["video_paths"]))
				if err != nil {
					return nil, err
				}
				audios, err := uploadResourceList(ctx, session, resources, "audio", anyStringSlice(inputMap["audio_paths"]))
				if err != nil {
					return nil, err
				}
				req.ImageResourceIDList = resourceIDs(images)
				req.VideoResourceIDList = resourceIDs(videos)
				req.AudioResourceIDList = resourceIDs(audios)
				resp, err := client.MultiModal2Video(ctx, session, req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON: buildDreaminaResultJSON("multimodal2video", inputMap, req, map[string]any{
						"uploaded_images": resourcesToView(images),
						"uploaded_videos": resourcesToView(videos),
						"uploaded_audios": resourcesToView(audios),
					}, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newText2ImageHandler(client *mcpclient.HTTPClient, _ *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "text2image",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			req := &mcpclient.Text2ImageRequest{
				Prompt:         anyString(inputMap["prompt"]),
				Ratio:          normalizeRatio(anyString(inputMap["ratio"])),
				ResolutionType: anyString(inputMap["resolution_type"]),
				ModelVersion:   anyString(inputMap["model_version"]),
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				resp, err := client.Text2Image(ctx, sessionFromContext(ctx), req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON:   buildDreaminaResultJSON("text2image", inputMap, req, nil, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newImage2ImageHandler(client *mcpclient.HTTPClient, resources *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "image2image",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			ratio, err := normalizeImage2ImageRatio(anyString(inputMap["ratio"]))
			if err != nil {
				return nil, err
			}
			req := &mcpclient.Image2ImageRequest{
				Prompt:         anyString(inputMap["prompt"]),
				Ratio:          ratio,
				ResolutionType: anyString(inputMap["resolution_type"]),
				ModelVersion:   anyString(inputMap["model_version"]),
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				session := sessionFromContext(ctx)
				uploaded, err := uploadImageList(ctx, session, resources, anyStringSlice(inputMap["image_paths"]))
				if err != nil {
					return nil, err
				}
				req.ResourceIDList = resourceIDs(uploaded)
				resp, err := client.Image2Image(ctx, session, req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON: buildDreaminaResultJSON("image2image", inputMap, req, map[string]any{
						"uploaded_images": resourcesToView(uploaded),
					}, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newImageUpscaleHandler(client *mcpclient.HTTPClient, resources *resourceclient.ByteDanceUploadClient) *HandlerEntry {
	return &HandlerEntry{
		GenTaskType: "image_upscale",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			_ = ctx
			_ = uid
			inputMap := cloneInputMap(input)
			req := &mcpclient.UpscaleRequest{
				ResolutionType: anyString(inputMap["resolution_type"]),
			}
			return newDreaminaSubmission(func(ctx context.Context) (*SubmitOutcome, error) {
				session := sessionFromContext(ctx)
				uploaded, err := uploadSingleImage(ctx, session, resources, anyString(inputMap["image_path"]))
				if err != nil {
					return nil, err
				}
				req.ResourceID = uploaded.ResourceID
				resp, err := client.Upscale(ctx, session, req)
				if err != nil {
					return nil, err
				}
				return &SubmitOutcome{
					ResultJSON: buildDreaminaResultJSON("image_upscale", inputMap, req, map[string]any{
						"uploaded_images": resourcesToView([]*resourceclient.Resource{uploaded}),
					}, resp),
					LogID:        strings.TrimSpace(resp.LogID),
					CommerceInfo: responseCommerceInfo(resp),
				}, nil
			}), nil
		},
		Query: newDreaminaQuery(client),
	}
}

func newDreaminaSubmission(commit func(ctx context.Context) (*SubmitOutcome, error)) *PreparedSubmit {
	return &PreparedSubmit{Commit: commit}
}

func uploadSingleImage(ctx context.Context, session *mcpclient.Session, resources *resourceclient.ByteDanceUploadClient, path string) (*resourceclient.Resource, error) {
	return uploadSingleResource(ctx, session, resources, "image", path)
}

func uploadSingleResource(ctx context.Context, session *mcpclient.Session, resources *resourceclient.ByteDanceUploadClient, resourceType string, path string) (*resourceclient.Resource, error) {
	uploaded, err := uploadResourceList(ctx, session, resources, resourceType, []string{path})
	if err != nil {
		return nil, err
	}
	if len(uploaded) == 0 {
		return nil, fmt.Errorf("%s resource path is required", resourceType)
	}
	return uploaded[0], nil
}

func uploadImageList(ctx context.Context, session *mcpclient.Session, resources *resourceclient.ByteDanceUploadClient, paths []string) ([]*resourceclient.Resource, error) {
	return uploadResourceList(ctx, session, resources, "image", paths)
}

func uploadResourceList(ctx context.Context, session *mcpclient.Session, resources *resourceclient.ByteDanceUploadClient, resourceType string, paths []string) ([]*resourceclient.Resource, error) {
	if resources == nil {
		return nil, fmt.Errorf("resource client is not configured")
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return resources.UploadResource(ctx, session, resourceType, paths)
}

func resolveRef2VideoTransitions(imagePaths []string, prompt string, duration string, transitionPrompts []string, transitionDurations []string) ([]string, []float64, error) {
	imageCount := len(imagePaths)
	if imageCount < 2 {
		return nil, nil, fmt.Errorf("multiframe2video requires at least 2 images")
	}

	prompts := append([]string(nil), transitionPrompts...)
	if len(prompts) == 0 && strings.TrimSpace(prompt) != "" {
		prompts = []string{strings.TrimSpace(prompt)}
	}
	if len(prompts) > 0 && len(prompts) != imageCount-1 {
		return nil, nil, fmt.Errorf("multiframe2video requires %d transition prompts for %d images", imageCount-1, imageCount)
	}

	var durations []float64
	if len(transitionDurations) == 0 && strings.TrimSpace(duration) != "" {
		transitionDurations = []string{strings.TrimSpace(duration)}
	}
	if len(transitionDurations) > 0 {
		if len(transitionDurations) != imageCount-1 {
			return nil, nil, fmt.Errorf("multiframe2video requires %d transition durations for %d images", imageCount-1, imageCount)
		}
		durations = make([]float64, 0, len(transitionDurations))
		for _, value := range transitionDurations {
			parsed, err := parseDurationFloat(value)
			if err != nil {
				return nil, nil, err
			}
			durations = append(durations, parsed)
		}
	} else if imageCount > 1 {
		durations = make([]float64, imageCount-1)
		for i := range durations {
			durations[i] = 3
		}
	}
	return prompts, durations, nil
}

func normalizeImage2ImageRatio(ratio string) (string, error) {
	ratio = normalizeRatio(ratio)
	return ratio, nil
}

func normalizeRatio(ratio string) string {
	ratio = strings.TrimSpace(ratio)
	replacer := strings.NewReplacer("/", ":", "_", ":", "x", ":", "X", ":")
	return replacer.Replace(ratio)
}

func newDreaminaQuery(client *mcpclient.HTTPClient) func(ctx context.Context, localTask any) (*RemoteQueryResult, error) {
	return func(ctx context.Context, localTask any) (*RemoteQueryResult, error) {
		taskValue, err := toLocalTask(localTask)
		if err != nil {
			return nil, err
		}
		queryCount := nextRecoveredQueryCount(taskValue.ResultJSON)
		historyID := queryHistoryIDFromTask(taskValue)
		submitID := strings.TrimSpace(taskValue.SubmitID)
		var historyItem map[string]any
		var historyQuery map[string]any
		if client != nil {
			req := &mcpclient.GetHistoryByIdsRequest{}
			if submitID != "" {
				req.SubmitIDs = []string{submitID}
			}
			if historyID != "" && historyID != submitID {
				req.HistoryIDs = []string{historyID}
			}
			historyResp, _ := client.GetHistoryByIds(ctx, sessionFromContext(ctx), req)
			matchID := historyID
			if strings.TrimSpace(matchID) == "" {
				matchID = submitID
			}
			historyItem = pickRecoveredHistoryItem(historyResp, matchID)
			historyQuery = historyQueryMetadata(historyResp)
		}
		// 旧程序里 image_upscale 的 query_result 对 history 命中非常保守：
		// 即使远端能查到完成态，也仍优先保留本地结果并输出 querying/logid。
		// 这里保留 history_query 的“查询成功”哨兵，但不再把 history 正文反写进结果。
		if taskValue != nil && strings.TrimSpace(taskValue.GenTaskType) == "image_upscale" {
			historyItem = nil
		}

		status, queueStatus, progress := deriveRecoveredQueryState(taskValue, historyItem, historyQuery)
		failReason := strings.TrimSpace(taskValue.FailReason)
		if status == 3 {
			failReason = recoveredHistoryFailReason(historyItem, failReason)
		}

		resultJSON := strings.TrimSpace(taskValue.ResultJSON)
		if resultJSON == "" {
			input := map[string]any{}
			if taskValue.Request != nil {
				input = taskValue.Request.Values
			}
			resultJSON = buildSkeletonResultJSON(taskValue.GenTaskType, input)
		}
		resultJSON = updateRecoveredQueryResultJSON(resultJSON, taskValue, historyID, queueStatus, progress, queryCount, historyItem, historyQuery)
		return &RemoteQueryResult{
			Status:         status,
			ResultJSON:     resultJSON,
			FallbackResult: resultJSON,
			FailReason:     failReason,
			CommerceInfo:   taskValue.CommerceInfo,
		}, nil
	}
}

func buildTaskQueueInfo(resp *mcpclient.BaseResponse, genTaskType string) any {
	// 提交后的排队信息仍然需要兼容恢复期的本地任务流。
	// 这里优先从 resp.Recovered 取提交态元数据，只在旧结构下才回落到 resp.Data。
	historyID := responseHistoryID(resp)
	queueStatus := responseQueueStatus(resp)
	if queueStatus == "" {
		queueStatus = "submitted"
	}
	progress := responseQueueProgress(resp)
	if progress <= 0 {
		progress = 5
	}
	queueInfo := map[string]any{
		"gen_task_type": genTaskType,
		"log_id":        strings.TrimSpace(responseLogID(resp)),
		"queue_status":  queueStatus,
		"history_id":    historyID,
		"progress":      progress,
		"query_count":   0,
		"recovered":     true,
	}
	if submittedAt := responseSubmittedAt(resp); submittedAt > 0 {
		queueInfo["submitted_at"] = submittedAt
	}
	return queueInfo
}

func dreaminaFailureReason(err error) string {
	if err == nil {
		return ""
	}
	if apiErr, ok := err.(*mcpclient.APIError); ok {
		return strings.TrimSpace(apiErr.Error())
	}
	return strings.TrimSpace(err.Error())
}

// buildDreaminaResultJSON 构造提交完成后落盘的恢复结果 JSON，统一保留请求、队列信息和响应元数据。
func buildDreaminaResultJSON(genTaskType string, input map[string]any, request any, uploaded map[string]any, resp *mcpclient.BaseResponse) string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(buildSkeletonResultJSON(genTaskType, input)), &payload); err != nil {
		payload = map[string]any{
			"gen_task_type": genTaskType,
			"recovered":     true,
			"input":         input,
		}
	}
	payload["backend"] = "dreamina"
	payload["request"] = request
	payload["queue_info"] = buildTaskQueueInfo(resp, genTaskType)
	if resp != nil {
		payload["response"] = map[string]any{
			"code":    strings.TrimSpace(resp.Code),
			"message": strings.TrimSpace(resp.Message),
			"log_id":  strings.TrimSpace(resp.LogID),
			"data":    resp.Data,
		}
		if recovered := responseRecoveredMap(resp); len(recovered) > 0 {
			payload["response"].(map[string]any)["recovered"] = recovered
		}
		payload["log_id"] = strings.TrimSpace(resp.LogID)
	}
	for key, value := range uploaded {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return buildSkeletonResultJSON(genTaskType, input)
	}
	return string(body)
}

// toLocalTask 把查询层拿到的本地任务对象统一转换成 *AIGCTask。
func toLocalTask(v any) (*task.AIGCTask, error) {
	switch value := v.(type) {
	case *task.AIGCTask:
		return value, nil
	case task.AIGCTask:
		return &value, nil
	default:
		return nil, fmt.Errorf("local task has unexpected type %T", v)
	}
}

// anyInt 把若干常见动态类型尽量收敛成 int，供恢复 JSON 和 history 字段解析复用。
func anyInt(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		n := 0
		for _, ch := range strings.TrimSpace(value) {
			if ch < '0' || ch > '9' {
				return 0
			}
			n = n*10 + int(ch-'0')
		}
		return n
	default:
		return 0
	}
}

func anyFloat64(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		if n, err := value.Float64(); err == nil {
			return n
		}
	case string:
		if n, err := parseDurationFloat(strings.TrimSpace(value)); err == nil {
			return n
		}
	}
	return 0
}

func parseDurationFloat(s string) (float64, error) {
	var whole float64
	var frac float64
	var scale float64 = 1
	parts := strings.SplitN(strings.TrimSpace(s), ".", 2)
	for _, ch := range parts[0] {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid float %q", s)
		}
		whole = whole*10 + float64(ch-'0')
	}
	if len(parts) == 2 {
		for _, ch := range parts[1] {
			if ch < '0' || ch > '9' {
				return 0, fmt.Errorf("invalid float %q", s)
			}
			scale *= 10
			frac = frac*10 + float64(ch-'0')
		}
	}
	return whole + frac/scale, nil
}

// resourceIDs 提取上传资源列表中的 resource_id，供提交请求组装使用。
func resourceIDs(items []*resourceclient.Resource) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.ResourceID) == "" {
			continue
		}
		out = append(out, item.ResourceID)
	}
	return out
}

// resourcesToView 把上传资源转换成适合落盘和展示的轻量视图。
func resourcesToView(items []*resourceclient.Resource) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		view := map[string]any{
			"resource_id": item.ResourceID,
			"type":        item.ResourceType,
		}
		if strings.TrimSpace(item.Name) != "" {
			view["name"] = item.Name
		}
		if item.Size > 0 {
			view["size"] = item.Size
		}
		if item.Scene > 0 {
			view["scene"] = item.Scene
		}
		if strings.TrimSpace(item.Kind) != "" {
			view["kind"] = item.Kind
		}
		if strings.TrimSpace(item.MimeType) != "" {
			view["mime_type"] = item.MimeType
		}
		if len(item.UploadSummary) > 0 {
			view["upload_summary"] = item.UploadSummary
		}
		out = append(out, view)
	}
	return out
}

// responseLogID 安全读取提交响应里的 log_id。
func responseLogID(resp *mcpclient.BaseResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.LogID)
}

func responseCommerceInfo(resp *mcpclient.BaseResponse) any {
	if resp == nil {
		return nil
	}
	data, ok := resp.Data.(map[string]any)
	if !ok || len(data) == 0 {
		return nil
	}
	if commerce, ok := data["commerce_info"].(map[string]any); ok && len(commerce) > 0 {
		return commerce
	}
	if commerce, ok := data["commerceInfo"].(map[string]any); ok && len(commerce) > 0 {
		return commerce
	}
	return nil
}

func nextRecoveredQueryCount(resultJSON string) int {
	root := parseRecoveredResultRoot(resultJSON)
	queueInfo, _ := root["queue_info"].(map[string]any)
	// 查询次数只从已有结果 JSON 里递增，不引入额外持久化状态。
	// 这样恢复版在 task.json / tasks.db 兼容路径下都能保持同一套 query fallback 语义。
	count := anyInt(queueInfo["query_count"]) + 1
	if count <= 0 {
		return 1
	}
	return count
}

func queryHistoryIDFromTask(taskValue *task.AIGCTask) string {
	if taskValue == nil {
		return ""
	}
	root := parseRecoveredResultRoot(taskValue.ResultJSON)
	if response, ok := root["response"].(map[string]any); ok {
		if recovered, ok := response["recovered"].(map[string]any); ok {
			// 提交响应里的 recovered 是恢复期最稳定的提交元数据来源，优先级高于旧版 response.data/queue_info。
			// 这样可以避免 data 被拆壳或 schema 漂移后，又退回到较旧的 history_id。
			if historyID := firstCleanRecoveredValue(recovered, "history_id", "historyId", "HistoryID", "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID"); historyID != "" {
				return historyID
			}
		}
		if data, ok := response["data"].(map[string]any); ok {
			if historyID := firstCleanRecoveredValue(data, "history_id", "historyId", "HistoryID", "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID"); historyID != "" {
				return historyID
			}
		}
	}
	if queueInfo, ok := root["queue_info"].(map[string]any); ok {
		if historyID := firstCleanRecoveredValue(queueInfo, "history_id", "historyId", "HistoryID"); historyID != "" {
			return historyID
		}
	}
	// 最后才回落 submit_id，且不再本地伪造 hist_*。
	// 这里保留的是“远端别名复用”，不是“本地构造一个看起来像 history_id 的值”。
	return strings.TrimSpace(taskValue.SubmitID)
}

func pickRecoveredHistoryItem(resp *mcpclient.GetHistoryByIdsResponse, historyID string) map[string]any {
	if resp == nil || len(resp.Items) == 0 {
		return nil
	}
	historyID = strings.TrimSpace(historyID)
	if item, ok := resp.Items[historyID]; ok && item != nil {
		return item.View()
	}
	// 一部分 keyed-object 会用 submit_id/task_id 做 key，而不是显式 history_id。
	// 这里允许再按视图里的几个主键别名扫描一次，但给定 history_id 时绝不退回“随便挑第一项”。
	for _, item := range resp.Items {
		if item == nil {
			continue
		}
		view := item.View()
		if historyID != "" && historyIDMatchesView(view, historyID) {
			return view
		}
	}
	if historyID != "" {
		return nil
	}
	for _, item := range resp.Items {
		if item != nil {
			return item.View()
		}
	}
	return nil
}

// historyIDMatchesView 判断一个 history 视图是否命中指定的 history_id/submit_id/task_id 别名。
func historyIDMatchesView(view map[string]any, historyID string) bool {
	if len(view) == 0 || strings.TrimSpace(historyID) == "" {
		return false
	}
	for _, key := range []string{"history_id", "submit_id", "task_id"} {
		if cleanRecoveredString(view[key]) == historyID {
			return true
		}
	}
	return false
}

func deriveRecoveredQueryState(taskValue *task.AIGCTask, historyItem map[string]any, historyQuery map[string]any) (int, string, int) {
	if taskValue == nil {
		return 3, "failed", 100
	}
	// query 恢复优先级始终是：
	// 1. 真实 history 远端状态
	// 2. 已经拿到的本地成功媒体
	// 3. 本地已知终态
	// 4. 最后才是弱估算 fallback
	// 这样可以尽量避免恢复链路把“后端未知”误写成“本地接近完成”。
	if status, queueStatus, progress, ok := deriveRemoteHistoryState(historyItem); ok {
		return status, queueStatus, progress
	}
	// history 查询已成功返回但没有命中任务时，不再继续用本地 query_count 推高进度。
	// 这种场景更接近“后端当前还没给出可用 history”，继续抬高 33/66/95 会制造额外成功假象。
	if historyQuerySucceeded(historyQuery) {
		queueStatus, progress := existingRecoveredQueueState(taskValue)
		if queueStatus == "" {
			queueStatus = "submitted"
		}
		if progress < 0 {
			progress = 0
		}
		// history 查询明确成功但没有命中任务时，不把“running + 0%”继续保留成运行态。
		// 这种本地状态缺少远端命中和正进度支撑，更接近“已提交但后端暂未返回可用 history”。
		if queueStatus == "running" && progress <= 0 {
			queueStatus = "submitted"
		}
		return 1, queueStatus, progress
	}
	if taskResultHasMedia(taskValue.ResultJSON) {
		return 2, "success", 100
	}
	switch strings.TrimSpace(taskValue.GenStatus) {
	case "failed":
		return 3, "failed", 100
	case "success":
		return 2, "success", 100
	}
	existingQueueStatus, existingProgress := existingRecoveredQueueState(taskValue)
	// history 请求失败时，恢复版现在不再按 query_count 把本地进度一路推到 33/66/95。
	// 这样即使连续多轮拿不到远端 history，也只保留“最小提交态”或“已经落盘的旧进度”，
	// 避免 CLI 仅凭本地轮询次数制造出“后端正在稳定推进”的假象。
	progress := 8
	queueStatus := "submitted"
	// history 请求失败时，只允许在已有进度基础上轻推，不能把本地已记录的更高进度回退掉。
	// 这样即使某一轮远端暂时超时，也不会把用户刚看到的运行态 72% 又降回提交态 33%。
	if existingProgress > progress {
		progress = existingProgress
	}
	if existingQueueStatus != "" && existingQueueStatus != "success" && existingQueueStatus != "failed" {
		// 只有已有正进度时才继续保留运行态。
		// 单独一个本地 running 且 progress=0 不足以说明远端真的进入执行阶段，
		// history 本轮又失败时，退回最小 submitted 更接近“远端状态未知”。
		if existingProgress > 0 {
			queueStatus = existingQueueStatus
		}
	}
	return 1, queueStatus, progress
}

func historyQuerySucceeded(historyQuery map[string]any) bool {
	if len(historyQuery) == 0 {
		return false
	}
	// 这里故意只把 code=0 视为“远端查询成功”。
	// 有 message/log_id 但 code 非 0 的场景仍按失败处理，避免把后端错误误当成“正常空结果”。
	return strings.TrimSpace(fmt.Sprint(historyQuery["code"])) == "0"
}

// existingRecoveredQueueState 读取本地 result_json 中已落盘的 queue_status/progress，作为恢复回退基线。
func existingRecoveredQueueState(taskValue *task.AIGCTask) (string, int) {
	if taskValue == nil {
		return "", 0
	}
	root := parseRecoveredResultRoot(taskValue.ResultJSON)
	queueInfo, _ := root["queue_info"].(map[string]any)
	if len(queueInfo) == 0 {
		return "", 0
	}
	return normalizeRecoveredQueueStatus(firstNormalizedHistoryStatus(
		queueInfo,
		"queue_status", "queueStatus", "QueueStatus",
		"queue_state", "queueState", "QueueState",
		"status", "Status",
	)), anyProgressInt(queueInfo["progress"])
}

// normalizeRecoveredQueueStatus 把远端或本地恢复状态统一压成 submitted/running/success/failed 四类。
func normalizeRecoveredQueueStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "queueing", "pending", "waiting":
		return "submitted"
	case "processing", "in_progress", "generating":
		return "running"
	case "succeeded", "done", "completed", "complete", "finished", "finish":
		return "success"
	case "fail", "failed", "error", "canceled", "cancelled", "timeout":
		return "failed"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func taskResultHasMedia(resultJSON string) bool {
	root := parseRecoveredResultRoot(resultJSON)
	if len(root) == 0 {
		return false
	}
	// 这里只判断最终结果里是否已经有可用媒体地址，不读取 queue/history 中的中间态 URL。
	// 这样可以保证“本地已成功”只建立在用户最终能拿到资源的结果 JSON 上。
	for _, key := range []string{"images", "videos"} {
		switch list := root[key].(type) {
		case []map[string]any:
			for _, item := range list {
				if isUsableMediaURL(anyString(item["url"])) {
					return true
				}
			}
		case []any:
			for _, raw := range list {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if isUsableMediaURL(anyString(item["url"])) {
					return true
				}
			}
		}
	}
	return false
}

func deriveRemoteHistoryState(historyItem map[string]any) (int, string, int, bool) {
	if len(historyItem) == 0 {
		return 0, "", 0, false
	}
	// history 里经常同时出现 status 和 queue_status，两者语义并不完全稳定。
	// 这里优先吃 queue_status；拿不到时才回退到 status/gen_status/state。
	queueStatus := firstNormalizedHistoryStatus(
		historyItem,
		"queue_status", "queueStatus", "QueueStatus",
		"queue_state", "queueState", "QueueState",
	)
	statusText := firstNormalizedHistoryStatus(
		historyItem,
		"status", "Status",
		"gen_status", "genStatus", "GenStatus",
		"state", "State",
	)
	progress := remoteHistoryProgress(historyItem)
	if queueStatus == "" {
		queueStatus = statusText
	}
	if historyIndicatesFailure(historyItem) {
		if queueStatus == "" {
			queueStatus = "Finish"
		}
		return 3, queueStatus, 100, true
	}
	if historyStateCode(historyItem) == 50 && historyItemHasMedia(historyItem) {
		if queueStatus == "" {
			queueStatus = "Finish"
		}
		return 2, queueStatus, 100, true
	}
	if historyStateCode(historyItem) == 45 && queueStatus == "" {
		if progress > 0 {
			return 1, "running", progress, true
		}
		return 1, "running", 55, true
	}
	if queueStatus == "" && historyItemHasMedia(historyItem) {
		return 2, "success", 100, true
	}
	switch queueStatus {
	case "success", "succeeded", "done", "completed", "complete", "finished", "finish":
		return 2, "success", 100, true
	case "failed", "fail", "error", "canceled", "cancelled", "timeout":
		return 3, "failed", 100, true
	case "submitted", "queued", "queueing", "pending", "waiting":
		if progress > 0 {
			return 1, "submitted", progress, true
		}
		return 1, "submitted", 8, true
	case "querying", "running", "processing", "in_progress", "generating":
		if progress > 0 {
			return 1, "running", progress, true
		}
		return 1, "running", 55, true
	}
	return 0, "", 0, false
}

func historyIndicatesFailure(historyItem map[string]any) bool {
	if len(historyItem) == 0 {
		return false
	}
	for _, key := range []string{
		"fail_reason", "failReason", "FailReason",
		"fail_msg", "failMsg", "FailMsg",
		"fail_starling_message", "failStarlingMessage", "FailStarlingMessage",
		"error_message", "errorMessage", "ErrorMessage",
	} {
		if value := cleanRecoveredString(historyItem[key]); value != "" && !strings.EqualFold(value, "success") {
			return true
		}
	}
	for _, key := range []string{"failed_item_list", "failedItemList", "FailedItemList"} {
		switch items := historyItem[key].(type) {
		case []any:
			if len(items) > 0 {
				return true
			}
		}
	}
	switch historyStateCode(historyItem) {
	case 30:
		return true
	default:
		return false
	}
}

func historyStateCode(historyItem map[string]any) int {
	if len(historyItem) == 0 {
		return 0
	}
	for _, key := range []string{"status", "Status", "task_status", "taskStatus", "TaskStatus"} {
		if code := anyInt(historyItem[key]); code > 0 {
			return code
		}
	}
	if taskValue, ok := historyItem["task"].(map[string]any); ok {
		for _, key := range []string{"status", "Status", "task_status", "taskStatus", "TaskStatus"} {
			if code := anyInt(taskValue[key]); code > 0 {
				return code
			}
		}
	}
	return 0
}

func recoveredHistoryFailReason(historyItem map[string]any, fallback string) string {
	if len(historyItem) == 0 {
		return strings.TrimSpace(fallback)
	}
	if historyIndicatesFailure(historyItem) {
		return "generation failed: final generation failed"
	}
	for _, key := range []string{
		"fail_reason", "failReason", "FailReason",
		"fail_msg", "failMsg", "FailMsg",
		"fail_starling_message", "failStarlingMessage", "FailStarlingMessage",
		"error_message", "errorMessage", "ErrorMessage",
		"errmsg", "err_msg", "message", "Message",
	} {
		if value := cleanRecoveredString(historyItem[key]); value != "" && !strings.EqualFold(value, "success") {
			return value
		}
	}
	return strings.TrimSpace(fallback)
}

func remoteHistoryProgress(historyItem map[string]any) int {
	if len(historyItem) == 0 {
		return 0
	}
	// query/history 返回有时会直接给 progress，有时会放在 queue 中。
	// 这里优先吃远端值，只有拿不到时才回落到本地估算，避免把真实进度抹平成固定 8/55。
	candidates := []any{
		historyItem["progress"],
		historyItem["Progress"],
		historyItem["percent"],
		historyItem["Percent"],
	}
	for _, rawQueue := range []any{historyItem["queue"], historyItem["Queue"]} {
		if queue, ok := rawQueue.(map[string]any); ok {
			candidates = append(candidates,
				queue["progress"], queue["Progress"],
				queue["percent"], queue["Percent"],
			)
		}
	}
	for _, candidate := range candidates {
		progress := anyProgressInt(candidate)
		switch {
		case progress <= 0:
			continue
		// success/failed 终态在外层已经单独处理。
		// 这里把 100% 收到 99%，是为了避免“running + 100%”在展示层看起来像已经成功完成。
		case progress >= 100:
			return 99
		default:
			return progress
		}
	}
	return 0
}

// firstNormalizedHistoryStatus 优先从 history 本层读取状态，再回退到 queue 子结构中的同名字段。
func firstNormalizedHistoryStatus(historyItem map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := normalizeHistoryStatus(historyItem[key]); text != "" {
			return text
		}
	}
	for _, rawQueue := range []any{historyItem["queue"], historyItem["Queue"]} {
		if queue, ok := rawQueue.(map[string]any); ok {
			for _, key := range keys {
				if text := normalizeHistoryStatus(queue[key]); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

// normalizeHistoryStatus 清理 history 状态字符串，去掉空值和 <nil> 噪音。
func normalizeHistoryStatus(value any) string {
	text := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

// anyProgressInt 把 progress/percent 这类动态字段统一解析成整数进度值。
func anyProgressInt(value any) int {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		text = strings.TrimSuffix(text, "%")
		text = strings.TrimSpace(text)
		if text == "" {
			return 0
		}
		n := 0
		for _, ch := range text {
			if ch < '0' || ch > '9' {
				return 0
			}
			n = n*10 + int(ch-'0')
		}
		return n
	default:
		return anyInt(value)
	}
}

func historyItemHasMedia(historyItem map[string]any) bool {
	if anyString(historyItem["image_url"]) != "" || anyString(historyItem["video_url"]) != "" {
		return true
	}
	// history 只要已经返回媒体列表，就可以视作远端成功的强信号。
	// 这里不要求列表内每个字段都完全标准化，后续 sanitize/merge 会再做净化。
	for _, key := range []string{"images", "videos"} {
		switch list := historyItem[key].(type) {
		case []map[string]any:
			if len(list) > 0 {
				return true
			}
		case []any:
			if len(list) > 0 {
				return true
			}
		}
	}
	return false
}

func updateRecoveredQueryResultJSON(resultJSON string, taskValue *task.AIGCTask, historyID string, queueStatus string, progress int, queryCount int, historyItem map[string]any, historyQuery map[string]any) string {
	root := parseRecoveredResultRoot(resultJSON)
	if len(root) == 0 {
		input := map[string]any{}
		if taskValue != nil && taskValue.Request != nil {
			input = taskValue.Request.Values
		}
		root = parseRecoveredResultRoot(buildSkeletonResultJSON(taskValue.GenTaskType, input))
	}
	root["recovered"] = true
	root["gen_status"] = normalizedRecoveredGenStatus(queueStatus)
	cleanHistoryID := cleanRecoveredString(historyID)
	persistHistoryQuery := shouldPersistHistoryQuery(historyQuery)
	persistQueryTimestamp := historyItem != nil || persistHistoryQuery
	queriedAt := time.Now().Unix()
	queueInfo, _ := root["queue_info"].(map[string]any)
	if queueInfo == nil {
		queueInfo = map[string]any{}
	}
	queueInfo["queue_status"] = queueStatus
	if cleanHistoryID != "" {
		queueInfo["history_id"] = cleanHistoryID
	} else if cleanRecoveredString(queueInfo["history_id"]) == "" {
		delete(queueInfo, "history_id")
	}
	queueInfo["progress"] = progress
	queueInfo["query_count"] = queryCount
	if persistQueryTimestamp {
		queueInfo["last_queried_at"] = queriedAt
	} else {
		delete(queueInfo, "last_queried_at")
	}
	queueInfo["recovered"] = true
	if historyItem != nil {
		mergeRecoveredHistoryQueueInfo(queueInfo, historyItem)
		// 把命中的 history 正文原样保进 queue_info，后续 view/query_result 可以直接看到“远端依据是什么”，
		// 也方便下一轮继续对照 schema，而不只剩下被压平后的 queue_status/progress。
		queueInfo["history"] = historyItem
	}
	if persistHistoryQuery {
		// history 查询元数据单独保留，用来区分“history 真返回空”和“history 请求本身失败”。
		queueInfo["history_query"] = historyQuery
	} else {
		delete(queueInfo, "history_query")
	}
	root["queue_info"] = queueInfo
	// 先保留本地骨架里的尺寸/时长等元数据，再让本轮 history 覆盖 URL。
	// 最后统一清一次结果，避免把 history 里的空 URL 或预览残留带出去。
	mergeRecoveredHistoryMedia(root, historyItem)
	sanitizeRecoveredMedia(root)
	if response, ok := root["response"].(map[string]any); ok {
		data, _ := response["data"].(map[string]any)
		if data == nil {
			data = map[string]any{}
		}
		if cleanHistoryID != "" {
			if cleanRecoveredString(data["history_id"]) == "" {
				data["history_id"] = cleanHistoryID
			}
		} else if cleanRecoveredString(data["history_id"]) == "" {
			delete(data, "history_id")
		}
		data["query"] = map[string]any{
			"queue_status":    queueStatus,
			"progress":        progress,
		}
		if persistQueryTimestamp {
			data["query"].(map[string]any)["last_queried_at"] = queriedAt
		}
		if historyItem != nil {
			data["history"] = historyItem
		}
		if persistHistoryQuery {
			data["query"].(map[string]any)["history_query"] = historyQuery
		}
		delete(data, "history_query")
		response["data"] = data
		root["response"] = response
	}
	body, err := json.Marshal(root)
	if err != nil {
		return resultJSON
	}
	return string(body)
}

func normalizedRecoveredGenStatus(queueStatus string) string {
	switch strings.ToLower(strings.TrimSpace(queueStatus)) {
	case "success", "succeeded", "done", "completed", "complete", "finished", "finish":
		return "success"
	case "failed", "fail", "error", "canceled", "cancelled", "timeout":
		return "failed"
	default:
		return "querying"
	}
}

func mergeRecoveredHistoryMedia(root map[string]any, historyItem map[string]any) {
	if len(root) == 0 || len(historyItem) == 0 {
		return
	}
	// history 媒体一旦命中，就让它覆盖结果根层的 images/videos。
	// query 阶段这里信任的是“最新一次远端查询结果”，而不是提交时留下的旧骨架数据。
	if videos := preferredVideoItemsFromHistory(historyItem); len(videos) > 0 {
		root["videos"] = mergeRecoveredMediaItems(root["videos"], videos, "video")
		// 视频 history 经常把首帧或封面图一并带回来；原程序在成功 query_result 中不会把它们塞进 images。
		root["images"] = []map[string]any{}
		return
	}
	if images := preferredImageItemsFromHistory(historyItem); len(images) > 0 {
		root["images"] = mergeRecoveredMediaItems(root["images"], images, "image")
	}
}

func mergeRecoveredMediaItems(existing any, recovered []map[string]any, mediaType string) []map[string]any {
	if len(recovered) == 0 {
		return nil
	}
	base := recoveredMediaList(existing)
	if len(base) == 0 {
		return recovered
	}
	out := make([]map[string]any, 0, len(base))
	for idx, item := range base {
		merged := cloneRecoveredMediaItem(item)
		if idx < len(recovered) {
			overlayRecoveredMediaItem(merged, recovered[idx])
		}
		merged["type"] = mediaType
		out = append(out, merged)
	}
	return out
}

func overlayRecoveredMediaItem(base map[string]any, recovered map[string]any) {
	if len(base) == 0 || len(recovered) == 0 {
		return
	}
	for key, value := range recovered {
		switch key {
		case "url", "image_url", "video_url", "cover_url":
			if cleanRecoveredString(value) != "" {
				base[key] = value
			}
		default:
			if cleanRecoveredString(base[key]) == "" && cleanRecoveredString(value) != "" {
				base[key] = value
			}
		}
	}
}

func recoveredMediaList(v any) []map[string]any {
	switch list := v.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			out = append(out, cloneRecoveredMediaItem(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(list))
		for _, raw := range list {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, cloneRecoveredMediaItem(item))
		}
		return out
	default:
		return nil
	}
}

func cloneRecoveredMediaItem(item map[string]any) map[string]any {
	if len(item) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(item))
	for key, value := range item {
		out[key] = value
	}
	return out
}

func preferredVideoItemsFromHistory(historyItem map[string]any) []map[string]any {
	candidates := append([]map[string]any{}, mediaItemsFromImageOrigins(historyItem)...)
	candidates = append(candidates, mediaItemsFromHistoryItemOrigins(historyItem)...)
	candidates = append(candidates, mediaItemsFromHistory(historyItem, "videos", "video_url", "video")...)
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	bestScore := historyVideoCandidateScore(best)
	for _, item := range candidates[1:] {
		if score := historyVideoCandidateScore(item); score > bestScore {
			best = item
			bestScore = score
		}
	}
	return []map[string]any{best}
}

func preferredImageItemsFromHistory(historyItem map[string]any) []map[string]any {
	candidates := append([]map[string]any{}, mediaItemsFromHistoryItemGeneratedImages(historyItem)...)
	candidates = append(candidates, mediaItemsFromHistoryGeneratedImages(historyItem)...)
	candidates = append(candidates, mediaItemsFromHistory(historyItem, "images", "image_url", "image")...)
	if len(candidates) == 0 {
		return nil
	}
	sorted := make([]map[string]any, len(candidates))
	copy(sorted, candidates)
	sort.SliceStable(sorted, func(i, j int) bool {
		return historyImageCandidateScore(sorted[i]) > historyImageCandidateScore(sorted[j])
	})
	return sorted
}

func mediaItemsFromImageOrigins(historyItem map[string]any) []map[string]any {
	var items []any
	switch list := historyItem["images"].(type) {
	case []any:
		items = list
	case []map[string]any:
		items = make([]any, 0, len(list))
		for _, item := range list {
			items = append(items, item)
		}
	default:
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	for _, raw := range items {
		imageItem, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		origin := recoveredOriginMap(imageItem["origin"])
		if len(origin) == 0 {
			continue
		}
		url := anyString(firstRecoveredOriginValue(origin, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "url", "URL"))
		if !isUsableMediaURL(url) {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		video := map[string]any{
			"url":       url,
			"video_url": url,
			"type":      "video",
		}
		if fps := anyInt(firstRecoveredOriginValue(origin, "fps", "FPS")); fps > 0 {
			video["fps"] = fps
		}
		if width := anyInt(firstRecoveredOriginValue(origin, "width", "Width")); width > 0 {
			video["width"] = width
		}
		if height := anyInt(firstRecoveredOriginValue(origin, "height", "Height")); height > 0 {
			video["height"] = height
		}
		if format := anyString(firstRecoveredOriginValue(origin, "format", "Format", "file_format", "fileFormat", "FileFormat")); format != "" {
			video["format"] = format
		}
		if duration := anyFloat64(firstRecoveredOriginValue(origin, "duration", "Duration")); duration > 0 {
			video["duration"] = duration
		}
		if coverURL := anyString(firstRecoveredOriginValue(origin, "cover_url", "coverUrl", "CoverURL")); coverURL != "" {
			video["cover_url"] = coverURL
		}
		out = append(out, video)
	}
	return out
}

func mediaItemsFromHistoryItemOrigins(historyItem map[string]any) []map[string]any {
	var items []any
	switch list := historyItem["item_list"].(type) {
	case []any:
		items = list
	case []map[string]any:
		items = make([]any, 0, len(list))
		for _, item := range list {
			items = append(items, item)
		}
	default:
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		videoNode, _ := item["video"].(map[string]any)
		if len(videoNode) == 0 {
			continue
		}
		transcoded, _ := videoNode["transcoded_video"].(map[string]any)
		if len(transcoded) == 0 {
			continue
		}
		origin := recoveredOriginMap(transcoded["origin"])
		if len(origin) == 0 {
			continue
		}
		url := anyString(firstRecoveredOriginValue(origin, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "url", "URL"))
		if !isUsableMediaURL(url) {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		video := map[string]any{
			"url":       url,
			"video_url": url,
			"type":      "video",
		}
		if fps := anyInt(firstRecoveredOriginValue(origin, "fps", "FPS")); fps > 0 {
			video["fps"] = fps
		}
		if width := anyInt(firstRecoveredOriginValue(origin, "width", "Width")); width > 0 {
			video["width"] = width
		}
		if height := anyInt(firstRecoveredOriginValue(origin, "height", "Height")); height > 0 {
			video["height"] = height
		}
		if format := anyString(firstRecoveredOriginValue(origin, "format", "Format", "file_format", "fileFormat", "FileFormat")); format != "" {
			video["format"] = format
		}
		duration := anyFloat64(firstRecoveredOriginValue(origin, "duration", "Duration"))
		if duration <= 0 {
			duration = historyVideoModelDuration(videoNode)
		}
		if duration > 0 {
			video["duration"] = duration
		}
		if coverURL := anyString(firstRecoveredOriginValue(origin, "cover_url", "coverUrl", "CoverURL")); coverURL != "" {
			video["cover_url"] = coverURL
		}
		out = append(out, video)
	}
	return out
}

func mediaItemsFromHistoryGeneratedImages(historyItem map[string]any) []map[string]any {
	return mediaItemsFromImageNode(historyItem["image"])
}

func mediaItemsFromHistoryItemGeneratedImages(historyItem map[string]any) []map[string]any {
	var items []any
	switch list := historyItem["item_list"].(type) {
	case []any:
		items = list
	case []map[string]any:
		items = make([]any, 0, len(list))
		for _, item := range list {
			items = append(items, item)
		}
	default:
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, image := range mediaItemsFromImageNode(item["image"]) {
			url := anyString(image["url"])
			if !isUsableMediaURL(url) {
				continue
			}
			if _, exists := seen[url]; exists {
				continue
			}
			seen[url] = struct{}{}
			out = append(out, image)
		}
	}
	return out
}

func mediaItemsFromImageNode(v any) []map[string]any {
	node, ok := v.(map[string]any)
	if !ok || len(node) == 0 {
		return nil
	}
	var items []any
	switch list := node["large_images"].(type) {
	case []any:
		items = list
	case []map[string]any:
		items = make([]any, 0, len(list))
		for _, item := range list {
			items = append(items, item)
		}
	default:
		if url := anyString(firstNonEmptyHistoryMediaValue(node, "image", "image_url", "url")); isUsableMediaURL(url) {
			return []map[string]any{buildRecoveredImageView(node, url)}
		}
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		url := anyString(firstNonEmptyHistoryMediaValue(item, "image", "image_url", "url"))
		if !isUsableMediaURL(url) {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		out = append(out, buildRecoveredImageView(item, url))
	}
	return out
}

func buildRecoveredImageView(item map[string]any, url string) map[string]any {
	view := map[string]any{
		"url":       url,
		"image_url": url,
		"type":      "image",
	}
	if width := anyInt(item["width"]); width > 0 {
		view["width"] = width
	}
	if height := anyInt(item["height"]); height > 0 {
		view["height"] = height
	}
	if format := anyString(item["format"]); format != "" {
		view["format"] = format
	}
	return view
}

func historyVideoModelDuration(videoNode map[string]any) float64 {
	if len(videoNode) == 0 {
		return 0
	}
	videoModel := parseHistoryVideoModel(videoNode["video_model"])
	if len(videoModel) == 0 {
		return 0
	}
	if thumbs, ok := videoModel["big_thumbs"].([]any); ok {
		for _, raw := range thumbs {
			thumb, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if duration := roundRecoveredDuration(anyFloat64(thumb["duration"])); duration > 0 {
				return duration
			}
		}
	}
	return roundRecoveredDuration(anyFloat64(videoModel["video_duration"]))
}

func parseHistoryVideoModel(v any) map[string]any {
	switch value := v.(type) {
	case map[string]any:
		return value
	case string:
		value = strings.TrimSpace(value)
		if value == "" || !json.Valid([]byte(value)) {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(value), &out); err == nil {
			return out
		}
	}
	return nil
}

func roundRecoveredDuration(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*1000) / 1000
}

func recoveredOriginMap(v any) map[string]any {
	switch value := v.(type) {
	case map[string]any:
		return value
	case string:
		return parseRecoveredOriginString(value)
	default:
		return nil
	}
}

func parseRecoveredOriginString(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "map[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return nil
	}
	out := map[string]any{}
	for _, token := range strings.Fields(raw) {
		key, value, ok := strings.Cut(token, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstRecoveredOriginValue(root map[string]any, keys ...string) any {
	for _, key := range keys {
		if cleanRecoveredString(root[key]) != "" {
			return root[key]
		}
	}
	return nil
}

func historyVideoCandidateScore(item map[string]any) int {
	urlText := strings.TrimSpace(anyString(firstNonEmptyHistoryMediaValue(item, "video", "video_url", "url")))
	if urlText == "" {
		return -1 << 20
	}
	score := 0
	if strings.Contains(urlText, "vlabvod.com") {
		score += 500
	}
	if strings.Contains(urlText, "douyinpic.com") {
		score -= 500
	}
	if strings.Contains(urlText, "mime_type=video_mp4") {
		score += 100
	}
	format := strings.ToLower(strings.TrimSpace(anyString(item["format"])))
	switch format {
	case "mp4":
		score += 200
	case "image", "png", "webp":
		score -= 200
	}
	if parsed, err := url.Parse(urlText); err == nil {
		q := parsed.Query()
		score += anyInt(q.Get("ds")) * 100
		score += anyInt(q.Get("br")) / 10
		if strings.HasSuffix(strings.ToLower(parsed.Path), ".mp4") {
			score += 100
		}
	}
	return score
}

func historyImageCandidateScore(item map[string]any) int {
	urlText := strings.TrimSpace(anyString(firstNonEmptyHistoryMediaValue(item, "image", "image_url", "url")))
	if urlText == "" {
		return -1 << 20
	}
	score := 0
	lowerURL := strings.ToLower(urlText)
	switch {
	case strings.Contains(lowerURL, "aigc_resize:0:0"):
		score += 1200
	case strings.Contains(lowerURL, "resize:0:0"):
		score += 800
	}
	if strings.Contains(lowerURL, ".png") || strings.Contains(lowerURL, "format=.png") {
		score += 200
	}
	if strings.Contains(lowerURL, ".jpeg") || strings.Contains(lowerURL, ".jpg") || strings.Contains(lowerURL, "format=.jpeg") {
		score += 80
	}
	if strings.Contains(lowerURL, ".image") || strings.Contains(lowerURL, "format=.") {
		score -= 1200
	}
	if strings.Contains(lowerURL, ":640:640") || strings.Contains(lowerURL, ":480:480") || strings.Contains(lowerURL, ":360:360") {
		score -= 400
	}
	width := anyInt(item["width"])
	height := anyInt(item["height"])
	switch {
	case width >= 4000 || height >= 3000:
		score += 300
	case width >= 2000 || height >= 2000:
		score += 180
	case width > 0 || height > 0:
		score += 40
	}
	if format := strings.ToLower(strings.TrimSpace(anyString(item["format"]))); format == "png" {
		score += 100
	}
	return score
}

func mergeRecoveredHistoryQueueInfo(queueInfo map[string]any, historyItem map[string]any) {
	if len(queueInfo) == 0 || len(historyItem) == 0 {
		return
	}
	for _, key := range []string{"queue_idx", "priority", "queue_length", "debug_info"} {
		if value, ok := historyQueueValue(historyItem, key); ok {
			queueInfo[key] = value
		}
	}
	if status, ok := historyQueueValue(historyItem, "queue_status"); ok {
		queueInfo["queue_status"] = status
	}
}

func historyQueueValue(historyItem map[string]any, key string) (any, bool) {
	if len(historyItem) == 0 {
		return nil, false
	}
	if value, ok := historyItem[key]; ok && cleanRecoveredString(value) != "" {
		return value, true
	}
	if value, ok := historyItem[key]; ok {
		switch value.(type) {
		case int, int64, float64, json.Number:
			return value, true
		}
	}
	for _, queueKey := range []string{"queue", "Queue", "queue_info", "QueueInfo"} {
		queue, ok := historyItem[queueKey].(map[string]any)
		if !ok {
			continue
		}
		if value, ok := queue[key]; ok && cleanRecoveredString(value) != "" {
			return value, true
		}
		if value, ok := queue[key]; ok {
			switch value.(type) {
			case int, int64, float64, json.Number:
				return value, true
			}
		}
	}
	return nil, false
}

func mediaItemsFromHistory(historyItem map[string]any, listKey string, urlKey string, mediaType string) []map[string]any {
	out := []map[string]any{}
	seen := map[string]struct{}{}
	add := func(url string, item map[string]any) {
		url = anyString(url)
		if !isUsableMediaURL(url) {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		view := map[string]any{
			"url":  url,
			"type": mediaType,
		}
		if mediaType == "image" {
			view["image_url"] = url
			if width := anyInt(item["width"]); width > 0 {
				view["width"] = width
			}
			if height := anyInt(item["height"]); height > 0 {
				view["height"] = height
			}
		} else {
			view["video_url"] = url
			if fps := anyInt(item["fps"]); fps > 0 {
				view["fps"] = fps
			}
			if width := anyInt(item["width"]); width > 0 {
				view["width"] = width
			}
			if height := anyInt(item["height"]); height > 0 {
				view["height"] = height
			}
			if format := anyString(item["format"]); format != "" {
				view["format"] = format
			}
			if duration := anyFloat64(item["duration"]); duration > 0 {
				view["duration"] = duration
			}
		}
		if coverURL := anyString(item["cover_url"]); coverURL != "" {
			view["cover_url"] = coverURL
		}
		out = append(out, view)
	}
	switch list := historyItem[listKey].(type) {
	case []map[string]any:
		for _, item := range list {
			add(anyString(firstNonEmptyHistoryMediaValue(item, mediaType,
				urlKey, "url",
				"image_url", "video_url",
			)), item)
		}
	case []any:
		for _, raw := range list {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			add(anyString(firstNonEmptyHistoryMediaValue(item, mediaType,
				urlKey, "url",
				"image_url", "video_url",
			)), item)
		}
	}
	add(anyString(historyItem[urlKey]), historyItem)
	return out
}

func firstNonEmptyHistoryMediaValue(root map[string]any, mediaType string, keys ...string) any {
	for _, key := range keys {
		if mediaType == "video" && key == "image_url" {
			continue
		}
		if mediaType == "image" && key == "video_url" {
			continue
		}
		if cleanRecoveredString(root[key]) != "" {
			return root[key]
		}
	}
	return nil
}

func sanitizeRecoveredMedia(root map[string]any) {
	for _, key := range []string{"images", "videos"} {
		switch list := root[key].(type) {
		case []map[string]any:
			filtered := make([]map[string]any, 0, len(list))
			for _, item := range list {
				if isUsableMediaURL(anyString(item["url"])) {
					filtered = append(filtered, item)
				}
			}
			if len(filtered) == 0 {
				delete(root, key)
				continue
			}
			root[key] = filtered
		case []any:
			filtered := make([]map[string]any, 0, len(list))
			for _, raw := range list {
				item, ok := raw.(map[string]any)
				if !ok || !isUsableMediaURL(anyString(item["url"])) {
					continue
				}
				filtered = append(filtered, item)
			}
			if len(filtered) == 0 {
				delete(root, key)
				continue
			}
			root[key] = filtered
		}
	}
}

func isUsableMediaURL(url string) bool {
	url = anyString(url)
	return url != "" && url != "<nil>"
}

func cleanRecoveredString(v any) string {
	value := strings.TrimSpace(fmt.Sprint(v))
	if value == "" || value == "<nil>" || strings.EqualFold(value, "null") {
		return ""
	}
	return value
}

func firstCleanRecoveredValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := cleanRecoveredString(root[key]); value != "" {
			return value
		}
	}
	return ""
}

func parseRecoveredResultRoot(resultJSON string) map[string]any {
	trimmed := strings.TrimSpace(resultJSON)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return map[string]any{}
	}
	// 恢复链路对 result_json 只做“能安全读就读”的解析，不在这里自动修 schema。
	// 所有 alias 合并、history 合并和媒体净化都留给后续显式步骤，避免基础解析层偷偷改写结果。
	root := map[string]any{}
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return map[string]any{}
	}
	return root
}

func responseDataMap(resp *mcpclient.BaseResponse) map[string]any {
	if resp == nil {
		return map[string]any{}
	}
	data, ok := resp.Data.(map[string]any)
	if !ok || data == nil {
		return map[string]any{}
	}
	// resp.Data 只代表已经压平后的主业务数据，不包含 recovered 诊断元数据。
	// 调用侧如果要取 history_id/submitted_at 等恢复字段，必须显式读 responseRecoveredMap。
	return data
}

func responseRecoveredMap(resp *mcpclient.BaseResponse) map[string]any {
	if resp == nil {
		return map[string]any{}
	}
	recovered, ok := resp.Recovered.(map[string]any)
	if !ok || recovered == nil {
		return map[string]any{}
	}
	// recovered 存的是“恢复期额外保留下来的提交/传输诊断信息”，不是标准 MCP schema 正文。
	// 后续读取这些字段时都要保持只读语义，避免把 recovered 再反写成主业务 data。
	return recovered
}

func responseHistoryID(resp *mcpclient.BaseResponse) string {
	recovered := responseRecoveredMap(resp)
	// 提交链路现在把 history_id/submit_id/task_id 等恢复元数据单独放在 recovered 里。
	// 这里优先读 recovered，避免再次依赖会被后续拆壳收紧的 response.data wrapper。
	if historyID := firstCleanRecoveredValue(recovered, "history_id", "historyId", "HistoryID", "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID"); historyID != "" {
		return historyID
	}
	data := responseDataMap(resp)
	return firstCleanRecoveredValue(data, "history_id", "historyId", "HistoryID", "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID")
}

func responseSubmittedAt(resp *mcpclient.BaseResponse) int64 {
	recovered := responseRecoveredMap(resp)
	// submitted_at 优先沿 recovered 回读，保证时间线尽量来自 MCP 提交响应本身，
	// 而不是重新回退到旧 data wrapper 或更晚的本地查询时间。
	for _, value := range []any{
		recovered["submitted_at"], recovered["submittedAt"], recovered["SubmittedAt"],
		recovered["submitted"], recovered["Submitted"],
	} {
		if submitted := anyInt64(value); submitted > 0 {
			return submitted
		}
	}
	data := responseDataMap(resp)
	for _, value := range []any{
		data["submitted_at"], data["submittedAt"], data["SubmittedAt"],
		data["submitted"], data["Submitted"],
	} {
		if submitted := anyInt64(value); submitted > 0 {
			return submitted
		}
	}
	return 0
}

func historyQueryMetadata(resp *mcpclient.GetHistoryByIdsResponse) map[string]any {
	if resp == nil {
		return nil
	}
	code := strings.TrimSpace(resp.Code)
	message := strings.TrimSpace(resp.Message)
	logID := strings.TrimSpace(resp.LogID)
	if code == "" && message == "" && logID == "" {
		return nil
	}
	meta := map[string]any{}
	if code != "" {
		meta["code"] = code
	}
	if message != "" {
		meta["message"] = message
	}
	if logID != "" {
		meta["log_id"] = logID
	}
	// code=0 说明查询正常返回。即便是成功空结果，也需要至少保留 code=0 供状态机识别“查询成功但未命中”，
	// 但没必要把默认 ok 文案和空 log_id 一起写回 queue_info/query 里制造额外噪音。
	if code == "0" && strings.EqualFold(message, "ok") && logID == "" {
		return map[string]any{"code": "0"}
	}
	return meta
}

func shouldPersistHistoryQuery(historyQuery map[string]any) bool {
	if len(historyQuery) == 0 {
		return false
	}
	// 纯成功标记只用于恢复状态判断，不需要持久化到 result_json 里影响外显紧凑视图。
	return !(strings.TrimSpace(fmt.Sprint(historyQuery["code"])) == "0" && len(historyQuery) == 1)
}

func responseQueueStatus(resp *mcpclient.BaseResponse) string {
	for _, candidate := range []map[string]any{
		responseRecoveredMap(resp),
		responseDataMap(resp),
	} {
		if status := firstNormalizedHistoryStatus(
			candidate,
			"queue_status", "queueStatus", "QueueStatus",
			"queue_state", "queueState", "QueueState",
			"status", "Status",
			"gen_status", "genStatus", "GenStatus",
			"state", "State",
		); status != "" {
			// 提交响应里的状态别名很多，这里统一压成 submitted/running/success/failed 四类，
			// 保证 queue_info 和后续 query/view 不再感知后端原始命名抖动。
			switch status {
			case "queued", "pending":
				return "submitted"
			case "processing", "in_progress", "generating":
				return "running"
			case "succeeded", "done", "completed", "complete", "finished":
				return "success"
			case "fail", "error", "canceled", "cancelled", "timeout":
				return "failed"
			default:
				return status
			}
		}
	}
	return ""
}

func responseQueueProgress(resp *mcpclient.BaseResponse) int {
	for _, candidate := range []map[string]any{
		responseRecoveredMap(resp),
		responseDataMap(resp),
	} {
		// progress 优先沿 recovered/data 中的远端值回读，不在提交阶段重新估算。
		// 这样 buildTaskQueueInfo 只负责消费已有信号，不再引入新的本地进度推断来源。
		if progress := remoteHistoryProgress(candidate); progress > 0 {
			return progress
		}
	}
	return 0
}

func anyInt64(v any) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return int64(anyInt(v))
	}
}
