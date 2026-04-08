package mcp_api

// 这个文件保留二进制里能确认到的 MCP 通用模型名。
// 字段只补“已在现有请求/响应或日志里反复出现”的部分；其余保留为空或 any，
// 避免在缺证据的情况下误写 schema。后续拿到更完整抓包/反射信息后再继续收紧。

// Content 对齐二进制中出现的通用内容节点类型名。
// 当前还没有足够证据确认其稳定字段布局，因此只保留最小占位字段。
type Content struct {
	Raw map[string]any `json:"-"`
}

// Resource 对齐 MCP 资源引用结构里最稳定的资源标识字段。
// 字段来自资源上传链路中反复出现的 JSON key。
type Resource struct {
	ResourceID    string         `json:"resource_id,omitempty"`
	ResourceType  string         `json:"resource_type,omitempty"`
	Path          string         `json:"path,omitempty"`
	Name          string         `json:"name,omitempty"`
	Size          int64          `json:"size,omitempty"`
	Scene         int            `json:"scene,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	MimeType      string         `json:"mime_type,omitempty"`
	UploadSummary map[string]any `json:"upload_summary,omitempty"`
}

// Resolution 对齐分辨率配置类型名。
// 目前仅补齐在请求体里出现频率较高的字段。
type Resolution struct {
	ResolutionType  string `json:"resolution_type,omitempty"`
	VideoResolution string `json:"video_resolution,omitempty"`
}

// SubmitInfo 对齐任务提交附带信息类型名。
// 当前已从原二进制 getter 确认稳定字段名至少包括 Code、Msg。
type SubmitInfo struct {
	Code string `json:"code,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

// MusicParam 对齐音频或配乐参数类型名。
// 等后续确认音频相关请求 schema 后再补字段。
type MusicParam struct {
	Raw map[string]any `json:"-"`
}

// CommerceInfo 对齐商业化信息类型名。
// 目前只补齐在任务结果与 commerce 查询中稳定出现的字段。
type CommerceInfo struct {
	CreditCount int              `json:"credit_count,omitempty"`
	BenefitType string           `json:"benefit_type,omitempty"`
	ChargeType  string           `json:"charge_type,omitempty"`
	Triplet     *BenefitTriplet  `json:"triplet,omitempty"`
	Triplets    []BenefitTriplet `json:"triplets,omitempty"`
}

// BenefitTriplet 对齐权益三元组类型名。
// 字段来自任务结果与 download 输出里出现的 triplet 结构。
type BenefitTriplet struct {
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	BenefitType  string `json:"benefit_type,omitempty"`
}

// GenerateToolData 对齐生成工具数据类型名。
// 字段名来自原二进制 getter/default 符号与现有请求/响应证据。
type GenerateToolData struct {
	Contents           []Content      `json:"contents,omitempty"`
	LlmContents        []Content      `json:"llm_contents,omitempty"`
	HistoryID          string         `json:"history_id,omitempty"`
	CommerceInfo       *CommerceInfo  `json:"commerce_info,omitempty"`
	SubmitInfo         *SubmitInfo    `json:"submit_info,omitempty"`
	SubmitID           string         `json:"submit_id,omitempty"`
	ResultCode         any            `json:"result_code,omitempty"`
	Metrics            map[string]any `json:"metrics,omitempty"`
	Ratio              string         `json:"ratio,omitempty"`
	PreGenItemIDs      []string       `json:"pre_gen_item_ids,omitempty"`
	ForecastResolution *Resolution    `json:"forecast_resolution,omitempty"`
	ItemIDList         []string       `json:"item_id_list,omitempty"`
	ResourceType       string         `json:"resource_type,omitempty"`
	End                bool           `json:"end,omitempty"`
	ModelKey           string         `json:"model_key,omitempty"`
	Raw                map[string]any `json:"-"`
}

// GenerateToolResp 对齐生成工具响应类型名。
// 当前按通用响应壳体补齐基础字段，具体 data 结构继续随证据收敛。
type GenerateToolResp struct {
	Code      string            `json:"code,omitempty"`
	Message   string            `json:"message,omitempty"`
	LogID     string            `json:"log_id,omitempty"`
	Data      *GenerateToolData `json:"data,omitempty"`
	Recovered map[string]any    `json:"recovered,omitempty"`
	Curl      string            `json:"curl,omitempty"`
}

// GenerateVideoToolReq 对齐视频生成工具请求类型名。
// 字段名来自二进制符号与 CLI 实测请求体。
type GenerateVideoToolReq struct {
	GenerateType         string              `json:"generate_type,omitempty"`
	AgentScene           string              `json:"agent_scene,omitempty"`
	Prompt               string              `json:"prompt,omitempty"`
	Ratio                string              `json:"ratio,omitempty"`
	FirstFrameResourceID string              `json:"first_frame_resource_id,omitempty"`
	LastFrameResourceID  string              `json:"last_frame_resource_id,omitempty"`
	Duration             int                 `json:"duration,omitempty"`
	CreationAgentVersion string              `json:"creation_agent_version,omitempty"`
	MediaResourceIDList  []string            `json:"media_resource_id_list,omitempty"`
	MediaTypeList        []string            `json:"media_type_list,omitempty"`
	SubmitID             string              `json:"submit_id,omitempty"`
	PromptList           []string            `json:"prompt_list,omitempty"`
	DurationList         []float64           `json:"duration_list,omitempty"`
	ModelKey             string              `json:"model_key,omitempty"`
	ResourceNamespace    string              `json:"resource_namespace,omitempty"`
	VideoResolution      string              `json:"video_resolution,omitempty"`
	ImageResourceIDList  []string            `json:"image_resource_id_list,omitempty"`
	VideoResourceIDList  []string            `json:"video_resource_id_list,omitempty"`
	AudioResourceIDList  []string            `json:"audio_resource_id_list,omitempty"`
	Music                *MusicParam         `json:"music,omitempty"`
	WorkspaceID          string              `json:"workspace_id,omitempty"`
	ResourceIDReuseType  ResourceIDReUseType `json:"resource_id_reuse_type,omitempty"`
}

// ResourceIDReUseType 对齐资源复用策略枚举底层类型。
// 先固定底层整数类型，具体枚举值等待更多证据后再补。
type ResourceIDReUseType int32
