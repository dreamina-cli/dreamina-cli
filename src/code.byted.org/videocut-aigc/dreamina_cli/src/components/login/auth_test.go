package login

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseAuthTokenRejectsBase64JSONPayloadWithoutDecryption(t *testing.T) {
	t.Helper()

	token := base64.StdEncoding.EncodeToString([]byte(`{"cookie":"sid=test"}`))
	_, err := ParseAuthToken(token, "secret-key")
	if err == nil {
		t.Fatalf("expected ParseAuthToken failure")
	}
	if !strings.Contains(err.Error(), "auth_token cannot be decrypted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseAuthTokenDecryptsAESCBCPayload(t *testing.T) {
	t.Helper()

	payload := []byte(`{"cookie":"sid=encrypted","headers":{"User-Agent":"ua-test","X-Test":"1","Authorization":"secret"},"request_headers":{"Accept":"application/json","X-Debug":"1"}}`)
	token := encryptAuthTokenForTest(t, payload, "secret-key")

	got, err := ParseAuthToken(token, "secret-key")
	if err != nil {
		t.Fatalf("ParseAuthToken failed: %v", err)
	}

	root, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got)
	}
	if root["cookie"] != "sid=encrypted" {
		t.Fatalf("unexpected cookie: %#v", root["cookie"])
	}
	headers, ok := root["headers"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected headers type: %T", root["headers"])
	}
	if headers["User-Agent"] != "ua-test" {
		t.Fatalf("unexpected sanitized user-agent: %#v", headers["User-Agent"])
	}
	if _, exists := headers["X-Test"]; exists {
		t.Fatalf("unexpected unsanitized custom header: %#v", headers)
	}
	if _, exists := headers["Authorization"]; exists {
		t.Fatalf("unexpected unsanitized auth header: %#v", headers)
	}
	requestHeaders, ok := root["request_headers"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected request_headers type: %T", root["request_headers"])
	}
	if requestHeaders["Accept"] != "application/json" {
		t.Fatalf("unexpected sanitized accept header: %#v", requestHeaders["Accept"])
	}
}

func TestParseAuthTokenRejectsLoosePayloadParsing(t *testing.T) {
	t.Helper()

	raw := `prefix "cookie":"sid=loose","headers":{"X-Test":"1"} suffix`
	token := base64.StdEncoding.EncodeToString([]byte(raw))

	_, err := ParseAuthToken(token, "secret-key")
	if err == nil {
		t.Fatalf("expected ParseAuthToken failure")
	}
	if !strings.Contains(err.Error(), "auth_token cannot be decrypted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseAuthTokenBackfillsRootSessionFieldsFromNestedWrappers(t *testing.T) {
	t.Helper()

	payload := []byte(`{
		"data": {
			"session": {
				"cookie": "sid=nested",
				"headers": {
					"User-Agent": "ua-nested",
					"Authorization": "secret"
				},
				"user": {
					"id": "u-nested-1",
					"name": "nested-name"
				},
				"workspace": {
					"id": "ws-nested-1"
				}
			}
		}
	}`)
	token := encryptAuthTokenForTest(t, payload, "secret-key")

	got, err := ParseAuthToken(token, "secret-key")
	if err != nil {
		t.Fatalf("ParseAuthToken failed: %v", err)
	}

	root, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got)
	}
	if root["cookie"] != "sid=nested" {
		t.Fatalf("unexpected root cookie: %#v", root["cookie"])
	}
	headers, ok := root["headers"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected root headers type: %T", root["headers"])
	}
	if headers["User-Agent"] != "ua-nested" {
		t.Fatalf("unexpected root user-agent: %#v", headers["User-Agent"])
	}
	if _, exists := headers["Authorization"]; exists {
		t.Fatalf("unexpected unsanitized auth header: %#v", headers)
	}
	if root["user_id"] != "u-nested-1" {
		t.Fatalf("unexpected root user_id: %#v", root["user_id"])
	}
	if root["display_name"] != "nested-name" {
		t.Fatalf("unexpected root display_name: %#v", root["display_name"])
	}
	if root["workspace_id"] != "ws-nested-1" {
		t.Fatalf("unexpected root workspace_id: %#v", root["workspace_id"])
	}
}

func TestParseAuthTokenPromotesNestedRequestHeadersToRootHeaders(t *testing.T) {
	t.Helper()

	payload := []byte(`{
		"payload": {
			"session": {
				"cookie": "sid=request-only",
				"request_headers": {
					"Accept": "application/json",
					"User-Agent": "ua-request-only"
				}
			}
		}
	}`)
	token := encryptAuthTokenForTest(t, payload, "secret-key")

	got, err := ParseAuthToken(token, "secret-key")
	if err != nil {
		t.Fatalf("ParseAuthToken failed: %v", err)
	}

	root, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got)
	}
	headers, ok := root["headers"].(map[string]any)
	if !ok {
		t.Fatalf("expected root headers fallback, got %T", root["headers"])
	}
	if headers["Accept"] != "application/json" || headers["User-Agent"] != "ua-request-only" {
		t.Fatalf("unexpected promoted root headers: %#v", headers)
	}
	requestHeaders, ok := root["request_headers"].(map[string]any)
	if !ok {
		t.Fatalf("expected root request_headers, got %T", root["request_headers"])
	}
	if requestHeaders["Accept"] != "application/json" {
		t.Fatalf("unexpected root request_headers: %#v", requestHeaders)
	}
}

func TestParseAuthTokenBackfillsSpaceTeamTenantAliases(t *testing.T) {
	t.Helper()

	payload := []byte(`{
		"session": {
			"space": {
				"id": "space-1"
			},
			"team": {
				"id": "team-1"
			},
			"tenant": {
				"id": "tenant-1"
			}
		}
	}`)
	token := encryptAuthTokenForTest(t, payload, "secret-key")

	got, err := ParseAuthToken(token, "secret-key")
	if err != nil {
		t.Fatalf("ParseAuthToken failed: %v", err)
	}

	root, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got)
	}
	if root["space_id"] != "space-1" {
		t.Fatalf("unexpected root space_id: %#v", root["space_id"])
	}
	if root["team_id"] != "team-1" {
		t.Fatalf("unexpected root team_id: %#v", root["team_id"])
	}
	if root["tenant_id"] != "tenant-1" {
		t.Fatalf("unexpected root tenant_id: %#v", root["tenant_id"])
	}
}

func TestParseAuthTokenDoesNotCrossFillWorkspaceFromSpaceOrTeam(t *testing.T) {
	t.Helper()

	payload := []byte(`{
		"session": {
			"space": {
				"id": "space-only-1"
			},
			"team": {
				"id": "team-only-1"
			}
		}
	}`)
	token := encryptAuthTokenForTest(t, payload, "secret-key")

	got, err := ParseAuthToken(token, "secret-key")
	if err != nil {
		t.Fatalf("ParseAuthToken failed: %v", err)
	}

	root, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got)
	}
	if _, exists := root["workspace_id"]; exists {
		t.Fatalf("did not expect workspace_id to be backfilled from space/team only: %#v", root["workspace_id"])
	}
	if root["space_id"] != "space-only-1" {
		t.Fatalf("unexpected root space_id: %#v", root["space_id"])
	}
	if root["team_id"] != "team-only-1" {
		t.Fatalf("unexpected root team_id: %#v", root["team_id"])
	}
}

func TestVerifyAuthTokenSignatureRejectsUnknownKeyPair(t *testing.T) {
	t.Helper()

	err := verifyAuthTokenSignature("token", "c2lnbg==", "missing-key")
	if err == nil {
		t.Fatalf("expected signature verification failure")
	}
	if !strings.Contains(err.Error(), "unknown sign_key_pair_name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func encryptAuthTokenForTest(t *testing.T, payload []byte, randomSecretKey string) string {
	t.Helper()

	key := sha256.Sum256([]byte(randomSecretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("new aes cipher: %v", err)
	}
	plain := pkcs7PadForTest(payload, aes.BlockSize)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, plain)
	return base64.StdEncoding.EncodeToString(out)
}

func pkcs7PadForTest(body []byte, blockSize int) []byte {
	padLen := blockSize - len(body)%blockSize
	if padLen == 0 {
		padLen = blockSize
	}
	out := make([]byte, len(body)+padLen)
	copy(out, body)
	for i := len(body); i < len(out); i++ {
		out[i] = byte(padLen)
	}
	return out
}

func TestParseAuthTokenRejectsUnknownPayload(t *testing.T) {
	t.Helper()

	token := base64.StdEncoding.EncodeToString([]byte("not-json-and-not-loose"))
	_, err := ParseAuthToken(token, "secret-key")
	if err == nil {
		t.Fatalf("expected ParseAuthToken failure")
	}
	if !strings.Contains(err.Error(), "auth_token cannot be decrypted") {
		t.Fatalf("unexpected error: %v", err)
	}
}
