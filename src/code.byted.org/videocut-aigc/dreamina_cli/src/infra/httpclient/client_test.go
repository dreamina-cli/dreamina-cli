package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyBackendHeadersAddsBrowserLikeDefaults(t *testing.T) {
	t.Helper()

	client := &Client{}
	req := &Request{
		Method:  "POST",
		Path:    "/mcp/v1/text2image",
		Headers: map[string]string{},
		Body:    []byte(`{"prompt":"test"}`),
	}

	client.ApplyBackendHeaders(req)

	if req.Headers["X-Use-Ppe"] != "1" {
		t.Fatalf("unexpected x-use-ppe: %#v", req.Headers["X-Use-Ppe"])
	}
}

func TestApplyBackendHeadersPreservesExplicitValues(t *testing.T) {
	t.Helper()

	client := &Client{}
	req := &Request{
		Method:  "GET",
		Path:    "/account/info",
		Headers: map[string]string{"User-Agent": "custom-agent"},
	}

	client.ApplyBackendHeaders(req)

	if req.Headers["User-Agent"] != "custom-agent" {
		t.Fatalf("unexpected user-agent override: %#v", req.Headers["User-Agent"])
	}
	if req.Headers["X-Use-Ppe"] != "1" {
		t.Fatalf("unexpected x-use-ppe override: %#v", req.Headers["X-Use-Ppe"])
	}
}

func TestBuildHTTPRequestPromotesHostHeader(t *testing.T) {
	t.Helper()

	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	req, err := client.NewRequest(context.Background(), "GET", server.URL, nil, map[string]string{
		"Host": "dreamina.vod.bytedanceapi.com",
	})
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := client.Do(context.Background(), req); err != nil {
		t.Fatalf("do request: %v", err)
	}
	if gotHost != "dreamina.vod.bytedanceapi.com" {
		t.Fatalf("unexpected host override: %q", gotHost)
	}
}
