package login

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func ParseAuthToken(authToken string, randomSecretKey string) (any, error) {
	// 原始二进制这里是严格的单一路径：
	// base64 -> sha256(random_secret_key) -> AES-CBC(key[:16] 作为 IV) -> PKCS7 -> JSON。
	// 之前实现里保留过直读 JSON 和 loose 文本兜底，但这和反汇编不符，继续保留会掩盖真实凭证错误。
	authToken = strings.TrimSpace(authToken)
	randomSecretKey = strings.TrimSpace(randomSecretKey)
	if authToken == "" {
		return nil, fmt.Errorf("auth_token is required")
	}
	if randomSecretKey == "" {
		return nil, fmt.Errorf("random_secret_key is missing locally, please rerun dreamina login")
	}

	decoded, err := base64.StdEncoding.DecodeString(authToken)
	if err != nil {
		return nil, err
	}

	key := sha256.Sum256([]byte(randomSecretKey))
	plain, err := decryptAESCBC(decoded, key[:], key[:aes.BlockSize])
	if err != nil {
		return nil, authTokenDecryptError()
	}
	unpadded, err := pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, authTokenDecryptError()
	}
	payload, err := parseSessionPayloadBytes(unpadded)
	if err != nil {
		return nil, authTokenDecryptError()
	}
	return payload, nil
}

// verifyAuthTokenSignature 校验 auth_token 的签名是否与内置公钥和 md5 摘要匹配。
func verifyAuthTokenSignature(authToken string, autoTokenMD5Sign string, signKeyPairName string) error {
	// 使用内置公钥校验 auth_token 的 MD5 签名，保证本地导入的凭证没有被篡改。
	authToken = strings.TrimSpace(authToken)
	autoTokenMD5Sign = strings.TrimSpace(autoTokenMD5Sign)
	signKeyPairName = strings.TrimSpace(signKeyPairName)
	if authToken == "" {
		return fmt.Errorf("auth_token is required")
	}
	if autoTokenMD5Sign == "" {
		return fmt.Errorf("auto_token_md5_sign is required")
	}
	if signKeyPairName == "" {
		return fmt.Errorf("sign_key_pair_name is required")
	}

	keyMaterial, ok := embeddedSignPublicKeys[signKeyPairName]
	if !ok {
		return fmt.Errorf("unknown sign_key_pair_name %q", signKeyPairName)
	}
	pubAny, err := parseECCPublicKey(keyMaterial)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := pubAny.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("unexpected public key type %T", pubAny)
	}
	signature, err := base64.StdEncoding.DecodeString(autoTokenMD5Sign)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	digest := md5.Sum([]byte(authToken))
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return fmt.Errorf("verify auth token signature: invalid signature")
	}
	return nil
}

// parseECCPublicKey 解析内置的 ECDSA 公钥材料，供 auth token 签名校验使用。
func parseECCPublicKey(v ...any) (any, error) {
	// 解析内置的 ECDSA 公钥材料。
	raw := firstString(v...)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("public key is required")
	}
	body, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(body)
	if err != nil {
		return nil, err
	}
	return pub, nil
}

// pkcs7Unpad 去掉 AES-CBC 解密结果尾部的 PKCS7 padding，并校验块大小与填充长度。
func pkcs7Unpad(v ...any) ([]byte, error) {
	// 校验并去掉 AES-CBC 解密结果尾部的 PKCS7 填充。
	var (
		body      []byte
		blockSize int
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case []byte:
			body = value
		case int:
			blockSize = value
		}
	}
	if blockSize <= 0 {
		blockSize = aes.BlockSize
	}
	if len(body) == 0 || len(body)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 body length")
	}
	padLen := int(body[len(body)-1])
	if padLen <= 0 || padLen > blockSize || padLen > len(body) {
		return nil, fmt.Errorf("invalid pkcs7 padding size")
	}
	for _, b := range body[len(body)-padLen:] {
		if int(b) != padLen {
			return nil, fmt.Errorf("invalid pkcs7 padding bytes")
		}
	}
	return body[:len(body)-padLen], nil
}

var embeddedSignPublicKeys = map[string]string{
	"v0.0.1-idx0": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE7sRTcsFAlhfrw8pGYdR8C9OGYH5P602tJlYT5davvqMuSJvc7j4fTqcChVh1mIj4GUS5SA73KR90ZvwTc1BFhQ==",
	"v0.0.1-idx1": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEa9Ow8PRpwJVxp8iu+x9lYiyli7rp+1a1o0Lg0MTg8dbnbf3zSX1OgDqNuwozcy2YV/4qQWE9asWW0UZoYGaGKg==",
	"v0.0.1-idx2": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEhrr91mzXyh2NQ1yUQFMEHsZfPB7u1W3PFhDgGNvI+ymZyi7L1QrxPOskxQvMeIdI0tq8gv3njjoleu29oG2eNA==",
}

func decryptAESCBC(body []byte, key []byte, iv []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length must be a positive multiple of %d", aes.BlockSize)
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid aes-cbc iv length %d", len(iv))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, body)
	return out, nil
}

// parseSessionPayloadBytes 解析解密后的 token JSON，并把结果收敛成统一的会话结构。
func parseSessionPayloadBytes(body []byte) (any, error) {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return nil, fmt.Errorf("auth token payload is empty")
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("auth token payload is not valid json")
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	// token 解密成功只说明“拿到了 JSON”；真正给后续链路用之前，还需要把 headers/cookie/user/workspace 等字段收敛到稳定根层。
	return sanitizeParsedSessionPayload(payload), nil
}

// sanitizeParsedSessionPayload 递归净化解密后的 session payload，并触发根层 canonical 字段回填。
func sanitizeParsedSessionPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return payload
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = sanitizeParsedSessionValue(key, value)
	}
	backfillParsedSessionRootFields(out)
	return out
}

// backfillParsedSessionRootFields 把嵌套 session/data/payload 中的 cookie、headers 和身份字段回填到根层。
func backfillParsedSessionRootFields(root map[string]any) {
	if len(root) == 0 {
		return
	}
	for _, key := range []string{"cookie", "Cookie"} {
		if text := strings.TrimSpace(fmt.Sprint(root[key])); text != "" && text != "<nil>" {
			goto headers
		}
	}
	if cookie := firstNestedSessionStringValue(root, "cookie", "Cookie"); cookie != "" {
		root["cookie"] = cookie
	}

headers:
	if _, ok := root["headers"].(map[string]any); !ok {
		if headers := firstNestedSessionHeaderMap(root, "headers", "Headers"); len(headers) > 0 {
			root["headers"] = headers
		} else if headers := firstNestedSessionHeaderMap(root, "request_headers", "requestHeaders", "RequestHeaders"); len(headers) > 0 {
			// 后续 commerce/auth/resource 客户端主要读取根层 headers。
			// 当 token payload 只有 request_headers 时，这里回填一份到 headers，避免解密成功但请求侧拿不到浏览器头。
			root["headers"] = headers
		}
	}
	if _, ok := root["request_headers"].(map[string]any); !ok {
		if headers := firstNestedSessionHeaderMap(root, "request_headers", "requestHeaders", "RequestHeaders"); len(headers) > 0 {
			root["request_headers"] = headers
		}
	}
	for _, alias := range []parsedSessionAliasSpec{
		// 根层 canonical 字段优先吸收同名别名，是为了让后续 auth/commerce/resource 客户端只读一套固定键名。
		// space/team/workspace 这几个 ID 容易串值，这里不再跨字段互认，跨 wrapper 的兼容统一交给具名 wrapper 回填逻辑处理。
		{Canonical: "user_id", Keys: []string{"user_id", "userId", "UserId", "UserID", "uid", "UID"}},
		{Canonical: "display_name", Keys: []string{"display_name", "displayName", "DisplayName", "name", "Name", "nickname", "nick_name"}},
		{Canonical: "workspace_id", Keys: []string{"workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID"}},
		{Canonical: "space_id", Keys: []string{"space_id", "spaceId", "SpaceId", "SpaceID"}},
		{Canonical: "team_id", Keys: []string{"team_id", "teamId", "TeamId", "TeamID"}},
		{Canonical: "tenant_id", Keys: []string{"tenant_id", "tenantId", "TenantId", "TenantID"}},
	} {
		if text := strings.TrimSpace(fmt.Sprint(root[alias.Canonical])); text != "" && text != "<nil>" {
			continue
		}
		if value := firstNestedSessionStringValue(root, alias.Keys...); value != "" {
			root[alias.Canonical] = value
		}
	}
	backfillParsedSessionScopedAliases(root)
}

type parsedSessionAliasSpec struct {
	Canonical string
	Keys      []string
}

// firstNestedSessionStringValue 递归读取 token payload 中最先命中的字符串字段，兼容常见 wrapper 和身份节点。
func firstNestedSessionStringValue(root any, keys ...string) string {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	var visit func(any, int) string
	visit = func(node any, depth int) string {
		if depth > 6 {
			return ""
		}
		current, ok := node.(map[string]any)
		if !ok || len(current) == 0 {
			return ""
		}
		for key, value := range current {
			if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
		// session/data/payload/result/response 是 token payload 最常见的包装层；
		// identity/user 继续单列，是为了兼容把 uid/name 直接挂在身份 wrapper 里的版本。
		for _, wrapper := range []string{"session", "Session", "data", "Data", "payload", "Payload", "result", "Result", "response", "Response", "identity", "Identity", "user", "User"} {
			if text := visit(current[wrapper], depth+1); text != "" {
				return text
			}
		}
		for _, child := range current {
			if text := visit(child, depth+1); text != "" {
				return text
			}
		}
		return ""
	}
	return visit(root, 0)
}

// firstNestedSessionHeaderMap 递归查找 token payload 中的 headers/request_headers 容器并归一化输出。
func firstNestedSessionHeaderMap(root any, keys ...string) map[string]any {
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
			if headers := headerMapFromParsedSessionValue(current[key]); len(headers) > 0 {
				return headers
			}
		}
		for _, wrapper := range []string{"session", "Session", "data", "Data", "payload", "Payload", "result", "Result", "response", "Response"} {
			if headers := visit(current[wrapper], depth+1); len(headers) > 0 {
				return headers
			}
		}
		for _, child := range current {
			if headers := visit(child, depth+1); len(headers) > 0 {
				return headers
			}
		}
		return nil
	}
	return visit(root, 0)
}

// headerMapFromParsedSessionValue 把 token payload 中的任意 header 容器清洗成稳定的 map[string]any 形态。
func headerMapFromParsedSessionValue(value any) map[string]any {
	headers, ok := value.(map[string]any)
	if !ok || len(headers) == 0 {
		return nil
	}
	out := make(map[string]any, len(headers))
	for key, item := range headers {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" && text != "<nil>" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstParsedSessionStringValue 只在当前 map 层读取首个非空字符串字段，不继续递归。
func firstParsedSessionStringValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		text := strings.TrimSpace(fmt.Sprint(root[key]))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

// backfillParsedSessionScopedAliases 从具名 user/workspace/team/tenant wrapper 回填根层 canonical 字段。
func backfillParsedSessionScopedAliases(root map[string]any) {
	if len(root) == 0 {
		return
	}
	if text := strings.TrimSpace(fmt.Sprint(root["user_id"])); text == "" || text == "<nil>" {
		if user := firstNestedSessionMap(root, "user", "User", "member", "Member", "profile", "Profile"); len(user) > 0 {
			if value := firstParsedSessionStringValue(user, "user_id", "userId", "UserId", "UserID", "uid", "UID", "id", "ID"); value != "" {
				root["user_id"] = value
			}
		}
	}
	if text := strings.TrimSpace(fmt.Sprint(root["display_name"])); text == "" || text == "<nil>" {
		if user := firstNestedSessionMap(root, "user", "User", "member", "Member", "profile", "Profile"); len(user) > 0 {
			if value := firstParsedSessionStringValue(user, "display_name", "displayName", "DisplayName", "nickname", "nick_name", "name", "Name"); value != "" {
				root["display_name"] = value
			}
		}
	}
	if text := strings.TrimSpace(fmt.Sprint(root["workspace_id"])); text == "" || text == "<nil>" {
		// workspace_id 只从具名 workspace wrapper 回填，避免把 space/team 的局部 ID 错绑成 workspace_id。
		if workspace := firstNestedSessionMap(root, "workspace", "Workspace"); len(workspace) > 0 {
			if value := firstParsedSessionStringValue(workspace, "workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID", "id", "ID"); value != "" {
				root["workspace_id"] = value
			}
		}
	}
	if text := strings.TrimSpace(fmt.Sprint(root["space_id"])); text == "" || text == "<nil>" {
		if space := firstNestedSessionMap(root, "space", "Space"); len(space) > 0 {
			if value := firstParsedSessionStringValue(space, "space_id", "spaceId", "SpaceId", "SpaceID", "id", "ID"); value != "" {
				root["space_id"] = value
			}
		}
	}
	if text := strings.TrimSpace(fmt.Sprint(root["team_id"])); text == "" || text == "<nil>" {
		if team := firstNestedSessionMap(root, "team", "Team"); len(team) > 0 {
			if value := firstParsedSessionStringValue(team, "team_id", "teamId", "TeamId", "TeamID", "id", "ID"); value != "" {
				root["team_id"] = value
			}
		}
	}
	if text := strings.TrimSpace(fmt.Sprint(root["tenant_id"])); text == "" || text == "<nil>" {
		if tenant := firstNestedSessionMap(root, "tenant", "Tenant"); len(tenant) > 0 {
			if value := firstParsedSessionStringValue(tenant, "tenant_id", "tenantId", "TenantId", "TenantID", "id", "ID"); value != "" {
				root["tenant_id"] = value
			}
		}
	}
}

// firstNestedSessionMap 递归查找 token payload 中首个命中的具名 map wrapper。
func firstNestedSessionMap(root any, keys ...string) map[string]any {
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
		for _, wrapper := range []string{"session", "Session", "data", "Data", "payload", "Payload", "result", "Result", "response", "Response"} {
			if value := visit(current[wrapper], depth+1); len(value) > 0 {
				return value
			}
		}
		for _, child := range current {
			if value := visit(child, depth+1); len(value) > 0 {
				return value
			}
		}
		return nil
	}
	return visit(root, 0)
}

// sanitizeParsedSessionValue 递归清理解密后 session payload 中的字符串、header 和子容器值。
func sanitizeParsedSessionValue(key string, value any) any {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	switch typed := value.(type) {
	case map[string]any:
		if lowerKey == "headers" || lowerKey == "request_headers" {
			return sanitizeParsedHeaderMap(typed)
		}
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = sanitizeParsedSessionValue(childKey, childValue)
		}
		return out
	case map[string]string:
		if lowerKey == "headers" || lowerKey == "request_headers" {
			return sanitizeParsedHeaderMap(typed)
		}
		out := make(map[string]string, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = strings.TrimSpace(childValue)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeParsedSessionValue(key, item))
		}
		return out
	case string:
		return strings.TrimSpace(typed)
	default:
		return value
	}
}

// sanitizeParsedHeaderMap 把 token payload 中的 headers 收敛成净化后的 header map。
func sanitizeParsedHeaderMap(value any) map[string]any {
	headers := http.Header{}
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			key = strings.TrimSpace(key)
			text := strings.TrimSpace(fmt.Sprint(item))
			if key != "" && text != "" && text != "<nil>" {
				headers.Add(key, text)
			}
		}
	case map[string]string:
		for key, item := range typed {
			key = strings.TrimSpace(key)
			item = strings.TrimSpace(item)
			if key != "" && item != "" {
				headers.Add(key, item)
			}
		}
	}
	sanitized := sanitizeSessionHeaders(headers)
	out := make(map[string]any, len(sanitized))
	for key, values := range sanitized {
		if len(values) == 1 {
			out[key] = values[0]
			continue
		}
		items := make([]any, 0, len(values))
		for _, item := range values {
			items = append(items, item)
		}
		out[key] = items
	}
	return out
}

// authTokenDecryptError 返回统一的本地随机密钥解密失败错误。
func authTokenDecryptError() error {
	return fmt.Errorf("auth_token cannot be decrypted by local random_secret_key")
}
