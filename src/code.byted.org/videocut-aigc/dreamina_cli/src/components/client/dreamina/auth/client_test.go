package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

func TestValidateAuthTokenSupportsPayloadWrapperAndMergesSession(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/token/validate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("nonce"); got == "" {
			t.Fatalf("expected nonce query")
		}
		if got := r.Header.Get("Cookie"); got != "sid=test" {
			t.Fatalf("unexpected cookie header: %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "ua-test" {
			t.Fatalf("unexpected user-agent: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "0",
			"Payload": map[string]any{
				"valid":        true,
				"user_id":      "u-1",
				"display_name": "tester",
				"workspace_id": "ws-1",
			},
			"log_id": "log-auth-1",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
		"headers": map[string]any{
			"x-test":     "1",
			"User-Agent": "ua-test",
		},
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected valid response: %#v", resp)
	}
	if resp.LogID != "log-auth-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if session["user_id"] != "u-1" || session["display_name"] != "tester" || session["workspace_id"] != "ws-1" {
		t.Fatalf("unexpected merged session: %#v", session)
	}
	if !strings.Contains(resp.Curl, "/auth/v1/token/validate") {
		t.Fatalf("unexpected curl: %q", resp.Curl)
	}
}

func TestValidateAuthTokenReturnsPreviewOnBackendFailure(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("token expired"))
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	_, err = client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err == nil {
		t.Fatalf("expected validation failure")
	}
	if !strings.Contains(err.Error(), "token expired") || !strings.Contains(err.Error(), "status=401") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAuthTokenRejectsNonJSONSuccessBody(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>gateway error</html>"))
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	_, err = client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err == nil {
		t.Fatal("expected non-json validate response to fail")
	}
	if !strings.Contains(err.Error(), "preview=") {
		t.Fatalf("expected preview in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "gateway error") {
		t.Fatalf("expected body preview in error, got: %v", err)
	}
}

func TestValidateAuthTokenSupportsCamelCaseValidAndSessionAliases(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Message":   "ok",
			"RequestID": "log-auth-camel",
			"Response": map[string]any{
				"Valid":       "true",
				"UserID":      "u-camel-1",
				"DisplayName": "camel-name",
				"WorkspaceId": "ws-camel-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected valid response: %#v", resp)
	}
	if resp.LogID != "log-auth-camel" || resp.Message != "ok" {
		t.Fatalf("unexpected response metadata: %#v", resp)
	}
	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if session["UserID"] != "u-camel-1" || session["DisplayName"] != "camel-name" || session["WorkspaceId"] != "ws-camel-1" {
		t.Fatalf("unexpected merged session: %#v", session)
	}
}

func TestValidateAuthTokenSupportsDeepNestedSessionWrapper(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Valid": true,
					"Session": map[string]any{
						"UserID":      "u-deep-1",
						"DisplayName": "deep-name",
						"WorkspaceID": "ws-deep-1",
					},
				},
			},
			"RequestID": "log-auth-deep",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected valid response: %#v", resp)
	}
	if resp.LogID != "log-auth-deep" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if session["UserID"] != "u-deep-1" || session["DisplayName"] != "deep-name" || session["WorkspaceID"] != "ws-deep-1" {
		t.Fatalf("unexpected deep merged session: %#v", session)
	}
}

func TestValidateAuthTokenSupportsDeepIdentityWrapperAndNestedMetadata(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Status": "success",
					"Meta": map[string]any{
						"Message":   "identity validated",
						"RequestID": "log-auth-identity",
					},
					"Identity": map[string]any{
						"UID":         "u-identity-1",
						"DisplayName": "identity-name",
						"SpaceID":     "ws-identity-1",
					},
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected valid response: %#v", resp)
	}
	if resp.LogID != "log-auth-identity" || resp.Message != "identity validated" {
		t.Fatalf("unexpected response metadata: %#v", resp)
	}
	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if session["UID"] != "u-identity-1" || session["DisplayName"] != "identity-name" || session["SpaceID"] != "ws-identity-1" {
		t.Fatalf("unexpected identity merged session: %#v", session)
	}
}

func TestValidateAuthTokenBackfillsCanonicalAliasesFromUserWorkspaceTenantWrappers(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Valid": true,
					"User": map[string]any{
						"ID":   "u-wrapper-1",
						"Name": "wrapper-name",
					},
					"Workspace": map[string]any{
						"ID": "ws-wrapper-1",
					},
					"Tenant": map[string]any{
						"ID": "tenant-wrapper-1",
					},
				},
			},
			"RequestID": "log-auth-wrapper-1",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected valid response: %#v", resp)
	}
	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if session["user_id"] != "u-wrapper-1" {
		t.Fatalf("unexpected canonical user_id: %#v", session)
	}
	if session["display_name"] != "wrapper-name" {
		t.Fatalf("unexpected canonical display_name: %#v", session)
	}
	if session["workspace_id"] != "ws-wrapper-1" {
		t.Fatalf("unexpected canonical workspace_id: %#v", session)
	}
	if session["tenant_id"] != "tenant-wrapper-1" {
		t.Fatalf("unexpected canonical tenant_id: %#v", session)
	}
}

func TestValidateAuthTokenBackfillsCanonicalAliasesFromSessionRawAliasFields(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Valid": true,
					"Session": map[string]any{
						"UID":         "u-session-raw-1",
						"DisplayName": "session-raw-name",
						"WorkspaceID": "ws-session-raw-1",
						"SpaceID":     "space-session-raw-1",
						"TeamID":      "team-session-raw-1",
						"TenantID":    "tenant-session-raw-1",
					},
				},
			},
			"RequestID": "log-auth-session-raw-1",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected valid response: %#v", resp)
	}
	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if session["user_id"] != "u-session-raw-1" {
		t.Fatalf("unexpected canonical user_id: %#v", session)
	}
	if session["display_name"] != "session-raw-name" {
		t.Fatalf("unexpected canonical display_name: %#v", session)
	}
	if session["workspace_id"] != "ws-session-raw-1" {
		t.Fatalf("unexpected canonical workspace_id: %#v", session)
	}
	if session["space_id"] != "space-session-raw-1" {
		t.Fatalf("unexpected canonical space_id: %#v", session)
	}
	if session["team_id"] != "team-session-raw-1" {
		t.Fatalf("unexpected canonical team_id: %#v", session)
	}
	if session["tenant_id"] != "tenant-session-raw-1" {
		t.Fatalf("unexpected canonical tenant_id: %#v", session)
	}
}

func TestValidateAuthTokenDoesNotCrossFillWorkspaceFromSpaceOrTeamWrapper(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"code": "0",
			"data": map[string]any{
				"Space": map[string]any{
					"ID": "space-only-1",
				},
				"Team": map[string]any{
					"ID": "team-only-1",
				},
			},
		}); err != nil {
			t.Fatalf("encode auth response: %v", err)
		}
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}

	session, ok := resp.Session.(map[string]any)
	if !ok {
		t.Fatalf("unexpected session type: %T", resp.Session)
	}
	if _, exists := session["workspace_id"]; exists {
		t.Fatalf("did not expect workspace_id to be backfilled from space/team wrapper: %#v", session["workspace_id"])
	}
	if session["space_id"] != "space-only-1" {
		t.Fatalf("unexpected canonical space_id: %#v", session)
	}
	if session["team_id"] != "team-only-1" {
		t.Fatalf("unexpected canonical team_id: %#v", session)
	}
}

func TestValidateAuthTokenExplicitValidFalseBeatsMissingCode(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Valid":   false,
					"Message": "token expired",
				},
			},
			"TraceID": "log-auth-valid-false",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.Valid {
		t.Fatalf("expected validation failure, got success: %#v", resp)
	}
	if resp.Message != "token expired" {
		t.Fatalf("unexpected message: %#v", resp.Message)
	}
	if resp.LogID != "log-auth-valid-false" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestValidateAuthTokenSupportsErrNoErrMsgAndTraceIDAliases(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Error": map[string]any{
					"ErrNo":   1001,
					"ErrMsg":  "signature mismatch",
					"TraceID": "log-auth-errno-1",
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.Valid {
		t.Fatalf("expected validation failure, got success: %#v", resp)
	}
	if resp.Message != "signature mismatch" {
		t.Fatalf("unexpected message: %#v", resp.Message)
	}
	if resp.LogID != "log-auth-errno-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestValidateAuthTokenRejectsEmptyJSONObjectAsSuccess(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.Valid {
		t.Fatalf("empty json should not be treated as success: %#v", resp)
	}
}

func TestValidateAuthTokenRejectsPayloadWithoutValidOrCode(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "missing success marker",
			"log_id":  "log-auth-missing-success-marker",
			"data": map[string]any{
				"user_id": "u-plain-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.Valid {
		t.Fatalf("payload without valid/code should not be treated as success: %#v", resp)
	}
	if resp.Message != "missing success marker" {
		t.Fatalf("unexpected message: %#v", resp.Message)
	}
	if resp.LogID != "log-auth-missing-success-marker" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestValidateAuthTokenStillSupportsExplicitSuccessCode(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   "0",
			"log_id": "log-auth-code-zero",
			"data": map[string]any{
				"user_id": "u-code-zero-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.ValidateAuthToken(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("ValidateAuthToken failed: %v", err)
	}
	if resp == nil || !resp.Valid {
		t.Fatalf("expected explicit code=0 to remain successful: %#v", resp)
	}
	if resp.LogID != "log-auth-code-zero" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}
