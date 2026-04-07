package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	appctx "code.byted.org/videocut-aigc/dreamina_cli/app"
	"code.byted.org/videocut-aigc/dreamina_cli/components/gen"
	"code.byted.org/videocut-aigc/dreamina_cli/components/login"
	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

// 该文件收敛生成类命令入口及其共享的提交辅助逻辑。

type GenerateInput map[string]any

type compactGeneratorSubmitOutput struct {
	SubmitID    string `json:"submit_id"`
	Prompt      any    `json:"prompt,omitempty"`
	LogID       string `json:"logid,omitempty"`
	GenStatus   string `json:"gen_status"`
	CreditCount *int   `json:"credit_count,omitempty"`
	QueueInfo   any    `json:"queue_info,omitempty"`
}

type compactGeneratorFailedOutput struct {
	SubmitID   string `json:"submit_id"`
	Prompt     any    `json:"prompt,omitempty"`
	LogID      string `json:"logid,omitempty"`
	GenStatus  string `json:"gen_status"`
	FailReason string `json:"fail_reason"`
}

// newText2ImageCommand 创建文生图命令入口。
func newText2ImageCommand(app any) *Command {
	// 当前 gen_task_type： "text2image"
	// 当前输入字段：
	// - prompt
	// - ratio
	// - resolution_type
	// - model_version
	return newGeneratorCommand("text2image")
}

// newImage2ImageCommand 创建图生图命令入口。
func newImage2ImageCommand(app any) *Command {
	// 当前 gen_task_type： "image2image"
	// 当前输入字段：
	// - image_paths
	// - prompt
	// - ratio
	// - resolution_type
	// - model_version
	return newGeneratorCommand("image2image")
}

// newImageUpscaleCommand 创建图片超分命令入口。
func newImageUpscaleCommand(app any) *Command {
	// 当前 gen_task_type： "image_upscale"
	// 当前输入字段：
	// - image_path
	// - resolution_type
	return newGeneratorCommand("image_upscale")
}

// newText2VideoCommand 创建文生视频命令入口。
func newText2VideoCommand(app any) *Command {
	// 当前 gen_task_type： "text2video"
	// 当前输入字段：
	// - prompt
	// - duration
	// - ratio
	// - video_resolution
	// - model_version
	return newGeneratorCommand("text2video")
}

// newImage2VideoCommand 创建图生视频命令入口。
func newImage2VideoCommand(app any) *Command {
	// 当前 gen_task_type： "image2video"
	// 当前输入字段：
	// - image_path
	// - prompt
	// - duration
	// - video_resolution
	// - model_version
	// - use_by_config
	//
	// 当显式传入 duration、video_resolution 或 model_version 时，
	// 会把 use_by_config 打开，走带高级配置的提交路径。
	return newGeneratorCommand("image2video")
}

// newFrames2VideoCommand 创建首尾帧转视频命令入口。
func newFrames2VideoCommand(app any) *Command {
	// 当前 gen_task_type： "frames2video"
	// 当前输入字段：
	// - first_path
	// - last_path
	// - prompt
	// - duration
	// - video_resolution
	// - model_version
	return newGeneratorCommand("frames2video")
}

// newMultiFrame2VideoCommandWithUse 创建多帧转视频命令入口，并兼容旧的 ref2video 命令别名。
func newMultiFrame2VideoCommandWithUse(app any, use string) *Command {
	// 当前命令名兼容 multiframe2video 和旧别名 ref2video。
	//
	// 当前 gen_task_type： "multiframe2video"
	// 当前输入字段：
	// - image_paths
	// - prompt
	// - duration
	// - transition_prompts
	// - transition_durations
	return &Command{
		Use: use,
		RunE: func(cmd *Command, args []string) error {
			input, pollSeconds, err := parseGeneratorArgs("multiframe2video", args)
			if err != nil {
				return err
			}
			return runGeneratorSubmit(cmd.Context(), cmd, nil, "multiframe2video", input, pollSeconds)
		},
	}
}

// newMultiModal2VideoCommand 创建多模态转视频命令入口。
func newMultiModal2VideoCommand(app any) *Command {
	// 当前 gen_task_type： "multimodal2video"
	// 当前输入字段：
	// - image_paths
	// - video_paths
	// - audio_paths
	// - prompt
	// - duration
	// - ratio
	// - video_resolution
	// - model_version
	return newGeneratorCommand("multimodal2video")
}

// runGeneratorSubmit 执行一次生成任务提交，并在需要时轮询最终结果后输出 JSON。
func runGeneratorSubmit(ctx context.Context, cmd *Command, app any, genTaskType string, input GenerateInput, pollSeconds int) error {
	// 提交命令会先补齐登录会话和用户标识，再调用生成服务提交任务；
	// 若开启轮询，则继续查询到终态后统一输出结构化 JSON。
	appContext, err := appctx.NewContext()
	if err != nil {
		return err
	}
	// 旧程序里 multimodal2video 在缺少登录态时，会先返回登录错误，
	// 而不是先做“至少传一个输入”的本地参数校验。
	if strings.TrimSpace(genTaskType) == "multimodal2video" {
		if err := appContext.RequireLogin(); err != nil {
			return err
		}
	}
	if err := validateGeneratorInput(genTaskType, input); err != nil {
		return err
	}
	if err := appContext.RequireLogin(); err != nil {
		return err
	}
	genService, ok := appContext.GenService().(*gen.Service)
	if !ok {
		return fmt.Errorf("generator service is not configured")
	}
	submitUID := "local-user"
	if svc, ok := appContext.Login.(*login.Service); ok {
		if payload, err := svc.LoadUsableSession(); err == nil {
			ctx = gen.ContextWithSession(ctx, payload)
			if uid := currentUserIDFromSession(payload); uid != "" {
				submitUID = uid
			}
		}
	}
	submitted, submitErr := genService.SubmitTask(ctx, submitUID, genTaskType, input)
	if submitted != nil {
		if pollSeconds > 0 && readTaskStatus(submitted) == "querying" {
			finalTask, err := pollSubmittedTask(ctx, genService, extractSubmitID(submitted), pollSeconds)
			if err != nil {
				return err
			}
			return printPolledGeneratorOutput(cmd, submitted, finalTask)
		}
		if compact := compactGeneratorSubmitView(submitted); compact != nil {
			return printJSON(compact, cmd.OutOrStdout())
		}
		if compact := compactGeneratorFailureView(submitted); compact != nil {
			return printJSON(compact, cmd.OutOrStdout())
		}
		if taskMap := taskResultView(submitted); taskMap != nil {
			return printJSON(taskMap, cmd.OutOrStdout())
		}
		return printJSON(submitted, cmd.OutOrStdout())
	}
	if submitErr != nil {
		return printJSON(map[string]any{
			"error":                  submitErr.Error(),
			"requires_confirmation":  requiresComplianceConfirmation(submitErr.Error()),
			"generator_submit_error": true,
		}, cmd.OutOrStdout())
	}
	return nil
}

// pollSubmittedTask 在给定超时时间内轮询查询任务结果，直到任务脱离 querying 或超时。
func pollSubmittedTask(ctx context.Context, genService any, submitID string, pollSeconds int) (any, error) {
	// 轮询期间每秒查询一次任务状态，超时前如果进入终态则立即返回；
	// 到达超时时间后会再做最后一次查询。
	service, ok := genService.(*gen.Service)
	if !ok {
		return nil, fmt.Errorf("invalid generator service")
	}
	timer := time.NewTimer(time.Duration(pollSeconds) * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			task, err := service.QueryResult(ctx, submitID)
			if err != nil {
				return nil, err
			}
			if readTaskStatus(task) == "querying" {
				continue
			}
			return task, nil
		case <-timer.C:
			return service.QueryResult(ctx, submitID)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// printPolledGeneratorOutput 对齐原程序轮询语义：若超时后仍在 querying，就保留提交态紧凑输出；只有终态才切到 query_result 视图。
func printPolledGeneratorOutput(cmd *Command, submitted any, finalTask any) error {
	if readTaskStatus(finalTask) == "querying" {
		if compact := compactGeneratorSubmitView(finalTask); compact != nil {
			return printJSON(compact, cmd.OutOrStdout())
		}
		if compact := compactGeneratorSubmitView(submitted); compact != nil {
			return printJSON(compact, cmd.OutOrStdout())
		}
		if taskMap := taskResultView(submitted); taskMap != nil {
			return printJSON(taskMap, cmd.OutOrStdout())
		}
		return printJSON(submitted, cmd.OutOrStdout())
	}
	return printGeneratorQueryResultOutput(cmd, finalTask)
}

// printGeneratorQueryResultOutput 按统一结果视图输出轮询后的任务查询结果。
func printGeneratorQueryResultOutput(cmd *Command, taskValue any) error {
	if localTask, ok := taskValue.(*task.AIGCTask); ok && localTask != nil {
		parsed, err := parseRemoteQueryResult(localTask.ResultJSON)
		if err != nil {
			return err
		}
		return printJSON(buildQueryResultOutput(localTask, parsed), cmd.OutOrStdout())
	}
	return printJSON(taskResultViewWithResult(taskValue), cmd.OutOrStdout())
}

// taskResultView 把提交返回对象包装成统一的顶层 `task` 输出结构。
func taskResultView(task any) any {
	if task == nil {
		return nil
	}
	return map[string]any{
		"task": task,
	}
}

// taskResultViewWithResult 把查询返回对象包装成统一的顶层 `task` 输出结构。
func taskResultViewWithResult(task any) any {
	if task == nil {
		return nil
	}
	return map[string]any{
		"task": task,
	}
}

// parseRef2VideoTransitionDurations 解析多帧转视频命令里重复传入的过渡时长参数。
func parseRef2VideoTransitionDurations(values []string) ([]float64, error) {
	// 把重复传入的过渡时长参数解析成浮点数组。
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]float64, 0, len(values))
	for _, value := range values {
		n, err := parseFloatString(value)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// validateRef2VideoCLIFlags 校验 multiframe2video/ref2video 命令的图片数量和过渡参数组合是否合法。
func validateRef2VideoCLIFlags(v ...any) error {
	// 这里统一校验多帧转视频命令的图片数量、简写模式和重复过渡参数数量。
	input := firstGenerateInput(v...)
	images := stringSliceInput(input, "image_paths")
	prompts := stringSliceInput(input, "transition_prompts")
	durations := stringSliceInput(input, "transition_durations")
	shorthandPrompt := strings.TrimSpace(stringInput(input, "prompt")) != ""
	shorthandDuration := strings.TrimSpace(stringInput(input, "duration")) != ""

	if len(images) < 2 || len(images) > 20 {
		return fmt.Errorf("multiframe2video requires 2-20 images")
	}
	if (shorthandPrompt || shorthandDuration) && (len(prompts) > 0 || len(durations) > 0) {
		return fmt.Errorf("use either shorthand --prompt/--duration for exactly 2 images, or repeated --transition-prompt/--transition-duration")
	}
	if shorthandPrompt && len(images) != 2 {
		return fmt.Errorf("--prompt shorthand only supports exactly 2 images; use repeated --transition-prompt for %d images", len(images))
	}
	if !shorthandPrompt && len(prompts) == 0 {
		return fmt.Errorf("multiframe2video requires either --prompt for exactly 2 images, or repeated --transition-prompt")
	}
	if len(prompts) > 0 && len(prompts) != len(images)-1 {
		return fmt.Errorf("multiframe2video requires %d transition prompts for %d images", len(images)-1, len(images))
	}
	if len(durations) > 0 && len(durations) != len(images)-1 {
		return fmt.Errorf("multiframe2video requires %d transition durations for %d images", len(images)-1, len(images))
	}
	return nil
}

// validateMultiModal2VideoCLIFlags 校验 multimodal2video 命令至少包含可生成视频的有效输入。
func validateMultiModal2VideoCLIFlags(v ...any) error {
	// 多模态转视频至少需要图片或视频输入，纯音频输入直接拒绝。
	input := firstGenerateInput(v...)
	images := stringSliceInput(input, "image_paths")
	videos := stringSliceInput(input, "video_paths")
	audios := stringSliceInput(input, "audio_paths")
	if len(images) == 0 && len(videos) == 0 && len(audios) == 0 {
		return fmt.Errorf("multimodal2video requires at least one --image, --video, or --audio input")
	}
	if len(images) == 0 && len(videos) == 0 && len(audios) > 0 {
		return fmt.Errorf("multimodal2video does not support audio-only input; add at least one --image or --video")
	}
	return nil
}

// newGeneratorCommand 为给定 gen_task_type 创建一个通用生成命令入口。
func newGeneratorCommand(genTaskType string) *Command {
	return &Command{
		Use: genTaskType,
		RunE: func(cmd *Command, args []string) error {
			input, pollSeconds, err := parseGeneratorArgs(genTaskType, args)
			if err != nil {
				return err
			}
			return runGeneratorSubmit(cmd.Context(), cmd, nil, genTaskType, input, pollSeconds)
		},
	}
}

// parseGeneratorArgs 解析生成命令的通用 flag/key/value 输入以及轮询秒数。
func parseGeneratorArgs(genTaskType string, args []string) (GenerateInput, int, error) {
	input := GenerateInput{}
	pollSeconds := 0
	allowed := allowedGeneratorFlags(genTaskType)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return nil, 0, fmt.Errorf("unknown command %q for %q", arg, "dreamina "+strings.TrimSpace(genTaskType))
		}
		if strings.HasPrefix(arg, "--poll=") {
			value, err := parseCLIIntFlag(strings.TrimPrefix(arg, "--poll="), "poll")
			if err != nil {
				return nil, 0, err
			}
			pollSeconds = value
			continue
		}
		if arg == "--poll" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, 0, fmt.Errorf("flag needs an argument: --poll")
			}
			value, err := parseCLIIntFlag(args[i+1], "poll")
			if err != nil {
				return nil, 0, err
			}
			pollSeconds = value
			i++
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		value := "true"
		if strings.Contains(key, "=") {
			parts := strings.SplitN(key, "=", 2)
			key = parts[0]
			value = parts[1]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			value = args[i+1]
			i++
		}
		normalizedKey := normalizeGeneratorKeyForTask(genTaskType, key)
		if _, ok := allowed[normalizedKey]; !ok {
			return nil, 0, fmt.Errorf("unknown flag: --%s", key)
		}
		if value == "true" && !strings.Contains(arg, "=") {
			return nil, 0, fmt.Errorf("flag needs an argument: --%s", key)
		}
		assignGeneratorInput(input, normalizedKey, value)
	}
	if missing := missingRequiredGeneratorFlags(genTaskType, input); len(missing) > 0 {
		return nil, 0, fmt.Errorf("%s", formatRequiredFlagsMessage(missing))
	}
	return input, pollSeconds, nil
}

// formatRequiredFlagsMessage 按原程序风格格式化缺失必填参数提示。
func formatRequiredFlagsMessage(flags []string) string {
	quoted := make([]string, 0, len(flags))
	for _, flag := range flags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}
		quoted = append(quoted, fmt.Sprintf("%q", flag))
	}
	return fmt.Sprintf("required flag(s) %s not set", strings.Join(quoted, ", "))
}

// missingRequiredGeneratorFlags 返回当前命令在参数解析阶段就必须存在的原生 flag。
func missingRequiredGeneratorFlags(genTaskType string, input GenerateInput) []string {
	missing := make([]string, 0, 2)
	switch strings.TrimSpace(genTaskType) {
	case "text2image", "text2video":
		if _, ok := input["prompt"]; !ok {
			missing = append(missing, "prompt")
		}
	case "image2image":
		if len(stringSliceInput(input, "image_paths")) == 0 {
			missing = append(missing, "images")
		}
		if _, ok := input["prompt"]; !ok {
			missing = append(missing, "prompt")
		}
	case "image2video":
		if _, ok := input["image_path"]; !ok {
			missing = append(missing, "image")
		}
		if _, ok := input["prompt"]; !ok {
			missing = append(missing, "prompt")
		}
	case "image_upscale":
		if _, ok := input["image_path"]; !ok {
			missing = append(missing, "image")
		}
		if _, ok := input["resolution_type"]; !ok {
			missing = append(missing, "resolution_type")
		}
	case "frames2video":
		if _, ok := input["first_path"]; !ok {
			missing = append(missing, "first")
		}
		if _, ok := input["last_path"]; !ok {
			missing = append(missing, "last")
		}
	case "multiframe2video":
		if len(stringSliceInput(input, "image_paths")) == 0 {
			missing = append(missing, "images")
		}
	}
	if len(missing) > 0 {
		return missing
	}
	return nil
}

// allowedGeneratorFlags 返回各生成命令允许的标准化 flag 集合，保证参数校验顺序与原程序一致。
func allowedGeneratorFlags(genTaskType string) map[string]struct{} {
	allowed := map[string]struct{}{
		"poll": {},
	}
	for _, key := range []string{"prompt", "model_version"} {
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	switch strings.TrimSpace(genTaskType) {
	case "text2image":
		for _, key := range []string{"prompt", "ratio", "resolution_type", "model_version"} {
			allowed[key] = struct{}{}
		}
	case "image2image":
		for _, key := range []string{"image_paths", "prompt", "ratio", "resolution_type", "model_version"} {
			allowed[key] = struct{}{}
		}
	case "image_upscale":
		for _, key := range []string{"image_path", "resolution_type"} {
			allowed[key] = struct{}{}
		}
	case "text2video":
		for _, key := range []string{"prompt", "duration", "ratio", "video_resolution", "model_version"} {
			allowed[key] = struct{}{}
		}
	case "image2video":
		for _, key := range []string{"image_path", "prompt", "duration", "video_resolution", "model_version"} {
			allowed[key] = struct{}{}
		}
	case "frames2video":
		for _, key := range []string{"first", "last", "prompt", "duration", "video_resolution", "model_version"} {
			allowed[normalizeGeneratorKeyForTask(genTaskType, key)] = struct{}{}
		}
	case "multiframe2video":
		for _, key := range []string{"image_paths", "prompt", "duration", "transition_prompts", "transition_durations"} {
			allowed[key] = struct{}{}
		}
	case "multimodal2video":
		for _, key := range []string{"image_paths", "video_paths", "audio_paths", "prompt", "duration", "ratio", "video_resolution", "model_version"} {
			allowed[key] = struct{}{}
		}
	}
	return allowed
}

// assignGeneratorInput 把一个命令行参数写入生成输入，并兼容重复参数与逗号分隔数组。
func assignGeneratorInput(input GenerateInput, key string, value string) {
	value = strings.TrimSpace(value)
	if existing, ok := input[key]; ok {
		switch current := existing.(type) {
		case []string:
			input[key] = append(current, value)
		default:
			input[key] = []string{fmt.Sprint(current), value}
		}
		return
	}
	if strings.Contains(value, ",") {
		items := strings.Split(value, ",")
		out := make([]string, 0, len(items))
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) > 0 {
			input[key] = out
			return
		}
	}
	input[key] = value
}

// normalizeGeneratorKey 把 CLI 参数名归一成生成服务使用的内部键名。
func normalizeGeneratorKey(key string) string {
	key = strings.ReplaceAll(strings.TrimSpace(key), "-", "_")
	switch key {
	case "first":
		return "first_path"
	case "last":
		return "last_path"
	case "video":
		return "video_paths"
	case "audio":
		return "audio_paths"
	case "transition_prompt":
		return "transition_prompts"
	case "transition_duration":
		return "transition_durations"
	default:
		return key
	}
}

// normalizeGeneratorKeyForTask 在公共归一规则之外，再按命令类型把单图/多图 flag 收敛到各自实际消费的内部键名。
func normalizeGeneratorKeyForTask(genTaskType string, key string) string {
	key = normalizeGeneratorKey(key)
	switch strings.TrimSpace(genTaskType) {
	case "image_upscale", "image2video":
		if key == "image" || key == "images" {
			return "image_path"
		}
	case "image2image", "multiframe2video":
		if key == "image" || key == "images" {
			return "image_paths"
		}
	default:
		if key == "image" {
			return "image_paths"
		}
	}
	return key
}

// parseFloatString 解析只包含数字和单个小数点的浮点数字符串。
func parseFloatString(s string) (float64, error) {
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

// readTaskStatus 从动态任务对象里尽量读取统一的生成状态字段。
func readTaskStatus(task any) string {
	if task == nil {
		return ""
	}
	if m, ok := task.(map[string]any); ok {
		if s, ok := m["GenStatus"].(string); ok {
			return s
		}
		if s, ok := m["gen_status"].(string); ok {
			return s
		}
	}
	body, err := jsonMarshalTask(task)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if s, ok := payload["GenStatus"].(string); ok {
		return s
	}
	if s, ok := payload["gen_status"].(string); ok {
		return s
	}
	return ""
}

// extractSubmitID 从动态任务对象里尽量读取 submit_id。
func extractSubmitID(task any) string {
	body, err := jsonMarshalTask(task)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if s, ok := payload["SubmitID"].(string); ok {
		return s
	}
	if s, ok := payload["submit_id"].(string); ok {
		return s
	}
	return ""
}

func compactGeneratorSubmitView(taskValue any) any {
	taskBody, ok := taskValue.(*task.AIGCTask)
	if !ok || taskBody == nil {
		return nil
	}
	if strings.TrimSpace(taskBody.GenStatus) != "querying" {
		return nil
	}
	root, ok := viewResultJSON(taskBody.ResultJSON).(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	response, _ := root["response"].(map[string]any)
	responseData, _ := response["data"].(map[string]any)
	submitID := firstCompactString(
		responseData["submit_id"],
		responseData["submitId"],
		responseData["SubmitID"],
		root["submit_id"],
		root["submitId"],
		taskBody.SubmitID,
	)
	if submitID == "" {
		return nil
	}
	out := &compactGeneratorSubmitOutput{
		SubmitID:  submitID,
		LogID:     firstCompactString(root["log_id"], response["log_id"], response["logId"], response["LogID"], taskBody.LogID),
		GenStatus: firstCompactString(root["gen_status"], taskBody.GenStatus),
	}
	if shouldIncludeGeneratorPrompt(taskBody) {
		if prompt := taskPrompt(taskBody); prompt != nil {
			out.Prompt = prompt
		}
	}
	if shouldIncludeGeneratorCreditCount(taskBody) {
		if creditCount, ok := firstCompactInt(
			responseData["credit_count"],
			responseData["creditCount"],
			nestedCompactValue(responseData, "commerce_info", "credit_count"),
			nestedCompactValue(responseData, "commerce_info", "creditCount"),
		); ok && creditCount > 0 {
			out.CreditCount = &creditCount
		}
	}
	if shouldIncludeGeneratorQueueInfo(taskBody) {
		if queueInfo := queryResultQueueInfo(taskBody.ResultJSON); queueInfo != nil {
			out.QueueInfo = queueInfo
		}
	}
	return out
}

func shouldIncludeGeneratorPrompt(taskBody *task.AIGCTask) bool {
	if taskBody == nil {
		return false
	}
	switch strings.TrimSpace(taskBody.GenTaskType) {
	case "image2image", "image2video", "frames2video", "multimodal2video":
		return true
	default:
		return false
	}
}

func shouldIncludeGeneratorCreditCount(taskBody *task.AIGCTask) bool {
	if taskBody == nil {
		return false
	}
	return true
}

func shouldIncludeGeneratorQueueInfo(taskBody *task.AIGCTask) bool {
	if taskBody == nil {
		return false
	}
	switch strings.TrimSpace(taskBody.GenTaskType) {
	case "image_upscale":
		return false
	default:
		return true
	}
}

func compactGeneratorFailureView(taskValue any) any {
	taskBody, ok := taskValue.(*task.AIGCTask)
	if !ok || taskBody == nil {
		return nil
	}
	if normalizeQueryResultGenStatus(taskBody.GenStatus) != "fail" {
		return nil
	}
	root, _ := viewResultJSON(taskBody.ResultJSON).(map[string]any)
	response, _ := root["response"].(map[string]any)
	responseData, _ := response["data"].(map[string]any)
	submitID := firstCompactString(
		responseData["submit_id"],
		responseData["submitId"],
		responseData["SubmitID"],
		root["submit_id"],
		root["submitId"],
		taskBody.SubmitID,
	)
	if submitID == "" {
		return nil
	}
	out := &compactGeneratorFailedOutput{
		SubmitID:   submitID,
		LogID:      firstCompactString(root["log_id"], response["log_id"], response["logId"], response["LogID"], taskBody.LogID),
		GenStatus:  "fail",
		FailReason: firstCompactString(root["fail_reason"], root["failReason"], root["FailReason"], taskBody.FailReason),
	}
	if shouldIncludeGeneratorPrompt(taskBody) {
		if prompt := taskPrompt(taskBody); prompt != nil {
			out.Prompt = prompt
		}
	}
	return out
}

func firstCompactString(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" && !strings.EqualFold(text, "null") {
			return text
		}
	}
	return ""
}

func firstCompactInt(values ...any) (int, bool) {
	for _, value := range values {
		switch current := value.(type) {
		case int:
			return current, true
		case int64:
			return int(current), true
		case float64:
			return int(current), true
		case json.Number:
			if n, err := current.Int64(); err == nil {
				return int(n), true
			}
		case string:
			current = strings.TrimSpace(current)
			if current == "" {
				continue
			}
			if n, err := strconv.Atoi(current); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func nestedCompactValue(root map[string]any, key string, childKey string) any {
	if len(root) == 0 {
		return nil
	}
	nested, _ := root[key].(map[string]any)
	if len(nested) == 0 {
		return nil
	}
	return nested[childKey]
}

// jsonMarshalTask 把动态任务对象编码成 JSON，供兼容读取字段时复用。
func jsonMarshalTask(v any) ([]byte, error) {
	return json.Marshal(v)
}

// validateGeneratorInput 按任务类型校验提交前必需的 prompt、资源路径和组合约束。
func validateGeneratorInput(genTaskType string, input GenerateInput) error {
	switch genTaskType {
	case "text2image", "text2video":
		if _, ok := input["prompt"]; !ok {
			return fmt.Errorf("prompt is required")
		}
	case "image2image":
		if _, ok := input["prompt"]; !ok {
			return fmt.Errorf("prompt is required")
		}
		if err := requireExistingFiles(stringSliceInput(input, "image_paths"), "images"); err != nil {
			return err
		}
	case "image_upscale":
		if err := requireExistingFiles([]string{stringInput(input, "image_path")}, "image"); err != nil {
			return err
		}
	case "image2video":
		if _, ok := input["prompt"]; !ok {
			return fmt.Errorf("prompt is required")
		}
		if err := requireExistingFiles([]string{stringInput(input, "image_path")}, "image"); err != nil {
			return err
		}
		if strings.TrimSpace(stringInput(input, "duration")) != "" ||
			strings.TrimSpace(stringInput(input, "video_resolution")) != "" ||
			strings.TrimSpace(stringInput(input, "model_version")) != "" {
			input["use_by_config"] = "true"
		} else if _, ok := input["use_by_config"]; !ok {
			input["use_by_config"] = "false"
		}
	case "frames2video":
		if err := requireExistingFiles([]string{stringInput(input, "first_path")}, "first"); err != nil {
			return err
		}
		if err := requireExistingFiles([]string{stringInput(input, "last_path")}, "last"); err != nil {
			return err
		}
	case "multiframe2video":
		if err := validateRef2VideoCLIFlags(input); err != nil {
			return err
		}
		if err := requireExistingFiles(stringSliceInput(input, "image_paths"), "images"); err != nil {
			return err
		}
	case "multimodal2video":
		if err := validateMultiModal2VideoCLIFlags(input); err != nil {
			return err
		}
		if err := requireExistingFiles(stringSliceInput(input, "image_paths"), "image_paths"); err != nil {
			return err
		}
		if err := requireExistingFiles(stringSliceInput(input, "video_paths"), "video_paths"); err != nil {
			return err
		}
		if err := requireExistingFiles(stringSliceInput(input, "audio_paths"), "audio_paths"); err != nil {
			return err
		}
	}
	return nil
}

// requireExistingFiles 校验一组本地文件路径是否存在，并按字段名返回更明确的错误。
func requireExistingFiles(paths []string, field string) error {
	if field == "image" || field == "first" || field == "last" {
		if len(paths) == 0 {
			return fmt.Errorf("%s", formatRequiredFlagsMessage([]string{field}))
		}
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				return nil
			}
		}
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			filtered = append(filtered, path)
		}
	}
	if field == "image" || field == "images" || field == "first" || field == "last" {
		if len(filtered) == 0 {
			return fmt.Errorf("%s", formatRequiredFlagsMessage([]string{field}))
		}
	}
	if strings.HasSuffix(field, "paths") && len(filtered) == 0 {
		return nil
	}
	for _, path := range filtered {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s %q: %w", field, path, err)
		}
	}
	return nil
}

// firstGenerateInput 从可变参数中提取第一份 GenerateInput。
func firstGenerateInput(v ...any) GenerateInput {
	for _, arg := range v {
		switch value := arg.(type) {
		case GenerateInput:
			return value
		case map[string]any:
			return GenerateInput(value)
		}
	}
	return GenerateInput{}
}

// stringInput 从生成输入里读取单个字符串字段。
func stringInput(input GenerateInput, key string) string {
	key = normalizeGeneratorKey(key)
	if value, ok := input[key]; ok {
		switch s := value.(type) {
		case string:
			return strings.TrimSpace(s)
		case []string:
			if len(s) > 0 {
				return strings.TrimSpace(s[0])
			}
		}
	}
	return ""
}

// stringSliceInput 从生成输入里读取字符串切片字段，并兼容单值转切片。
func stringSliceInput(input GenerateInput, key string) []string {
	key = normalizeGeneratorKey(key)
	if value, ok := input[key]; ok {
		switch s := value.(type) {
		case []string:
			out := make([]string, 0, len(s))
			for _, item := range s {
				item = strings.TrimSpace(item)
				if item != "" {
					out = append(out, item)
				}
			}
			return out
		case string:
			s = strings.TrimSpace(s)
			if s == "" {
				return nil
			}
			return []string{s}
		}
	}
	return nil
}

// currentUserIDFromSession 从登录会话 payload 中递归提取当前用户 ID。
func currentUserIDFromSession(v any) string {
	if session, ok := v.(map[string]any); ok {
		return recursiveUserIDString(session)
	}
	return ""
}

// recursiveUserIDString 递归扫描任意节点中的 uid/user_id/UserID 等用户 ID 别名。
func recursiveUserIDString(node any) string {
	switch value := node.(type) {
	case map[string]any:
		// 提交入口直接用这里的返回值决定本地任务 UID。
		// 如果这里漏掉 UserID/UID 这类别名，就会出现“已经登录成功，但新任务写成 local-user”的假降级。
		for _, key := range []string{"user_id", "uid", "userId", "UserId", "UserID", "UID"} {
			if raw, ok := value[key]; ok {
				switch current := raw.(type) {
				case string:
					if s := strings.TrimSpace(current); s != "" {
						return normalizeUserIDString(s)
					}
				case json.Number:
					return current.String()
				case int:
					return fmt.Sprintf("%d", current)
				case int64:
					return fmt.Sprintf("%d", current)
				case float64:
					return fmt.Sprintf("%.0f", current)
				}
			}
		}
		for _, item := range value {
			if text := recursiveUserIDString(item); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range value {
			if text := recursiveUserIDString(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func normalizeUserIDString(text string) string {
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
