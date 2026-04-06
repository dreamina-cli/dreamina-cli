package mcp_api

// 这个文件保留二进制里能确认到的 MCP 通用模型名，当前先以最小结构体占位对齐类型名。
// 这里的空结构体不表示“字段已经全部恢复”，而表示“类型名已经确认、字段证据仍不足”。
// 后续如果拿到更完整的抓包、反射信息或调用点证据，再按证据驱动方式补字段，
// 避免现在凭经验硬补，把占位模型误写成“已还原 schema”。

// Content 对齐二进制中出现的通用内容节点类型名。
// 当前还没有足够证据确认其稳定字段布局，因此先只保留类型名占位。
type Content struct{}

// Resource 对齐 MCP 资源引用结构里最稳定的资源标识字段。
// 目前只保留已经在多条恢复链路里反复出现的 ResourceID/ResourceType，
// 避免把 path/url/scene 一类上下游偶发字段误固化到公共模型中。
type Resource struct {
	ResourceID   string
	ResourceType string
}

// Resolution 对齐分辨率配置类型名。
// 字段仍待后续从请求体和反射证据中继续恢复。
type Resolution struct{}

// SubmitInfo 对齐任务提交附带信息类型名。
// 当前已从原二进制 getter 确认稳定字段名至少包括 Code、Msg，
// 但在缺少完整 thrift/json 标签证据前，先不把字段布局硬编码进公共模型。
type SubmitInfo struct{}

// MusicParam 对齐音频或配乐参数类型名。
// 等后续确认音频相关请求 schema 后再补字段。
type MusicParam struct{}

// CommerceInfo 对齐商业化信息类型名。
// 当前工程里已有若干 commerce 信息透传，但公共模型字段仍缺少稳定证据。
type CommerceInfo struct{}

// BenefitTriplet 对齐权益三元组类型名。
// 暂时只做类型名对齐，等待后续结合商业化返回体再细化。
type BenefitTriplet struct{}

// GenerateToolData 对齐生成工具数据类型名。
// 当前已从原二进制 getter/default 符号确认该包装体至少涉及：
// Contents、LlmContents、HistoryID、CommerceInfo、SubmitInfo、SubmitID、
// ResultCode、Metrics、Ratio、PreGenItemIds、ForecastResolution、
// ItemIDList、ResourceType、End、ModelKey。
// 由于其中多数字段的确切类型和 tag 仍未完全确认，这里继续保留最小占位。
type GenerateToolData struct{}

// GenerateToolResp 对齐生成工具响应类型名。
// 当前先保留占位，避免在没有统一 schema 证据时提前固定响应结构。
type GenerateToolResp struct{}

// GenerateVideoToolReq 对齐视频生成工具请求类型名。
// 当前已从原二进制 getter/read/write 符号确认字段名至少包括：
// GenerateType、Prompt、Ratio、FirstFrameResourceID、LastFrameResourceID、
// Duration、CreationAgentVersion、MediaResourceIDList、MediaTypeList、
// SubmitID、PromptList、DurationList、ModelKey、ResourceNamespace、
// VideoResolution、ImageResourceIDList、VideoResourceIDList、AudioResourceIDList、
// Music、WorkspaceID、ResourceIDReuseType。
// 其中 text2video 已进一步通过断点确认默认请求体会显式发送：
// generate_type=text2VideoByConfig、model_key=seedance2.0fast、ratio=16:9、duration=5。
// 由于公共模型尚缺少完整字段类型和 tag 证据，这里先保留占位结构体，避免误固化错误 schema。
type GenerateVideoToolReq struct{}

// ResourceIDReUseType 对齐资源复用策略枚举底层类型。
// 先固定底层整数类型，具体枚举值等待更多证据后再补。
type ResourceIDReUseType int32
