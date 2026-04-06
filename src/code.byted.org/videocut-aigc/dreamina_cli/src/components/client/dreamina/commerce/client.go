package commerce

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

type HTTPClient struct {
	http *httpclient.Client
}

const (
	commercePFDefault         = "7"
	commerceAppVersionDefault = "8.4.0"
	commerceAppSDKVersion     = "48.0.0"
	commerceAcceptLanguage    = "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7"
	commerceSecCHUA           = `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`
	commerceSecCHUAPlatform   = `"macOS"`
	commerceSignPrefix        = "9e2c"
	commerceSignSuffix        = "11ac"
)

type UserCredit struct {
	CreditCount    int    `json:"credit_count"`
	BenefitType    string `json:"benefit_type"`
	VIPCredit      int    `json:"vip_credit"`
	GiftCredit     int    `json:"gift_credit"`
	PurchaseCredit int    `json:"purchase_credit"`
	TotalCredit    int    `json:"total_credit"`
}

type UserInfo struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	WorkspaceID string `json:"workspace_id"`
}

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

func (c *HTTPClient) GetUserCredit(ctx context.Context, session any) (*UserCredit, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	params := map[string]any{"aid": 513695}
	resp := map[string]any{}
	if err := c.callAPI(ctx, session, "GetUserCredit", "/commerce/v1/benefits/user_credit", params, &resp); err != nil {
		return nil, err
	}
	creditWrapper, _ := firstNestedMapValue(resp, "credit", "Credit", "balance", "Balance")
	summaryWrapper, _ := firstNestedMapValue(resp, "summary", "Summary")
	benefitWrapper, _ := firstNestedMapValue(resp, "benefit", "Benefit", "plan", "Plan")
	credit := &UserCredit{
		CreditCount:    firstIntValue(resp, "credit_count", "creditCount", "CreditCount", "available_credit", "availableCredit", "AvailableCredit"),
		VIPCredit:      firstIntValue(resp, "vip_credit", "vipCredit", "VipCredit", "VIPCredit"),
		GiftCredit:     firstIntValue(resp, "gift_credit", "giftCredit", "GiftCredit"),
		PurchaseCredit: firstIntValue(resp, "purchase_credit", "purchaseCredit", "PurchaseCredit"),
		TotalCredit:    firstIntValue(resp, "total_credit", "totalCredit", "TotalCredit", "total_available_credit", "totalAvailableCredit", "TotalAvailableCredit"),
	}
	if credit.CreditCount == 0 && len(creditWrapper) != 0 {
		credit.CreditCount = firstIntValue(creditWrapper, "credit_count", "creditCount", "CreditCount", "available_credit", "availableCredit", "AvailableCredit")
	}
	if credit.VIPCredit == 0 && len(creditWrapper) != 0 {
		credit.VIPCredit = firstIntValue(creditWrapper, "vip_credit", "vipCredit", "VipCredit", "VIPCredit")
	}
	if credit.GiftCredit == 0 && len(creditWrapper) != 0 {
		credit.GiftCredit = firstIntValue(creditWrapper, "gift_credit", "giftCredit", "GiftCredit")
	}
	if credit.PurchaseCredit == 0 && len(creditWrapper) != 0 {
		credit.PurchaseCredit = firstIntValue(creditWrapper, "purchase_credit", "purchaseCredit", "PurchaseCredit")
	}
	if credit.TotalCredit == 0 {
		switch {
		case len(summaryWrapper) != 0:
			credit.TotalCredit = firstIntValue(summaryWrapper, "total_credit", "totalCredit", "TotalCredit", "total_available_credit", "totalAvailableCredit", "TotalAvailableCredit")
		case len(creditWrapper) != 0:
			credit.TotalCredit = firstIntValue(creditWrapper, "total_credit", "totalCredit", "TotalCredit", "total_available_credit", "totalAvailableCredit", "TotalAvailableCredit")
		}
	}
	if credit.TotalCredit == 0 {
		credit.TotalCredit = credit.CreditCount + credit.VIPCredit + credit.GiftCredit + credit.PurchaseCredit
	}
	benefitType := strings.TrimSpace(firstStringValue(resp, "benefit_type", "benefitType", "BenefitType", "benefit_name", "BenefitName"))
	if benefitType == "" && len(benefitWrapper) != 0 {
		benefitType = strings.TrimSpace(firstStringValue(benefitWrapper, "benefit_type", "benefitType", "BenefitType", "benefit_name", "BenefitName", "name", "Name", "plan_name", "planName", "PlanName"))
	}
	if benefitType == "" && len(summaryWrapper) != 0 {
		benefitType = strings.TrimSpace(firstStringValue(summaryWrapper, "benefit_type", "benefitType", "BenefitType", "benefit_name", "BenefitName"))
	}
	if benefitType == "" {
		if vipCredits, ok := firstNestedSliceValue(resp, "vip_credits", "vipCredits", "VipCredits"); ok {
			benefitType = strings.TrimSpace(firstStringValueFromSliceMaps(vipCredits, "vip_level", "vipLevel", "VipLevel", "benefit_type", "benefitType", "BenefitType", "benefit_name", "BenefitName", "name", "Name"))
		}
	}
	credit.BenefitType = benefitType
	return credit, nil
}

func (c *HTTPClient) GetUserInfo(ctx context.Context, session any) (*UserInfo, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	params := map[string]any{
		"aid":              513695,
		"platform_app_id":  14942,
		"subscription_env": "prod",
	}
	resp := map[string]any{}
	if err := c.callAPI(ctx, session, "GetUserInfo", "/commerce/v1/subscription/user_info", params, &resp); err != nil {
		return nil, err
	}
	userID, _ := sessionUserID(session)
	if parsedUserID := strings.TrimSpace(firstStringValue(resp, "user_id", "userId", "UserId", "UserID", "uid", "UID")); parsedUserID != "" {
		userID = parsedUserID
	}
	displayName := strings.TrimSpace(firstStringValue(resp, "display_name", "displayName", "DisplayName", "nickname", "nick_name", "screen_name", "name"))
	if wrapperUser, ok := firstNestedMapValue(resp, "user", "User", "member", "Member", "profile", "Profile", "account", "Account"); ok {
		if userID == "" {
			if parsedUserID := strings.TrimSpace(firstStringValue(wrapperUser, "user_id", "userId", "UserId", "UserID", "uid", "UID", "id", "ID")); parsedUserID != "" {
				userID = parsedUserID
			}
		}
		if displayName == "" {
			if parsedName := strings.TrimSpace(firstStringValue(wrapperUser, "display_name", "displayName", "DisplayName", "nickname", "nick_name", "screen_name", "name", "Name")); parsedName != "" {
				displayName = parsedName
			}
		}
	}
	if sessionName := sessionStringByKeys(session, "display_name", "nickname", "nick_name", "screen_name", "name"); sessionName != "" {
		if displayName == "" {
			displayName = sessionName
		}
	}
	workspaceID := strings.TrimSpace(firstStringValue(resp,
		"workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID",
		"team_id", "teamId", "TeamId", "TeamID",
		"space_id", "spaceId", "SpaceId", "SpaceID",
	))
	if wrapperWorkspace, ok := firstNestedMapValue(resp, "workspace", "Workspace", "space", "Space", "team", "Team"); ok {
		if workspaceID == "" {
			if parsedWorkspace := strings.TrimSpace(firstStringValue(wrapperWorkspace,
				"workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID",
				"space_id", "spaceId", "SpaceId", "SpaceID",
				"team_id", "teamId", "TeamId", "TeamID",
				"id", "ID",
			)); parsedWorkspace != "" {
				workspaceID = parsedWorkspace
			}
		}
	}
	if sessionWorkspace := sessionStringByKeys(session,
		"workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID",
		"team_id", "teamId", "TeamId", "TeamID",
		"space_id", "spaceId", "SpaceId", "SpaceID",
	); sessionWorkspace != "" {
		if workspaceID == "" {
			workspaceID = sessionWorkspace
		}
	}
	return &UserInfo{
		UserID:      userID,
		DisplayName: displayName,
		WorkspaceID: workspaceID,
	}, nil
}

func (c *HTTPClient) callAPI(ctx context.Context, session any, op string, path string, params any, out any) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	reqHeaders := collectCommerceHeaders(session)
	if err := applyCommerceSignHeaders(reqHeaders, path, commerceAppVersionDefault, time.Now().Unix()); err != nil {
		return err
	}
	// 原始 commerce 接口虽然没有业务字段，但仍会发送 JSON body；
	// 真实服务端对空 body 会返回 bad request，而对 {} 会正常返回。
	req, err := c.http.NewRequest(ctx, "POST", path, map[string]any{}, reqHeaders, params)
	if err != nil {
		return err
	}
	c.http.ApplyBackendHeaders(req)
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		return err
	}
	resp, ok := respAny.(*httpclient.Response)
	if !ok || resp == nil {
		return fmt.Errorf("%s: invalid response", op)
	}
	rawBody, err := httpclient.ReadResponseBody(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: status=%d body=%s", op, resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}
	if err := validateCommerceBusinessResponse(op, rawBody); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rawBody, out)
}

func shouldSkipCommerceHeader(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "", "host", "accept", "cookie", "content-type", "content-length", "connection", "accept-encoding":
		return true
	default:
		return false
	}
}

func applyCommerceSignHeaders(v ...any) error {
	var (
		headers    map[string]string
		path       string
		appVersion string
		ts         int64
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case map[string]string:
			headers = value
		case string:
			if path == "" {
				path = value
			} else if appVersion == "" {
				appVersion = value
			}
		case int64:
			ts = value
		}
	}
	if headers == nil {
		return fmt.Errorf("headers are required")
	}
	if strings.TrimSpace(appVersion) == "" {
		appVersion = commerceAppVersionDefault
	}
	if ts == 0 {
		ts = time.Now().Unix()
	}
	pf := strings.TrimSpace(firstNonEmptyHeader(headers, "pf", "PF"))
	if pf == "" {
		pf = commercePFDefault
	}
	setCommerceHeader(headers, "Appid", "513695")
	setCommerceHeader(headers, "Pf", pf)
	setCommerceHeader(headers, "Appvr", appVersion)
	setCommerceHeader(headers, "App-Sdk-Version", commerceAppSDKVersion)
	setCommerceHeader(headers, "X-Client-Scheme", "https")
	setCommerceHeader(headers, "Device-Time", fmt.Sprintf("%d", ts))
	setCommerceHeader(headers, "Content-Type", "application/json")
	setCommerceHeader(headers, "Accept", "application/json")
	setCommerceHeader(headers, "Accept-Language", commerceAcceptLanguage)
	setCommerceHeader(headers, "Sec-Fetch-Mode", "cors")
	setCommerceHeader(headers, "Sec-Fetch-Site", "same-origin")
	setCommerceHeader(headers, "Sec-CH-UA-Mobile", "?0")
	setCommerceHeader(headers, "Sec-CH-UA-Platform", commerceSecCHUAPlatform)
	setCommerceHeader(headers, "Sec-CH-UA", commerceSecCHUA)
	if firstNonEmptyHeader(headers, "X-Tt-Logid") == "" {
		setCommerceHeader(headers, "X-Tt-Logid", buildCommerceLogID(path, ts))
	}
	sign, err := buildCommerceSign(path, pf, appVersion, ts, "")
	if err != nil {
		return err
	}
	setCommerceHeader(headers, "Sign", sign)
	setCommerceHeader(headers, "Sign-Ver", "1")
	return nil
}

func buildCommerceLogID(path string, ts int64) string {
	sum := md5.Sum([]byte(strings.TrimSpace(path) + "|" + fmt.Sprintf("%d", ts)))
	return fmt.Sprintf("%x", sum)
}

func buildCommerceSign(v ...any) (string, error) {
	path := ""
	pf := commercePFDefault
	appVersion := commerceAppVersionDefault
	ts := int64(0)
	tdid := ""
	seenPF := false
	seenAppVersion := false
	for _, arg := range v {
		switch value := arg.(type) {
		case string:
			switch {
			case path == "":
				path = value
			case !seenPF:
				pf = strings.TrimSpace(value)
				seenPF = true
			case !seenAppVersion:
				appVersion = strings.TrimSpace(value)
				seenAppVersion = true
			default:
				tdid = strings.TrimSpace(value)
			}
		case int64:
			ts = value
		case int:
			ts = int64(value)
		}
	}
	path = strings.TrimSpace(path)
	if strings.TrimSpace(pf) == "" {
		pf = commercePFDefault
	}
	if strings.TrimSpace(appVersion) == "" {
		appVersion = commerceAppVersionDefault
	}
	// 原始二进制这里不是直接拼完整 path，而是只取 URL.Path 的末 7 个字符参与签名。
	if len(path) > 7 {
		path = path[len(path)-7:]
	}
	raw := fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s", commerceSignPrefix, path, pf, appVersion, ts, strings.TrimSpace(tdid), commerceSignSuffix)
	sum := md5.Sum([]byte(raw))
	return fmt.Sprintf("%x", sum), nil
}

func buildCurlCommand(v ...any) string {
	for _, arg := range v {
		req, ok := arg.(*httpclient.Request)
		if !ok || req == nil {
			continue
		}
		parts := []string{"curl", "-X", req.Method, quoteShell(req.Path)}
		for _, key := range sortedKeys(req.Headers) {
			parts = append(parts, "-H", quoteShell(key+": "+req.Headers[key]))
		}
		if len(req.Body) > 0 {
			parts = append(parts, "--data", quoteShell(string(req.Body)))
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func collectCommerceHeaders(session any) map[string]string {
	root, ok := session.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	headers := map[string]string{}
	if cookie := strings.TrimSpace(fmt.Sprint(root["cookie"])); cookie != "" && cookie != "<nil>" {
		setCommerceHeader(headers, "Cookie", cookie)
	}
	if rawHeaders, ok := root["headers"].(map[string]any); ok {
		keys := make([]string, 0, len(rawHeaders))
		for key := range rawHeaders {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if shouldSkipCommerceHeader(key) {
				continue
			}
			value := strings.TrimSpace(fmt.Sprint(rawHeaders[key]))
			if value != "" && value != "<nil>" {
				setCommerceHeader(headers, key, value)
			}
		}
	}
	return headers
}

func setCommerceHeader(headers map[string]string, key string, value string) {
	if headers == nil {
		return
	}
	key = canonicalHeaderKey(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	for existing := range headers {
		if strings.EqualFold(strings.TrimSpace(existing), key) {
			delete(headers, existing)
		}
	}
	headers[key] = value
}

func firstNonEmptyHeader(headers map[string]string, keys ...string) string {
	for _, key := range keys {
		for _, candidate := range []string{key, canonicalHeaderKey(key), strings.ToLower(strings.TrimSpace(key))} {
			if value := strings.TrimSpace(headers[candidate]); value != "" {
				return value
			}
		}
	}
	return ""
}

func validateCommerceBusinessResponse(op string, rawBody []byte) error {
	if !json.Valid(rawBody) {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil
	}
	ret := strings.TrimSpace(firstStringValue(payload, "ret", "Ret", "code", "Code"))
	if ret == "" || ret == "0" || ret == "200" {
		return nil
	}
	errmsg := strings.TrimSpace(firstStringValue(payload, "errmsg", "err_msg", "ErrMsg", "message", "Message"))
	logID := strings.TrimSpace(firstStringValue(payload, "logId", "log_id", "LogId", "LogID"))
	switch op {
	case "GetUserCredit":
		return fmt.Errorf("query user credit failed: ret=%s errmsg=%s log_id=%s", ret, errmsg, logID)
	case "GetUserInfo":
		return fmt.Errorf("query user info failed: ret=%s errmsg=%s log_id=%s", ret, errmsg, logID)
	default:
		return fmt.Errorf("%s failed: ret=%s errmsg=%s log_id=%s", op, ret, errmsg, logID)
	}
}

func sessionUserID(session any) (string, bool) {
	root, ok := session.(map[string]any)
	if !ok {
		return "", false
	}
	if text := recursiveSessionIdentifier(root); text != "" {
		return text, true
	}
	return "", false
}

func sessionStringByKeys(session any, keys ...string) string {
	root, ok := session.(map[string]any)
	if !ok {
		return ""
	}
	lowered := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		lowered[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return firstSessionString(root, lowered)
}

func recursiveSessionIdentifier(node any) string {
	switch value := node.(type) {
	case map[string]any:
		for key, raw := range value {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "user_id", "userid", "uid":
				text := commerceScalarString(raw)
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
		for _, item := range value {
			if text := recursiveSessionIdentifier(item); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range value {
			if text := recursiveSessionIdentifier(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstSessionString(node any, keys map[string]struct{}) string {
	switch value := node.(type) {
	case map[string]any:
		for key, item := range value {
			if _, ok := keys[strings.ToLower(strings.TrimSpace(key))]; ok {
				text := commerceScalarString(item)
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
		for _, item := range value {
			if text := firstSessionString(item, keys); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range value {
			if text := firstSessionString(item, keys); text != "" {
				return text
			}
		}
	}
	return ""
}

func commerceScalarString(v any) string {
	switch value := v.(type) {
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
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func firstStringValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := root[key]; ok {
			if text := commerceScalarString(value); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	for _, key := range []string{"data", "Data", "result", "Result", "response", "Response", "payload", "Payload"} {
		if nested, ok := root[key].(map[string]any); ok {
			if text := firstStringValue(nested, keys...); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstNestedMapValue(root any, keys ...string) (map[string]any, bool) {
	lowered := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		lowered[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	var visit func(any, int) (map[string]any, bool)
	visit = func(node any, depth int) (map[string]any, bool) {
		if depth > 6 {
			return nil, false
		}
		switch current := node.(type) {
		case map[string]any:
			for key, value := range current {
				if _, ok := lowered[strings.ToLower(strings.TrimSpace(key))]; ok {
					if nested, ok := value.(map[string]any); ok && len(nested) > 0 {
						return nested, true
					}
				}
			}
			for _, child := range current {
				if nested, ok := visit(child, depth+1); ok {
					return nested, true
				}
			}
		case []any:
			for _, child := range current {
				if nested, ok := visit(child, depth+1); ok {
					return nested, true
				}
			}
		}
		return nil, false
	}
	return visit(root, 0)
}

func firstIntValue(root map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := root[key]; ok {
			if parsed, ok := parseIntValue(value); ok {
				return parsed
			}
		}
	}
	for _, key := range []string{"data", "Data", "result", "Result", "response", "Response", "payload", "Payload"} {
		if nested, ok := root[key].(map[string]any); ok {
			if parsed := firstIntValue(nested, keys...); parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}

func firstNestedSliceValue(root any, keys ...string) ([]any, bool) {
	lowered := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		lowered[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	var visit func(any, int) ([]any, bool)
	visit = func(node any, depth int) ([]any, bool) {
		if depth > 6 {
			return nil, false
		}
		switch current := node.(type) {
		case map[string]any:
			for key, value := range current {
				if _, ok := lowered[strings.ToLower(strings.TrimSpace(key))]; ok {
					if nested, ok := value.([]any); ok && len(nested) > 0 {
						return nested, true
					}
				}
			}
			for _, child := range current {
				if nested, ok := visit(child, depth+1); ok {
					return nested, true
				}
			}
		case []any:
			for _, child := range current {
				if nested, ok := visit(child, depth+1); ok {
					return nested, true
				}
			}
		}
		return nil, false
	}
	return visit(root, 0)
}

func firstStringValueFromSliceMaps(items []any, keys ...string) string {
	for _, item := range items {
		nested, ok := item.(map[string]any)
		if !ok || len(nested) == 0 {
			continue
		}
		if text := firstStringValue(nested, keys...); text != "" {
			return text
		}
	}
	return ""
}

func parseIntValue(v any) (int, bool) {
	switch value := v.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		n, err := value.Int64()
		if err == nil {
			return int(n), true
		}
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return 0, false
		}
		n := 0
		for _, ch := range value {
			if ch < '0' || ch > '9' {
				return 0, false
			}
			n = n*10 + int(ch-'0')
		}
		return n, true
	}
	return 0, false
}

func canonicalHeaderKey(key string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "-")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func quoteShell(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
