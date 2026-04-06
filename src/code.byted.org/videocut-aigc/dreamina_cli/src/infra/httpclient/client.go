package httpclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type Request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Body    []byte            `json:"-"`
}

type Response struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       []byte            `json:"-"`
	Request    *Request          `json:"request,omitempty"`
}

// New 创建 HTTP 客户端；如果未显式指定 baseURL，就从配置里的登录域名推导。
func New(v ...any) (*Client, error) {
	baseURL := ""
	for _, arg := range v {
		switch value := arg.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				baseURL = strings.TrimSpace(value)
			}
		}
	}
	if baseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		baseURL = loginOriginFromURL(cfg.LoginURL)
	}
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// NewRequest 构造内部 Request，对 body、headers 和 query 做基础归一化。
func (c *Client) NewRequest(ctx context.Context, method string, path string, body any, args ...any) (*Request, error) {
	_ = ctx
	req := &Request{
		Method:  strings.ToUpper(strings.TrimSpace(method)),
		Path:    strings.TrimSpace(path),
		Headers: map[string]string{},
		Query:   map[string]string{},
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if req.Path == "" {
		return nil, fmt.Errorf("request.path is required")
	}
	switch value := body.(type) {
	case nil:
	case []byte:
		req.Body = append([]byte(nil), value...)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		req.Body = encoded
	}
	for _, arg := range args {
		switch value := arg.(type) {
		case map[string]string:
			for k, v := range value {
				if strings.TrimSpace(k) != "" {
					req.Headers[k] = strings.TrimSpace(v)
				}
			}
		case map[string]any:
			for k, v := range value {
				if strings.TrimSpace(k) != "" {
					req.Query[k] = strings.TrimSpace(fmt.Sprint(v))
				}
			}
		}
	}
	return req, nil
}

// ApplyBackendHeaders 为后端请求补原始二进制已确认的公共后端头。
func (c *Client) ApplyBackendHeaders(req any) {
	request, ok := req.(*Request)
	if !ok || request == nil {
		return
	}
	if request.Headers == nil {
		request.Headers = map[string]string{}
	}
	if request.Headers["X-Use-Ppe"] == "" {
		request.Headers["X-Use-Ppe"] = "1"
	}
}

// Do 发送一次 HTTP 请求；在强制 fallback 模式下直接返回本地模拟响应。
func (c *Client) Do(ctx context.Context, req any) (any, error) {
	request, ok := req.(*Request)
	if !ok || request == nil {
		return nil, fmt.Errorf("request is required")
	}
	if shouldForceFallback() {
		return fallbackResponse(request), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stdReq, err := c.buildHTTPRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(stdReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
		Body:       body,
		Request:    request,
	}, nil
}

// ReadResponseBody 读取并复制 Response 中保存的原始响应体。
func ReadResponseBody(v any) ([]byte, error) {
	resp, ok := v.(*Response)
	if !ok || resp == nil {
		return nil, fmt.Errorf("response is required")
	}
	return append([]byte(nil), resp.Body...), nil
}

// ReadDecodedResponseBody 按 Content-Encoding 解码响应体，目前支持 identity 和 gzip。
func ReadDecodedResponseBody(v any) ([]byte, string, error) {
	resp, ok := v.(*Response)
	if !ok || resp == nil {
		return nil, "", fmt.Errorf("response is required")
	}
	body, err := ReadResponseBody(resp)
	if err != nil {
		return nil, "", err
	}
	encoding := strings.ToLower(strings.TrimSpace(resp.Headers["Content-Encoding"]))
	switch encoding {
	case "", "identity":
		return body, "identity", nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body, encoding, nil
		}
		defer reader.Close()
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return body, encoding, nil
		}
		return decoded, encoding, nil
	default:
		return body, encoding, nil
	}
}

// decodeBodyOrString 尝试把响应体解析成 JSON，否则回退为字符串。
func decodeBodyOrString(body []byte) any {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var payload any
	if json.Valid(body) && json.Unmarshal(body, &payload) == nil {
		return payload
	}
	return trimmed
}

// buildHTTPRequest 把内部 Request 转换成标准库 http.Request。
func (c *Client) buildHTTPRequest(ctx context.Context, request *Request) (*http.Request, error) {
	if c == nil {
		c = &Client{}
	}
	target, err := c.resolveURL(request.Path, request.Query)
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if len(request.Body) > 0 {
		bodyReader = strings.NewReader(string(request.Body))
	}
	stdReq, err := http.NewRequestWithContext(ctx, request.Method, target, bodyReader)
	if err != nil {
		return nil, err
	}
	for key, value := range request.Headers {
		if strings.EqualFold(strings.TrimSpace(key), "Host") {
			stdReq.Host = strings.TrimSpace(value)
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "Accept-Encoding") {
			continue
		}
		stdReq.Header.Set(key, value)
	}
	return stdReq, nil
}

// resolveURL 把相对路径和查询参数解析成最终请求 URL。
func (c *Client) resolveURL(path string, query map[string]string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("request.path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		return applyQuery(u, query).String(), nil
	}
	base := strings.TrimSpace(c.baseURL)
	if base == "" {
		base = "https://jimeng.jianying.com"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u.Path = path
	return applyQuery(u, query).String(), nil
}

// applyQuery 把查询参数写入 URL。
func applyQuery(u *url.URL, query map[string]string) *url.URL {
	if u == nil {
		return &url.URL{}
	}
	if len(query) == 0 {
		return u
	}
	q := u.Query()
	for key, value := range query {
		if strings.TrimSpace(key) == "" {
			continue
		}
		q.Set(key, strings.TrimSpace(value))
	}
	u.RawQuery = q.Encode()
	return u
}

// flattenHeaders 把 http.Header 压平成 map[string]string，便于落盘和诊断输出。
func flattenHeaders(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) == 0 {
			continue
		}
		out[key] = strings.Join(values, ", ")
	}
	return out
}

// fallbackResponse 生成本地回退模式使用的模拟 HTTP 响应。
func fallbackResponse(request *Request) *Response {
	payload := map[string]any{
		"code":    "0",
		"message": "ok",
		"data": map[string]any{
			"path":           request.Path,
			"method":         request.Method,
			"query":          request.Query,
			"headers":        request.Headers,
			"body":           decodeBodyOrString(request.Body),
			"transport_mode": "fallback",
		},
	}
	body, _ := json.Marshal(payload)
	return &Response{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body:    body,
		Request: request,
	}
}

// loginOriginFromURL 从完整登录 URL 中提取 scheme://host 形式的基准域名。
func loginOriginFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "https://jimeng.jianying.com"
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

// shouldForceFallback 根据环境变量判断是否强制使用本地 HTTP fallback。
func shouldForceFallback() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("DREAMINA_HTTP_FAKE")))
	return value == "1" || value == "true" || value == "yes"
}
