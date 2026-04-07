package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"code.byted.org/videocut-aigc/dreamina_cli/buildinfo"
	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

type HTTPClient struct {
	http *httpclient.Client
}

const (
	mcpAppIDHeader = "513695"
	mcpPFHeader    = "7"
	historyProbeEnv = "DREAMINA_DEBUG_HISTORY_PROBE_DIR"
)

// getCookieFromCookieFile  reads cookie from cookie.json file
func getCookieFromCookieFile() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cookieFilePath := filepath.Join(homeDir, ".dreamina_cli", "cookie.json")
	cookieData, err := os.ReadFile(cookieFilePath)
	if err != nil {
		return ""
	}
	var cookieStruct struct {
		Cookie string `json:"cookie"`
	}
	if err := json.Unmarshal(cookieData, &cookieStruct); err != nil {
		return ""
	}
	return strings.TrimSpace(cookieStruct.Cookie)
}

// BuildMCPSessionFromCookieFile creates an MCP Session using cookie.json file
func BuildMCPSessionFromCookieFile() *Session {
	session := &Session{
		Headers: map[string]string{},
	}

	// Read cookie from cookie.json file
	if cookie := getCookieFromCookieFile(); cookie != "" {
		session.Cookie = cookie
	}

	return session
}

type Session struct {
	Cookie  string            `json:"cookie,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	UserID  string            `json:"user_id,omitempty"`
}

type APIError struct {
	Code     string
	Message  string
	LogID    string
	SubmitID string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("api error: ret=%s, message=%s, logid=%s", e.Code, e.Message, e.LogID)
}

type BaseResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	LogID     string `json:"log_id"`
	Data      any    `json:"data"`
	Recovered any    `json:"recovered,omitempty"`
	Curl      string `json:"curl,omitempty"`
}

type Text2VideoRequest struct {
	Prompt          string `json:"prompt,omitempty"`
	Duration        int    `json:"duration,omitempty"`
	Ratio           string `json:"ratio,omitempty"`
	VideoResolution string `json:"video_resolution,omitempty"`
	ModelVersion    string `json:"model_version,omitempty"`
}

type Image2VideoRequest struct {
	FirstFrameResourceID string `json:"first_frame_resource_id,omitempty"`
	Prompt               string `json:"prompt,omitempty"`
	Duration             int    `json:"duration,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	ModelVersion         string `json:"model_version,omitempty"`
	UseByConfig          bool   `json:"use_by_config,omitempty"`
}

type Frames2VideoRequest struct {
	FirstFrameResourceID string `json:"first_frame_resource_id,omitempty"`
	LastFrameResourceID  string `json:"last_frame_resource_id,omitempty"`
	Prompt               string `json:"prompt,omitempty"`
	Duration             int    `json:"duration,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	ModelVersion         string `json:"model_version,omitempty"`
}

type Ref2VideoRequest struct {
	MediaResourceIDList []string  `json:"media_resource_id_list,omitempty"`
	MediaTypeList       []string  `json:"media_type_list,omitempty"`
	PromptList          []string  `json:"prompt_list,omitempty"`
	DurationList        []float64 `json:"duration_list,omitempty"`
}

type MultiModal2VideoRequest struct {
	ImageResourceIDList []string `json:"image_resource_id_list,omitempty"`
	VideoResourceIDList []string `json:"video_resource_id_list,omitempty"`
	AudioResourceIDList []string `json:"audio_resource_id_list,omitempty"`
	Prompt              string   `json:"prompt,omitempty"`
	Duration            int      `json:"duration,omitempty"`
	Ratio               string   `json:"ratio,omitempty"`
	VideoResolution     string   `json:"video_resolution,omitempty"`
	ModelVersion        string   `json:"model_version,omitempty"`
}

type Text2ImageRequest struct {
	Prompt         string `json:"prompt,omitempty"`
	Ratio          string `json:"ratio,omitempty"`
	ResolutionType string `json:"resolution_type,omitempty"`
	ModelVersion   string `json:"model_version,omitempty"`
}

type Image2ImageRequest struct {
	ResourceIDList []string `json:"resource_id_list,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	Ratio          string   `json:"ratio,omitempty"`
	ResolutionType string   `json:"resolution_type,omitempty"`
	ModelVersion   string   `json:"model_version,omitempty"`
	SubmitID       string   `json:"submit_id,omitempty"`
}

type UpscaleRequest struct {
	ResourceID     string `json:"resource_id,omitempty"`
	ResolutionType string `json:"resolution_type,omitempty"`
}

type GetHistoryByIdsRequest struct {
	SubmitIDs  []string `json:"submit_ids"`
	HistoryIDs []string `json:"history_ids"`
	NeedBatch  bool     `json:"need_batch"`
}

type GetHistoryByIdsResponse struct {
	Code        string                  `json:"code,omitempty"`
	Message     string                  `json:"message,omitempty"`
	LogID       string                  `json:"log_id,omitempty"`
	BodyPreview string                  `json:"body_preview,omitempty"`
	Items       map[string]*HistoryItem `json:"items,omitempty"`
}

// New 创建 Dreamina MCP 客户端；如果没有注入 HTTP 客户端，就使用默认实现。
func New(v ...any) *HTTPClient {
	var http *httpclient.Client
	for _, arg := range v {
		if value, ok := arg.(*httpclient.Client); ok {
			http = value
			break
		}
	}
	if http == nil {
		http, _ = httpclient.New()
	}
	return &HTTPClient{http: http}
}

// Text2Video 提交文生视频任务。
func (c *HTTPClient) Text2Video(ctx context.Context, sess *Session, req *Text2VideoRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Text2Video", "/dreamina/cli/v1/video_generate", buildText2VideoPayload(req))
}

// Image2Video 提交图生视频任务。
func (c *HTTPClient) Image2Video(ctx context.Context, sess *Session, req *Image2VideoRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Image2Video", "/dreamina/cli/v1/video_generate", buildImage2VideoPayload(req))
}

// Image2VideoByConfig 提交按配置变体执行的图生视频任务。
func (c *HTTPClient) Image2VideoByConfig(ctx context.Context, sess *Session, req *Image2VideoRequest) (*BaseResponse, error) {
	if req != nil {
		req.UseByConfig = true
	}
	return c.doGenerate(ctx, sess, "Image2VideoByConfig", "/dreamina/cli/v1/video_generate", buildImage2VideoPayload(req))
}

// Frames2Video 提交多帧生成视频任务。
func (c *HTTPClient) Frames2Video(ctx context.Context, sess *Session, req *Frames2VideoRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Frames2Video", "/dreamina/cli/v1/video_generate", buildFrames2VideoPayload(req))
}

// Ref2Video 提交参考视频驱动的视频生成任务。
func (c *HTTPClient) Ref2Video(ctx context.Context, sess *Session, req *Ref2VideoRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Ref2Video", "/dreamina/cli/v1/video_generate", buildRef2VideoPayload(req))
}

// MultiModal2Video 提交混合文本与资源输入的视频生成任务。
func (c *HTTPClient) MultiModal2Video(ctx context.Context, sess *Session, req *MultiModal2VideoRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "MultiModal2Video", "/dreamina/cli/v1/video_generate", buildMultiModal2VideoPayload(req))
}

// Text2Image 提交文生图任务。
func (c *HTTPClient) Text2Image(ctx context.Context, sess *Session, req *Text2ImageRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Text2Image", "/dreamina/cli/v1/image_generate/", buildText2ImagePayload(req))
}

// Image2Image 提交图生图任务。
func (c *HTTPClient) Image2Image(ctx context.Context, sess *Session, req *Image2ImageRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Image2Image", "/dreamina/cli/v1/image_generate", buildImage2ImagePayload(req))
}

// Upscale 提交图片超分任务。
func (c *HTTPClient) Upscale(ctx context.Context, sess *Session, req *UpscaleRequest) (*BaseResponse, error) {
	return c.doGenerate(ctx, sess, "Upscale", "/dreamina/cli/v1/image_generate", buildUpscalePayload(req))
}

// GetHistoryByIds 查询一个或多个提交记录的历史状态，并把远端 wrapper 结果整理成统一 history 视图。
func (c *HTTPClient) GetHistoryByIds(ctx context.Context, sess *Session, req *GetHistoryByIdsRequest) (*GetHistoryByIdsResponse, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	normalized := normalizeHistoryLookupRequest(req)
	if len(normalized.HistoryIDs) == 0 && len(normalized.SubmitIDs) == 0 {
		return &GetHistoryByIdsResponse{
			Code:    "0",
			Message: "ok",
			LogID:   buildMCPLogID("history"),
			Items:   map[string]*HistoryItem{},
		}, nil
	}
	reqHeaders := map[string]string{}
	mergeSessionForwardHeaders(reqHeaders, sess)
	reqHeaders["Accept"] = "application/json"
	reqHeaders["Appid"] = mcpAppIDHeader
	reqHeaders["Content-Type"] = "application/json"
	reqHeaders["Pf"] = mcpPFHeader
	reqHeaders["X-Tt-Logid"] = buildMCPLogID("history")
	request, err := c.http.NewRequest(ctx, "POST", "/mweb/v1/get_history_by_ids", normalized, reqHeaders, defaultMCPQuery())
	if err != nil {
		return buildHistoryRequestErrorResponse("request_build_error", err), nil
	}
	c.applyHeaders(ctx, sess, request)
	respAny, err := c.http.Do(ctx, request)
	if err != nil {
		return buildHistoryRequestErrorResponse("transport_error", err), nil
	}
	resp, ok := respAny.(*httpclient.Response)
	if !ok || resp == nil {
		return buildHistoryRequestErrorResponse("invalid_response", fmt.Errorf("response is required")), nil
	}
	body, encoding, err := httpclient.ReadDecodedResponseBody(resp)
	if err != nil {
		writeHistoryProbe(request, resp, nil, "", err)
		return buildHistoryTransportResponse(resp, nil, "", err), nil
	}
	writeHistoryProbe(request, resp, body, encoding, nil)
	if !json.Valid(body) {
		return buildHistoryTransportResponse(resp, body, encoding, nil), nil
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return buildHistoryTransportResponse(resp, body, encoding, err), nil
	}
	parsed := parseHistoryResponsePayload(payload)
	return parsed, nil
}

// doGenerate 统一处理各类生成接口提交，并把非成功响应码转换成 APIError。
func (c *HTTPClient) doGenerate(ctx context.Context, sess *Session, op string, path string, body any) (*BaseResponse, error) {
	if sess == nil {
		return nil, &APIError{Code: "session_required", Message: "session is required", LogID: buildMCPLogID("session-required")}
	}
	var response BaseResponse
	if err := c.doPost(ctx, sess, op, path, body, nil, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.Code) != "0" {
		return nil, &APIError{
			Code:     response.Code,
			Message:  response.Message,
			LogID:    response.LogID,
			SubmitID: responseSubmitID(&response),
		}
	}
	return &response, nil
}

func responseSubmitID(resp *BaseResponse) string {
	if resp == nil {
		return ""
	}
	if submitID := firstNestedStringValue(resp.Data, "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID"); strings.TrimSpace(submitID) != "" {
		return strings.TrimSpace(submitID)
	}
	return strings.TrimSpace(firstNestedStringValue(resp.Recovered, "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID"))
}

// doPost 负责发送 MCP POST 请求、解析响应，并补齐恢复期需要的 recovered 诊断信息。
func (c *HTTPClient) doPost(ctx context.Context, sess *Session, op string, path string, body any, extraHeaders map[string]string, out any) error {
	if sess == nil {
		return fmt.Errorf("session is required")
	}
	reqHeaders := map[string]string{}
	mergeSessionForwardHeaders(reqHeaders, sess)
	reqHeaders["Accept"] = "application/json"
	reqHeaders["Appid"] = mcpAppIDHeader
	if strings.TrimSpace(sess.Cookie) != "" {
		reqHeaders["Cookie"] = sess.Cookie
	}
	if body != nil {
		reqHeaders["Content-Type"] = "application/json"
	}
	reqHeaders["Pf"] = mcpPFHeader
	reqHeaders["X-Tt-Logid"] = buildMCPLogID(op)
	for key, value := range extraHeaders {
		reqHeaders[key] = value
	}
	req, err := c.http.NewRequest(ctx, "POST", path, body, reqHeaders, defaultMCPQuery())
	if err != nil {
		return err
	}
	c.applyHeaders(ctx, sess, req)
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		return err
	}
	resp, ok := respAny.(*httpclient.Response)
	if !ok || resp == nil {
		return fmt.Errorf("%s: invalid response", op)
	}
	rawBody, encoding, err := httpclient.ReadDecodedResponseBody(resp)
	if err != nil {
		return err
	}
	payload := parseMCPResponsePayload(resp, rawBody, encoding)
	code := strings.TrimSpace(firstMetadataStringValue(payload, "ret", "Ret", "code", "Code", "status_code", "statusCode", "StatusCode"))
	if code == "" {
		code = "0"
	}
	message := strings.TrimSpace(firstMetadataStringValue(payload, "message", "Message", "msg", "Msg", "description", "Description", "error_message", "errorMessage", "ErrorMessage"))
	if message == "" {
		message = "ok"
	}
	logID := strings.TrimSpace(firstMetadataStringValue(payload, "log_id", "logId", "LogID", "logid", "request_id", "requestId", "RequestId", "RequestID"))
	if logID == "" {
		logID = strings.TrimSpace(firstResponseHeaderValue(resp, "X-Tt-Logid", "X-TT-LOGID"))
	}
	if logID == "" {
		logID = buildMCPLogID(op)
	}
	responseData := primaryResponseData(payload)
	result := BaseResponse{
		Code:      code,
		Message:   message,
		LogID:     logID,
		Data:      responseData,
		Recovered: buildRecoveredResponseMeta(op, path, body, sess, payload, responseData),
		Curl:      buildCurl(req),
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

func (c *HTTPClient) applyHeaders(ctx context.Context, sess *Session, req any) {
	_ = ctx
	request, ok := req.(*httpclient.Request)
	if !ok || request == nil {
		return
	}
	if request.Headers == nil {
		request.Headers = map[string]string{}
	}
	c.http.ApplyBackendHeaders(request)
}

func mergeSessionForwardHeaders(dst map[string]string, sess *Session) {
	if dst == nil || sess == nil || len(sess.Headers) == 0 {
		return
	}
	for key, value := range sess.Headers {
		canonicalKey := canonicalMCPHeaderKey(key)
		if shouldSkipMCPForwardHeader(canonicalKey) {
			continue
		}
		value = strings.TrimSpace(value)
		if canonicalKey == "" || value == "" {
			continue
		}
		dst[canonicalKey] = value
	}
}

func shouldSkipMCPForwardHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "host", "content-length", "connection":
		return true
	default:
		return false
	}
}

func canonicalMCPHeaderKey(key string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "-")
}

func defaultMCPQuery() map[string]any {
	return map[string]any{
		"aid":         mcpAppIDHeader,
		"from":        "dreamina_cli",
		"cli_version": strings.TrimSpace(buildinfo.Version),
	}
}

func buildMCPLogID(seed string) string {
	seed = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(seed), " ", "-"))
	if seed == "" {
		seed = "request"
	}
	prefix := time.Now().Format("20060102150405")
	var suffix string
	var buf [10]byte
	if _, err := rand.Read(buf[:]); err == nil {
		suffix = strings.ToUpper(hex.EncodeToString(buf[:]))
	} else {
		sum := sha1.Sum([]byte(fmt.Sprintf("%s:%d", seed, time.Now().UnixNano())))
		suffix = strings.ToUpper(hex.EncodeToString(sum[:10]))
	}
	if len(suffix) < 19 {
		suffix = suffix + strings.Repeat("0", 19-len(suffix))
	}
	return prefix + suffix[:19]
}

func buildCurl(req *httpclient.Request) string {
	if req == nil {
		return ""
	}
	parts := []string{"curl", "-X", req.Method, quoteShell(req.Path)}
	keys := make([]string, 0, len(req.Headers))
	for key := range req.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, "-H", quoteShell(key+": "+req.Headers[key]))
	}
	if len(req.Body) > 0 {
		parts = append(parts, "--data", quoteShell(string(req.Body)))
	}
	return strings.Join(parts, " ")
}

func buildText2ImagePayload(req *Text2ImageRequest) map[string]any {
	// 原始二进制会把文生图提交改写成专用 image_generate schema，而不是直接序列化 Text2ImageRequest。
	// 这里按断点抓到的真实请求体对齐固定字段、默认 ratio 以及 model_key 映射。
	submitID := newText2ImageSubmitID()
	payload := map[string]any{
		"agent_scene":            "workbench",
		"creation_agent_version": "3.0.0",
		"generate_type":          "text2imageByConfig",
		"prompt":                 "",
		"ratio":                  "16:9",
		"subject_id":             submitID,
		"submit_id":              submitID,
	}
	if req == nil {
		return payload
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if ratio := strings.TrimSpace(req.Ratio); ratio != "" {
		payload["ratio"] = ratio
	}
	if modelKey := strings.TrimSpace(req.ModelVersion); modelKey != "" {
		payload["model_key"] = modelKey
	}
	if resolutionType := strings.TrimSpace(req.ResolutionType); resolutionType != "" {
		payload["resolution_type"] = resolutionType
	}
	return payload
}

func buildImage2ImagePayload(req *Image2ImageRequest) map[string]any {
	// 原始二进制的图生图提交同样走 image_generate，但 generate_type 固定为 editImageByConfig，
	// 并且资源字段名是 resource_id_list，而不是旧 MCP 结构里的 media_resource_id_list。
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"agent_scene":            "workbench",
		"creation_agent_version": "3.0.0",
		"generate_type":          "editImageByConfig",
		"prompt":                 "",
		"ratio":                  "16:9",
		"resource_id_list":       []string{},
		"subject_id":             submitID,
		"submit_id":              submitID,
	}
	if req == nil {
		return payload
	}
	if explicitSubmitID := strings.TrimSpace(req.SubmitID); explicitSubmitID != "" {
		payload["submit_id"] = explicitSubmitID
		payload["subject_id"] = explicitSubmitID
	}
	if resourceIDs := append([]string(nil), req.ResourceIDList...); len(resourceIDs) > 0 {
		payload["resource_id_list"] = resourceIDs
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if ratio := strings.TrimSpace(req.Ratio); ratio != "" {
		payload["ratio"] = ratio
	}
	if resolutionType := strings.TrimSpace(req.ResolutionType); resolutionType != "" {
		payload["resolution_type"] = resolutionType
	}
	if modelKey := strings.TrimSpace(req.ModelVersion); modelKey != "" {
		payload["model_key"] = modelKey
	}
	return payload
}

func buildUpscalePayload(req *UpscaleRequest) map[string]any {
	// 原始二进制的图片超分并不走旧的 /mcp/v1/upscale，
	// 而是复用 image_generate 提交面，并固定 generate_type=imageSuperResolution。
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"agent_scene":            "workbench",
		"creation_agent_version": "3.0.0",
		"generate_type":          "imageSuperResolution",
		"subject_id":             submitID,
		"submit_id":              submitID,
	}
	if req == nil {
		return payload
	}
	if resourceID := strings.TrimSpace(req.ResourceID); resourceID != "" {
		payload["resource_id"] = resourceID
	}
	if resolutionType := strings.TrimSpace(req.ResolutionType); resolutionType != "" {
		payload["resolution_type"] = resolutionType
	}
	return payload
}

func buildImage2VideoPayload(req *Image2VideoRequest) map[string]any {
	// 图生视频现在有两条提交形态：
	// - 默认路径 generate_type=image2video
	// - 高级参数路径 generate_type=image2VideoByConfig，并补默认 model_key
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"generate_type":          "image2video",
		"agent_scene":            "workbench",
		"prompt":                 "",
		"ratio":                  "",
		"duration":               5,
		"creation_agent_version": "3.0.0",
		"submit_id":              submitID,
	}
	if req == nil {
		return payload
	}
	if req.UseByConfig {
		payload["generate_type"] = "firstFrameVideoByConfig"
		payload["model_key"] = "seedance2.0fast"
	}
	if firstFrameResourceID := strings.TrimSpace(req.FirstFrameResourceID); firstFrameResourceID != "" {
		payload["first_frame_resource_id"] = firstFrameResourceID
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if req.Duration > 0 {
		payload["duration"] = req.Duration
	}
	if videoResolution := strings.TrimSpace(req.VideoResolution); videoResolution != "" {
		// 原程序在高级路径未显式指定模型时，会落到 seedance2.0fast 默认模型；
		// 该模型只接受 720p，所以这里避免把不兼容分辨率直接传给后端。
		if !req.UseByConfig || strings.TrimSpace(req.ModelVersion) != "" || strings.EqualFold(videoResolution, "720p") {
			payload["video_resolution"] = videoResolution
		}
	}
	if modelKey := strings.TrimSpace(req.ModelVersion); modelKey != "" {
		payload["model_key"] = modelKey
	}
	return payload
}

func buildText2VideoPayload(req *Text2VideoRequest) map[string]any {
	// 原始二进制的文生视频走 by-config 提交形态：
	// generate_type=text2VideoByConfig，并默认显式携带 model_key=seedance2.0fast。
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"generate_type":          "text2VideoByConfig",
		"agent_scene":            "workbench",
		"prompt":                 "",
		"ratio":                  "16:9",
		"duration":               5,
		"creation_agent_version": "3.0.0",
		"model_key":              "seedance2.0fast",
		"submit_id":              submitID,
	}
	if req == nil {
		return payload
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if ratio := strings.TrimSpace(req.Ratio); ratio != "" {
		payload["ratio"] = ratio
	}
	if req.Duration > 0 {
		payload["duration"] = req.Duration
	}
	if videoResolution := strings.TrimSpace(req.VideoResolution); videoResolution != "" {
		payload["video_resolution"] = videoResolution
	}
	if modelKey := strings.TrimSpace(req.ModelVersion); modelKey != "" {
		payload["model_key"] = modelKey
	}
	return payload
}

func buildFrames2VideoPayload(req *Frames2VideoRequest) map[string]any {
	// 原始二进制的首尾帧视频同样走统一视频生成工具接口，
	// generate_type 固定为 startEndFrameVideoByConfig。
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"generate_type":          "startEndFrameVideoByConfig",
		"agent_scene":            "workbench",
		"prompt":                 "",
		"duration":               5,
		"creation_agent_version": "3.0.0",
		"submit_id":              submitID,
	}
	if req == nil {
		return payload
	}
	if firstFrameResourceID := strings.TrimSpace(req.FirstFrameResourceID); firstFrameResourceID != "" {
		payload["first_frame_resource_id"] = firstFrameResourceID
	}
	if lastFrameResourceID := strings.TrimSpace(req.LastFrameResourceID); lastFrameResourceID != "" {
		payload["last_frame_resource_id"] = lastFrameResourceID
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if req.Duration > 0 {
		payload["duration"] = req.Duration
	}
	if videoResolution := strings.TrimSpace(req.VideoResolution); videoResolution != "" {
		payload["video_resolution"] = videoResolution
	}
	if modelKey := strings.TrimSpace(req.ModelVersion); modelKey != "" {
		payload["model_key"] = modelKey
	}
	return payload
}

func buildRef2VideoPayload(req *Ref2VideoRequest) map[string]any {
	// 原始二进制的 multiframe2video/ref2video 也走统一视频生成接口，
	// generate_type 固定为 multiFrame2video，并显式携带 media_type_list/prompt_list/duration_list。
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"generate_type":          "multiFrame2video",
		"agent_scene":            "workbench",
		"creation_agent_version": "3.0.0",
		"submit_id":              submitID,
		"media_resource_id_list": []string{},
		"media_type_list":        []string{},
		"prompt_list":            []string{},
		"duration_list":          []float64{},
	}
	if req == nil {
		return payload
	}
	if mediaIDs := append([]string(nil), req.MediaResourceIDList...); len(mediaIDs) > 0 {
		payload["media_resource_id_list"] = mediaIDs
		mediaTypes := append([]string(nil), req.MediaTypeList...)
		if len(mediaTypes) == 0 {
			mediaTypes = make([]string, len(mediaIDs))
			for i := range mediaTypes {
				mediaTypes[i] = "图片"
			}
		}
		payload["media_type_list"] = mediaTypes
	}
	if promptList := append([]string(nil), req.PromptList...); len(promptList) > 0 {
		payload["prompt_list"] = promptList
	}
	if durationList := append([]float64(nil), req.DurationList...); len(durationList) > 0 {
		payload["duration_list"] = durationList
	}
	return payload
}

func buildMultiModal2VideoPayload(req *MultiModal2VideoRequest) map[string]any {
	// 原始二进制里 multimodal2video 仍然走统一视频生成工具接口，
	// generate_type 对齐到 multiModal2VideoByConfig，而不是旧的 /mcp/v1/multimodal2video 页面路由。
	submitID := newImageGenerateSubmitID()
	payload := map[string]any{
		"generate_type":          "multiModal2VideoByConfig",
		"agent_scene":            "workbench",
		"creation_agent_version": "3.0.0",
		"submit_id":              submitID,
		"prompt":                 "",
		"ratio":                  "16:9",
		"duration":               5,
		"model_key":              "seedance2.0fast",
		"image_resource_id_list": []string{},
		"video_resource_id_list": []string{},
		"audio_resource_id_list": []string{},
	}
	if req == nil {
		return payload
	}
	if imageIDs := append([]string(nil), req.ImageResourceIDList...); len(imageIDs) > 0 {
		payload["image_resource_id_list"] = imageIDs
	}
	if videoIDs := append([]string(nil), req.VideoResourceIDList...); len(videoIDs) > 0 {
		payload["video_resource_id_list"] = videoIDs
	}
	if audioIDs := append([]string(nil), req.AudioResourceIDList...); len(audioIDs) > 0 {
		payload["audio_resource_id_list"] = audioIDs
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if ratio := strings.TrimSpace(req.Ratio); ratio != "" {
		payload["ratio"] = ratio
	}
	if req.Duration > 0 {
		payload["duration"] = req.Duration
	}
	if videoResolution := strings.TrimSpace(req.VideoResolution); videoResolution != "" {
		payload["video_resolution"] = videoResolution
	}
	if modelKey := strings.TrimSpace(req.ModelVersion); modelKey != "" {
		payload["model_key"] = modelKey
	}
	return payload
}

func newText2ImageSubmitID() string {
	return newImageGenerateSubmitID()
}

func newImageGenerateSubmitID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		sum := sha1.Sum([]byte(fmt.Sprintf("text2image:%d", time.Now().UnixNano())))
		return hex.EncodeToString(sum[:8])
	}
	return hex.EncodeToString(buf[:])
}

func quoteShell(s string) string {
	body, _ := json.Marshal(s)
	return string(body)
}

// sessionView 生成一个可安全写入 recovered 的精简会话视图，不直接暴露 cookie 和 header 值。
func sessionView(sess *Session) map[string]any {
	if sess == nil {
		return map[string]any{}
	}
	return map[string]any{
		"user_id":     strings.TrimSpace(sess.UserID),
		"has_cookie":  strings.TrimSpace(sess.Cookie) != "",
		"header_keys": sortedHeaderKeys(sess.Headers),
	}
}

// buildRecoveredResponseMeta 构造提交响应旁路保存的 recovered 元数据，供 query/view 在恢复期继续追踪提交依据。
func buildRecoveredResponseMeta(op string, path string, body any, sess *Session, rawPayload map[string]any, responseData any) map[string]any {
	// 生成接口的真实返回优先保留在 Data 中；恢复补出来的上下文统一放到 recovered，
	// 这样下游仍可读取提交态信息，但不会继续把恢复包装误当成远端主 schema。
	// 这里的 recovered 不是“另一个响应体”，而是“为了让 query/view 在缺少原始私有 schema 时还能拿到提交依据”的旁路元数据。
	meta := map[string]any{
		"op":         strings.TrimSpace(op),
		"path":       strings.TrimSpace(path),
		"request":    body,
		"session":    sessionView(sess),
		"history_id": recoveredHistoryID(historyIDFromBody(body), responseData, rawPayload),
	}
	if submittedAt := recoveredSubmittedAt(responseData, rawPayload); submittedAt > 0 {
		meta["submitted"] = submittedAt
	}
	if transport := sanitizeTransportPayload(transportPayloadSource(responseData, rawPayload)); transport != nil {
		meta["transport"] = transport
	}
	return meta
}

func recoveredHistoryID(fallback string, sources ...any) string {
	// 提交接口常把 history_id/submit_id 包在 Payload/Data/Result 多层 wrapper 里。
	// 这里递归吸收常见别名，避免 recovered 元数据因为外层包装而丢失。
	for _, source := range sources {
		value := firstNestedStringValue(source, "history_id", "historyId", "HistoryID", "submit_id", "submitId", "SubmitID", "task_id", "taskId", "TaskID")
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(fallback)
}

func recoveredSubmittedAt(sources ...any) int64 {
	for _, source := range sources {
		// submitted_at / submitted 两种键都保留，是为了兼容提交响应里不同版本的时间字段命名。
		// 这里只吸收远端已有时间，不再生成本地当前时间，避免恢复链路继续污染时间线。
		if submittedAt := firstNestedInt64Value(
			source,
			"submitted_at", "submittedAt", "SubmittedAt",
			"submitted", "Submitted",
		); submittedAt > 0 {
			return submittedAt
		}
	}
	return 0
}

func transportPayloadSource(sources ...any) any {
	// Transport 既可能直接挂在 data 下，也可能继续包在 Data/Result/Payload 中。
	// 优先递归寻找具名 transport，找不到时再回落到当前 responseData。
	for _, source := range sources {
		if transport := firstNestedMapValue(source, "transport", "Transport"); len(transport) > 0 {
			return transport
		}
	}
	if len(sources) > 0 {
		return sources[0]
	}
	return nil
}

func sanitizeTransportPayload(v any) any {
	root, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	// transport 里的 method/path/query 对后续排查 schema 很有价值，但 headers 里容易夹带 cookie/token。
	// 这里做“保结构、去敏感”的净化，保证 recovered 可用于诊断而不继续泄露认证材料。
	out := map[string]any{}
	for _, keys := range [][]string{
		{"method", "Method"},
		{"path", "Path"},
		{"query", "Query"},
		{"headers", "Headers"},
	} {
		value := firstPresentValue(root, keys...)
		if value == nil {
			continue
		}
		switch keys[0] {
		case "headers":
			out[keys[0]] = sanitizeTransportHeaders(value)
		default:
			out[keys[0]] = value
		}
	}
	out["sanitized"] = true
	return out
}

// primaryResponseData 从 MCP 响应里挑出最像“主业务正文”的那层 data/result/payload。
func primaryResponseData(payload map[string]any) any {
	for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
		if value, ok := payload[key]; ok {
			return unwrapPrimaryResponseData(value)
		}
	}
	for _, wrapper := range []string{"response", "Response", "error", "Error", "meta", "Meta"} {
		wrapped, ok := payload[wrapper].(map[string]any)
		if !ok || len(wrapped) == 0 {
			continue
		}
		for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
			if value, ok := wrapped[key]; ok {
				return unwrapPrimaryResponseData(value)
			}
		}
	}
	if len(payload) == 0 {
		return nil
	}
	// 如果没有标准 data/result/payload 容器，就把“去掉元数据壳之后的正文”直接当成业务层数据。
	// 这样可以尽量避免下游继续感知 ret/code/message 这一层包装。
	out := map[string]any{}
	for key, value := range payload {
		switch key {
		case "ret", "Ret", "code", "Code", "status", "Status", "message", "Message", "msg", "Msg", "log_id", "logId", "LogID", "logid", "request_id", "requestId", "RequestId":
			continue
		default:
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return unwrapPrimaryResponseData(out)
}

func unwrapPrimaryResponseData(value any) any {
	current, ok := value.(map[string]any)
	if !ok || len(current) == 0 {
		return value
	}
	for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
		child, exists := current[key]
		if !exists || child == nil {
			continue
		}
		if shouldUnwrapPrimaryResponseData(current, key) {
			return unwrapPrimaryResponseData(child)
		}
	}
	return value
}

func shouldUnwrapPrimaryResponseData(root map[string]any, selectedKey string) bool {
	remaining := 0
	for key, value := range root {
		if key == selectedKey || value == nil {
			continue
		}
		switch key {
		case "ret", "Ret", "code", "Code", "status", "Status",
			"message", "Message", "msg", "Msg", "description", "Description",
			"log_id", "logId", "LogID", "logid", "request_id", "requestId", "RequestId", "RequestID",
			"meta", "Meta", "error", "Error", "response", "Response", "transport", "Transport":
			continue
		default:
			remaining++
		}
	}
	return remaining == 0
}

// sanitizeTransportHeaders 对 transport.headers 做白名单筛选和脱敏，保留诊断价值但去掉敏感信息。
func sanitizeTransportHeaders(v any) any {
	headers, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowTransportHeaderKey(key) {
			continue
		}
		out[key] = redactTransportHeaderValue(key, fmt.Sprint(headers[key]))
	}
	out["header_count"] = len(headers)
	out["forwarded_keys"] = keys
	return out
}

// redactTransportHeaderValue 按 header 类型对敏感值做脱敏或摘要化展示。
func redactTransportHeaderValue(key string, value string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch {
	case lower == "cookie":
		return "<redacted-cookie>"
	case strings.Contains(lower, "auth"), strings.Contains(lower, "token"), strings.Contains(lower, "sign"), strings.Contains(lower, "secret"):
		if len(value) <= 8 {
			return "<redacted>"
		}
		return value[:4] + "..." + value[len(value)-4:]
	default:
		return value
	}
}

// allowTransportHeaderKey 控制 recovered.transport 中允许透传的请求头白名单。
func allowTransportHeaderKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch lower {
	case "accept", "content-type", "user-agent", "x-client-scheme", "x-request-op", "x-tt-logid", "x-user-id", "cookie":
		return true
	default:
		return false
	}
}

// sortedHeaderKeys 返回排序后的 header key 列表，避免 recovered/session 视图受 map 顺序抖动影响。
func sortedHeaderKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func firstResponseHeaderValue(resp *httpclient.Response, keys ...string) string {
	if resp == nil || len(resp.Headers) == 0 {
		return ""
	}
	for _, key := range keys {
		for currentKey, value := range resp.Headers {
			if strings.EqualFold(strings.TrimSpace(currentKey), strings.TrimSpace(key)) {
				value = strings.TrimSpace(value)
				if value != "" && value != "<nil>" {
					return value
				}
			}
		}
	}
	return ""
}

func historyIDFromBody(body any) string {
	payload, ok := body.(interface{ GetHistoryID() string })
	if ok {
		if historyID := strings.TrimSpace(payload.GetHistoryID()); historyID != "" {
			return historyID
		}
	}
	// 提交接口如果没有远端 history_id，就保持为空，避免继续伪造本地 hash ID 污染 query 链路。
	return ""
}

// normalizeHistoryLookupRequest 归一化 history 查询参数，统一 submit_id/history_id 去重和批量开关语义。
func normalizeHistoryLookupRequest(req *GetHistoryByIdsRequest) *GetHistoryByIdsRequest {
	if req == nil {
		req = &GetHistoryByIdsRequest{}
	}
	historyIDs := uniqueTrimmedStrings(req.HistoryIDs)
	submitIDs := uniqueTrimmedStrings(req.SubmitIDs)
	needBatch := true
	return &GetHistoryByIdsRequest{
		SubmitIDs:  submitIDs,
		HistoryIDs: historyIDs,
		NeedBatch:  needBatch,
	}
}

func buildHistoryTransportResponse(resp *httpclient.Response, body []byte, encoding string, readErr error) *GetHistoryByIdsResponse {
	// history 查询一旦已经拿到服务端响应，就尽量保留后端状态而不是伪造 submitted。
	// 当前已经不再走本地 submitted fallback，避免把 4xx/5xx/非 JSON 误报成排队中。
	statusCode := responseStatusCode(resp)
	code := "response_decode_error"
	message := "unexpected non-json response"
	logID := buildMCPLogID("history-transport")
	bodyPreview := nonJSONBodyPreview(body)
	bodyMessage := summarizeNonJSONBodyPreview(bodyPreview)

	if statusCode >= 400 {
		code = strconv.Itoa(statusCode)
		message = "backend request failed"
	}
	if bodyMessage != "" {
		message = bodyMessage
	}
	if readErr != nil {
		code = "response_read_error"
		message = strings.TrimSpace(readErr.Error())
	}
	if strings.TrimSpace(encoding) != "" {
		logID = buildMCPLogID("history-" + strings.ToLower(strings.TrimSpace(encoding)))
	}
	if statusCode == 0 && readErr == nil && len(body) == 0 {
		message = "empty response body"
	}
	return &GetHistoryByIdsResponse{
		Code:        code,
		Message:     message,
		LogID:       logID,
		BodyPreview: bodyPreview,
		Items:       map[string]*HistoryItem{},
	}
}

func buildHistoryRequestErrorResponse(code string, err error) *GetHistoryByIdsResponse {
	message := "history request failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}
	// 这里只有“本地请求阶段就失败”才会走到这里，和已经拿到服务端响应的 transport error 分开。
	// 这样 query 侧后续可以明确区分：是请求没发出去，还是后端已经返回了错误正文/状态码。
	logID := buildMCPLogID("history-request")
	switch strings.TrimSpace(code) {
	case "transport_error":
		logID = buildMCPLogID("history-transport")
	case "invalid_response":
		logID = buildMCPLogID("history-invalid-response")
	}
	return &GetHistoryByIdsResponse{
		Code:    strings.TrimSpace(code),
		Message: message,
		LogID:   logID,
		Items:   map[string]*HistoryItem{},
	}
}

// parseHistoryResponsePayload 解析 history 查询响应，并把远端 wrapper 结果压平成统一的 GetHistoryByIdsResponse。
func parseHistoryResponsePayload(payload map[string]any) *GetHistoryByIdsResponse {
	code := strings.TrimSpace(firstMetadataStringValue(payload, "ret", "Ret", "code", "Code", "status_code", "statusCode", "StatusCode"))
	if code == "" {
		code = "0"
	}
	message := strings.TrimSpace(firstMetadataStringValue(payload, "message", "Message", "msg", "Msg", "description", "Description", "error_message", "errorMessage", "ErrorMessage"))
	if message == "" {
		message = "ok"
	}
	logID := strings.TrimSpace(firstMetadataStringValue(payload, "log_id", "logId", "LogID", "logid", "request_id", "requestId", "RequestId", "RequestID"))
	if logID == "" {
		logID = buildMCPLogID("history")
	}
	// history 响应常见 Data/Result/Payload 多层套壳，甚至 Items/Results 再包一层 Data。
	// 这里递归查找真实 item 容器，尽量避免“后端有结果但本地没命中”。
	items := parseHistoryItems(findHistoryItemsValue(payload))
	return &GetHistoryByIdsResponse{
		Code:    code,
		Message: message,
		LogID:   logID,
		Items:   items,
	}
}

// parseHistoryItems 把 history 响应中的数组或 keyed-object 容器统一转换成可按主键索引的 history map。
func parseHistoryItems(v any) map[string]*HistoryItem {
	out := map[string]*HistoryItem{}
	switch value := v.(type) {
	case map[string]any:
		// 一部分 history 返回会把真正的 items 容器再包进 keyed-object 的 data/payload/result 里。
		// 这里先尝试继续向下拆壳；只有确认当前层已经是“history_id -> item”对象时，才按键值对逐项吸收。
		if !looksLikeKeyedHistoryItemsObject(value) {
			if nested := parseHistoryItems(findHistoryItemsValue(value)); len(nested) > 0 {
				return nested
			}
		}
		for key, itemValue := range value {
			item := historyItemFromValue(itemValue)
			if item == nil {
				continue
			}
			if strings.TrimSpace(item.HistoryID) == "" && strings.TrimSpace(item.SubmitID) == "" {
				item.HistoryID = strings.TrimSpace(key)
			}
			addHistoryItem(out, item, key)
		}
	case []any:
		for _, itemValue := range value {
			item := historyItemFromValue(itemValue)
			if item == nil {
				continue
			}
			addHistoryItem(out, item, "")
		}
	}
	return out
}

// historyItemFromValue 把单个 history 节点解析成统一的 HistoryItem 视图。
func historyItemFromValue(v any) *HistoryItem {
	root, ok := v.(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	queueStatus := firstHistoryQueueStatusValue(root,
		"queue_status", "queueStatus", "QueueStatus",
		"queue_state", "queueState", "QueueState",
		"status", "Status",
	)
	item := &HistoryItem{
		SubmitID:        firstNestedStringValue(root, "submit_id", "submitId", "SubmitID"),
		HistoryID:       firstNestedStringValue(root, "history_id", "historyId", "HistoryID"),
		HistoryRecordID: firstNestedStringValue(root, "history_record_id", "historyRecordId", "HistoryRecordID"),
		TaskID:          firstNestedStringValue(root, "task_id", "taskId", "TaskID"),
		Status:          firstNestedStringValue(root, "status", "Status", "gen_status", "genStatus", "GenStatus", "state", "State"),
		QueueStatus:     queueStatus,
		QueueLength:     firstNestedIntValue(root, "queue_length", "queueLength", "QueueLength"),
		QueueIdx:        firstNestedIntValue(root, "queue_idx", "queueIdx", "QueueIdx"),
		QueuePriority:   firstNestedIntValue(root, "priority", "Priority"),
		QueueProgress:   firstNestedProgressValue(root, "progress", "Progress", "percent", "Percent"),
		QueueDebugInfo:  firstNestedStringValue(root, "debug_info", "debugInfo", "DebugInfo"),
		ImageURL:        firstNestedStringValue(root, "image_url", "imageUrl", "ImageURL", "image_uri", "imageUri", "ImageUri", "ImageURI", "file_url", "fileUrl", "FileURL"),
		VideoURL:        firstNestedStringValue(root, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI"),
		Raw:             cloneAnyMap(root),
	}
	// history 返回里的队列信息、媒体数组和 details 经常散在不同层级。
	// 这里优先吸收常见别名和嵌套列表，尽量把远端结果整理成统一视图供 gen/query 复用。
	item.Images = parseHistoryImages(root)
	item.Videos = parseHistoryVideos(root)
	if preferredVideos := parseHistoryOriginVideos(root); len(preferredVideos) > 0 {
		item.Videos = preferredVideos
	}
	item.Details = parseHistoryDetails(root)
	if len(item.Images) == 0 {
		// 某些 schema 不给 images 数组，只把多个 image_url 散落在 detail/result wrapper 里。
		// 这里做一次兜底聚合，保证 query/view 至少还能拿到稳定的一组图片地址。
		imageURLs := uniqueTrimmedStrings(append(collectNestedStringValues(root, "image_url", "imageUrl"), item.ImageURL))
		if len(imageURLs) > 0 {
			item.Images = make([]*HistoryImage, 0, len(imageURLs))
			for _, url := range imageURLs {
				item.Images = append(item.Images, &HistoryImage{
					URL:      url,
					ImageURL: url,
				})
			}
		}
	}
	if len(item.Videos) == 0 {
		// 视频结果也存在“只有若干 video_url/play_url，没有标准 videos 容器”的形态。
		// 这里和图片一样补一次聚合，避免媒体已经返回但本地视图仍然空白。
		videoURLs := uniqueTrimmedStrings(append(collectNestedStringValues(root, "video_url", "videoUrl", "play_url", "playUrl"), item.VideoURL))
		if len(videoURLs) > 0 {
			item.Videos = make([]*HistoryVideo, 0, len(videoURLs))
			for _, url := range videoURLs {
				item.Videos = append(item.Videos, &HistoryVideo{
					URL:      url,
					VideoURL: url,
				})
			}
		}
	}
	if len(item.Images) > 0 {
		item.ImageURL = firstNonEmptyHistoryString(item.Images[0].ImageURL, item.Images[0].URL, item.ImageURL)
	}
	if len(item.Videos) > 0 {
		item.VideoURL = firstNonEmptyHistoryString(item.Videos[0].VideoURL, item.Videos[0].URL, item.VideoURL)
	}
	if firstNestedValue(root, "queue_info", "queueInfo", "QueueInfo", "queue", "Queue") != nil ||
		item.QueueStatus != "" || item.QueueLength > 0 || item.QueueIdx > 0 || item.QueuePriority > 0 || item.QueueProgress > 0 || item.QueueDebugInfo != "" {
		item.Queue = &QueueInfo{
			QueueStatus: item.QueueStatus,
			QueueLength: item.QueueLength,
			QueueIdx:    item.QueueIdx,
			Priority:    item.QueuePriority,
			Progress:    item.QueueProgress,
			DebugInfo:   item.QueueDebugInfo,
		}
	}
	if item.GetHistoryID() == "" && item.GetSubmitID() == "" && item.GetStatus() == "" {
		return nil
	}
	return item
}

func parseHistoryImages(root map[string]any) []*HistoryImage {
	for _, source := range preferredHistoryImageSources(root) {
		if images := parseHistoryImageList(source); len(images) > 0 {
			return images
		}
	}
	return nil
}

func parseHistoryImageList(value any) []*HistoryImage {
	list := normalizeHistoryCollection(value, looksLikeHistoryImageNode)
	if len(list) == 0 {
		return nil
	}
	out := make([]*HistoryImage, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		root, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		image := &HistoryImage{
			URL:      firstNestedStringValue(root, "url", "URL", "image_url", "imageUrl", "ImageURL", "image_uri", "imageUri", "ImageUri", "ImageURI", "file_url", "fileUrl", "FileURL"),
			ImageURL: firstNestedStringValue(root, "image_url", "imageUrl", "ImageURL", "image_uri", "imageUri", "ImageUri", "ImageURI", "file_url", "fileUrl", "FileURL", "url", "URL"),
			Origin:   firstNestedStringValue(root, "origin", "Origin", "type", "Type", "resource_type", "resourceType", "ResourceType"),
			Width:    firstNestedIntValue(root, "width", "Width"),
			Height:   firstNestedIntValue(root, "height", "Height"),
		}
		// history 图片节点经常会同时带 url/image_url/file_url 等别名。
		// 这里统一用首个非空媒体地址去重，避免同一张图因为字段名不同被重复塞回结果。
		key := firstNonEmptyHistoryString(image.ImageURL, image.URL)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, image)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseHistoryVideos(root map[string]any) []*HistoryVideo {
	return parseHistoryVideoList(
		firstNestedValue(root, "videos", "Videos", "video_list", "videoList", "VideoList", "video_info_list", "videoInfoList", "VideoInfoList", "output_videos", "outputVideos", "OutputVideos", "item_list", "itemList", "ItemList"),
	)
}

func parseHistoryOriginVideos(root map[string]any) []*HistoryVideo {
	for _, source := range preferredHistoryImageSources(root) {
		if videos := parseHistoryOriginVideoList(source); len(videos) > 0 {
			return videos
		}
	}
	return nil
}

func preferredHistoryImageSources(root map[string]any) []any {
	if len(root) == 0 {
		return nil
	}
	collectionKeys := []string{
		"images", "Images",
		"image_list", "imageList", "ImageList",
		"image_info_list", "imageInfoList", "ImageInfoList",
		"output_images", "outputImages", "OutputImages",
		"large_images", "largeImages", "LargeImages",
	}
	sources := make([]any, 0, 8)
	if direct := firstPresentValue(root, collectionKeys...); direct != nil {
		sources = append(sources, direct)
	}
	for _, nested := range []map[string]any{
		firstMapValue(root, "image", "Image"),
		firstMapValue(root, "output", "Output"),
		firstMapValue(root, "result", "Result"),
		firstMapValue(root, "data", "Data"),
	} {
		if len(nested) == 0 {
			continue
		}
		if candidate := firstPresentValue(nested, collectionKeys...); candidate != nil {
			sources = append(sources, candidate)
		}
		if imageNode := firstMapValue(nested, "image", "Image"); len(imageNode) > 0 {
			if candidate := firstPresentValue(imageNode, collectionKeys...); candidate != nil {
				sources = append(sources, candidate)
			}
		}
	}
	if itemList := firstPresentValue(root, "item_list", "itemList", "ItemList"); itemList != nil {
		sources = append(sources, itemList)
	}
	return sources
}

func parseHistoryOriginVideoList(value any) []*HistoryVideo {
	list := normalizeHistoryCollection(value, looksLikeHistoryImageNode)
	if len(list) == 0 {
		return nil
	}
	out := make([]*HistoryVideo, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		root, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		origin := historyOriginMap(root)
		if len(origin) == 0 {
			continue
		}
		video := &HistoryVideo{
			URL:      firstNestedStringValue(origin, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI", "url", "URL"),
			VideoURL: firstNestedStringValue(origin, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI", "url", "URL"),
			CoverURL: firstNestedStringValue(origin, "cover_url", "coverUrl", "CoverURL", "poster_url", "posterUrl", "PosterURL"),
			FPS:      firstNestedIntValue(origin, "fps", "FPS"),
			Width:    firstNestedIntValue(origin, "width", "Width"),
			Height:   firstNestedIntValue(origin, "height", "Height"),
			Format:   firstNestedStringValue(origin, "format", "Format", "file_format", "fileFormat", "FileFormat"),
			Duration: firstNestedFloat64Value(origin, "duration", "Duration"),
		}
		key := firstNonEmptyHistoryString(video.VideoURL, video.URL)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, video)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func historyOriginMap(root map[string]any) map[string]any {
	switch origin := firstValue(root, "origin", "Origin").(type) {
	case map[string]any:
		return origin
	case string:
		return parseStringifiedOriginMap(origin)
	default:
		return nil
	}
}

func parseStringifiedOriginMap(raw string) map[string]any {
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

func parseHistoryVideoList(value any) []*HistoryVideo {
	list := normalizeHistoryCollection(value, looksLikeHistoryVideoNode)
	if len(list) == 0 {
		return nil
	}
	out := make([]*HistoryVideo, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		root, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		video := &HistoryVideo{
			URL:      firstNestedStringValue(root, "url", "URL", "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI"),
			VideoURL: firstNestedStringValue(root, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI", "url", "URL"),
			CoverURL: firstNestedStringValue(root, "cover_url", "coverUrl", "CoverURL", "cover_uri", "coverUri", "CoverUri", "CoverURI", "poster_url", "posterUrl", "PosterURL", "poster_uri", "posterUri", "PosterUri", "PosterURI", "snapshot_url", "snapshotUrl", "SnapshotURL", "snapshot_uri", "snapshotUri", "SnapshotUri", "SnapshotURI"),
			FPS:      firstNestedIntValue(root, "fps", "FPS"),
			Width:    firstNestedIntValue(root, "width", "Width"),
			Height:   firstNestedIntValue(root, "height", "Height"),
			Format:   firstNestedStringValue(root, "format", "Format", "file_format", "fileFormat", "FileFormat"),
			Duration: firstNestedFloat64Value(root, "duration", "Duration"),
			Resources: parseVideoResources(
				firstValue(root, "resources", "Resources", "resource_list", "resourceList", "ResourceList", "video_resource_list", "videoResourceList", "VideoResourceList"),
			),
		}
		// 视频节点的 URL 字段别名更多，还可能同时出现播放地址和文件地址。
		// 这里同样按首个可用视频地址去重，优先保证“同一个远端视频只保留一项稳定视图”。
		key := firstNonEmptyHistoryString(video.VideoURL, video.URL)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, video)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseVideoResources(value any) []*VideoResource {
	list := normalizeHistoryCollection(value, looksLikeVideoResourceNode)
	if len(list) == 0 {
		return nil
	}
	out := make([]*VideoResource, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		root, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resource := &VideoResource{
			URL:      firstNestedStringValue(root, "url", "URL", "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI"),
			VideoURL: firstNestedStringValue(root, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI", "url", "URL"),
			Type:     firstNestedStringValue(root, "type", "Type", "resource_type", "resourceType", "ResourceType"),
		}
		// resource_list 下经常夹杂封面、转码物、空壳 wrapper。
		// 这里只保留能抽出稳定视频地址的资源项，避免 query/view 被不可用节点污染。
		key := firstNonEmptyHistoryString(resource.VideoURL, resource.URL)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, resource)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseHistoryDetails(root map[string]any) []*HistoryItemDetail {
	return parseHistoryDetailList(
		firstNestedValue(root, "details", "Details", "detail_list", "detailList", "DetailList", "results", "Results", "result_list", "resultList", "ResultList"),
	)
}

func parseHistoryDetailList(value any) []*HistoryItemDetail {
	list := normalizeHistoryCollection(value, looksLikeHistoryDetailNode)
	if len(list) == 0 {
		return nil
	}
	out := make([]*HistoryItemDetail, 0, len(list))
	for _, raw := range list {
		root, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		detail := &HistoryItemDetail{
			HistoryRecordID: firstNestedStringValue(root, "history_record_id", "historyRecordId", "HistoryRecordID"),
			Status:          firstNestedStringValue(root, "status", "Status", "gen_status", "genStatus", "GenStatus", "state", "State"),
			QueueStatus:     firstNestedStringValue(root, "queue_status", "queueStatus", "QueueStatus", "queue_state", "queueState", "QueueState"),
			ImageURL:        firstNestedStringValue(root, "image_url", "imageUrl", "ImageURL", "image_uri", "imageUri", "ImageUri", "ImageURI", "file_url", "fileUrl", "FileURL"),
			VideoURL:        firstNestedStringValue(root, "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI"),
		}
		// detail 节点不是每次都会给完整 schema；只要能命中记录 ID、状态或媒体地址中的任一类信息，
		// 就保留下来给上游继续推断，避免远端返回了局部结果却被本地当成空 detail 丢掉。
		if strings.TrimSpace(detail.HistoryRecordID) == "" &&
			strings.TrimSpace(detail.Status) == "" &&
			strings.TrimSpace(detail.QueueStatus) == "" &&
			strings.TrimSpace(detail.ImageURL) == "" &&
			strings.TrimSpace(detail.VideoURL) == "" {
			continue
		}
		out = append(out, detail)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeHistoryCollection(value any, looksSingle func(map[string]any) bool) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		if looksSingle != nil && looksSingle(typed) {
			return []any{typed}
		}
		// keyed-object 容器来自 map，Go 迭代顺序不稳定。
		// 这里统一按 key 排序展开，避免首图/首视频/首资源在同一响应上来回抖动。
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(typed))
		for _, key := range keys {
			child := typed[key]
			switch child.(type) {
			case map[string]any:
				out = append(out, child)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func looksLikeHistoryImageNode(root map[string]any) bool {
	return firstDirectStringValue(root, "url", "URL", "image_url", "imageUrl", "ImageURL", "image_uri", "imageUri", "ImageUri", "ImageURI", "file_url", "fileUrl", "FileURL") != ""
}

func looksLikeHistoryVideoNode(root map[string]any) bool {
	return firstDirectStringValue(root, "url", "URL", "video_url", "videoUrl", "VideoURL", "play_url", "playUrl", "PlayURL", "media_url", "mediaUrl", "MediaURL", "file_url", "fileUrl", "FileURL", "video_uri", "videoUri", "VideoUri", "VideoURI") != ""
}

func looksLikeVideoResourceNode(root map[string]any) bool {
	return looksLikeHistoryVideoNode(root)
}

func looksLikeHistoryDetailNode(root map[string]any) bool {
	return firstDirectStringValue(root,
		"history_record_id", "historyRecordId", "HistoryRecordID",
		"status", "Status",
		"queue_status", "queueStatus", "QueueStatus",
		"image_url", "imageUrl", "ImageURL",
		"video_url", "videoUrl", "VideoURL",
		"media_url", "mediaUrl", "MediaURL",
	) != ""
}

func firstDirectStringValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(root[key]))
		if value == "" || value == "<nil>" {
			continue
		}
		return value
	}
	return ""
}

func firstNonEmptyHistoryString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func addHistoryItem(out map[string]*HistoryItem, item *HistoryItem, fallbackKey string) {
	if item == nil {
		return
	}
	// history keyed-object、数组项和 detail 回填出的主键来源不一致。
	// 这里把 history_id/submit_id/history_record_id 都注册成可查 key，尽量避免查询侧因为字段名漂移取不到命中的项。
	keys := uniqueTrimmedStrings([]string{
		fallbackKey,
		item.GetHistoryID(),
		item.GetSubmitID(),
		item.HistoryRecordID,
	})
	if len(keys) == 0 {
		return
	}
	for _, key := range keys {
		out[key] = item
	}
}

func uniqueTrimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstValue(root map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := root[key]; ok {
			return value
		}
	}
	return nil
}

func firstNestedStringValue(root any, keys ...string) string {
	values := collectNestedStringValues(root, keys...)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func collectNestedStringValues(root any, keys ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	var visit func(any, int)
	visit = func(node any, depth int) {
		if depth > 6 {
			return
		}
		switch value := node.(type) {
		case map[string]any:
			for _, key := range keys {
				text := strings.TrimSpace(fmt.Sprint(value[key]))
				if text == "" || text == "<nil>" {
					continue
				}
				if _, ok := seen[text]; ok {
					continue
				}
				seen[text] = struct{}{}
				out = append(out, text)
			}
			childKeys := make([]string, 0, len(value))
			for key := range value {
				childKeys = append(childKeys, key)
			}
			sort.Strings(childKeys)
			for _, key := range childKeys {
				visit(value[key], depth+1)
			}
		case []any:
			for _, child := range value {
				visit(child, depth+1)
			}
		}
	}
	visit(root, 0)
	return out
}

func firstNestedInt64Value(root any, keys ...string) int64 {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	var visit func(any, int) int64
	visit = func(node any, depth int) int64 {
		if depth > 6 {
			return 0
		}
		switch current := node.(type) {
		case map[string]any:
			for key, value := range current {
				if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; !ok {
					continue
				}
				if number := parsePositiveInt64Value(value); number > 0 {
					return number
				}
			}
			for _, value := range current {
				if number := visit(value, depth+1); number > 0 {
					return number
				}
			}
		case []any:
			for _, value := range current {
				if number := visit(value, depth+1); number > 0 {
					return number
				}
			}
		}
		return 0
	}
	return visit(root, 0)
}

func parsePositiveInt64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return int64(typed)
		}
	case int64:
		if typed > 0 {
			return typed
		}
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil && parsed > 0 {
			return int64(parsed)
		}
	}
	return 0
}

func firstNestedIntValue(root any, keys ...string) int {
	values := collectNestedStringValues(root, keys...)
	for _, value := range values {
		var out int
		if _, err := fmt.Sscanf(value, "%d", &out); err == nil {
			return out
		}
	}
	switch value := root.(type) {
	case map[string]any:
		for _, key := range keys {
			switch typed := value[key].(type) {
			case int:
				return typed
			case int64:
				return int(typed)
			case float64:
				return int(typed)
			}
		}
	}
	return 0
}

func firstNestedFloat64Value(root any, keys ...string) float64 {
	values := collectNestedStringValues(root, keys...)
	for _, value := range values {
		if out, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && out > 0 {
			return out
		}
	}
	switch value := root.(type) {
	case map[string]any:
		for _, key := range keys {
			switch typed := value[key].(type) {
			case float64:
				if typed > 0 {
					return typed
				}
			case float32:
				if typed > 0 {
					return float64(typed)
				}
			case int:
				if typed > 0 {
					return float64(typed)
				}
			case int64:
				if typed > 0 {
					return float64(typed)
				}
			}
		}
	}
	return 0
}

func firstNestedProgressValue(root any, keys ...string) int {
	// history 里的进度既可能是数字，也可能是 "72%" 这类百分比字符串。
	// 这里统一转换成 int，避免 item.View() 在重建 queue 视图时把真实远端进度抹掉。
	values := collectNestedStringValues(root, keys...)
	for _, value := range values {
		if progress := parseProgressValue(value); progress > 0 {
			return progress
		}
	}
	switch value := root.(type) {
	case map[string]any:
		for _, key := range keys {
			if progress := parseProgressValue(value[key]); progress > 0 {
				return progress
			}
		}
	}
	return 0
}

func parseProgressValue(value any) int {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return typed
		}
	case int64:
		if typed > 0 {
			return int(typed)
		}
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case string:
		text := strings.TrimSpace(strings.TrimSuffix(typed, "%"))
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
	}
	return 0
}

func firstNestedMapValue(root any, keys ...string) map[string]any {
	var visit func(any, int) map[string]any
	visit = func(node any, depth int) map[string]any {
		if depth > 6 {
			return nil
		}
		current, ok := node.(map[string]any)
		if !ok || len(current) == 0 {
			return nil
		}
		for _, key := range keys {
			if value, ok := current[key].(map[string]any); ok && len(value) > 0 {
				return value
			}
		}
		for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
			if child := visit(current[key], depth+1); len(child) > 0 {
				return child
			}
		}
		for _, child := range current {
			if nested, ok := child.(map[string]any); ok && len(nested) > 0 {
				if found := visit(nested, depth+1); len(found) > 0 {
					return found
				}
			}
		}
		for _, child := range current {
			if list, ok := child.([]any); ok {
				for _, item := range list {
					if found := visit(item, depth+1); len(found) > 0 {
						return found
					}
				}
			}
		}
		return nil
	}
	return visit(root, 0)
}

func firstNestedValue(root any, keys ...string) any {
	var visit func(any, int) any
	visit = func(node any, depth int) any {
		if depth > 6 {
			return nil
		}
		switch current := node.(type) {
		case map[string]any:
			for _, key := range keys {
				if value, ok := current[key]; ok && value != nil {
					return value
				}
			}
			for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
				if value := visit(current[key], depth+1); value != nil {
					return value
				}
			}
			for _, child := range current {
				if value := visit(child, depth+1); value != nil {
					return value
				}
			}
		case []any:
			for _, child := range current {
				if value := visit(child, depth+1); value != nil {
					return value
				}
			}
		}
		return nil
	}
	return visit(root, 0)
}

func findHistoryItemsValue(root any) any {
	const maxDepth = 7
	var visit func(any, int) any
	visit = func(node any, depth int) any {
		if depth > maxDepth {
			return nil
		}
		switch current := node.(type) {
		case map[string]any:
			for _, key := range []string{"items", "Items", "records", "Records", "history", "History", "history_items", "historyItems", "HistoryItems", "list", "List", "results", "Results"} {
				if value, ok := current[key]; ok && value != nil {
					return value
				}
			}
			if looksLikeKeyedHistoryItemsObject(current) {
				return current
			}
			for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
				if value := visit(current[key], depth+1); value != nil {
					return value
				}
			}
			for _, child := range current {
				if value := visit(child, depth+1); value != nil {
					return value
				}
			}
		case []any:
			for _, child := range current {
				if value := visit(child, depth+1); value != nil {
					return value
				}
			}
		}
		return nil
	}
	return visit(root, 0)
}

func looksLikeKeyedHistoryItemsObject(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	matched := 0
	for key, value := range root {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "", "ret", "code", "message", "msg", "errmsg", "logid", "log_id", "request_id", "requestid", "systime", "data", "result", "payload", "items", "results", "list", "records", "history", "meta", "response":
			continue
		}
		child, ok := value.(map[string]any)
		if !ok || len(child) == 0 {
			continue
		}
		if looksLikeHistoryItemObject(child) {
			matched++
		}
	}
	return matched > 0
}

func looksLikeHistoryItemObject(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	if firstDirectStringValue(root,
		"submit_id", "submitId", "SubmitID",
		"history_id", "historyId", "HistoryID",
		"history_record_id", "historyRecordId", "HistoryRecordID",
		"task_id", "taskId", "TaskID",
	) != "" {
		return true
	}
	for _, key := range []string{"task", "Task", "item_list", "itemList", "ItemList", "queue_info", "queueInfo", "QueueInfo"} {
		if value, ok := root[key]; ok && value != nil {
			return true
		}
	}
	return false
}

func firstHistoryQueueStatusValue(root any, keys ...string) string {
	for _, key := range keys {
		if value := firstNestedValue(root, key); value != nil {
			if status := formatHistoryQueueStatus(value); status != "" {
				return status
			}
		}
	}
	return ""
}

func formatHistoryQueueStatus(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	switch strings.ToLower(text) {
	case "0":
		return "Pending"
	case "1":
		return "Queueing"
	case "2":
		return "Generating"
	case "3":
		return "Finish"
	case "4":
		return "Failed"
	default:
		return text
	}
}

func firstMapValue(root map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := root[key].(map[string]any); ok {
			return value
		}
	}
	return map[string]any{}
}

func firstPresentValue(root map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := root[key]; ok {
			return value
		}
	}
	return nil
}

func firstStringValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := root[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	if nested := firstMapValue(root, "data", "result"); len(nested) > 0 {
		for _, key := range keys {
			if value, ok := nested[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
	}
	return ""
}

func firstMetadataStringValue(root any, keys ...string) string {
	// 远端会把 code/message/log_id 包进 Meta/Error/Response 等壳中，
	// 这里限制在“响应元数据 wrapper”里递归查找，避免误把 history item 的业务字段当成响应头信息。
	var visit func(any, int) string
	visit = func(node any, depth int) string {
		if depth > 6 {
			return ""
		}
		current, ok := node.(map[string]any)
		if !ok || len(current) == 0 {
			return ""
		}
		for _, key := range keys {
			if value, ok := current[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
		for _, wrapper := range []string{
			"error", "Error",
			"meta", "Meta",
			"response", "Response",
			"payload", "Payload",
			"data", "Data",
			"result", "Result",
		} {
			if value := visit(current[wrapper], depth+1); value != "" {
				return value
			}
		}
		return ""
	}
	return visit(root, 0)
}

func parseMCPResponsePayload(resp *httpclient.Response, body []byte, encoding string) map[string]any {
	payload := map[string]any{}
	if json.Valid(body) && json.Unmarshal(body, &payload) == nil {
		return payload
	}
	statusCode := responseStatusCode(resp)
	code := "response_decode_error"
	bodyPreview := nonJSONBodyPreview(body)
	message := summarizeNonJSONBodyPreview(bodyPreview)
	if message == "" {
		message = "unexpected non-json response"
	}
	if statusCode >= 400 {
		code = strconv.Itoa(statusCode)
	}
	return map[string]any{
		"code":    code,
		"message": message,
		"log_id":  buildMCPLogID("non-json-response"),
		"data": map[string]any{
			"transport_mode": "non-json-fallback",
			"status_code":    statusCode,
			"encoding":       strings.TrimSpace(encoding),
			"body_preview":   bodyPreview,
		},
	}
}

func responseStatusCode(resp *httpclient.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func nonJSONBodyPreview(body []byte) string {
	return strings.TrimSpace(string(body))
}

func summarizeNonJSONBodyPreview(preview string) string {
	preview = strings.TrimSpace(preview)
	if len(preview) > 240 {
		return preview[:240] + "..."
	}
	return preview
}

func writeHistoryProbe(req *httpclient.Request, resp *httpclient.Response, body []byte, encoding string, readErr error) {
	dir := strings.TrimSpace(os.Getenv(historyProbeEnv))
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	payload := map[string]any{
		"request": map[string]any{
			"method":  "",
			"path":    "",
			"headers": map[string]string{},
			"query":   map[string]string{},
			"body":    "",
		},
		"response": map[string]any{
			"status_code": 0,
			"headers":     map[string]string{},
			"encoding":    strings.TrimSpace(encoding),
			"body_len":    len(body),
			"body_text":   string(body),
		},
		"captured_at": time.Now().Format(time.RFC3339Nano),
	}
	if req != nil {
		payload["request"] = map[string]any{
			"method":  strings.TrimSpace(req.Method),
			"path":    strings.TrimSpace(req.Path),
			"headers": cloneStringMap(req.Headers),
			"query":   cloneStringMap(req.Query),
			"body":    string(req.Body),
		}
	}
	if resp != nil {
		payload["response"] = map[string]any{
			"status_code": resp.StatusCode,
			"headers":     cloneStringMap(resp.Headers),
			"encoding":    strings.TrimSpace(encoding),
			"body_len":    len(body),
			"body_text":   string(body),
		}
	}
	if readErr != nil {
		payload["error"] = strings.TrimSpace(readErr.Error())
	}
	name := "history_probe_" + time.Now().Format("20060102150405.000000000") + ".json"
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), encoded, 0o600)
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
