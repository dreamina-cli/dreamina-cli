package resource

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
	"code.byted.org/videocut-aigc/dreamina_cli/infra/logging"
)

type Client interface{}

const (
	// Dreamina 当前图片上传链路在原程序中稳定落到这个 ImageX 空间。
	// 线上 token 偶尔不会直接返回 space_name，这里保留对齐后的兜底值。
	defaultDreaminaImageXSpaceName = "tb4s082cfz"
	// 原程序在 cn 区域进入 uploadImage 前，upload_domain 会被补成该域名。
	defaultDreaminaImageXHost   = "imagex.bytedanceapi.com"
	defaultDreaminaImageXRegion = "cn-north-1"
	// 原程序的音视频上传 token 在稳定场景下会落到这个 VOD 默认域名与空间名。
	defaultDreaminaVODHost      = "vod.bytedanceapi.com"
	defaultDreaminaVODSpaceName = "dreamina"
	// 原始二进制里同时出现了 TOS4-HMAC-SHA256 和 UNSIGNED-PAYLOAD 字符串，
	// 说明音视频 phase 上传走的是 TOS 风格签名而不是后端普通 header 鉴权。
	tosSigningAlgorithm    = "TOS4-HMAC-SHA256"
	unsignedTOSPayloadHash = "UNSIGNED-PAYLOAD"
)

type imageXUploader interface {
	SetAccessKey(ak string)
	SetSecretKey(sk string)
	SetSessionToken(token string)
	SetHost(host string)
	UploadImages(params *imageXApplyUploadParam, images [][]byte) (*imageXCommitUploadResult, error)
}

type imageXApplyUploadParam struct {
	ServiceID     string
	SessionKey    string
	UploadNum     int
	UploadHost    string
	StoreKeys     []string
	ContentTypes  []string
	Prefix        string
	FileExtension string
	Overwrite     bool
}

type imageXApplyUploadResult struct {
	UploadAddress imageXUploadAddress `json:"UploadAddress"`
	RequestID     string              `json:"RequestId"`
}

type imageXUploadAddress struct {
	SessionKey  string            `json:"SessionKey"`
	UploadHosts []string          `json:"UploadHosts"`
	StoreInfos  []imageXStoreInfo `json:"StoreInfos"`
}

type imageXStoreInfo struct {
	StoreURI string `json:"StoreUri"`
	Auth     string `json:"Auth"`
}

type imageXCommitUploadParam struct {
	ServiceID   string   `json:"-"`
	SessionKey  string   `json:"SessionKey"`
	SuccessOids []string `json:"SuccessOids"`
}

type imageXCommitUploadResult struct {
	Results    []imageXCommitResult `json:"Results"`
	RequestID  string               `json:"RequestId"`
	ImageInfos []imageXImageInfo    `json:"PluginResult"`
}

type imageXCommitResult struct {
	URI       string          `json:"Uri"`
	URIStatus int             `json:"UriStatus"`
	PutError  *imageXPutError `json:"-"`
}

type imageXPutError struct {
	ErrorCode int
	Error     string
	Message   string
}

type imageXImageInfo struct {
	ImageURI    string `json:"ImageUri"`
	ImageWidth  int    `json:"ImageWidth"`
	ImageHeight int    `json:"ImageHeight"`
	ImageFormat string `json:"ImageFormat"`
	ImageSize   int    `json:"ImageSize"`
}

type imageXCommonResponse struct {
	ResponseMetadata *imageXResponseMetadata `json:"ResponseMetadata"`
	Result           json.RawMessage         `json:"Result"`
}

type imageXResponseMetadata struct {
	RequestID string               `json:"RequestId"`
	Error     *imageXResponseError `json:"Error,omitempty"`
}

type imageXResponseError struct {
	CodeN   int    `json:"CodeN,omitempty"`
	Code    string `json:"Code,omitempty"`
	Message string `json:"Message,omitempty"`
}

type imageXDirectUploadResponse struct {
	Success int                `json:"success"`
	Error   *imageXUploadError `json:"error"`
	Payload struct {
		Hash string `json:"hash"`
	} `json:"payload,omitempty"`
}

type imageXUploadError struct {
	HTTPCode  int    `json:"code"`
	Error     string `json:"error"`
	ErrorCode int    `json:"error_code"`
	Message   string `json:"message"`
}

type ByteDanceUploadClient struct {
	http *httpclient.Client

	getUploadTokenFunc   func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error)
	uploadImageFunc      func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error)
	uploadVideoAudioFunc func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error)
	resourceStoreFunc    func(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error)
	probeDurationFunc    func(ctx context.Context, path string) (float64, error)
	newImageXClientFunc  func(region string) imageXUploader
}

type Resource struct {
	ResourceID    string         `json:"resource_id"`
	ResourceType  string         `json:"resource_type"`
	Path          string         `json:"path,omitempty"`
	Name          string         `json:"name,omitempty"`
	Size          int64          `json:"size,omitempty"`
	Scene         int            `json:"scene,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	MimeType      string         `json:"mime_type,omitempty"`
	UploadSummary map[string]any `json:"upload_summary,omitempty"`
}

type SingleUploadRes struct {
	ResourceID   string         `json:"resource_id"`
	StoreURI     string         `json:"store_uri,omitempty"`
	UploadID     string         `json:"upload_id,omitempty"`
	UploadDomain string         `json:"upload_domain,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// vodUploadConfig 先对齐 UploadVideoWithUploadAuth 一类调用会消费的关键入参。
// 当前还拿不到私有 SDK 源码，但先把路径、桶、区域和上传凭证收束在这里，
// 后续切回真实 SDK 时可以直接复用，不需要再从 phase 上传结果里反推。
type vodUploadConfig struct {
	ResourceType   string   `json:"resource_type,omitempty"`
	FilePath       string   `json:"file_path,omitempty"`
	FileName       string   `json:"file_name,omitempty"`
	UploadAuth     string   `json:"upload_auth,omitempty"`
	SessionKey     string   `json:"session_key,omitempty"`
	ServiceID      string   `json:"service_id,omitempty"`
	SpaceName      string   `json:"space_name,omitempty"`
	Bucket         string   `json:"bucket,omitempty"`
	Region         string   `json:"region,omitempty"`
	IDC            string   `json:"idc,omitempty"`
	StoreURI       string   `json:"store_uri,omitempty"`
	UploadDomain   string   `json:"upload_domain,omitempty"`
	Buckets        []string `json:"buckets,omitempty"`
	InVolcanoCloud bool     `json:"in_volcano_cloud,omitempty"`
}

type vodUploadResult struct {
	ResourceID      string         `json:"resource_id,omitempty"`
	StoreURI        string         `json:"store_uri,omitempty"`
	UploadID        string         `json:"upload_id,omitempty"`
	UploadDomain    string         `json:"upload_domain,omitempty"`
	DurationSeconds float64        `json:"duration_seconds,omitempty"`
	Raw             map[string]any `json:"raw,omitempty"`
}

type vodApplyUploadResponse struct {
	ResponseMetadata *imageXResponseMetadata `json:"ResponseMetadata,omitempty"`
	Result           vodApplyUploadResult    `json:"Result"`
}

type vodApplyUploadResult struct {
	UploadAddress *vodUploadAddress `json:"UploadAddress,omitempty"`
	RequestID     string            `json:"RequestId,omitempty"`
}

type vodUploadAddress struct {
	UploadNodes []vodUploadNode `json:"UploadNodes,omitempty"`
}

type vodUploadNode struct {
	VID          string            `json:"VID,omitempty"`
	StoreInfos   []vodStoreInfo    `json:"StoreInfos,omitempty"`
	UploadHost   string            `json:"UploadHost,omitempty"`
	SessionKey   string            `json:"SessionKey,omitempty"`
	Type         string            `json:"Type,omitempty"`
	Protocol     string            `json:"Protocol,omitempty"`
	UploadHeader map[string]string `json:"UploadHeader,omitempty"`
}

type vodStoreInfo struct {
	StoreURI string `json:"StoreUri,omitempty"`
	Auth     string `json:"Auth,omitempty"`
}

type vodCommitUploadResponse struct {
	ResponseMetadata *imageXResponseMetadata `json:"ResponseMetadata,omitempty"`
	Result           vodCommitUploadResult   `json:"Result"`
}

type vodCommitUploadResult struct {
	Results   []vodCommitResult `json:"Results,omitempty"`
	RequestID string            `json:"RequestId,omitempty"`
}

type vodCommitResult struct {
	VID string `json:"Vid,omitempty"`
	URI string `json:"Uri,omitempty"`
}

type vodDirectUploadResponse struct {
	Code       int    `json:"code,omitempty"`
	APIVersion string `json:"apiversion,omitempty"`
	Message    string `json:"message,omitempty"`
	Data       struct {
		CRC32 string `json:"crc32,omitempty"`
	} `json:"data,omitempty"`
}

type vodMultipartInitResponse struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Data    struct {
		UploadID string `json:"uploadid,omitempty"`
	} `json:"data,omitempty"`
}

type vodCommitUploadBody struct {
	CallbackArgs string `json:"CallbackArgs"`
	SessionKey   string `json:"SessionKey"`
	TTL          string `json:"TTL"`
	ODM          bool   `json:"ODM"`
	Functions    any    `json:"Functions"`
}

type uploadTokenReq struct {
	Scene        int    `json:"scene,omitempty"`
	AgentScene   int    `json:"agent_scene,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
}

type uploadTokenResp struct {
	Ret          string           `json:"ret,omitempty"`
	Code         string           `json:"code,omitempty"`
	Msg          string           `json:"msg,omitempty"`
	Message      string           `json:"message,omitempty"`
	Scene        int              `json:"scene,omitempty"`
	ResourceType string           `json:"resource_type,omitempty"`
	Data         *uploadTokenData `json:"data,omitempty"`
	Raw          map[string]any   `json:"-"`
}

type uploadTokenData struct {
	Scene           int                `json:"scene,omitempty"`
	AgentScene      int                `json:"agent_scene,omitempty"`
	ResourceType    string             `json:"resource_type,omitempty"`
	AccessKeyID     string             `json:"access_key_id,omitempty"`
	SecretAccessKey string             `json:"secret_access_key,omitempty"`
	SessionToken    string             `json:"session_token,omitempty"`
	UploadDomain    string             `json:"upload_domain,omitempty"`
	ServiceID       string             `json:"service_id,omitempty"`
	SpaceName       string             `json:"space_name,omitempty"`
	SessionKey      string             `json:"session_key,omitempty"`
	UploadAuth      string             `json:"upload_auth,omitempty"`
	StoreURI        string             `json:"store_uri,omitempty"`
	StoreKeys       []string           `json:"store_keys,omitempty"`
	UploadNum       int                `json:"upload_num,omitempty"`
	TosHeaders      string             `json:"tos_headers,omitempty"`
	TosMeta         string             `json:"tos_meta,omitempty"`
	Bucket          string             `json:"bucket,omitempty"`
	Buckets         []string           `json:"buckets,omitempty"`
	Region          string             `json:"region,omitempty"`
	IDC             string             `json:"idc,omitempty"`
	InVolcanoCloud  bool               `json:"in_volcano_cloud,omitempty"`
	StoreInfos      []*uploadStoreInfo `json:"store_infos,omitempty"`
	Extra           map[string]any     `json:"extra,omitempty"`
	Raw             map[string]any     `json:"-"`
}

type uploadStoreInfo struct {
	StoreURI     string `json:"store_uri,omitempty"`
	StoreKey     string `json:"store_key,omitempty"`
	UploadDomain string `json:"upload_domain,omitempty"`
}

type resourceStoreReq struct {
	ResourceItems []*resourceStoreItem `json:"resource_items,omitempty"`
}

type resourceStoreItem struct {
	ResourceType  string `json:"resource_type,omitempty"`
	ResourceValue string `json:"resource_value,omitempty"`
}

type resourceStoreResp struct {
	Ret     string               `json:"ret,omitempty"`
	Code    string               `json:"code,omitempty"`
	Msg     string               `json:"msg,omitempty"`
	Message string               `json:"message,omitempty"`
	Data    *resourceStoreResult `json:"data,omitempty"`
}

type resourceStoreResult struct {
	Stored []*Resource    `json:"stored"`
	Raw    map[string]any `json:"-"`
}

type uploadResult struct {
	index    int
	resource *Resource
	err      error
}

type uploadModelVersionKey struct{}

// ContextWithUploadModelVersion 把上传链路依赖的模型版本写入上下文，供时长校验等后续步骤读取。
func ContextWithUploadModelVersion(ctx context.Context, modelVersion string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	modelVersion = strings.TrimSpace(modelVersion)
	if modelVersion == "" {
		return ctx
	}
	return context.WithValue(ctx, uploadModelVersionKey{}, modelVersion)
}

// New 创建字节资源上传客户端；如果没有注入 HTTP 客户端，就使用默认实现。
func New(v ...any) *ByteDanceUploadClient {
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
	return &ByteDanceUploadClient{
		http:                 http,
		getUploadTokenFunc:   nil,
		uploadImageFunc:      nil,
		uploadVideoAudioFunc: nil,
		resourceStoreFunc:    nil,
		probeDurationFunc:    nil,
		newImageXClientFunc:  nil,
	}
}

// getUploadToken 调用远端上传令牌接口，并把返回结构归一成 uploadTokenResp。
func (c *ByteDanceUploadClient) getUploadToken(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
	if c != nil && c.getUploadTokenFunc != nil {
		return c.getUploadTokenFunc(ctx, session, resourceType)
	}
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return nil, fmt.Errorf("resource_type is required")
	}

	scene := uploadSceneForType(resourceType)
	reqBody := &uploadTokenReq{
		Scene:        scene,
		AgentScene:   scene,
		ResourceType: resourceType,
	}
	req, err := c.http.NewRequest(ctx, "POST", "/mweb/v1/get_upload_token", reqBody, sessionHeaders(session))
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.getUploadToken] build request failed scene=%d", scene)
		return nil, fmt.Errorf("marshal upload token request: %w", err)
	}
	c.http.ApplyBackendHeaders(req)
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	body, err := httpclient.ReadResponseBody(respAny)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.getUploadToken] read response failed scene=%d", scene)
		return nil, err
	}
	resp, _ := respAny.(*httpclient.Response)
	if resp == nil {
		return nil, fmt.Errorf("parse upload token response: response is required")
	}
	if resp.StatusCode != 200 {
		trimmed := strings.TrimSpace(string(body))
		logging.ErrorfContext(ctx, "[ResourceUpload.getUploadToken] unexpected status status=%d scene=%d body=%q", resp.StatusCode, scene, trimmed)
		return nil, fmt.Errorf("get upload token failed: ret=%s", trimmed)
	}
	parsed, err := parseUploadTokenResponse(body, resourceType)
	if err != nil {
		return nil, fmt.Errorf("parse upload token response: %w", err)
	}
	if !uploadBackendSucceeded(parsed.Ret, parsed.Code) || parsed.Data == nil {
		return nil, fmt.Errorf("get upload token failed: ret=%s", firstNonEmpty(parsed.Ret, parsed.Code))
	}
	if parsed.Scene == 0 {
		parsed.Scene = parsed.Data.Scene
	}
	if parsed.Scene == 0 {
		parsed.Scene = scene
	}
	if parsed.ResourceType == "" {
		parsed.ResourceType = resourceType
	}
	if parsed.Data.Scene == 0 {
		parsed.Data.Scene = parsed.Scene
	}
	if parsed.Data.AgentScene == 0 {
		parsed.Data.AgentScene = parsed.Scene
	}
	if parsed.Data.ResourceType == "" {
		parsed.Data.ResourceType = resourceType
	}
	logging.InfofContext(ctx, "[ResourceUpload.getUploadToken] ok scene=%d resource_type=%s upload_domain=%q upload_auth_present=%t sts_present=%t service_id=%q space_name=%q bucket=%q store_info_count=%d",
		parsed.Data.Scene,
		strings.TrimSpace(parsed.Data.ResourceType),
		strings.TrimSpace(parsed.Data.UploadDomain),
		strings.TrimSpace(parsed.Data.UploadAuth) != "",
		strings.TrimSpace(parsed.Data.AccessKeyID) != "" && strings.TrimSpace(parsed.Data.SecretAccessKey) != "" && strings.TrimSpace(parsed.Data.SessionToken) != "",
		strings.TrimSpace(parsed.Data.ServiceID),
		strings.TrimSpace(parsed.Data.SpaceName),
		strings.TrimSpace(parsed.Data.Bucket),
		len(parsed.Data.StoreInfos),
	)
	return parsed, nil
}

// uploadImage 执行图片上传；原程序这里走 ImageX STS 上传，不再复用 phase HTTP 上传。
func (c *ByteDanceUploadClient) uploadImage(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
	if c != nil && c.uploadImageFunc != nil {
		return c.uploadImageFunc(ctx, token, path)
	}
	return c.uploadImageWithImageX(ctx, token, path)
}

type imageXUploadConfig struct {
	Region          string
	APIHost         string
	UploadHost      string
	ServiceID       string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type imageXHTTPClient struct {
	region       string
	host         string
	accessKey    string
	secretKey    string
	sessionToken string
	httpClient   *http.Client
}

// imageXClient 创建图片上传客户端，并允许测试注入替身实现。
func (c *ByteDanceUploadClient) imageXClient(region string) imageXUploader {
	if c != nil && c.newImageXClientFunc != nil {
		return c.newImageXClientFunc(region)
	}
	return &imageXHTTPClient{
		region:     normalizeImageXRegion(region),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *imageXHTTPClient) SetAccessKey(ak string) {
	c.accessKey = strings.TrimSpace(ak)
}

func (c *imageXHTTPClient) SetSecretKey(sk string) {
	c.secretKey = strings.TrimSpace(sk)
}

func (c *imageXHTTPClient) SetSessionToken(token string) {
	c.sessionToken = strings.TrimSpace(token)
}

func (c *imageXHTTPClient) SetHost(host string) {
	c.host = strings.TrimSpace(host)
}

func (c *imageXHTTPClient) UploadImages(params *imageXApplyUploadParam, images [][]byte) (*imageXCommitUploadResult, error) {
	if params == nil {
		return nil, fmt.Errorf("imagex upload params are required")
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("no image data to upload")
	}
	params.UploadNum = len(images)
	applyResp, err := c.applyUploadImage(params)
	if err != nil {
		return nil, err
	}
	logging.InfofContext(context.Background(), "[ImageXUpload.apply] request_id=%q service_id=%q upload_num=%d upload_host=%q session_key_present=%t store_info_count=%d",
		strings.TrimSpace(applyResp.RequestID),
		strings.TrimSpace(params.ServiceID),
		len(images),
		firstNonEmpty(strings.TrimSpace(params.UploadHost), strings.Join(applyResp.UploadAddress.UploadHosts, ",")),
		strings.TrimSpace(applyResp.UploadAddress.SessionKey) != "",
		len(applyResp.UploadAddress.StoreInfos),
	)
	if len(applyResp.UploadAddress.StoreInfos) != len(images) {
		return nil, fmt.Errorf("imagex apply returned %d store infos for %d images", len(applyResp.UploadAddress.StoreInfos), len(images))
	}
	uploadHost := strings.TrimSpace(params.UploadHost)
	if uploadHost == "" && len(applyResp.UploadAddress.UploadHosts) > 0 {
		uploadHost = strings.TrimSpace(applyResp.UploadAddress.UploadHosts[0])
	}
	if uploadHost == "" {
		return nil, fmt.Errorf("imagex apply missing upload host")
	}

	results := make([]imageXCommitResult, 0, len(images))
	successOids := make([]string, 0, len(images))
	for index, imageBytes := range images {
		storeInfo := applyResp.UploadAddress.StoreInfos[index]
		result := imageXCommitResult{
			URI:       strings.TrimSpace(storeInfo.StoreURI),
			URIStatus: 2000,
		}
		contentType := ""
		if index < len(params.ContentTypes) {
			contentType = strings.TrimSpace(params.ContentTypes[index])
		}
		if err := c.directUpload(uploadHost, storeInfo, imageBytes, contentType); err != nil {
			logging.InfofContext(context.Background(), "[ImageXUpload.put] index=%d status=failed upload_host=%q store_uri=%q content_type=%q size=%d err=%q",
				index,
				uploadHost,
				strings.TrimSpace(storeInfo.StoreURI),
				contentType,
				len(imageBytes),
				strings.TrimSpace(err.Error()),
			)
			result.URIStatus = 2001
			result.PutError = &imageXPutError{
				ErrorCode: -2001,
				Error:     err.Error(),
				Message:   err.Error(),
			}
		} else {
			logging.InfofContext(context.Background(), "[ImageXUpload.put] index=%d status=ok upload_host=%q store_uri=%q content_type=%q size=%d",
				index,
				uploadHost,
				strings.TrimSpace(storeInfo.StoreURI),
				contentType,
				len(imageBytes),
			)
			successOids = append(successOids, strings.TrimSpace(storeInfo.StoreURI))
		}
		results = append(results, result)
	}
	if len(successOids) == 0 {
		return &imageXCommitUploadResult{
			Results:   results,
			RequestID: applyResp.RequestID,
		}, nil
	}

	commitResp, err := c.commitUploadImage(&imageXCommitUploadParam{
		ServiceID:   params.ServiceID,
		SessionKey:  strings.TrimSpace(applyResp.UploadAddress.SessionKey),
		SuccessOids: successOids,
	})
	if err != nil {
		return nil, err
	}
	logging.InfofContext(context.Background(), "[ImageXUpload.commit] request_id=%q service_id=%q success_oid_count=%d",
		strings.TrimSpace(commitResp.RequestID),
		strings.TrimSpace(params.ServiceID),
		len(successOids),
	)
	if len(commitResp.Results) == 0 {
		commitResp.Results = results
	}
	return commitResp, nil
}

// uploadImageWithImageX 使用 ImageX SDK 走 apply/upload/commit 主链路，对齐原程序图片上传行为。
func (c *ByteDanceUploadClient) uploadImageWithImageX(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
	if token == nil {
		return nil, fmt.Errorf("upload token is required")
	}
	fileBody, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	if len(fileBody) == 0 {
		return nil, fmt.Errorf("no image data to upload")
	}

	cfg, err := buildImageXUploadConfig(token, path)
	if err != nil {
		return nil, err
	}
	client := c.imageXClient(cfg.Region)
	client.SetAccessKey(cfg.AccessKeyID)
	client.SetSecretKey(cfg.SecretAccessKey)
	client.SetSessionToken(cfg.SessionToken)
	client.SetHost(cfg.APIHost)

	resp, err := client.UploadImages(&imageXApplyUploadParam{
		ServiceID:    cfg.ServiceID,
		UploadHost:   cfg.UploadHost,
		ContentTypes: []string{mimeTypeForPath(path)},
		Overwrite:    true,
	}, [][]byte{fileBody})
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}
	return parseImageXUploadResult(resp, cfg.APIHost)
}

// uploadVideoAudio 执行视频或音频上传；当前先维持真实远端确认链路，并为未来切回 VOD SDK 预留兼容层。
func (c *ByteDanceUploadClient) uploadVideoAudio(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
	if c != nil && c.uploadVideoAudioFunc != nil {
		return c.uploadVideoAudioFunc(ctx, token, path)
	}
	vodCfg, err := buildVODUploadConfig(token, path)
	if err != nil {
		return nil, err
	}
	var result *SingleUploadRes
	logging.InfofContext(ctx, "[ResourceUpload.uploadVideoAudio] kind=%s path=%q mode=%s upload_auth_present=%t session_key_present=%t sts_present=%t upload_domain=%q service_id=%q space_name=%q bucket=%q",
		actualResourceKind(firstNonEmpty(strings.TrimSpace(token.ResourceType), "video"), path),
		strings.TrimSpace(path),
		map[bool]string{true: "vod_openapi", false: "phase_http"}[shouldUseVODOpenAPIUpload(token, path)],
		token != nil && strings.TrimSpace(token.UploadAuth) != "",
		token != nil && strings.TrimSpace(token.SessionKey) != "",
		token != nil && strings.TrimSpace(token.AccessKeyID) != "" && strings.TrimSpace(token.SecretAccessKey) != "" && strings.TrimSpace(token.SessionToken) != "",
		firstNonEmpty(strings.TrimSpace(token.UploadDomain), vodCfg.UploadDomain),
		vodCfg.ServiceID,
		vodCfg.SpaceName,
		vodCfg.Bucket,
	)
	if shouldUseVODOpenAPIUpload(token, path) {
		// 原始二进制在视频资源场景下会走 VOD OpenAPI：
		// ApplyUploadInner -> 直传 -> CommitUploadInner。
		result, err = c.uploadVideoWithVODOpenAPI(ctx, token, path)
	} else {
		// 音频链路当前仍维持 phase HTTP 路径，避免影响已经对齐的现有行为。
		result, err = c.uploadWithPhases(ctx, token, path)
	}
	if err != nil {
		return nil, fmt.Errorf("upload media: %w", err)
	}
	if result != nil {
		annotateVODUploadMetadata(result, vodCfg, token, shouldUseVODOpenAPIUpload(token, path))
	}
	return result, nil
}

// shouldUseVODOpenAPIUpload 判断当前资源是否应切到原程序确认过的 VOD OpenAPI 主链。
func shouldUseVODOpenAPIUpload(token *uploadTokenData, path string) bool {
	if token != nil {
		switch strings.ToLower(strings.TrimSpace(token.ResourceType)) {
		case "video":
			return true
		case "audio":
			return strings.TrimSpace(token.AccessKeyID) != "" &&
				strings.TrimSpace(token.SecretAccessKey) != "" &&
				strings.TrimSpace(token.SessionToken) != ""
		}
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeTypeForPath(path))), "video/")
}

// annotateVODUploadMetadata 把视频上传兼容层和当前选择的上传模式写回诊断字段。
func annotateVODUploadMetadata(result *SingleUploadRes, vodCfg *vodUploadConfig, token *uploadTokenData, usedOpenAPI bool) {
	if result == nil || vodCfg == nil {
		return
	}
	if result.Extra == nil {
		result.Extra = map[string]any{}
	}
	result.Extra["vod_upload"] = map[string]any{
		"resource_type":    vodCfg.ResourceType,
		"file_path":        vodCfg.FilePath,
		"file_name":        vodCfg.FileName,
		"store_uri":        vodCfg.StoreURI,
		"upload_domain":    vodCfg.UploadDomain,
		"service_id":       vodCfg.ServiceID,
		"space_name":       vodCfg.SpaceName,
		"bucket":           vodCfg.Bucket,
		"buckets":          vodCfg.Buckets,
		"region":           vodCfg.Region,
		"idc":              vodCfg.IDC,
		"in_volcano_cloud": vodCfg.InVolcanoCloud,
	}
	if usedOpenAPI {
		result.Extra["vod_upload_mode"] = "vod_openapi"
	} else {
		result.Extra["vod_fallback_mode"] = "phase_http"
	}
	result.Extra["upload_auth_present"] = token != nil && strings.TrimSpace(token.UploadAuth) != ""
	result.Extra["session_key_present"] = token != nil && strings.TrimSpace(token.SessionKey) != ""
}

// uploadVideoWithVODOpenAPI 使用原始二进制确认过的 Apply/Upload/Commit 链路上传视频。
func (c *ByteDanceUploadClient) uploadVideoWithVODOpenAPI(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
	if token == nil {
		return nil, fmt.Errorf("upload token is required")
	}
	fileBody, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	if len(fileBody) == 0 {
		return nil, fmt.Errorf("file size is zero")
	}

	applyResp, err := c.applyVODUpload(ctx, token)
	if err != nil {
		return nil, err
	}
	if applyResp == nil || applyResp.Result.UploadAddress == nil || len(applyResp.Result.UploadAddress.UploadNodes) == 0 {
		return nil, fmt.Errorf("vod apply upload returned empty upload nodes")
	}

	contentType := mimeTypeForPath(path)
	var lastErr error
	for _, node := range applyResp.Result.UploadAddress.UploadNodes {
		if err := c.uploadVODNodeDirect(ctx, node, fileBody, contentType); err != nil {
			lastErr = err
			continue
		}
		commitResp, err := c.commitVODUpload(ctx, token, node.SessionKey)
		if err != nil {
			lastErr = err
			continue
		}
		return parseCommittedVODUploadResult(commitResp, node), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("vod upload returned no usable upload node")
	}
	return nil, lastErr
}

// applyVODUpload 调用 ApplyUploadInner 获取视频上传节点、store_uri 与 session_key。
func (c *ByteDanceUploadClient) applyVODUpload(ctx context.Context, token *uploadTokenData) (*vodApplyUploadResponse, error) {
	target, err := buildVODOpenAPITarget(token, map[string]string{
		"Action":     "ApplyUploadInner",
		"FileType":   "video",
		"SessionKey": "",
		"SpaceName":  normalizeVODSpaceName(token),
		"Version":    "2020-11-19",
	})
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
	}
	req, err := c.http.NewRequest(ctx, http.MethodGet, target, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("build vod apply upload request: %w", err)
	}
	if err := signAWSServiceRequest(req, strings.TrimSpace(token.AccessKeyID), strings.TrimSpace(token.SecretAccessKey), strings.TrimSpace(token.SessionToken), normalizeVODRegion(token.Region), "vod"); err != nil {
		return nil, fmt.Errorf("sign vod apply upload request: %w", err)
	}
	respBody, _, err := c.doRawRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("apply vod upload: %w", err)
	}
	result := &vodApplyUploadResponse{}
	if err := decodeVODResponse(respBody, result); err != nil {
		return nil, fmt.Errorf("apply vod upload: %w", err)
	}
	normalizeVODApplyUploadResponse(result, respBody)
	return result, nil
}

// commitVODUpload 调用 CommitUploadInner，把已经上传完成的视频提交成最终 VID。
func (c *ByteDanceUploadClient) commitVODUpload(ctx context.Context, token *uploadTokenData, sessionKey string) (*vodCommitUploadResponse, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, fmt.Errorf("vod commit upload session key is required")
	}
	target, err := buildVODOpenAPITarget(token, map[string]string{
		"Action":    "CommitUploadInner",
		"SpaceName": normalizeVODSpaceName(token),
		"Version":   "2020-11-19",
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(&vodCommitUploadBody{
		CallbackArgs: "",
		SessionKey:   sessionKey,
		TTL:          "",
		ODM:          false,
		Functions:    nil,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal vod commit upload payload: %w", err)
	}
	headers := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	req, err := c.http.NewRequest(ctx, http.MethodPost, target, payload, headers)
	if err != nil {
		return nil, fmt.Errorf("build vod commit upload request: %w", err)
	}
	if err := signAWSServiceRequest(req, strings.TrimSpace(token.AccessKeyID), strings.TrimSpace(token.SecretAccessKey), strings.TrimSpace(token.SessionToken), normalizeVODRegion(token.Region), "vod"); err != nil {
		return nil, fmt.Errorf("sign vod commit upload request: %w", err)
	}
	respBody, _, err := c.doRawRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("commit vod upload: %w", err)
	}
	result := &vodCommitUploadResponse{}
	if err := decodeVODResponse(respBody, result); err != nil {
		return nil, fmt.Errorf("commit vod upload: %w", err)
	}
	normalizeVODCommitUploadResponse(result, respBody)
	return result, nil
}

// uploadVODNodeDirect 把视频二进制直传到 ApplyUploadInner 返回的 upload host。
func (c *ByteDanceUploadClient) uploadVODNodeDirect(ctx context.Context, node vodUploadNode, fileBytes []byte, contentType string) error {
	if len(fileBytes) == 0 {
		return fmt.Errorf("file size is zero")
	}
	if strings.TrimSpace(node.UploadHost) == "" {
		return fmt.Errorf("vod upload node missing upload host")
	}
	if len(node.StoreInfos) == 0 {
		return fmt.Errorf("vod upload node missing store infos")
	}

	expectedCRC32 := fmt.Sprintf("%08x", crc32.ChecksumIEEE(fileBytes))
	var lastErr error
	for _, storeInfo := range node.StoreInfos {
		storeURI := strings.TrimSpace(storeInfo.StoreURI)
		if storeURI == "" {
			continue
		}
		auth := strings.TrimSpace(storeInfo.Auth)
		if auth == "" {
			lastErr = fmt.Errorf("vod upload node missing authorization for %s", storeURI)
			continue
		}
		for _, target := range candidateVODDirectUploadTargets(node.UploadHost, storeURI) {
			headers := map[string]string{
				"Authorization":          auth,
				"Content-CRC32":          expectedCRC32,
				"Content-Type":           "application/octet-stream",
				"Specified-Content-Type": contentType,
			}
			for key, value := range node.UploadHeader {
				key = strings.TrimSpace(key)
				value = strings.TrimSpace(value)
				if key != "" && value != "" {
					headers[key] = value
				}
			}
			req, err := c.http.NewRequest(ctx, http.MethodPut, target, fileBytes, headers)
			if err != nil {
				return fmt.Errorf("build vod direct upload request: %w", err)
			}
			respBody, status, err := c.doRawRequest(ctx, req)
			if err != nil {
				lastErr = err
				continue
			}
			if len(bytes.TrimSpace(respBody)) == 0 {
				return nil
			}
			parsed := &vodDirectUploadResponse{}
			if json.Unmarshal(respBody, parsed) != nil {
				return nil
			}
			if parsed.Code != 0 {
				lastErr = fmt.Errorf("vod direct upload code %d: %s", parsed.Code, firstNonEmpty(strings.TrimSpace(parsed.Message), strings.TrimSpace(string(respBody))))
				if status >= 400 {
					continue
				}
				continue
			}
			if gotCRC32 := strings.TrimSpace(parsed.Data.CRC32); gotCRC32 != "" && !strings.EqualFold(gotCRC32, expectedCRC32) {
				lastErr = fmt.Errorf("vod direct upload crc32 mismatch: got=%s want=%s", gotCRC32, expectedCRC32)
				continue
			}
			return nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("vod direct upload failed")
	}
	return lastErr
}

// doRawRequest 发送通用 HTTP 请求并统一返回响应体和状态码。
func (c *ByteDanceUploadClient) doRawRequest(ctx context.Context, req *httpclient.Request) ([]byte, int, error) {
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	resp, _ := respAny.(*httpclient.Response)
	if resp == nil {
		return nil, 0, fmt.Errorf("response is required")
	}
	respBody, err := httpclient.ReadResponseBody(resp)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, resp.StatusCode, nil
}

// parseCommittedVODUploadResult 把 CommitUploadInner 的结果收束成统一上传结果。
func parseCommittedVODUploadResult(commitResp *vodCommitUploadResponse, node vodUploadNode) *SingleUploadRes {
	result := &SingleUploadRes{
		ResourceID:   strings.TrimSpace(node.VID),
		StoreURI:     strings.TrimSpace(vodNodeStoreURI(node)),
		UploadDomain: strings.TrimSpace(node.UploadHost),
		Extra: map[string]any{
			"resource_id_source": "remote",
			"session_key":        strings.TrimSpace(node.SessionKey),
			"upload_host":        strings.TrimSpace(node.UploadHost),
			"vid":                strings.TrimSpace(node.VID),
		},
	}
	if commitResp != nil {
		requestID := strings.TrimSpace(commitResp.Result.RequestID)
		if commitResp.ResponseMetadata != nil {
			requestID = firstNonEmpty(requestID, strings.TrimSpace(commitResp.ResponseMetadata.RequestID))
		}
		if requestID != "" {
			result.Extra["request_id"] = requestID
		}
		if len(commitResp.Result.Results) > 0 {
			first := commitResp.Result.Results[0]
			if vid := strings.TrimSpace(first.VID); vid != "" {
				result.ResourceID = vid
				result.Extra["vid"] = vid
			}
			if uri := strings.TrimSpace(first.URI); uri != "" {
				result.StoreURI = uri
			}
		}
	}
	if result.ResourceID == "" {
		result.ResourceID = result.StoreURI
		result.Extra["resource_id_source"] = "store_uri_fallback"
	}
	return result
}

// vodNodeStoreURI 返回当前上传节点里第一个可用的 store_uri。
func vodNodeStoreURI(node vodUploadNode) string {
	for _, storeInfo := range node.StoreInfos {
		if uri := strings.TrimSpace(storeInfo.StoreURI); uri != "" {
			return uri
		}
	}
	return ""
}

// decodeVODResponse 解析 VOD OpenAPI 响应，并统一处理 ResponseMetadata.Error。
func decodeVODResponse(body []byte, out any) error {
	wrapper := struct {
		ResponseMetadata *imageXResponseMetadata `json:"ResponseMetadata,omitempty"`
	}{}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return fmt.Errorf("unmarshal vod response: %w", err)
	}
	if wrapper.ResponseMetadata != nil && wrapper.ResponseMetadata.Error != nil {
		errObj := wrapper.ResponseMetadata.Error
		if errObj.CodeN != 0 || strings.TrimSpace(errObj.Code) != "" || strings.TrimSpace(errObj.Message) != "" {
			return fmt.Errorf("request %s error %s", strings.TrimSpace(wrapper.ResponseMetadata.RequestID), strings.TrimSpace(errObj.Message))
		}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unmarshal vod payload: %w", err)
	}
	return nil
}

// normalizeVODApplyUploadResponse 用递归兜底解析 UploadNodes，兼容 wire format 与 SDK 结构差异。
func normalizeVODApplyUploadResponse(resp *vodApplyUploadResponse, body []byte) {
	if resp == nil || !json.Valid(body) {
		return
	}
	payload := map[string]any{}
	if json.Unmarshal(body, &payload) != nil {
		return
	}
	if resp.Result.RequestID == "" {
		resp.Result.RequestID = recursiveStringValue(payload, "RequestId", "RequestID")
	}
	if resp.Result.UploadAddress == nil {
		resp.Result.UploadAddress = &vodUploadAddress{}
	}
	if len(resp.Result.UploadAddress.UploadNodes) > 0 {
		return
	}
	resp.Result.UploadAddress.UploadNodes = parseVODUploadNodes(recursiveAnyValue(payload, "UploadNodes", "upload_nodes"))
}

// normalizeVODCommitUploadResponse 用递归兜底解析 CommitUploadInner 的 Results。
func normalizeVODCommitUploadResponse(resp *vodCommitUploadResponse, body []byte) {
	if resp == nil || !json.Valid(body) {
		return
	}
	payload := map[string]any{}
	if json.Unmarshal(body, &payload) != nil {
		return
	}
	if resp.Result.RequestID == "" {
		resp.Result.RequestID = recursiveStringValue(payload, "RequestId", "RequestID")
	}
	if len(resp.Result.Results) > 0 {
		return
	}
	resp.Result.Results = parseVODCommitResults(recursiveAnyValue(payload, "Results", "results"))
}

func parseVODUploadNodes(value any) []vodUploadNode {
	normalized := normalizeJSONObjectString(value)
	items := sliceOfAny(normalized)
	if len(items) == 0 {
		items = keyedValuesSlice(normalized)
	}
	if len(items) == 0 {
		if single := parseVODUploadNode(normalized); single != nil {
			return []vodUploadNode{*single}
		}
		return nil
	}
	out := make([]vodUploadNode, 0, len(items))
	for _, item := range items {
		if parsed := parseVODUploadNode(item); parsed != nil {
			out = append(out, *parsed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseVODUploadNode(value any) *vodUploadNode {
	root, ok := normalizeJSONObjectString(value).(map[string]any)
	if !ok {
		return nil
	}
	node := &vodUploadNode{
		VID:        recursiveStringValue(root, "VID", "Vid", "vid"),
		UploadHost: recursiveStringValue(root, "UploadHost", "uploadHost", "upload_host"),
		SessionKey: recursiveStringValue(root, "SessionKey", "sessionKey", "session_key"),
		Type:       recursiveStringValue(root, "Type", "type"),
		Protocol:   recursiveStringValue(root, "Protocol", "protocol"),
	}
	if headers, ok := normalizeJSONObjectString(recursiveAnyValue(root, "UploadHeader", "uploadHeader", "upload_header")).(map[string]any); ok {
		node.UploadHeader = map[string]string{}
		for key, value := range headers {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				node.UploadHeader[key] = text
			}
		}
		if len(node.UploadHeader) == 0 {
			node.UploadHeader = nil
		}
	}
	node.StoreInfos = parseVODStoreInfos(recursiveAnyValue(root, "StoreInfos", "storeInfos", "store_infos"))
	if node.VID == "" && node.UploadHost == "" && node.SessionKey == "" && len(node.StoreInfos) == 0 {
		return nil
	}
	return node
}

func parseVODStoreInfos(value any) []vodStoreInfo {
	normalized := normalizeJSONObjectString(value)
	items := sliceOfAny(normalized)
	if len(items) == 0 {
		items = keyedValuesSlice(normalized)
	}
	if len(items) == 0 {
		root, ok := normalized.(map[string]any)
		if !ok {
			return nil
		}
		info := vodStoreInfo{
			StoreURI: recursiveStringValue(root, "StoreUri", "StoreURI", "storeUri", "store_uri", "Uri", "uri"),
			Auth:     recursiveStringValue(root, "Auth", "auth"),
		}
		if info.StoreURI == "" && info.Auth == "" {
			return nil
		}
		return []vodStoreInfo{info}
	}
	out := make([]vodStoreInfo, 0, len(items))
	for _, item := range items {
		root, ok := normalizeJSONObjectString(item).(map[string]any)
		if !ok {
			continue
		}
		info := vodStoreInfo{
			StoreURI: recursiveStringValue(root, "StoreUri", "StoreURI", "storeUri", "store_uri", "Uri", "uri"),
			Auth:     recursiveStringValue(root, "Auth", "auth"),
		}
		if info.StoreURI != "" || info.Auth != "" {
			out = append(out, info)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseVODCommitResults(value any) []vodCommitResult {
	normalized := normalizeJSONObjectString(value)
	items := sliceOfAny(normalized)
	if len(items) == 0 {
		items = keyedValuesSlice(normalized)
	}
	if len(items) == 0 {
		root, ok := normalized.(map[string]any)
		if !ok {
			return nil
		}
		result := vodCommitResult{
			VID: recursiveStringValue(root, "Vid", "VID", "vid"),
			URI: recursiveStringValue(root, "Uri", "URI", "uri", "StoreUri", "storeUri"),
		}
		if result.VID == "" && result.URI == "" {
			return nil
		}
		return []vodCommitResult{result}
	}
	out := make([]vodCommitResult, 0, len(items))
	for _, item := range items {
		root, ok := normalizeJSONObjectString(item).(map[string]any)
		if !ok {
			continue
		}
		result := vodCommitResult{
			VID: recursiveStringValue(root, "Vid", "VID", "vid"),
			URI: recursiveStringValue(root, "Uri", "URI", "uri", "StoreUri", "storeUri"),
		}
		if result.VID != "" || result.URI != "" {
			out = append(out, result)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeJSONObjectString(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	text = strings.TrimSpace(text)
	if text == "" || !json.Valid([]byte(text)) {
		return value
	}
	var decoded any
	if json.Unmarshal([]byte(text), &decoded) != nil {
		return value
	}
	return decoded
}

func keyedValuesSlice(value any) []any {
	root, ok := value.(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, root[key])
	}
	return out
}

// buildVODOpenAPITarget 组装 VOD OpenAPI 请求地址；测试时允许把 upload_domain 指到本地服务。
func buildVODOpenAPITarget(token *uploadTokenData, query map[string]string) (string, error) {
	scheme, host := normalizeVODOpenAPIEndpoint("", "")
	if token != nil {
		scheme, host = normalizeVODOpenAPIEndpoint(token.UploadDomain, defaultDreaminaVODHost)
	}
	target := &url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/top/v1",
	}
	values := url.Values{}
	for key, value := range query {
		values.Set(strings.TrimSpace(key), value)
	}
	target.RawQuery = values.Encode()
	return target.String(), nil
}

// normalizeVODOpenAPIEndpoint 把 upload_domain 归一成可直接访问的 scheme + host。
func normalizeVODOpenAPIEndpoint(rawHost string, fallbackHost string) (string, string) {
	rawHost = strings.TrimSpace(rawHost)
	fallbackHost = strings.TrimSpace(fallbackHost)
	if rawHost == "" {
		rawHost = fallbackHost
	}
	if rawHost == "" {
		rawHost = defaultDreaminaVODHost
	}
	if strings.HasPrefix(rawHost, "http://") || strings.HasPrefix(rawHost, "https://") {
		parsed, err := url.Parse(rawHost)
		if err == nil && parsed.Host != "" {
			return firstNonEmpty(parsed.Scheme, "http"), parsed.Host
		}
	}
	return "http", rawHost
}

// normalizeVODRegion 归一化 VOD OpenAPI 的 region，和原程序观察到的 cn-north-1 对齐。
func normalizeVODRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "", "cn":
		return "cn-north-1"
	default:
		return strings.TrimSpace(region)
	}
}

// normalizeVODSpaceName 返回当前上传应落入的 VOD 空间名。
func normalizeVODSpaceName(token *uploadTokenData) string {
	if token == nil {
		return defaultDreaminaVODSpaceName
	}
	return firstNonEmpty(strings.TrimSpace(token.SpaceName), strings.TrimSpace(token.ServiceID), defaultDreaminaVODSpaceName)
}

// candidateVODDirectUploadTargets 生成直传 URL 候选，优先对齐原程序最可能的两条路径。
func candidateVODDirectUploadTargets(uploadHost string, storeURI string) []string {
	scheme, host := normalizeVODUploadHost(uploadHost)
	storeURI = strings.TrimSpace(storeURI)
	if host == "" || storeURI == "" {
		return nil
	}
	return orderedNonEmptyUnique(
		buildVODDirectUploadTarget(scheme, host, "", storeURI),
		buildVODDirectUploadTarget(scheme, host, "upload/v1", storeURI),
	)
}

// normalizeVODUploadHost 把直传节点里的 upload host 归一成可访问的 scheme + host。
func normalizeVODUploadHost(rawHost string) (string, string) {
	rawHost = strings.TrimSpace(rawHost)
	if rawHost == "" {
		return "https", ""
	}
	if strings.HasPrefix(rawHost, "http://") || strings.HasPrefix(rawHost, "https://") {
		parsed, err := url.Parse(rawHost)
		if err == nil && parsed.Host != "" {
			return firstNonEmpty(parsed.Scheme, "https"), parsed.Host
		}
	}
	return "https", rawHost
}

// buildVODDirectUploadTarget 拼出单个直传路径，并按段转义 store_uri。
func buildVODDirectUploadTarget(scheme string, host string, prefix string, storeURI string) string {
	segments := make([]string, 0, 8)
	for _, part := range strings.Split(strings.Trim(prefix, "/"), "/") {
		part = strings.TrimSpace(part)
		if part != "" {
			segments = append(segments, part)
		}
	}
	for _, part := range strings.Split(strings.Trim(storeURI, "/"), "/") {
		part = strings.TrimSpace(part)
		if part != "" {
			segments = append(segments, part)
		}
	}
	escaped := make([]string, 0, len(segments))
	for _, part := range segments {
		escaped = append(escaped, url.PathEscape(part))
	}
	return (&url.URL{
		Scheme: firstNonEmpty(strings.TrimSpace(scheme), "https"),
		Host:   strings.TrimSpace(host),
		Path:   "/" + strings.Join(escaped, "/"),
	}).String()
}

// resourceStore 调用远端 resource_store，把上传中间态资源确认成可提交给 MCP 的最终资源记录。
func (c *ByteDanceUploadClient) resourceStore(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error) {
	if c != nil && c.resourceStoreFunc != nil {
		return c.resourceStoreFunc(ctx, session, items)
	}
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	payload := buildResourceStoreRequest(items)
	body, err := json.Marshal(payload)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] marshal request failed item_count=%d", len(items))
		return nil, fmt.Errorf("marshal resource store request: %w", err)
	}
	headers := sessionHeaders(session)
	headers["Content-Type"] = "application/json"
	headers["X-Tt-Logid"] = buildResourceBackendLogID("/dreamina/mcp/v1/resource_store", time.Now().Unix())
	req, err := c.http.NewRequest(ctx, "POST", "/dreamina/mcp/v1/resource_store", body, headers)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] build request failed item_count=%d", len(items))
		return nil, err
	}
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] request failed item_count=%d", len(items))
		return nil, err
	}
	resp, _ := respAny.(*httpclient.Response)
	if resp == nil {
		return nil, fmt.Errorf("resource store: response is required")
	}
	respBody, err := httpclient.ReadResponseBody(resp)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] read response failed item_count=%d", len(items))
		return nil, fmt.Errorf("read resource store response: %w", err)
	}
	if resp.StatusCode != 200 {
		trimmed := strings.TrimSpace(string(respBody))
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] unexpected status status=%d item_count=%d body=%q", resp.StatusCode, len(items), trimmed)
		return nil, fmt.Errorf("resource store status %d: %s", resp.StatusCode, trimmed)
	}
	parsed, err := parseResourceStoreResponse(respBody)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] parse response failed item_count=%d", len(items))
		return nil, fmt.Errorf("parse resource store response: %w", err)
	}
	if !uploadBackendSucceeded(parsed.Ret, parsed.Code) {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] backend rejected ret=%s msg=%q item_count=%d", firstNonEmpty(parsed.Ret, parsed.Code), firstNonEmpty(parsed.Msg, parsed.Message), len(items))
		return nil, fmt.Errorf("resource store failed: ret=%s msg=%s", firstNonEmpty(parsed.Ret, parsed.Code), firstNonEmpty(parsed.Msg, parsed.Message))
	}
	// resource_store 是最终落库确认接口。
	// 这里必须拿到与请求数量一致的远端 stored 结果，不能再把上传阶段的中间态当成最终成功。
	expectedCount := countNonNilResources(items)
	// 后端 stored 结果可能乱序、keyed-object、或只回一部分字段。
	// 这里统一经过 mergeStoredResources 做“按 store_uri/path/resource_id/name”对齐，
	// 再在数量层面收紧，避免资源身份和本地元信息串位。
	merged := mergeStoredResources(parsed.Data.Stored, items)
	if len(merged) == 0 {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] empty stored result item_count=%d", expectedCount)
		return nil, fmt.Errorf("resource store returned empty stored result")
	}
	if len(merged) < expectedCount {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] partial stored result got=%d want=%d", len(merged), expectedCount)
		return nil, fmt.Errorf("resource store returned partial stored result: got=%d want=%d", len(merged), expectedCount)
	}
	if len(merged) > expectedCount {
		logging.ErrorfContext(ctx, "[ResourceUpload.resourceStore] unexpected stored result count got=%d want=%d", len(merged), expectedCount)
		return nil, fmt.Errorf("resource store returned unexpected stored result count: got=%d want=%d", len(merged), expectedCount)
	}
	logging.InfofContext(ctx, "[ResourceUpload.resourceStore] stored resources item_count=%d", len(merged))
	return &resourceStoreResult{Stored: merged, Raw: parsed.Data.Raw}, nil
}

// UploadResource 是上传主入口：拿 token、执行单文件上传、做最终 resource_store 确认，并返回归一后的资源列表。
func (c *ByteDanceUploadClient) UploadResource(ctx context.Context, session any, resourceType string, paths []string) ([]*Resource, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	resourceType = strings.TrimSpace(resourceType)
	token, err := c.getUploadToken(ctx, session, resourceType)
	if err != nil {
		return nil, err
	}

	modelVersion := uploadModelVersionFromContext(ctx)
	results := make([]*Resource, len(paths))
	resultCh := make(chan uploadResult, len(paths))
	var wg sync.WaitGroup

	for index, path := range paths {
		path = strings.TrimSpace(path)
		// 这里继续保留并发上传，但结果回收必须按原始 index 归位，
		// 否则多文件上传一旦乱序，就会把 resource_store 前的本地 path/name/size 对错资源。
		wg.Add(1)
		go func(index int, path string) {
			defer wg.Done()
			resource, err := c.uploadSinglePath(ctx, token, resourceType, path, modelVersion, index)
			if err != nil {
				err = fmt.Errorf("upload resource %q: %w", path, err)
			}
			resultCh <- uploadResult{
				index:    index,
				resource: resource,
				err:      err,
			}
		}(index, path)
	}

	wg.Wait()
	close(resultCh)

	var firstErr error
	successCount := 0
	for result := range resultCh {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			continue
		}
		if result.resource == nil {
			continue
		}
		results[result.index] = result.resource
		successCount++
	}
	if firstErr != nil {
		return nil, firstErr
	}

	out := make([]*Resource, 0, successCount)
	for _, item := range results {
		if item != nil {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}

	stored, err := c.resourceStore(ctx, session, out)
	if err != nil {
		logging.ErrorfContext(ctx, "[ResourceUpload] resource store failed resource_type=%s success_upload_count=%d", resourceType, len(out))
		return nil, err
	}
	if stored != nil && len(stored.Stored) > 0 {
		// 再做一层调用侧数量校验，避免测试注入或未来替换 resourceStore 实现时重新放开 partial success。
		if len(stored.Stored) < len(out) {
			return nil, fmt.Errorf("resource store returned partial stored result: got=%d want=%d", len(stored.Stored), len(out))
		}
		if len(stored.Stored) > len(out) {
			return nil, fmt.Errorf("resource store returned unexpected stored result count: got=%d want=%d", len(stored.Stored), len(out))
		}
		return stored.Stored, nil
	}
	return nil, fmt.Errorf("resource store returned empty stored result")
}

// uploadSinglePath 上传单个本地文件，并把上传阶段结果整理成待 resource_store 确认的 Resource。
func (c *ByteDanceUploadClient) uploadSinglePath(ctx context.Context, token *uploadTokenResp, resourceType string, path string, modelVersion string, index int) (*Resource, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("read file %s: open %s: no such file or directory", path, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	kind := actualResourceKind(resourceType, path)

	tokenData := token.Data.forUpload(path, index)
	var upload *SingleUploadRes
	if resourceType == "image" {
		upload, err = c.uploadImage(ctx, tokenData, path)
	} else {
		upload, err = c.uploadVideoAudio(ctx, tokenData, path)
	}
	if err != nil {
		return nil, err
	}
	if upload == nil || strings.TrimSpace(upload.ResourceID) == "" {
		return nil, fmt.Errorf("upload result missing resource_id")
	}
	// 图片 phase 上传的 finish 结果经常只返回 store_uri，真正的 resource_id 依赖后续 resource_store。
	// 但音视频原始链路更接近 VOD UploadVideoWithUploadAuth，finish/commit 阶段通常会直接返回 vid/file_id。
	// 这里先收紧音视频：如果 resource_id 只是本地用 store_uri 顶上的兼容值，则直接判失败，避免把中间态误报成真实远端资源。
	if kind != "image" && usesStoreURIFallbackResourceID(upload) {
		return nil, fmt.Errorf("upload result missing remote resource_id")
	}
	// 原始二进制更接近“上传完成后读取 VOD 返回元数据再校验时长”。
	// 当前实现优先使用上传结果里的 duration/meta.duration，缺失时才回落到本地探测。
	if err := validateUploadedMediaDuration(ctx, resourceType, path, modelVersion, upload, c.durationProbe()); err != nil {
		return nil, err
	}

	resource := &Resource{
		ResourceID:   strings.TrimSpace(upload.ResourceID),
		ResourceType: resourceType,
		Path:         path,
		Name:         filepath.Base(path),
		Size:         info.Size(),
		Scene:        token.Scene,
		Kind:         kind,
		MimeType:     mimeTypeForPath(path),
		UploadSummary: map[string]any{
			"scene":         token.Scene,
			"resource_type": resourceType,
			"kind":          kind,
			"name":          filepath.Base(path),
			"size":          info.Size(),
			"stored":        true,
			"upload_domain": strings.TrimSpace(upload.UploadDomain),
			"store_uri":     strings.TrimSpace(upload.StoreURI),
			"upload_id":     strings.TrimSpace(upload.UploadID),
		},
	}
	if vodResult := normalizeVODUploadResult(upload); vodResult != nil {
		// 这些字段目前主要用于“把真实远端响应里已经出现的诊断信息保住”。
		// 它们不参与主流程判断，但后续如果能拿到私有 VOD SDK，对照这些落盘字段能更快确认差异点。
		if vodResult.DurationSeconds > 0 {
			resource.UploadSummary["duration_seconds"] = vodResult.DurationSeconds
		}
		if mediaURL := extractUploadMediaURL(vodResult.Raw); mediaURL != "" {
			resource.UploadSummary["media_url"] = mediaURL
		}
		if coverURL := extractUploadCoverURL(vodResult.Raw); coverURL != "" {
			resource.UploadSummary["cover_url"] = coverURL
		}
		if snapshotURI := extractUploadSnapshotURI(vodResult.Raw); snapshotURI != "" {
			resource.UploadSummary["snapshot_uri"] = snapshotURI
		}
		if requestID := extractUploadRequestID(vodResult.Raw); requestID != "" {
			resource.UploadSummary["request_id"] = requestID
		}
		if statusCode := extractUploadStatusCode(vodResult.Raw); statusCode != 0 {
			resource.UploadSummary["status_code"] = statusCode
		}
		if errorCode := extractUploadErrorCode(vodResult.Raw); errorCode != "" {
			resource.UploadSummary["error_code"] = errorCode
		}
		if errorMessage := extractUploadErrorMessage(vodResult.Raw); errorMessage != "" {
			resource.UploadSummary["error_message"] = errorMessage
		}
		if hostID := extractUploadHostID(vodResult.Raw); hostID != "" {
			resource.UploadSummary["host_id"] = hostID
		}
		if ec := extractUploadEC(vodResult.Raw); ec != "" {
			resource.UploadSummary["ec"] = ec
		}
		if detailErrCode := extractUploadDetailErrCode(vodResult.Raw); detailErrCode != 0 {
			resource.UploadSummary["detail_err_code"] = detailErrCode
		}
		// 原始 TOS 错误串里还会带 ResponseErr/ExpectedCodes。
		// 当前实现先把这些诊断字段透传到 upload_summary，后续切回真实 SDK 时可以直接对照。
		if responseErr := extractUploadResponseErr(vodResult.Raw); responseErr != "" {
			resource.UploadSummary["response_err"] = responseErr
		}
		if expectedCodes := extractUploadExpectedCodes(vodResult.Raw); len(expectedCodes) > 0 {
			resource.UploadSummary["expected_codes"] = expectedCodes
		}
		if upload.Extra != nil {
			if fallbackMode := strings.TrimSpace(fmt.Sprint(upload.Extra["vod_fallback_mode"])); fallbackMode != "" {
				resource.UploadSummary["vod_fallback_mode"] = fallbackMode
			}
		}
	}
	logSuccessfulUpload(ctx, resource, modelVersion)
	return resource, nil
}

// uploadWithPhases 按 init -> transfer -> finish 三阶段执行上传，兼容当前恢复版的 HTTP phase 路径。
func (c *ByteDanceUploadClient) uploadWithPhases(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
	if token == nil {
		return nil, fmt.Errorf("upload token is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	storeURI := token.selectedStoreURI(path)
	if strings.TrimSpace(storeURI) == "" {
		return nil, fmt.Errorf("upload token missing store key")
	}
	initURL, err := buildUploadPhaseURL(token, storeURI, "init", "")
	if err != nil {
		return nil, err
	}
	initRespBody, _, err := c.doUploadPhaseRequest(ctx, "POST", initURL, token, phaseHeaders(token, ""), nil)
	if err != nil {
		return nil, err
	}
	uploadID := parseUploadID(initRespBody)
	if strings.TrimSpace(uploadID) == "" {
		return nil, fmt.Errorf("upload init missing uploadID")
	}

	fileBody, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	if len(fileBody) == 0 {
		return nil, fmt.Errorf("file size is zero")
	}

	transferURL, err := buildUploadPhaseURL(token, storeURI, "transfer", uploadID)
	if err != nil {
		return nil, err
	}
	_, _, err = c.doUploadPhaseRequest(ctx, "PUT", transferURL, token, phaseHeaders(token, mimeTypeForPath(path)), fileBody)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}

	finishURL, err := buildUploadPhaseURL(token, storeURI, "finish", uploadID)
	if err != nil {
		return nil, err
	}
	finishRespBody, _, err := c.doUploadPhaseRequest(ctx, "POST", finishURL, token, phaseHeaders(token, ""), nil)
	if err != nil {
		return nil, err
	}
	return parseUploadedResource(finishRespBody, storeURI, uploadID, token.UploadDomain), nil
}

// buildImageXUploadConfig 把图片上传所需的 STS 凭证、空间和域名收束成稳定配置。
func buildImageXUploadConfig(token *uploadTokenData, path string) (*imageXUploadConfig, error) {
	if token == nil {
		return nil, fmt.Errorf("upload token is required")
	}
	region := normalizeImageXRegion(token.Region)
	apiHost := normalizeImageXHost(token.UploadDomain, region)
	serviceID := firstNonEmpty(
		strings.TrimSpace(token.ServiceID),
		strings.TrimSpace(token.SpaceName),
		inferImageXServiceIDFromStoreURI(token.selectedStoreURI(path)),
	)
	if serviceID == "" {
		serviceID = defaultDreaminaImageXSpaceName
	}
	if apiHost == "" {
		return nil, fmt.Errorf("imagex api host is required")
	}
	if strings.TrimSpace(token.AccessKeyID) == "" || strings.TrimSpace(token.SecretAccessKey) == "" || strings.TrimSpace(token.SessionToken) == "" {
		return nil, fmt.Errorf("upload token missing imagex sts credentials")
	}
	return &imageXUploadConfig{
		Region:          region,
		APIHost:         apiHost,
		UploadHost:      "",
		ServiceID:       serviceID,
		AccessKeyID:     strings.TrimSpace(token.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(token.SecretAccessKey),
		SessionToken:    strings.TrimSpace(token.SessionToken),
	}, nil
}

// parseImageXUploadResult 把 ImageX SDK 返回收敛成当前资源上传链路使用的统一结果。
func parseImageXUploadResult(resp *imageXCommitUploadResult, uploadHost string) (*SingleUploadRes, error) {
	if resp == nil {
		return nil, fmt.Errorf("upload image: no result returned")
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("upload image: empty result set")
	}
	result := resp.Results[0]
	storeURI := strings.TrimSpace(firstNonEmpty(result.URI, imageXImageURIAt(resp, 0)))
	if storeURI == "" {
		return nil, fmt.Errorf("upload image failed: uri is empty")
	}
	if result.URIStatus != 0 && result.URIStatus != 2000 {
		return nil, fmt.Errorf("upload image failed: uri=%s status=%d", storeURI, result.URIStatus)
	}

	extra := map[string]any{
		"request_id":    strings.TrimSpace(resp.RequestID),
		"uri_status":    result.URIStatus,
		"upload_domain": strings.TrimSpace(uploadHost),
	}
	if info := imageXImageInfoAt(resp, 0); info != nil {
		if strings.TrimSpace(info.ImageURI) != "" {
			extra["image_uri"] = strings.TrimSpace(info.ImageURI)
		}
		if info.ImageWidth > 0 {
			extra["image_width"] = info.ImageWidth
		}
		if info.ImageHeight > 0 {
			extra["image_height"] = info.ImageHeight
		}
		if info.ImageSize > 0 {
			extra["image_size"] = info.ImageSize
		}
		if strings.TrimSpace(info.ImageFormat) != "" {
			extra["image_format"] = strings.TrimSpace(info.ImageFormat)
		}
	}
	return &SingleUploadRes{
		// 图片最终 resource_id 仍由后续 resource_store 回填；这里先沿用 store_uri 作为中间态标识。
		ResourceID:   storeURI,
		StoreURI:     storeURI,
		UploadDomain: strings.TrimSpace(uploadHost),
		Extra:        extra,
	}, nil
}

func imageXImageInfoAt(resp *imageXCommitUploadResult, index int) *imageXImageInfo {
	if resp == nil || index < 0 || index >= len(resp.ImageInfos) {
		return nil
	}
	return &resp.ImageInfos[index]
}

func imageXImageURIAt(resp *imageXCommitUploadResult, index int) string {
	if info := imageXImageInfoAt(resp, index); info != nil {
		return strings.TrimSpace(info.ImageURI)
	}
	return ""
}

func (c *imageXHTTPClient) applyUploadImage(params *imageXApplyUploadParam) (*imageXApplyUploadResult, error) {
	query := url.Values{}
	query.Set("Action", "ApplyImageUpload")
	query.Set("Version", "2018-08-01")
	query.Set("ServiceId", strings.TrimSpace(params.ServiceID))
	query.Set("NeedFallback", "true")
	if params.UploadNum > 0 {
		query.Set("UploadNum", strconv.Itoa(params.UploadNum))
	} else {
		query.Set("UploadNum", "1")
	}

	body, err := c.doSignedRequest(http.MethodGet, "/", query, nil, "application/json")
	if err != nil {
		return nil, fmt.Errorf("ApplyUploadImage: %w", err)
	}
	result := &imageXApplyUploadResult{}
	if err := decodeImageXResult(body, result); err != nil {
		return nil, fmt.Errorf("ApplyUploadImage: %w", err)
	}
	return result, nil
}

func (c *imageXHTTPClient) commitUploadImage(params *imageXCommitUploadParam) (*imageXCommitUploadResult, error) {
	payload, err := json.Marshal(map[string]any{
		"SessionKey":  strings.TrimSpace(params.SessionKey),
		"SuccessOids": params.SuccessOids,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal commit image upload payload: %w", err)
	}
	query := url.Values{}
	query.Set("Action", "CommitImageUpload")
	query.Set("Version", "2018-08-01")
	query.Set("ServiceId", strings.TrimSpace(params.ServiceID))
	query.Set("SkipMeta", "false")

	body, err := c.doSignedRequest(http.MethodPost, "/", query, payload, "application/json")
	if err != nil {
		return nil, fmt.Errorf("CommitUploadImage: %w", err)
	}
	result := &imageXCommitUploadResult{}
	if err := decodeImageXResult(body, result); err != nil {
		return nil, fmt.Errorf("CommitUploadImage: %w", err)
	}
	return result, nil
}

func (c *imageXHTTPClient) directUpload(host string, storeInfo imageXStoreInfo, imageBytes []byte, contentType string) error {
	if len(imageBytes) == 0 {
		return fmt.Errorf("file size is zero")
	}
	target := fmt.Sprintf("https://%s/%s", strings.TrimSpace(host), escapeImageXPath(storeInfo.StoreURI))
	req, err := http.NewRequest(http.MethodPut, target, bytes.NewReader(imageBytes))
	if err != nil {
		return fmt.Errorf("build imagex direct upload request: %w", err)
	}
	req.Header.Set("Content-CRC32", fmt.Sprintf("%08x", crc32.ChecksumIEEE(imageBytes)))
	req.Header.Set("Authorization", strings.TrimSpace(storeInfo.Auth))
	if contentType != "" {
		req.Header.Set("Specified-Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("direct upload request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read direct upload response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("direct upload status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	parsed := &imageXDirectUploadResponse{Success: -1}
	if err := json.Unmarshal(body, parsed); err != nil {
		return fmt.Errorf("unmarshal direct upload response: %w", err)
	}
	if parsed.Success != 0 {
		message := strings.TrimSpace(fmt.Sprint(string(body)))
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			message = strings.TrimSpace(parsed.Error.Message)
		}
		return fmt.Errorf("upload fail, %s", message)
	}
	expectedCRC := fmt.Sprintf("%08x", crc32.ChecksumIEEE(imageBytes))
	if got := strings.TrimSpace(parsed.Payload.Hash); got != "" && got != expectedCRC {
		return fmt.Errorf("direct upload crc32 mismatch: got=%s want=%s", got, expectedCRC)
	}
	return nil
}

func (c *imageXHTTPClient) doSignedRequest(method string, requestPath string, query url.Values, body []byte, contentType string) ([]byte, error) {
	host := normalizeImageXHost(c.host, c.region)
	if host == "" {
		return nil, fmt.Errorf("imagex host is required")
	}
	target := url.URL{
		Scheme:   "http",
		Host:     host,
		Path:     requestPath,
		RawQuery: query.Encode(),
	}
	req, err := http.NewRequest(method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Vod-Upload-Appid", "")
	req.Header.Set("X-Vod-Upload-Userid", "")
	req.Header.Set("X-Vod-Upload-Psm", "-")
	if err := signImageXRequest(req, c.region, c.accessKey, c.secretKey, c.sessionToken); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("imagex api status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func decodeImageXResult(body []byte, out any) error {
	resp := &imageXCommonResponse{}
	if err := json.Unmarshal(body, resp); err != nil {
		return fmt.Errorf("unmarshal imagex response: %w", err)
	}
	if resp.ResponseMetadata != nil && resp.ResponseMetadata.Error != nil {
		errObj := resp.ResponseMetadata.Error
		if errObj.CodeN != 0 || strings.TrimSpace(errObj.Code) != "" {
			return fmt.Errorf("request %s error %s", strings.TrimSpace(resp.ResponseMetadata.RequestID), strings.TrimSpace(errObj.Message))
		}
	}
	if len(resp.Result) == 0 || string(resp.Result) == "null" {
		return fmt.Errorf("imagex response missing result")
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("unmarshal imagex result: %w", err)
	}
	return nil
}

func signImageXRequest(req *http.Request, region string, accessKey string, secretKey string, sessionToken string) error {
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("imagex credentials are required")
	}
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}
	query := req.URL.Query()
	req.URL.RawQuery = query.Encode()

	bodyHash, body, err := imageXRequestBodyHash(req)
	if err != nil {
		return err
	}
	nowUTC := time.Now().UTC()
	xDate := nowUTC.Format("20060102T150405Z")
	scopeDate := nowUTC.Format("20060102")
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", xDate)
	req.Header.Set("X-Amz-Content-Sha256", bodyHash)
	if sessionToken = strings.TrimSpace(sessionToken); sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
	}

	signedHeaders, canonicalHeaders := canonicalImageXHeaders(req.Header)
	canonicalRequest := strings.Join([]string{
		req.Method,
		normalizedImageXPath(req.URL.Path),
		normalizedImageXQuery(query),
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")
	credentialScope := strings.Join([]string{scopeDate, normalizeImageXRegion(region), "ImageX", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		xDate,
		credentialScope,
		hashSHA256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := imageXSigningKey(secretKey, scopeDate, normalizeImageXRegion(region), "ImageX")
	signature := hex.EncodeToString(hmacSHA256Bytes(signingKey, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", accessKey, credentialScope, signedHeaders, signature))
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return nil
}

func imageXRequestBodyHash(req *http.Request) (string, []byte, error) {
	if req.Body == nil {
		return hashSHA256Hex(nil), nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return "", nil, fmt.Errorf("read imagex request body: %w", err)
	}
	return hashSHA256Hex(body), body, nil
}

func canonicalImageXHeaders(headers http.Header) (string, string) {
	signMap := map[string]string{}
	for key, values := range headers {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if lowerKey == "" || len(values) == 0 {
			continue
		}
		signMap[lowerKey] = strings.Join(normalizeHeaderValues(values), ",")
	}
	keys := make([]string, 0, len(signMap))
	for key := range signMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := signMap[key]
		if key == "host" {
			if parsedHost, _, found := strings.Cut(value, ":"); found && parsedHost != "" && (strings.HasSuffix(value, ":80") || strings.HasSuffix(value, ":443")) {
				value = parsedHost
			}
		}
		lines = append(lines, key+":"+value)
	}
	return strings.Join(keys, ";"), strings.Join(lines, "\n") + "\n"
}

func normalizedImageXPath(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func escapeImageXPath(path string) string {
	return normalizedImageXPath(path)
}

func normalizedImageXQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0)
	for _, key := range keys {
		queryValues := append([]string(nil), values[key]...)
		if len(queryValues) == 0 {
			pairs = append(pairs, awsURLEscape(key)+"=")
			continue
		}
		sort.Strings(queryValues)
		for _, value := range queryValues {
			pairs = append(pairs, awsURLEscape(key)+"="+awsURLEscape(value))
		}
	}
	return strings.Join(pairs, "&")
}

func imageXSigningKey(secretKey string, date string, region string, service string) []byte {
	dateKey := hmacSHA256Bytes([]byte("AWS4"+secretKey), date)
	regionKey := hmacSHA256Bytes(dateKey, region)
	serviceKey := hmacSHA256Bytes(regionKey, service)
	return hmacSHA256Bytes(serviceKey, "aws4_request")
}

func hmacSHA256Bytes(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func hashSHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// signAWSServiceRequest 以 AWS4-HMAC-SHA256 方式为 VOD OpenAPI 请求补齐鉴权头。
func signAWSServiceRequest(req *httpclient.Request, accessKey string, secretKey string, sessionToken string, region string, service string) error {
	return signAWSServiceRequestAt(req, accessKey, secretKey, sessionToken, region, service, time.Now().UTC())
}

// signAWSServiceRequestAt 使用固定时间签名，便于测试稳定断言。
func signAWSServiceRequestAt(req *httpclient.Request, accessKey string, secretKey string, sessionToken string, region string, service string, now time.Time) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	region = strings.TrimSpace(region)
	service = strings.TrimSpace(service)
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("aws credentials are required")
	}
	if region == "" {
		return fmt.Errorf("aws region is required")
	}
	if service == "" {
		return fmt.Errorf("aws service is required")
	}

	target, query, err := parseRequestTarget(req)
	if err != nil {
		return err
	}
	bodyHash := hashSHA256Hex(req.Body)
	xDate := now.UTC().Format("20060102T150405Z")
	scopeDate := now.UTC().Format("20060102")

	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Host"] = canonicalTOSHost(target.Host)
	req.Headers["X-Amz-Date"] = xDate
	req.Headers["X-Amz-Content-Sha256"] = bodyHash
	if sessionToken = strings.TrimSpace(sessionToken); sessionToken != "" {
		req.Headers["X-Amz-Security-Token"] = sessionToken
	}

	signedHeaders, canonicalHeaders := canonicalAWSServiceHeaders(req.Headers)
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(req.Method)),
		normalizedTOSPath(target.Path),
		normalizedImageXQuery(query),
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")
	credentialScope := strings.Join([]string{scopeDate, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		xDate,
		credentialScope,
		hashSHA256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256Bytes(imageXSigningKey(secretKey, scopeDate, region, service), stringToSign))
	req.Headers["Authorization"] = fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", accessKey, credentialScope, signedHeaders, signature)
	return nil
}

func parseRequestTarget(req *httpclient.Request) (*url.URL, url.Values, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("request is required")
	}
	target, err := url.Parse(strings.TrimSpace(req.Path))
	if err != nil {
		return nil, nil, fmt.Errorf("parse request target: %w", err)
	}
	if target.Path == "" {
		target.Path = "/"
	}
	query := target.Query()
	for key, value := range req.Query {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		query.Set(key, strings.TrimSpace(value))
	}
	target.RawQuery = query.Encode()
	req.Path = target.String()
	return target, query, nil
}

func canonicalAWSServiceHeaders(headers map[string]string) (string, string) {
	signMap := map[string]string{}
	for key, value := range headers {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		text := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if lowerKey == "" || text == "" || lowerKey == "authorization" {
			continue
		}
		if lowerKey != "content-type" && !strings.HasPrefix(lowerKey, "x-amz-") {
			continue
		}
		signMap[lowerKey] = text
	}
	keys := make([]string, 0, len(signMap))
	for key := range signMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+":"+signMap[key])
	}
	return strings.Join(keys, ";"), strings.Join(lines, "\n") + "\n"
}

// shouldSignTOSPhaseRequest 判断当前 phase 请求是否具备可用的 TOS STS 凭证。
func shouldSignTOSPhaseRequest(token *uploadTokenData) bool {
	if token == nil {
		return false
	}
	return strings.TrimSpace(token.AccessKeyID) != "" &&
		strings.TrimSpace(token.SecretAccessKey) != "" &&
		strings.TrimSpace(token.SessionToken) != ""
}

// signTOSPhaseRequest 以 TOS4-HMAC-SHA256 方式为 phase 上传请求补齐鉴权头。
func signTOSPhaseRequest(req *httpclient.Request, token *uploadTokenData) error {
	return signTOSPhaseRequestAt(req, token, time.Now().UTC())
}

// signTOSPhaseRequestAt 使用固定时间签名，便于测试稳定断言。
func signTOSPhaseRequestAt(req *httpclient.Request, token *uploadTokenData, now time.Time) error {
	requestHost, requestPath := resolveSignedTOSLocation(req, token)
	region := normalizeTOSRegion(token.Region, requestHost)
	return signTOSPhaseRequestVariantAt(req, token, now, requestHost, requestPath, region, unsignedTOSPayloadHash)
}

func signTOSPhaseRequestVariantAt(req *httpclient.Request, token *uploadTokenData, now time.Time, requestHost string, requestPath string, region string, payloadHash string) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	if !shouldSignTOSPhaseRequest(token) {
		return fmt.Errorf("tos credentials are required")
	}
	target := strings.TrimSpace(req.Path)
	if target == "" {
		return fmt.Errorf("request.path is required")
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("parse upload target: %w", err)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		return fmt.Errorf("upload target missing host")
	}
	xDate := now.UTC().Format("20060102T150405Z")
	scopeDate := now.UTC().Format("20060102")
	if strings.TrimSpace(requestHost) == "" {
		requestHost = parsed.Host
	}
	if strings.TrimSpace(requestPath) == "" {
		requestPath = parsed.Path
	}
	if strings.TrimSpace(region) == "" {
		region = normalizeTOSRegion(token.Region, requestHost)
	}
	if strings.TrimSpace(payloadHash) == "" {
		payloadHash = unsignedTOSPayloadHash
	}

	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Host"] = canonicalTOSHost(requestHost)
	req.Headers["X-Tos-Date"] = xDate
	req.Headers["X-Tos-Content-Sha256"] = payloadHash
	if sessionToken := strings.TrimSpace(token.SessionToken); sessionToken != "" {
		req.Headers["X-Tos-Security-Token"] = sessionToken
	}

	signedHeaders, canonicalHeaders := canonicalTOSHeaders(req.Headers)
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(req.Method)),
		normalizedTOSPath(requestPath),
		normalizedTOSQuery(parsed.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	credentialScope := strings.Join([]string{scopeDate, region, "tos", "request"}, "/")
	stringToSign := strings.Join([]string{
		tosSigningAlgorithm,
		xDate,
		credentialScope,
		hashSHA256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256Bytes(tosSigningKey(strings.TrimSpace(token.SecretAccessKey), scopeDate, region), stringToSign))
	req.Headers["Authorization"] = fmt.Sprintf("%s Credential=%s/%s,SignedHeaders=%s,Signature=%s", tosSigningAlgorithm, strings.TrimSpace(token.AccessKeyID), credentialScope, signedHeaders, signature)
	return nil
}

func resolveSignedTOSLocation(req *httpclient.Request, token *uploadTokenData) (string, string) {
	if req == nil {
		return "", ""
	}
	target := strings.TrimSpace(req.Path)
	if target == "" {
		return "", ""
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return "", ""
	}
	return resolveTOSRequestHostAndPath(token, parsed.Host, parsed.Path)
}

func tosSigningKey(secretKey string, date string, region string) []byte {
	dateKey := hmacSHA256Bytes([]byte(secretKey), date)
	regionKey := hmacSHA256Bytes(dateKey, region)
	serviceKey := hmacSHA256Bytes(regionKey, "tos")
	return hmacSHA256Bytes(serviceKey, "request")
}

func canonicalTOSHeaders(headers map[string]string) (string, string) {
	signMap := map[string]string{}
	for key, value := range headers {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		text := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if lowerKey == "" || text == "" || lowerKey == "authorization" {
			continue
		}
		if lowerKey != "host" && lowerKey != "content-type" && !strings.HasPrefix(lowerKey, "x-tos") {
			continue
		}
		if lowerKey == "host" {
			text = canonicalTOSHost(text)
		}
		signMap[lowerKey] = text
	}
	keys := make([]string, 0, len(signMap))
	for key := range signMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+":"+signMap[key])
	}
	return strings.Join(keys, ";"), strings.Join(lines, "\n") + "\n"
}

func canonicalTOSHost(host string) string {
	host = strings.TrimSpace(host)
	if parsedHost, port, found := strings.Cut(host, ":"); found && parsedHost != "" && (port == "80" || port == "443") {
		return parsedHost
	}
	return host
}

func normalizedTOSPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if index == 0 && part == "" {
			continue
		}
		parts[index] = awsURLEscape(part)
	}
	return strings.Join(parts, "/")
}

func normalizedTOSQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0)
	for _, key := range keys {
		queryValues := append([]string(nil), values[key]...)
		if len(queryValues) == 0 {
			pairs = append(pairs, awsURLEscape(key)+"=")
			continue
		}
		sort.Strings(queryValues)
		for _, value := range queryValues {
			pairs = append(pairs, awsURLEscape(key)+"="+awsURLEscape(value))
		}
	}
	return strings.Join(pairs, "&")
}

// doUploadPhaseRequest 发送单次上传 phase 请求，并统一校验 HTTP 状态码与响应正文。
func (c *ByteDanceUploadClient) doUploadPhaseRequest(ctx context.Context, method string, target string, token *uploadTokenData, headers map[string]string, body []byte) ([]byte, int, error) {
	if shouldSignTOSPhaseRequest(token) {
		return c.doSignedUploadPhaseRequest(ctx, method, target, token, headers, body)
	}

	req, err := c.http.NewRequest(ctx, method, target, body, headers)
	if err != nil {
		return nil, 0, err
	}
	c.http.ApplyBackendHeaders(req)
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	resp, _ := respAny.(*httpclient.Response)
	if resp == nil {
		return nil, 0, fmt.Errorf("response is required")
	}
	respBody, err := httpclient.ReadResponseBody(resp)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, resp.StatusCode, nil
}

func (c *ByteDanceUploadClient) doSignedUploadPhaseRequest(ctx context.Context, method string, target string, token *uploadTokenData, headers map[string]string, body []byte) ([]byte, int, error) {
	var lastBody []byte
	var lastStatus int
	var lastErr error
	for _, variant := range candidateTOSSignVariants(target, token) {
		req, err := c.http.NewRequest(ctx, method, targetWithPath(target, variant.Path), body, headers)
		if err != nil {
			return nil, 0, err
		}
		if err := signTOSPhaseRequestVariantAt(req, token, time.Now().UTC(), variant.Host, variant.Path, variant.Region, variant.PayloadHash); err != nil {
			return nil, 0, err
		}
		respAny, err := c.http.Do(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		resp, _ := respAny.(*httpclient.Response)
		if resp == nil {
			lastErr = fmt.Errorf("response is required")
			continue
		}
		respBody, readErr := httpclient.ReadResponseBody(resp)
		if readErr != nil {
			lastBody = respBody
			lastStatus = resp.StatusCode
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, resp.StatusCode, nil
		}
		lastBody = respBody
		lastStatus = resp.StatusCode
		lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		if !isTOSInvalidAuthorization(resp.StatusCode, respBody) {
			return respBody, resp.StatusCode, lastErr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("signed upload request failed")
	}
	return lastBody, lastStatus, lastErr
}

type tosSignVariant struct {
	Host        string
	Path        string
	Region      string
	PayloadHash string
}

func candidateTOSSignVariants(target string, token *uploadTokenData) []tosSignVariant {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return []tosSignVariant{{PayloadHash: unsignedTOSPayloadHash}}
	}
	baseHost := canonicalTOSHost(parsed.Host)
	basePath := parsed.Path
	derivedHost, derivedPath := resolveTOSRequestHostAndPath(token, parsed.Host, parsed.Path)
	regionCandidates := orderedNonEmptyUnique(
		strings.TrimSpace(token.Region),
		normalizeTOSRegion(token.Region, derivedHost),
		"cn",
		"cn-beijing",
	)
	hostCandidates := orderedNonEmptyUnique(derivedHost, baseHost)
	pathCandidates := orderedNonEmptyUnique(derivedPath, basePath)

	var variants []tosSignVariant
	for _, region := range regionCandidates {
		for _, host := range hostCandidates {
			for _, path := range pathCandidates {
				variants = append(variants, tosSignVariant{
					Host:        host,
					Path:        path,
					Region:      region,
					PayloadHash: unsignedTOSPayloadHash,
				})
			}
		}
	}
	if len(variants) == 0 {
		return []tosSignVariant{{
			Host:        baseHost,
			Path:        basePath,
			Region:      normalizeTOSRegion(token.Region, baseHost),
			PayloadHash: unsignedTOSPayloadHash,
		}}
	}
	return variants
}

func targetWithPath(target string, requestPath string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	if strings.TrimSpace(requestPath) != "" {
		parsed.Path = requestPath
	}
	return parsed.String()
}

func isTOSInvalidAuthorization(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	trimmed := strings.TrimSpace(string(body))
	return strings.Contains(trimmed, "InvalidAuthorization")
}

func orderedNonEmptyUnique(values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
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

// durationProbe 返回媒体时长探测函数，优先使用外部注入实现，默认回落到本地探测。
func (c *ByteDanceUploadClient) durationProbe() func(context.Context, string) (float64, error) {
	if c != nil && c.probeDurationFunc != nil {
		return c.probeDurationFunc
	}
	return probeMediaDurationSeconds
}

// buildVODUploadConfig 从上传 token 和本地文件路径收束出一份可对接 VOD SDK 的兼容配置。
func buildVODUploadConfig(token *uploadTokenData, path string) (*vodUploadConfig, error) {
	if token == nil {
		return nil, fmt.Errorf("upload token is required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return &vodUploadConfig{
		ResourceType: strings.TrimSpace(token.ResourceType),
		FilePath:     path,
		FileName:     filepath.Base(path),
		UploadAuth:   strings.TrimSpace(token.UploadAuth),
		SessionKey:   strings.TrimSpace(token.SessionKey),
		ServiceID:    strings.TrimSpace(token.ServiceID),
		SpaceName:    strings.TrimSpace(token.SpaceName),
		Bucket:       strings.TrimSpace(token.Bucket),
		// buckets 可能继续参与真实 SDK 的路由/容灾选择，这里复制一份避免外部切片后续被污染。
		Buckets:        append([]string(nil), token.Buckets...),
		Region:         strings.TrimSpace(token.Region),
		IDC:            strings.TrimSpace(token.IDC),
		InVolcanoCloud: token.InVolcanoCloud,
		StoreURI:       strings.TrimSpace(token.selectedStoreURI(path)),
		UploadDomain:   strings.TrimSpace(token.UploadDomain),
	}, nil
}

// normalizeVODUploadResult 把当前上传结果转换成统一的 VOD 诊断视图，便于后续校验和落盘。
func normalizeVODUploadResult(upload *SingleUploadRes) *vodUploadResult {
	if upload == nil {
		return nil
	}
	result := &vodUploadResult{
		ResourceID:   strings.TrimSpace(upload.ResourceID),
		StoreURI:     strings.TrimSpace(upload.StoreURI),
		UploadID:     strings.TrimSpace(upload.UploadID),
		UploadDomain: strings.TrimSpace(upload.UploadDomain),
	}
	if upload.Extra != nil {
		result.Raw = upload.Extra
		result.DurationSeconds = extractUploadDurationSeconds(upload.Extra)
	}
	return result
}

// validateUploadedMediaDuration 优先使用远端上传结果里的时长元数据校验媒体长度，缺失时再回落本地探测。
func validateUploadedMediaDuration(ctx context.Context, resourceType string, path string, modelVersion string, upload *SingleUploadRes, probe func(context.Context, string) (float64, error)) error {
	vodResult := normalizeVODUploadResult(upload)
	if vodResult != nil && vodResult.DurationSeconds > 0 {
		kind := actualResourceKind(resourceType, path)
		return validateDurationSeconds(kind, vodResult.DurationSeconds, modelVersion)
	}
	return validateVideoAudioDuration(ctx, resourceType, path, modelVersion, probe)
}

// logSuccessfulUpload 记录上传成功后的关键资源信息，便于排查上传与时长校验问题。
func logSuccessfulUpload(v ...any) {
	var (
		ctx          context.Context
		resource     *Resource
		modelVersion string
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case context.Context:
			ctx = value
		case *Resource:
			resource = value
		case string:
			if modelVersion == "" {
				modelVersion = strings.TrimSpace(value)
			}
		}
	}
	if resource == nil {
		return
	}
	fields := []any{
		"resource_id", resource.ResourceID,
		"resource_type", resource.ResourceType,
		"kind", resource.Kind,
		"path", resource.Path,
		"size", resource.Size,
		"scene", resource.Scene,
	}
	if strings.TrimSpace(modelVersion) != "" {
		fields = append(fields, "model_version", strings.TrimSpace(modelVersion))
	}
	args := append([]any{ctx}, fields...)
	logUploadedValue(args...)
}

// logUploadedValue 把上传日志字段整理成稳定的 key/value 序列并写入日志。
func logUploadedValue(v ...any) {
	var (
		ctx    context.Context
		fields []any
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case context.Context:
			ctx = value
		default:
			fields = append(fields, value)
		}
	}
	if len(fields) == 0 {
		return
	}
	parts := make([]string, 0, len(fields)/2+1)
	for i := 0; i < len(fields); i += 2 {
		key := strings.TrimSpace(fmt.Sprint(fields[i]))
		if key == "" {
			continue
		}
		value := ""
		if i+1 < len(fields) {
			value = strings.TrimSpace(fmt.Sprint(fields[i+1]))
		}
		parts = append(parts, key+"="+value)
	}
	if len(parts) == 0 {
		return
	}
	logging.InfofContext(ctx, "[ResourceUpload] %s", strings.Join(parts, " "))
}

// validateVideoAudioDuration 校验视频或音频资源时长是否满足当前模型版本要求。
func validateVideoAudioDuration(v ...any) error {
	var (
		ctx          context.Context
		resourceType string
		path         string
		modelVersion string
		probe        func(context.Context, string) (float64, error)
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case context.Context:
			ctx = value
		case string:
			if resourceType == "" {
				resourceType = strings.TrimSpace(value)
				continue
			}
			if path == "" {
				path = strings.TrimSpace(value)
				continue
			}
			if modelVersion == "" {
				modelVersion = strings.TrimSpace(value)
			}
		case func(context.Context, string) (float64, error):
			probe = value
		}
	}
	kind := actualResourceKind(resourceType, path)
	if kind != "video" && kind != "audio" {
		return nil
	}
	// 时长校验只针对音视频生效，并且优先复用模型版本对应的远端限制窗口。
	// 如果探测工具不可用，当前实现选择“记录日志并放行”，避免本地环境差异把可上传任务提前拦死。
	// 这条路径目前仍是“本地前置探测”；原始二进制中的同名 helper 已确认更接近 VOD 上传结果校验。
	minSeconds, maxSeconds := supportedDurationRange(modelVersion)
	if minSeconds <= 0 || maxSeconds <= 0 {
		return nil
	}
	if probe == nil {
		probe = probeMediaDurationSeconds
	}
	durationSeconds, err := probe(ctx, path)
	if err != nil {
		logging.InfofContext(ctx, "[ResourceUpload] skip duration validation kind=%s path=%q model_version=%s reason=%s", kind, path, strings.TrimSpace(modelVersion), strings.TrimSpace(err.Error()))
		return nil
	}
	if err := validateDurationSeconds(kind, durationSeconds, modelVersion); err != nil {
		return err
	}
	logging.InfofContext(ctx, "[ResourceUpload] validated duration kind=%s path=%q model_version=%s duration_seconds=%.3f range=%.0f-%.0f", kind, path, strings.TrimSpace(modelVersion), durationSeconds, minSeconds, maxSeconds)
	return nil
}

// validateDurationSeconds 按资源类型和模型版本检查时长是否落在允许区间内。
func validateDurationSeconds(kind string, durationSeconds float64, modelVersion string) error {
	minSeconds, maxSeconds := supportedDurationRange(modelVersion)
	if minSeconds <= 0 || maxSeconds <= 0 {
		return nil
	}
	if durationSeconds < minSeconds || durationSeconds > maxSeconds {
		if strings.TrimSpace(modelVersion) == "" {
			return fmt.Errorf("%s duration in seconds is out of range: got %.3f, supported range: %.0f-%.0f", kind, durationSeconds, minSeconds, maxSeconds)
		}
		return fmt.Errorf("%s duration in seconds is out of range for model %s: got %.3f, supported range: %.0f-%.0f", kind, strings.TrimSpace(modelVersion), durationSeconds, minSeconds, maxSeconds)
	}
	return nil
}

// uploadSceneForType 把资源类型映射到上传接口要求的 scene 编号。
func uploadSceneForType(resourceType string) int {
	switch strings.TrimSpace(resourceType) {
	case "image":
		return 1
	case "video":
		return 2
	case "audio":
		return 3
	default:
		return 0
	}
}

func uploadKindFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg":
		return "audio"
	default:
		return "video"
	}
}

func actualResourceKind(resourceType string, path string) string {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "image" {
		return "image"
	}
	if resourceType == "audio" {
		return "audio"
	}
	if resourceType == "video" {
		return uploadKindFromPath(path)
	}
	return uploadKindFromPath(path)
}

func mimeTypeForPath(path string) string {
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(strings.TrimSpace(path))))
	if strings.TrimSpace(mimeType) == "" {
		switch actualResourceKind("", path) {
		case "audio":
			return "audio/mpeg"
		case "video":
			return "video/mp4"
		default:
			return "application/octet-stream"
		}
	}
	return mimeType
}

func marshalUploadSummary(v any) string {
	body, _ := json.Marshal(v)
	return string(body)
}

func uploadModelVersionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	modelVersion, _ := ctx.Value(uploadModelVersionKey{}).(string)
	return strings.TrimSpace(modelVersion)
}

func supportedDurationRange(modelVersion string) (float64, float64) {
	switch normalizeModelVersion(modelVersion) {
	case "3.0", "3.0fast", "3.0pro", "3.1":
		return 3, 10
	case "3.5pro":
		return 4, 12
	case "", "seedance2.0", "seedance2.0fast", "seedance2.0vip", "seedance2.0fastvip":
		return 4, 15
	default:
		return 4, 15
	}
}

func normalizeModelVersion(modelVersion string) string {
	modelVersion = strings.ToLower(strings.TrimSpace(modelVersion))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(modelVersion)
}

func probeMediaDurationSeconds(ctx context.Context, path string) (float64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("path is required")
	}
	if ffprobePath, err := exec.LookPath("ffprobe"); err == nil && strings.TrimSpace(ffprobePath) != "" {
		duration, err := probeDurationWithFFprobe(ctx, ffprobePath, path)
		if err == nil && duration > 0 {
			return duration, nil
		}
	}
	if mdlsPath, err := exec.LookPath("mdls"); err == nil && strings.TrimSpace(mdlsPath) != "" {
		duration, err := probeDurationWithMDLS(ctx, mdlsPath, path)
		if err == nil && duration > 0 {
			return duration, nil
		}
	}
	return 0, fmt.Errorf("unable to probe media duration")
}

func probeDurationWithFFprobe(ctx context.Context, ffprobePath string, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return 0, fmt.Errorf("ffprobe returned empty duration")
	}
	duration, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	return duration, nil
}

func probeDurationWithMDLS(ctx context.Context, mdlsPath string, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, mdlsPath, "-raw", "-name", "kMDItemDurationSeconds", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(output))
	if text == "" || text == "(null)" {
		return 0, fmt.Errorf("mdls returned empty duration")
	}
	duration, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	return duration, nil
}

// parseUploadTokenResponse 解析上传 token 接口响应，并抽出最外层状态字段与 data 区。
func parseUploadTokenResponse(body []byte, resourceType string) (*uploadTokenResp, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid JSON")
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	dataMap := firstAnyMap(payload["data"], payload["Data"], payload["result"], payload["Result"], payload["payload"], payload["Payload"])
	if len(dataMap) == 0 && uploadTokenRootLooksUsable(payload) {
		dataMap = payload
	}
	data := parseUploadTokenData(dataMap, resourceType)
	return &uploadTokenResp{
		Ret:          firstMetadataStringValue(payload, "ret", "Ret", "status", "Status"),
		Code:         firstMetadataStringValue(payload, "code", "Code", "status_code", "statusCode", "StatusCode"),
		Msg:          firstMetadataStringValue(payload, "msg", "Msg", "message", "Message"),
		Message:      firstMetadataStringValue(payload, "message", "Message", "msg", "Msg", "description", "Description"),
		Scene:        firstIntValue(payload["scene"], payload["Scene"]),
		ResourceType: strings.TrimSpace(resourceType),
		Data:         data,
		Raw:          payload,
	}, nil
}

// parseUploadTokenData 归一化上传 token 的 data 区字段，兼容历史别名和 extra wrapper。
func parseUploadTokenData(payload map[string]any, resourceType string) *uploadTokenData {
	if len(payload) == 0 {
		return nil
	}
	// 上传 token 的返回在恢复过程中出现过多种形态。
	// 这里优先吃掉已经在原始二进制和线上响应里观察到的字段别名，减少后续接口细节差异带来的误判。
	extra := firstAnyMap(payload["extra"], payload["Extra"], payload["ext"], payload["Ext"])
	data := &uploadTokenData{
		Scene:           firstIntValue(payload["scene"], payload["Scene"]),
		AgentScene:      firstIntValue(payload["agent_scene"], payload["agentScene"], payload["AgentScene"], payload["scene"], payload["Scene"]),
		ResourceType:    firstStringValue(payload, "resource_type", "resourceType", "ResourceType", "type", "Type"),
		AccessKeyID:     firstStringValue(payload, "access_key_id", "accessKeyId", "AccessKeyId", "AccessKeyID"),
		SecretAccessKey: firstStringValue(payload, "secret_access_key", "secretAccessKey", "SecretAccessKey", "SecretAccesskey"),
		SessionToken:    firstStringValue(payload, "session_token", "sessionToken", "SessionToken"),
		UploadDomain:    firstStringValue(payload, "upload_domain", "uploadDomain", "UploadDomain"),
		ServiceID:       firstStringValue(payload, "service_id", "serviceId", "ServiceId", "ServiceID"),
		SpaceName:       firstStringValue(payload, "space_name", "spaceName", "SpaceName"),
		SessionKey:      firstStringValue(payload, "session_key", "sessionKey", "SessionKey"),
		UploadAuth:      firstScalarString(payload["upload_auth"], payload["uploadAuth"], payload["UploadAuth"], payload["upload_token"], payload["uploadToken"], payload["UploadToken"]),
		StoreURI:        firstStringValue(payload, "store_uri", "storeUri", "StoreURI", "uri", "Uri"),
		StoreKeys:       firstStringSlice(payload["store_keys"], payload["storeKeys"], payload["StoreKeys"], payload["store_uris"], payload["storeUris"], payload["uris"]),
		UploadNum:       firstIntValue(payload["upload_num"], payload["uploadNum"], payload["UploadNum"]),
		TosHeaders:      firstStringValue(payload, "tos_headers", "tosHeaders", "TosHeaders"),
		TosMeta:         firstStringValue(payload, "tos_meta", "tosMeta", "TosMeta"),
		Bucket:          firstStringValue(payload, "bucket", "Bucket"),
		Buckets:         firstStringSlice(payload["buckets"], payload["Buckets"]),
		Region:          firstStringValue(payload, "region", "Region"),
		IDC:             firstStringValue(payload, "idc", "IDC"),
		InVolcanoCloud:  firstBoolValue(payload["in_volcano_cloud"], payload["inVolcanoCloud"], payload["InVolcanoCloud"]),
		StoreInfos:      parseUploadStoreInfos(payload["store_infos"], payload["StoreInfos"], payload["storeInfos"], payload),
		Extra:           extra,
		Raw:             payload,
	}
	if data.Scene == 0 {
		data.Scene = uploadSceneForType(resourceType)
	}
	if data.AgentScene == 0 {
		data.AgentScene = data.Scene
	}
	if data.ResourceType == "" {
		data.ResourceType = strings.TrimSpace(resourceType)
	}
	if data.Extra != nil {
		if len(data.StoreInfos) == 0 {
			data.StoreInfos = parseUploadStoreInfos(
				data.Extra["store_infos"], data.Extra["StoreInfos"], data.Extra["storeInfos"],
				data.Extra["store_info"], data.Extra["StoreInfo"], data.Extra["storeInfo"],
				data.Extra,
			)
		}
		if data.TosHeaders == "" {
			data.TosHeaders = firstStringValue(data.Extra, "tos_headers", "tosHeaders", "TosHeaders")
		}
		if data.TosMeta == "" {
			data.TosMeta = firstStringValue(data.Extra, "tos_meta", "tosMeta", "TosMeta")
		}
		if data.AccessKeyID == "" {
			data.AccessKeyID = firstStringValue(data.Extra, "access_key_id", "accessKeyId", "AccessKeyId", "AccessKeyID")
		}
		if data.SecretAccessKey == "" {
			data.SecretAccessKey = firstStringValue(data.Extra, "secret_access_key", "secretAccessKey", "SecretAccessKey", "SecretAccesskey")
		}
		if data.SessionToken == "" {
			data.SessionToken = firstStringValue(data.Extra, "session_token", "sessionToken", "SessionToken")
		}
		if data.UploadDomain == "" {
			data.UploadDomain = firstStringValue(data.Extra, "upload_domain", "uploadDomain", "UploadDomain")
		}
		if data.StoreURI == "" {
			data.StoreURI = firstStringValue(data.Extra, "store_uri", "storeUri", "StoreURI", "uri", "Uri")
		}
		if data.UploadAuth == "" {
			data.UploadAuth = firstScalarString(data.Extra["upload_auth"], data.Extra["uploadAuth"], data.Extra["UploadAuth"], data.Extra["upload_token"], data.Extra["uploadToken"], data.Extra["UploadToken"])
		}
		if data.SessionKey == "" {
			data.SessionKey = firstScalarString(data.Extra["session_key"], data.Extra["sessionKey"], data.Extra["SessionKey"])
		}
		if data.ServiceID == "" {
			data.ServiceID = firstStringValue(data.Extra, "service_id", "serviceId", "ServiceId", "ServiceID")
		}
		if data.SpaceName == "" {
			data.SpaceName = firstStringValue(data.Extra, "space_name", "spaceName", "SpaceName")
		}
		if data.Bucket == "" {
			data.Bucket = firstStringValue(data.Extra, "bucket", "Bucket")
		}
		if len(data.Buckets) == 0 {
			data.Buckets = firstStringSlice(data.Extra["buckets"], data.Extra["Buckets"])
		}
		if data.Region == "" {
			data.Region = firstStringValue(data.Extra, "region", "Region")
		}
		if data.IDC == "" {
			data.IDC = firstStringValue(data.Extra, "idc", "IDC")
		}
		if !data.InVolcanoCloud {
			data.InVolcanoCloud = firstBoolValue(data.Extra["in_volcano_cloud"], data.Extra["inVolcanoCloud"], data.Extra["InVolcanoCloud"])
		}
	}
	if data.UploadAuth == "" {
		data.UploadAuth = recursiveStringValue(payload, "upload_auth", "uploadAuth", "UploadAuth", "upload_token", "uploadToken", "UploadToken")
	}
	if data.AccessKeyID == "" {
		data.AccessKeyID = recursiveStringValue(payload, "access_key_id", "accessKeyId", "AccessKeyId", "AccessKeyID")
	}
	if data.SecretAccessKey == "" {
		data.SecretAccessKey = recursiveStringValue(payload, "secret_access_key", "secretAccessKey", "SecretAccessKey", "SecretAccesskey")
	}
	if data.SessionToken == "" {
		data.SessionToken = recursiveStringValue(payload, "session_token", "sessionToken", "SessionToken")
	}
	if data.SessionKey == "" {
		data.SessionKey = firstNonEmpty(
			recursiveStringValue(payload, "session_key", "sessionKey", "SessionKey"),
			data.UploadAuth,
		)
	}
	if data.UploadDomain == "" {
		data.UploadDomain = recursiveStringValue(payload, "upload_domain", "uploadDomain", "UploadDomain")
	}
	if data.StoreURI == "" {
		data.StoreURI = recursiveStringValue(payload, "store_uri", "storeUri", "StoreURI", "uri", "Uri")
	}
	if data.ServiceID == "" {
		data.ServiceID = recursiveStringValue(payload, "service_id", "serviceId", "ServiceId", "ServiceID")
	}
	if data.SpaceName == "" {
		data.SpaceName = recursiveStringValue(payload, "space_name", "spaceName", "SpaceName")
	}
	if len(data.StoreInfos) > 0 {
		if data.StoreURI == "" {
			data.StoreURI = firstNonEmpty(strings.TrimSpace(data.StoreInfos[0].StoreURI), strings.TrimSpace(data.StoreInfos[0].StoreKey))
		}
		if len(data.StoreKeys) == 0 {
			keys := make([]string, 0, len(data.StoreInfos))
			for _, item := range data.StoreInfos {
				if item == nil {
					continue
				}
				if key := firstNonEmpty(strings.TrimSpace(item.StoreKey), strings.TrimSpace(item.StoreURI)); key != "" {
					keys = append(keys, key)
				}
				if data.UploadDomain == "" && strings.TrimSpace(item.UploadDomain) != "" {
					data.UploadDomain = strings.TrimSpace(item.UploadDomain)
				}
			}
			if len(keys) > 0 {
				data.StoreKeys = keys
			}
		}
		if data.UploadNum == 0 {
			data.UploadNum = len(data.StoreInfos)
		}
	}
	if data.ResourceType != "image" {
		if data.UploadDomain == "" && data.AccessKeyID != "" && data.SecretAccessKey != "" && data.SessionToken != "" {
			data.UploadDomain = defaultDreaminaVODHost
		}
		if data.SpaceName == "" && data.StoreURI != "" {
			data.SpaceName = defaultDreaminaVODSpaceName
		}
		if data.ServiceID == "" && data.SpaceName != "" {
			data.ServiceID = data.SpaceName
		}
	}
	if data.ResourceType == "image" {
		if data.SpaceName == "" {
			data.SpaceName = firstNonEmpty(data.ServiceID, inferImageXServiceIDFromStoreURI(data.StoreURI))
		}
		if data.ServiceID == "" {
			data.ServiceID = firstNonEmpty(data.SpaceName, inferImageXServiceIDFromStoreURI(data.StoreURI))
		}
		if data.UploadDomain == "" {
			data.UploadDomain = normalizeImageXHost("", data.Region)
		}
		if data.SpaceName == "" && (data.AccessKeyID != "" || data.SessionToken != "") {
			data.SpaceName = defaultDreaminaImageXSpaceName
		}
		if data.ServiceID == "" && data.SpaceName != "" {
			data.ServiceID = data.SpaceName
		}
	}
	return data
}

// parseResourceStoreResponse 解析 resource_store 响应，并抽出最终 stored 资源列表。
func parseResourceStoreResponse(body []byte) (*resourceStoreResp, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid JSON")
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	dataMap := firstAnyMap(payload["data"], payload["Data"], payload["result"], payload["Result"], payload["payload"], payload["Payload"])
	if len(dataMap) == 0 {
		for _, key := range []string{"resource_items", "resourceItems", "ResourceItems"} {
			if _, ok := payload[key]; ok {
				dataMap = payload
				break
			}
		}
	}
	if len(dataMap) == 0 && resourceStoreRootLooksUsable(payload) {
		dataMap = payload
	}
	stored := parseStoredResources(dataMap)
	return &resourceStoreResp{
		Ret:     firstMetadataStringValue(payload, "ret", "Ret", "status", "Status"),
		Code:    firstMetadataStringValue(payload, "code", "Code", "status_code", "statusCode", "StatusCode"),
		Msg:     firstMetadataStringValue(payload, "msg", "Msg", "message", "Message"),
		Message: firstMetadataStringValue(payload, "message", "Message", "msg", "Msg", "description", "Description"),
		Data: &resourceStoreResult{
			Stored: stored,
			Raw:    dataMap,
		},
	}, nil
}

// parseStoredResources 从 resource_store 的 data 区中提取最终 stored 资源列表。
func parseStoredResources(data map[string]any) []*Resource {
	if len(data) == 0 {
		return nil
	}
	if nested := parseStoredResourcesFromValue(data, 0); len(nested) > 0 {
		return nested
	}
	return nil
}

// parseStoredResourcesFromValue 递归遍历 stored/resources/items 等容器，兼容 wrapper 和 keyed-object 形态。
func parseStoredResourcesFromValue(value any, depth int) []*Resource {
	if depth > 4 {
		return nil
	}
	data, ok := value.(map[string]any)
	if !ok || len(data) == 0 {
		return nil
	}
	// 递归进入 wrapper 子项后，当前层本身就可能已经是最终资源对象。
	if single := parseStoredResource(data); single != nil {
		return []*Resource{single}
	}
	candidates := []any{
		data["resource_items"],
		data["resourceItems"],
		data["ResourceItems"],
		data["stored"],
		data["Stored"],
		data["items"],
		data["Items"],
		data["resources"],
		data["Resources"],
		data["resource"],
		data["Resource"],
		data["list"],
		data["List"],
	}
	for _, candidate := range candidates {
		items := resourceSliceOfAny(candidate)
		if len(items) == 0 {
			continue
		}
		out := make([]*Resource, 0, len(items))
		for _, item := range items {
			parsed := parseStoredResource(item)
			if parsed != nil {
				out = append(out, parsed)
				continue
			}
			// 有些 stored 子项自己还会再包一层 Data/Payload/Result。
			// 这里继续向下递归，避免 keyed-object / list 容器里混入 wrapper 子项时整项漏掉。
			if nested := parseStoredResourcesFromValue(item, depth+1); len(nested) > 0 {
				out = append(out, nested...)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if single := parseStoredResource(firstNonNil(data["resource"], data["Resource"])); single != nil {
		return []*Resource{single}
	}
	for _, key := range []string{"data", "Data", "result", "Result", "payload", "Payload"} {
		if nested := parseStoredResourcesFromValue(data[key], depth+1); len(nested) > 0 {
			return nested
		}
	}
	return nil
}

// parseStoredResource 解析单个最终 stored 资源节点；这里只接受明确的 resource_id。
func parseStoredResource(value any) *Resource {
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	// resource_store 是最终落库确认接口，这里只接受明确的 resource_id。
	// uri/store_uri 仍然只作为路径信息保留，避免把存储键继续误当成最终资源身份。
	resourceID := firstStringValue(root, "resource_id", "resourceId", "ResourceId", "ResourceID")
	if resourceID == "" {
		return nil
	}
	return &Resource{
		ResourceID:   resourceID,
		ResourceType: firstStringValue(root, "resource_type", "resourceType", "ResourceType"),
		Path:         firstStringValue(root, "path", "Path", "store_uri", "storeUri", "StoreURI", "uri", "Uri", "resource_value", "resourceValue", "ResourceValue"),
		Name:         firstStringValue(root, "name", "Name"),
		Size:         firstInt64Value(root["size"], root["Size"]),
		Scene:        firstIntValue(root["scene"], root["Scene"], root["agent_scene"], root["agentScene"], root["AgentScene"]),
		Kind:         firstStringValue(root, "kind", "Kind"),
		MimeType:     firstStringValue(root, "mime_type", "mimeType", "MimeType"),
	}
}

// buildResourceStoreRequest 把本地上传结果转换成 resource_store 请求体。
func buildResourceStoreRequest(items []*Resource) *resourceStoreReq {
	storeItems := make([]*resourceStoreItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		storeItems = append(storeItems, &resourceStoreItem{
			ResourceType:  item.ResourceType,
			ResourceValue: item.ResourceID,
		})
	}
	return &resourceStoreReq{
		ResourceItems: storeItems,
	}
}

// parseUploadedResource 解析 finish/commit 阶段的上传结果，并尽量提取真实远端 resource_id 与诊断字段。
func parseUploadedResource(body []byte, fallbackStoreURI string, fallbackUploadID string, uploadDomain string) *SingleUploadRes {
	// finish 阶段的返回在图片和音视频场景下并不统一。
	// 这里优先提取真实 resource_id/vid/file_id，其次才回落到 store_uri，避免继续把 store key 当成最终资源 ID。
	result := &SingleUploadRes{
		ResourceID:   strings.TrimSpace(fallbackStoreURI),
		StoreURI:     strings.TrimSpace(fallbackStoreURI),
		UploadID:     strings.TrimSpace(fallbackUploadID),
		UploadDomain: strings.TrimSpace(uploadDomain),
		Extra: map[string]any{
			"resource_id_source": "store_uri_fallback",
		},
	}
	if !json.Valid(body) {
		return result
	}
	payload := map[string]any{}
	if json.Unmarshal(body, &payload) != nil {
		return result
	}
	resourceID := recursiveStringValue(
		payload,
		"resource_id", "resourceId", "ResourceID",
		"vid", "Vid", "VID",
		"file_id", "fileId", "FileId", "FileID",
	)
	storeURI := recursiveStringValue(
		payload,
		"store_uri", "storeUri", "storeURI", "StoreUri", "StoreURI",
		"uri", "Uri",
		"source_uri", "sourceUri", "sourceURI", "SourceUri", "SourceURI",
	)
	uploadID := recursiveStringValue(payload, "uploadID", "UploadID", "uploadId", "UploadId", "uploadid", "upload_id")
	parsedUploadDomain := recursiveStringValue(payload, "upload_domain", "uploadDomain", "UploadDomain")
	if strings.TrimSpace(resourceID) != "" {
		result.ResourceID = strings.TrimSpace(resourceID)
	}
	if strings.TrimSpace(storeURI) != "" {
		result.StoreURI = strings.TrimSpace(storeURI)
	}
	if strings.TrimSpace(uploadID) != "" {
		result.UploadID = strings.TrimSpace(uploadID)
	}
	if strings.TrimSpace(parsedUploadDomain) != "" {
		result.UploadDomain = strings.TrimSpace(parsedUploadDomain)
	}
	result.Extra = mergeAnyMaps(
		result.Extra,
		payload,
		firstAnyMap(payload["data"], payload["Data"], payload["result"], payload["Result"], payload["payload"], payload["Payload"]),
	)
	if strings.TrimSpace(resourceID) != "" {
		result.Extra["resource_id_source"] = "remote"
	}
	if strings.TrimSpace(result.ResourceID) == "" {
		result.ResourceID = result.StoreURI
	}
	return result
}

// usesStoreURIFallbackResourceID 判断上传结果里的 resource_id 是否仍只是由 store_uri 兜底构造。
func usesStoreURIFallbackResourceID(upload *SingleUploadRes) bool {
	if upload == nil {
		return false
	}
	if strings.TrimSpace(upload.ResourceID) == "" || strings.TrimSpace(upload.StoreURI) == "" {
		return false
	}
	if strings.TrimSpace(upload.ResourceID) != strings.TrimSpace(upload.StoreURI) {
		return false
	}
	if upload.Extra == nil {
		return true
	}
	return strings.TrimSpace(fmt.Sprint(upload.Extra["resource_id_source"])) == "store_uri_fallback"
}

func extractUploadDurationSeconds(payload map[string]any) float64 {
	if len(payload) == 0 {
		return 0
	}
	if seconds := recursiveFloatValue(payload, "duration", "Duration", "duration_seconds", "durationSeconds", "DurationSeconds"); seconds > 0 {
		return seconds
	}
	for _, candidate := range []any{
		payload["meta"],
		payload["Meta"],
		payload["source_info"],
		payload["SourceInfo"],
		payload["video_info"],
		payload["VideoInfo"],
		payload["result"],
		payload["Result"],
		payload["data"],
		payload["Data"],
		payload["payload"],
		payload["Payload"],
	} {
		if seconds := extractUploadDurationFromAny(candidate); seconds > 0 {
			return seconds
		}
	}
	return 0
}

func extractUploadDurationFromAny(value any) float64 {
	switch current := value.(type) {
	case float64:
		if current > 0 {
			return current
		}
	case float32:
		if current > 0 {
			return float64(current)
		}
	case int:
		if current > 0 {
			return float64(current)
		}
	case int64:
		if current > 0 {
			return float64(current)
		}
	case json.Number:
		if number, err := current.Float64(); err == nil && number > 0 {
			return number
		}
	case string:
		if number, err := strconv.ParseFloat(strings.TrimSpace(current), 64); err == nil && number > 0 {
			return number
		}
	case map[string]any:
		if seconds := extractUploadDurationSeconds(current); seconds > 0 {
			return seconds
		}
	}
	return 0
}

func extractUploadMediaURL(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"media_url", "mediaUrl", "MediaURL",
		"play_url", "playUrl", "PlayURL",
		"video_url", "videoUrl", "VideoURL",
		"file_url", "fileUrl", "FileURL",
		"source_url", "sourceUrl", "SourceURL",
		"main_url", "mainUrl", "MainURL",
		"url", "Url", "URL",
	)
}

func extractUploadCoverURL(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"cover_url", "coverUrl", "CoverURL",
		"cover_uri", "coverUri", "CoverUri", "CoverURI",
		"poster_url", "posterUrl", "PosterURL",
		"poster_uri", "posterUri", "PosterUri", "PosterURI",
		"snapshot_url", "snapshotUrl", "SnapshotURL",
	)
}

func extractUploadSnapshotURI(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"snapshot_uri", "snapshotUri", "SnapshotUri",
		"snapshot_url", "snapshotUrl", "SnapshotURL",
	)
}

func extractUploadRequestID(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"request_id", "requestId", "RequestId", "RequestID",
	)
}

func extractUploadStatusCode(payload map[string]any) int {
	return recursiveIntValue(payload, "status_code", "statusCode", "StatusCode", "response_code", "responseCode")
}

func extractUploadErrorCode(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"error_code", "errorCode", "ErrorCode",
		"code", "Code", "ExtendedCode",
	)
}

func extractUploadErrorMessage(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"error_message", "errorMessage", "ErrorMessage",
		"message", "Message", "detail", "Detail",
	)
}

func extractUploadHostID(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"host_id", "hostId", "HostID", "HostId",
	)
}

func extractUploadEC(payload map[string]any) string {
	return recursiveStringValue(
		payload,
		"ec", "EC",
	)
}

func extractUploadDetailErrCode(payload map[string]any) int {
	return recursiveIntValue(
		payload,
		"detail_err_code", "detailErrCode", "DetailErrCode",
	)
}

func extractUploadResponseErr(payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	return normalizeUploadDiagnosticValue(
		recursiveAnyValue(payload, "response_err", "responseErr", "ResponseErr"),
	)
}

func extractUploadExpectedCodes(payload map[string]any) []string {
	if len(payload) == 0 {
		return nil
	}
	return normalizeUploadExpectedCodes(
		recursiveAnyValue(payload, "expected_codes", "expectedCodes", "ExpectedCodes"),
	)
}

func normalizeUploadDiagnosticValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case map[string]any, []any:
		body, err := json.Marshal(typed)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		return strings.TrimSpace(string(body))
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func normalizeUploadExpectedCodes(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := normalizeUploadDiagnosticValue(item)
			if text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		text := normalizeUploadDiagnosticValue(typed)
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func mergeAnyMaps(values ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, value := range values {
		for key, item := range value {
			out[key] = item
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeStoredResources 把 resource_store 最终结果和本地上传阶段元信息按最佳匹配规则合并。
func mergeStoredResources(stored []*Resource, original []*Resource) []*Resource {
	if len(stored) == 0 {
		return nil
	}
	out := make([]*Resource, 0, len(stored))
	usedOriginal := make([]bool, len(original))
	for index, item := range stored {
		if item == nil {
			continue
		}
		merged := *item
		if matched := findOriginalResourceForStored(item, original, usedOriginal); matched >= 0 {
			applyMissingResourceFields(&merged, original[matched])
			usedOriginal[matched] = true
		} else if index < len(original) && original[index] != nil && !usedOriginal[index] {
			applyMissingResourceFields(&merged, original[index])
			usedOriginal[index] = true
		}
		out = append(out, &merged)
	}
	return out
}

// countNonNilResources 统计资源列表中非空项数量，用于收紧上传结果数量校验。
func countNonNilResources(items []*Resource) int {
	count := 0
	for _, item := range items {
		if item != nil {
			count++
		}
	}
	return count
}

// findOriginalResourceForStored 为单个 stored 结果找到最匹配的原始上传项索引。
func findOriginalResourceForStored(stored *Resource, original []*Resource, used []bool) int {
	if stored == nil {
		return -1
	}
	for _, candidate := range []string{
		strings.TrimSpace(stored.ResourceID),
		strings.TrimSpace(stored.Path),
		strings.TrimSpace(stored.Name),
	} {
		if candidate == "" {
			continue
		}
		for index, item := range original {
			if index >= len(used) || used[index] || item == nil {
				continue
			}
			if candidate == strings.TrimSpace(item.ResourceID) {
				return index
			}
			if candidate == strings.TrimSpace(item.Path) {
				return index
			}
			if candidate == strings.TrimSpace(item.Name) {
				return index
			}
			if candidate == resourceUploadStoreURI(item) {
				return index
			}
		}
	}
	return -1
}

func resourceUploadStoreURI(item *Resource) string {
	if item == nil || len(item.UploadSummary) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(item.UploadSummary["store_uri"]))
}

// applyMissingResourceFields 用原始上传项补齐 stored 结果中仍缺失的本地字段。
func applyMissingResourceFields(dst *Resource, src *Resource) {
	if dst == nil || src == nil {
		return
	}
	if strings.TrimSpace(dst.ResourceID) == "" {
		dst.ResourceID = src.ResourceID
	}
	if strings.TrimSpace(dst.ResourceType) == "" {
		dst.ResourceType = src.ResourceType
	}
	if strings.TrimSpace(dst.Path) == "" {
		dst.Path = src.Path
	}
	if strings.TrimSpace(dst.Name) == "" {
		dst.Name = src.Name
	}
	if dst.Size == 0 {
		dst.Size = src.Size
	}
	if dst.Scene == 0 {
		dst.Scene = src.Scene
	}
	if strings.TrimSpace(dst.Kind) == "" {
		dst.Kind = src.Kind
	}
	if strings.TrimSpace(dst.MimeType) == "" {
		dst.MimeType = src.MimeType
	}
	if len(dst.UploadSummary) == 0 {
		dst.UploadSummary = src.UploadSummary
	}
}

func (d *uploadTokenData) forUpload(path string, index int) *uploadTokenData {
	if d == nil {
		return nil
	}
	copyValue := *d
	// 上传 token 往往是“整批资源”维度返回的。
	// 单文件上传前需要把 store_uri/store_info 收束到当前索引，否则后续 selectedStoreURI()
	// 会重新读回第一个 store_info，导致多文件上传串到错误的对象 key。
	if len(d.StoreInfos) > index && d.StoreInfos[index] != nil {
		selected := *d.StoreInfos[index]
		copyValue.StoreInfos = []*uploadStoreInfo{&selected}
		if domain := strings.TrimSpace(selected.UploadDomain); domain != "" {
			copyValue.UploadDomain = domain
		}
	} else {
		copyValue.StoreInfos = nil
	}
	copyValue.StoreKeys = nil
	if len(d.StoreKeys) > index && strings.TrimSpace(d.StoreKeys[index]) != "" {
		copyValue.StoreKeys = []string{strings.TrimSpace(d.StoreKeys[index])}
	}
	copyValue.StoreURI = d.selectedIndexedStoreURI(index, path)
	if copyValue.StoreURI != "" && len(copyValue.StoreKeys) == 0 {
		copyValue.StoreKeys = []string{copyValue.StoreURI}
	}
	return &copyValue
}

func (d *uploadTokenData) selectedStoreURI(path string) string {
	return d.selectedIndexedStoreURI(0, path)
}

func (d *uploadTokenData) selectedIndexedStoreURI(index int, path string) string {
	if d == nil {
		return ""
	}
	if len(d.StoreInfos) > index && d.StoreInfos[index] != nil {
		if uri := firstNonEmpty(strings.TrimSpace(d.StoreInfos[index].StoreURI), strings.TrimSpace(d.StoreInfos[index].StoreKey)); uri != "" {
			return uri
		}
	}
	if len(d.StoreKeys) > index && strings.TrimSpace(d.StoreKeys[index]) != "" {
		return strings.TrimSpace(d.StoreKeys[index])
	}
	if strings.TrimSpace(d.StoreURI) != "" {
		return strings.TrimSpace(d.StoreURI)
	}
	base := strings.TrimSpace(filepath.Base(path))
	if base == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join("dreamina", base))
}

func normalizeImageXRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "", "cn", "cn-north-1":
		return defaultDreaminaImageXRegion
	default:
		return strings.TrimSpace(region)
	}
}

// normalizeTOSRegion 归一化 TOS credential scope 使用的 region。
// 原程序视频 token 里出现过 region=cn，但 TOS 侧 scope 需要具体地域名。
func normalizeTOSRegion(region string, host string) string {
	normalized := strings.ToLower(strings.TrimSpace(region))
	if strings.HasPrefix(normalized, "tos-") {
		normalized = strings.TrimPrefix(normalized, "tos-")
	}
	if normalized != "" {
		return normalized
	}
	lowerHost := strings.ToLower(strings.TrimSpace(host))
	switch {
	case strings.Contains(lowerHost, "beijing2"):
		return "cn-beijing2"
	case strings.Contains(lowerHost, "beijing"):
		return "cn-beijing"
	case strings.Contains(lowerHost, "shanghai"):
		return "cn-shanghai"
	case strings.Contains(lowerHost, "singapore"):
		return "ap-singapore-1"
	}
	return "cn-beijing"
}

func normalizeImageXHost(host string, region string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.Trim(host, "/")
	if host != "" {
		return host
	}
	switch normalizeImageXRegion(region) {
	case defaultDreaminaImageXRegion:
		return defaultDreaminaImageXHost
	case "ap-singapore-1":
		return "imagex-ap-singapore-1.volcengineapi.com"
	case "us-east-1":
		return "imagex-us-east-1.volcengineapi.com"
	}
	return ""
}

func inferImageXServiceIDFromStoreURI(storeURI string) string {
	storeURI = strings.TrimSpace(storeURI)
	if storeURI == "" || !strings.HasPrefix(storeURI, "tos-cn-i-") {
		return ""
	}
	storeURI = strings.TrimPrefix(storeURI, "tos-cn-i-")
	if storeURI == "" {
		return ""
	}
	if slash := strings.Index(storeURI, "/"); slash > 0 {
		return strings.TrimSpace(storeURI[:slash])
	}
	return ""
}

func normalizeHeaderValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	}
	return normalized
}

func awsURLEscape(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func buildUploadPhaseURL(token *uploadTokenData, storeURI string, phase string, uploadID string) (string, error) {
	if token == nil {
		return "", fmt.Errorf("upload token is required")
	}
	host := strings.TrimSpace(token.UploadDomain)
	if host == "" {
		return "", fmt.Errorf("upload token missing upload_domain")
	}
	storeURI = strings.TrimLeft(strings.TrimSpace(storeURI), "/")
	if storeURI == "" {
		return "", fmt.Errorf("upload token missing store_uri")
	}

	if shouldSignTOSPhaseRequest(token) {
		_, objectKey := resolveTOSUploadLocation(token, host, storeURI)
		if objectKey != "" {
			storeURI = objectKey
		}
	}

	base := host
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + strings.TrimLeft(base, "/")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	target := base + storeURI
	parsed, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("phase", phase)
	if uploadID != "" {
		query.Set("uploadid", uploadID)
	}
	if phase == "transfer" {
		query.Set("part_number", "1")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func resolveTOSUploadLocation(token *uploadTokenData, host string, storeURI string) (string, string) {
	host = strings.TrimSpace(host)
	storeURI = strings.TrimLeft(strings.TrimSpace(storeURI), "/")
	if host == "" || storeURI == "" {
		return "", ""
	}

	bucket := strings.TrimSpace(token.SpaceName)
	if bucket == "" {
		bucket = strings.TrimSpace(token.Bucket)
	}
	if bucket == "" {
		if prefix, rest, found := strings.Cut(storeURI, "/"); found {
			bucket = strings.TrimSpace(prefix)
			storeURI = strings.TrimLeft(strings.TrimSpace(rest), "/")
		}
	} else if strings.HasPrefix(storeURI, bucket+"/") {
		storeURI = strings.TrimLeft(strings.TrimPrefix(storeURI, bucket+"/"), "/")
	}
	if bucket == "" || storeURI == "" {
		return "", ""
	}

	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.Trim(host, "/")
	if !strings.HasPrefix(strings.ToLower(host), strings.ToLower(bucket)+".") {
		host = bucket + "." + host
	}
	return host, storeURI
}

func resolveTOSRequestHostAndPath(token *uploadTokenData, host string, path string) (string, string) {
	requestHost := strings.TrimSpace(host)
	requestPath := strings.TrimSpace(path)
	if token == nil {
		return requestHost, requestPath
	}
	storeURI := strings.TrimLeft(strings.TrimSpace(path), "/")
	bucketHost, objectKey := resolveTOSUploadLocation(token, host, storeURI)
	if bucketHost != "" {
		requestHost = bucketHost
	}
	if objectKey != "" {
		requestPath = "/" + strings.TrimLeft(objectKey, "/")
	}
	if requestPath == "" {
		requestPath = "/"
	}
	return requestHost, requestPath
}

func phaseHeaders(token *uploadTokenData, contentType string) map[string]string {
	headers := map[string]string{}
	for key, value := range decodeHeaderPayload(token.TosHeaders) {
		headers[key] = value
	}
	for key, value := range decodeMetaHeaderPayload(token.TosMeta) {
		headers[key] = value
	}
	if strings.TrimSpace(contentType) != "" {
		headers["Content-Type"] = contentType
	}
	return headers
}

func decodeHeaderPayload(payload string) map[string]string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil
	}
	var value any
	if json.Unmarshal([]byte(payload), &value) != nil {
		return nil
	}
	return flattenHeaderPayload(value, 0)
}

func flattenHeaderPayload(value any, depth int) map[string]string {
	if depth > 4 {
		return nil
	}
	switch typed := value.(type) {
	case map[string]string:
		out := map[string]string{}
		for key, item := range typed {
			key = strings.TrimSpace(key)
			text := strings.TrimSpace(item)
			if key != "" && text != "" {
				out[key] = text
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]any:
		out := map[string]string{}
		for key, item := range typed {
			key = strings.TrimSpace(key)
			text := strings.TrimSpace(fmt.Sprint(item))
			if key != "" && text != "" && text != "<nil>" && !looksLikeNestedHeaderPayload(item) {
				out[key] = text
			}
		}
		if len(out) > 0 {
			return out
		}
		for _, key := range []string{"headers", "Headers", "meta", "Meta", "data", "Data", "payload", "Payload"} {
			if nested := flattenHeaderPayload(typed[key], depth+1); len(nested) > 0 {
				return nested
			}
		}
	}
	return nil
}

func looksLikeNestedHeaderPayload(value any) bool {
	switch value.(type) {
	case map[string]any, map[string]string:
		return true
	default:
		return false
	}
}

func decodeMetaHeaderPayload(payload string) map[string]string {
	raw := decodeHeaderPayload(payload)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		// tos_meta 返回的是业务键值，需要在上传阶段转换成 x-tos-meta-* 头。
		// 如果服务端已经给了完整前缀，这里保持原样，避免重复拼接。
		normalized := strings.TrimSpace(key)
		if normalized == "" {
			continue
		}
		lower := strings.ToLower(normalized)
		if !strings.HasPrefix(lower, "x-tos-meta-") {
			normalized = "x-tos-meta-" + normalized
		}
		out[normalized] = value
	}
	return out
}

func parseUploadID(body []byte) string {
	if !json.Valid(body) {
		return ""
	}
	payload := map[string]any{}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	// init/finish 返回里出现过多种大小写组合，统一递归提取，避免大小写差异影响 phase 上传。
	return recursiveStringValue(payload, "uploadID", "UploadID", "uploadId", "UploadId", "uploadid", "upload_id")
}

func parseFinishedStoreURI(body []byte) string {
	if !json.Valid(body) {
		return ""
	}
	payload := map[string]any{}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	// finish 返回在不同 wrapper 下会混用 uri/store_uri/resource_id 字段，这里只负责取回最终落库键。
	return recursiveStringValue(
		payload,
		"store_uri", "storeUri", "storeURI", "StoreUri", "StoreURI",
		"uri", "Uri",
		"resource_id", "resourceId", "ResourceID",
	)
}

func uploadBackendSucceeded(ret string, code string) bool {
	for _, value := range []string{ret, code} {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "", "0", "success", "succeed", "ok", "200":
			return true
		}
	}
	return false
}

func sessionHeaders(session any) map[string]string {
	switch value := session.(type) {
	case *mcpclient.Session:
		headers := map[string]string{}
		for key, item := range value.Headers {
			key = strings.TrimSpace(key)
			item = strings.TrimSpace(item)
			if key != "" && item != "" && item != "<nil>" {
				headers[key] = item
			}
		}
		if cookie := strings.TrimSpace(value.Cookie); cookie != "" {
			headers["Cookie"] = cookie
		}
		if userID := strings.TrimSpace(value.UserID); userID != "" {
			headers["X-User-Id"] = userID
		}
		return headers
	case map[string]any:
		headers := map[string]string{}
		if cookie := strings.TrimSpace(fmt.Sprint(value["cookie"])); cookie != "" && cookie != "<nil>" {
			headers["Cookie"] = cookie
		}
		if rawHeaders, ok := value["headers"].(map[string]any); ok {
			for key, item := range rawHeaders {
				key = strings.TrimSpace(key)
				text := strings.TrimSpace(fmt.Sprint(item))
				if key != "" && text != "" && text != "<nil>" {
					headers[key] = text
				}
			}
		}
		return headers
	default:
		return map[string]string{}
	}
}

func buildResourceBackendLogID(path string, ts int64) string {
	if ts == 0 {
		ts = time.Now().Unix()
	}
	sum := md5.Sum([]byte(strings.TrimSpace(path) + "|" + fmt.Sprintf("%d", ts)))
	return fmt.Sprintf("%x", sum)
}

func firstStringValue(root map[string]any, keys ...string) string {
	if len(root) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := root[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func firstMetadataStringValue(root any, keys ...string) string {
	// 上传 token/resource_store 的 ret/code/msg/message 常被 Meta/Error/Response 包一层，
	// 这里限制在元数据 wrapper 内递归查找，避免把资源条目里的普通字段误吸成响应元数据。
	var visit func(any, int) string
	visit = func(value any, depth int) string {
		if depth > 6 {
			return ""
		}
		current, ok := value.(map[string]any)
		if !ok || len(current) == 0 {
			return ""
		}
		for _, key := range keys {
			if item, ok := current[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(item))
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
			if text := visit(current[wrapper], depth+1); text != "" {
				return text
			}
		}
		return ""
	}
	return visit(root, 0)
}

func firstAnyMap(values ...any) map[string]any {
	for _, value := range values {
		if root, ok := value.(map[string]any); ok && len(root) > 0 {
			return root
		}
	}
	return nil
}

func firstStringSlice(values ...any) []string {
	for _, value := range values {
		switch typed := value.(type) {
		case []string:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				item = strings.TrimSpace(item)
				if item != "" {
					out = append(out, item)
				}
			}
			if len(out) > 0 {
				return out
			}
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" && text != "<nil>" {
					out = append(out, text)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func firstScalarString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			text := strings.TrimSpace(typed)
			if text != "" {
				return text
			}
		case json.Number:
			text := strings.TrimSpace(typed.String())
			if text != "" {
				return text
			}
		case fmt.Stringer:
			text := strings.TrimSpace(typed.String())
			if text != "" {
				return text
			}
		case int, int8, int16, int32, int64, float32, float64, bool:
			text := strings.TrimSpace(fmt.Sprint(typed))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func parseUploadStoreInfos(values ...any) []*uploadStoreInfo {
	for _, value := range values {
		parsed := parseUploadStoreInfoValue(value)
		if len(parsed) > 0 {
			return parsed
		}
	}
	return nil
}

func parseUploadStoreInfoValue(value any) []*uploadStoreInfo {
	switch typed := value.(type) {
	case []any:
		out := make([]*uploadStoreInfo, 0, len(typed))
		for _, item := range typed {
			info := parseSingleUploadStoreInfo(item)
			if info != nil {
				out = append(out, info)
			}
		}
		if len(out) > 0 {
			return out
		}
	case map[string]any:
		// upload token 的 store_infos 既可能是标准数组，也可能是 keyed object，
		// 甚至再套一层 Data/Payload。这里递归回收所有可识别的 store info。
		if nested := parseUploadStoreInfos(
			typed["store_infos"], typed["StoreInfos"], typed["storeInfos"],
			typed["store_info"], typed["StoreInfo"], typed["storeInfo"],
			typed["list"], typed["List"], typed["items"], typed["Items"],
		); len(nested) > 0 {
			return nested
		}
		if info := parseSingleUploadStoreInfo(typed); info != nil {
			return []*uploadStoreInfo{info}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]*uploadStoreInfo, 0, len(typed))
		seen := map[string]struct{}{}
		for _, key := range keys {
			child := typed[key]
			for _, info := range parseUploadStoreInfoValue(child) {
				if info == nil {
					continue
				}
				key := firstNonEmpty(strings.TrimSpace(info.StoreURI), strings.TrimSpace(info.StoreKey))
				if key == "" {
					key = strings.TrimSpace(info.UploadDomain)
				}
				if key != "" {
					if _, exists := seen[key]; exists {
						continue
					}
					seen[key] = struct{}{}
				}
				out = append(out, info)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func parseSingleUploadStoreInfo(value any) *uploadStoreInfo {
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	info := &uploadStoreInfo{
		StoreURI:     firstStringValue(root, "store_uri", "storeUri", "StoreURI", "uri", "Uri"),
		StoreKey:     firstStringValue(root, "store_key", "storeKey", "StoreKey", "key", "Key"),
		UploadDomain: firstStringValue(root, "upload_domain", "uploadDomain", "UploadDomain"),
	}
	// 只有 upload_domain 而没有 store_uri/store_key 时，还不足以构成可上传目标，避免把 wrapper 误判成 store info。
	if strings.TrimSpace(info.StoreURI) == "" && strings.TrimSpace(info.StoreKey) == "" {
		return nil
	}
	return info
}

func firstIntValue(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			if typed != 0 {
				return typed
			}
		case int64:
			if typed != 0 {
				return int(typed)
			}
		case float64:
			if typed != 0 {
				return int(typed)
			}
		case json.Number:
			if parsed, err := typed.Int64(); err == nil && parsed != 0 {
				return int(parsed)
			}
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil && parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}

func firstInt64Value(values ...any) int64 {
	for _, value := range values {
		switch typed := value.(type) {
		case int64:
			if typed != 0 {
				return typed
			}
		case int:
			if typed != 0 {
				return int64(typed)
			}
		case float64:
			if typed != 0 {
				return int64(typed)
			}
		case json.Number:
			if parsed, err := typed.Int64(); err == nil && parsed != 0 {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil && parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}

func firstBoolValue(values ...any) bool {
	for _, value := range values {
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "1", "true", "yes":
				return true
			}
		}
	}
	return false
}

func recursiveStringValue(value any, keys ...string) string {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return findRecursiveStringValue(value, lookup)
}

func recursiveFloatValue(value any, keys ...string) float64 {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return findRecursiveFloatValue(value, lookup)
}

func recursiveIntValue(value any, keys ...string) int {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return findRecursiveIntValue(value, lookup)
}

func recursiveAnyValue(value any, keys ...string) any {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return findRecursiveAnyValue(value, lookup)
}

func findRecursiveStringValue(value any, lookup map[string]struct{}) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok {
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
		for _, item := range typed {
			if text := findRecursiveStringValue(item, lookup); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range typed {
			if text := findRecursiveStringValue(item, lookup); text != "" {
				return text
			}
		}
	}
	return ""
}

func findRecursiveFloatValue(value any, lookup map[string]struct{}) float64 {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok {
				if number := extractUploadDurationFromAny(item); number > 0 {
					return number
				}
			}
		}
		for _, item := range typed {
			if number := findRecursiveFloatValue(item, lookup); number > 0 {
				return number
			}
		}
	case []any:
		for _, item := range typed {
			if number := findRecursiveFloatValue(item, lookup); number > 0 {
				return number
			}
		}
	}
	return 0
}

func findRecursiveIntValue(value any, lookup map[string]struct{}) int {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok {
				if number := firstIntValue(item); number != 0 {
					return number
				}
			}
		}
		for _, item := range typed {
			if number := findRecursiveIntValue(item, lookup); number != 0 {
				return number
			}
		}
	case []any:
		for _, item := range typed {
			if number := findRecursiveIntValue(item, lookup); number != 0 {
				return number
			}
		}
	}
	return 0
}

func findRecursiveAnyValue(value any, lookup map[string]struct{}) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok && item != nil {
				switch current := item.(type) {
				case string:
					if strings.TrimSpace(current) != "" {
						return current
					}
				default:
					return current
				}
			}
		}
		for _, item := range typed {
			if current := findRecursiveAnyValue(item, lookup); current != nil {
				return current
			}
		}
	case []any:
		for _, item := range typed {
			if current := findRecursiveAnyValue(item, lookup); current != nil {
				return current
			}
		}
	}
	return nil
}

func sliceOfAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

func resourceSliceOfAny(value any) []any {
	if items := sliceOfAny(value); len(items) > 0 {
		return items
	}
	root, ok := value.(map[string]any)
	if !ok || len(root) == 0 {
		return nil
	}
	if parseStoredResource(root) != nil {
		return []any{root}
	}
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(root))
	for _, key := range keys {
		child := root[key]
		if item, ok := child.(map[string]any); ok && len(item) > 0 {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func uploadTokenRootLooksUsable(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	return firstNonEmpty(
		recursiveStringValue(root, "upload_auth", "uploadAuth", "UploadAuth", "upload_token", "uploadToken", "UploadToken"),
		recursiveStringValue(root, "store_uri", "storeUri", "StoreURI", "uri", "Uri"),
		recursiveStringValue(root, "upload_domain", "uploadDomain", "UploadDomain"),
	) != ""
}

func resourceStoreRootLooksUsable(root map[string]any) bool {
	if len(root) == 0 {
		return false
	}
	return parseStoredResource(firstNonNil(root["resource"], root["Resource"])) != nil || len(parseStoredResources(root)) > 0
}
