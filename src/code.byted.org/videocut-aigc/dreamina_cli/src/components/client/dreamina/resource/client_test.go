package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

type fakeImageXClient struct {
	accessKey    string
	secretKey    string
	sessionToken string
	host         string
	params       *imageXApplyUploadParam
	images       [][]byte
	resp         *imageXCommitUploadResult
	err          error
}

func (f *fakeImageXClient) SetAccessKey(ak string) {
	f.accessKey = ak
}

func (f *fakeImageXClient) SetSecretKey(sk string) {
	f.secretKey = sk
}

func TestUploadSinglePathReportsEmptyPathAsReadFileError(t *testing.T) {
	t.Helper()

	client := &ByteDanceUploadClient{}
	_, err := client.uploadSinglePath(context.Background(), nil, "image", "", "", 0)
	if err == nil {
		t.Fatalf("expected empty path error")
	}
	if got := err.Error(); got != "read file : open : no such file or directory" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func (f *fakeImageXClient) SetSessionToken(token string) {
	f.sessionToken = token
}

func (f *fakeImageXClient) SetHost(host string) {
	f.host = host
}

func (f *fakeImageXClient) UploadImages(params *imageXApplyUploadParam, images [][]byte) (*imageXCommitUploadResult, error) {
	f.params = params
	f.images = images
	return f.resp, f.err
}

func TestUploadResourceImageFlow(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "sample.png")
	if err := os.WriteFile(imagePath, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	var (
		sawImageUpload bool
		serverURL      string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mweb/v1/get_upload_token":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected get_upload_token method: %s", r.Method)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode get_upload_token request: %v", err)
			}
			if got := strings.TrimSpace(req["resource_type"].(string)); got != "image" {
				t.Fatalf("unexpected resource_type: %q", got)
			}
			if got := int(req["scene"].(float64)); got != 1 {
				t.Fatalf("unexpected scene: %d", got)
			}
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"data": map[string]any{
					"scene":         1,
					"resource_type": "image",
					"upload_domain": serverURL,
					"store_keys":    []string{"resource/test-image.png"},
					"tos_headers":   `{"X-Test-Upload":"yes"}`,
					"tos_meta":      `{"scene":"dreamina","trace_id":"trace-001"}`,
				},
			})
		case r.URL.Path == "/dreamina/mcp/v1/resource_store":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected resource_store method: %s", r.Method)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resource_store request: %v", err)
			}
			resourceItems, ok := req["resource_items"].([]any)
			if !ok || len(resourceItems) != 1 {
				t.Fatalf("unexpected resource payload: %#v", req["resource_items"])
			}
			item, ok := resourceItems[0].(map[string]any)
			if !ok {
				t.Fatalf("unexpected resource item: %#v", resourceItems[0])
			}
			if strings.TrimSpace(item["resource_type"].(string)) != "image" {
				t.Fatalf("unexpected resource_type: %#v", item)
			}
			if strings.TrimSpace(item["resource_value"].(string)) != "resource/test-image.png" {
				t.Fatalf("unexpected resource_value: %#v", item)
			}
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"resource_items": []map[string]any{
					{
						"resource_id":   "rid-image-1",
						"resource_type": "image",
						"path":          imagePath,
						"store_uri":     "resource/test-image.png",
						"name":          filepath.Base(imagePath),
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)
	client.uploadImageFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		if path != imagePath {
			t.Fatalf("unexpected image upload path: %q", path)
		}
		if token == nil || token.UploadDomain != serverURL {
			t.Fatalf("unexpected upload token: %#v", token)
		}
		sawImageUpload = true
		return &SingleUploadRes{
			ResourceID:   "resource/test-image.png",
			StoreURI:     "resource/test-image.png",
			UploadDomain: serverURL,
		}, nil
	}

	got, err := client.UploadResource(context.Background(), &mcpclient.Session{Cookie: "sid=test"}, "image", []string{imagePath})
	if err != nil {
		t.Fatalf("UploadResource failed: %v", err)
	}
	if !sawImageUpload {
		t.Fatalf("expected image upload to run")
	}
	if len(got) != 1 {
		t.Fatalf("unexpected result count: %d", len(got))
	}
	if got[0].ResourceID != "rid-image-1" {
		t.Fatalf("unexpected resource_id: %q", got[0].ResourceID)
	}
	if got[0].Path != imagePath {
		t.Fatalf("unexpected stored path: %q", got[0].Path)
	}
	if got[0].MimeType != "image/png" {
		t.Fatalf("unexpected mime type: %q", got[0].MimeType)
	}
}

func TestUploadResourceImageFlowUsesIndexedStoreInfos(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	firstImagePath := filepath.Join(tmpDir, "first.png")
	secondImagePath := filepath.Join(tmpDir, "second.png")
	if err := os.WriteFile(firstImagePath, []byte("first-bytes"), 0o644); err != nil {
		t.Fatalf("write first temp image: %v", err)
	}
	if err := os.WriteFile(secondImagePath, []byte("second-bytes"), 0o644); err != nil {
		t.Fatalf("write second temp image: %v", err)
	}

	var (
		mu                sync.Mutex
		serverURL         string
		uploadStoreURIs   []string
		storedResourceIDs []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mweb/v1/get_upload_token":
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"data": map[string]any{
					"scene":         1,
					"resource_type": "image",
					"upload_domain": serverURL,
					"store_infos": []any{
						map[string]any{
							"store_uri":     "resource/first.png",
							"upload_domain": serverURL,
						},
						map[string]any{
							"store_uri":     "resource/second.png",
							"upload_domain": serverURL,
						},
					},
				},
			})
		case r.URL.Path == "/dreamina/mcp/v1/resource_store":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resource_store request: %v", err)
			}
			items, ok := req["resource_items"].([]any)
			if !ok || len(items) != 2 {
				t.Fatalf("unexpected resource items: %#v", req["resource_items"])
			}
			mu.Lock()
			defer mu.Unlock()
			for _, item := range items {
				typed, ok := item.(map[string]any)
				if !ok {
					t.Fatalf("unexpected resource store item: %#v", item)
				}
				storedResourceIDs = append(storedResourceIDs, strings.TrimSpace(typed["resource_value"].(string)))
			}
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"resource_items": []any{
					map[string]any{
						"resource_id":   "rid-first",
						"resource_type": "image",
						"path":          firstImagePath,
						"store_uri":     "resource/first.png",
						"name":          filepath.Base(firstImagePath),
					},
					map[string]any{
						"resource_id":   "rid-second",
						"resource_type": "image",
						"path":          secondImagePath,
						"store_uri":     "resource/second.png",
						"name":          filepath.Base(secondImagePath),
					},
				},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)
	client.uploadImageFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		mu.Lock()
		uploadStoreURIs = append(uploadStoreURIs, token.selectedStoreURI(path))
		mu.Unlock()
		storeURI := token.selectedStoreURI(path)
		name := strings.TrimSuffix(filepath.Base(storeURI), filepath.Ext(storeURI))
		return &SingleUploadRes{
			ResourceID:   "rid-" + name,
			StoreURI:     storeURI,
			UploadDomain: serverURL,
		}, nil
	}

	got, err := client.UploadResource(context.Background(), &mcpclient.Session{Cookie: "sid=test"}, "image", []string{firstImagePath, secondImagePath})
	if err != nil {
		t.Fatalf("upload resources failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected uploaded resource count: %d", len(got))
	}
	if got[0].ResourceID != "rid-first" || got[1].ResourceID != "rid-second" {
		t.Fatalf("unexpected stored resources: %#v", got)
	}

	mu.Lock()
	defer mu.Unlock()
	slices.Sort(uploadStoreURIs)
	expectedStoreURIs := []string{
		"resource/first.png",
		"resource/second.png",
	}
	if !reflect.DeepEqual(uploadStoreURIs, expectedStoreURIs) {
		t.Fatalf("unexpected upload store uris: %#v", uploadStoreURIs)
	}
	slices.Sort(storedResourceIDs)
	if !reflect.DeepEqual(storedResourceIDs, []string{"rid-first", "rid-second"}) {
		t.Fatalf("unexpected stored resource ids: %#v", storedResourceIDs)
	}
}

func TestUploadResourceUsesInjectedSteps(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	client := New()
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        3,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        3,
				AgentScene:   3,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadVideoAudioFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		return &SingleUploadRes{ResourceID: "uploaded-audio-id"}, nil
	}
	client.resourceStoreFunc = func(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error) {
		if len(items) != 1 {
			t.Fatalf("unexpected item count: %d", len(items))
		}
		if items[0].ResourceID != "uploaded-audio-id" {
			t.Fatalf("unexpected pre-store resource id: %q", items[0].ResourceID)
		}
		return &resourceStoreResult{
			Stored: []*Resource{
				{
					ResourceID:   "stored-audio-id",
					ResourceType: "audio",
				},
			},
		}, nil
	}
	client.probeDurationFunc = func(ctx context.Context, path string) (float64, error) {
		return 5, nil
	}

	ctx := ContextWithUploadModelVersion(context.Background(), "seedance2.0")
	got, err := client.UploadResource(ctx, &mcpclient.Session{}, "audio", []string{audioPath})
	if err != nil {
		t.Fatalf("UploadResource failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected result count: %d", len(got))
	}
	if got[0].ResourceID != "stored-audio-id" {
		t.Fatalf("unexpected stored resource id: %q", got[0].ResourceID)
	}
}

func TestUploadResourceFailsWhenResourceStoreReturnsNoStoredItems(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	client := New()
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        3,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        3,
				AgentScene:   3,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadVideoAudioFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		return &SingleUploadRes{
			ResourceID: "uploaded-audio-id",
			StoreURI:   "audio/sample.mp3",
		}, nil
	}
	client.resourceStoreFunc = func(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error) {
		return &resourceStoreResult{Stored: nil}, nil
	}
	client.probeDurationFunc = func(ctx context.Context, path string) (float64, error) {
		return 5, nil
	}

	_, err := client.UploadResource(ContextWithUploadModelVersion(context.Background(), "seedance2.0"), &mcpclient.Session{}, "audio", []string{audioPath})
	if err == nil {
		t.Fatalf("expected UploadResource to fail on empty stored result")
	}
	if !strings.Contains(err.Error(), "resource store returned empty stored result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadResourceFailsWhenRealResourceStoreReturnsNoStoredItems(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "sample.png")
	if err := os.WriteFile(imagePath, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/mcp/v1/resource_store" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode resource_store request: %v", err)
		}
		items, ok := req["resource_items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("unexpected resource items: %#v", req["resource_items"])
		}
		writeJSON(t, w, map[string]any{
			"ret":            "0",
			"resource_items": []any{},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        1,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        1,
				AgentScene:   1,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadImageFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		return &SingleUploadRes{
			ResourceID: "uploaded-image-id",
			StoreURI:   "image/sample.png",
		}, nil
	}

	_, err = client.UploadResource(context.Background(), &mcpclient.Session{}, "image", []string{imagePath})
	if err == nil {
		t.Fatalf("expected UploadResource to fail on empty real resource_store result")
	}
	if !strings.Contains(err.Error(), "resource store returned empty stored result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadResourceFailsWhenRealResourceStoreReturnsPartialStoredItems(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	firstImagePath := filepath.Join(tmpDir, "first.png")
	secondImagePath := filepath.Join(tmpDir, "second.png")
	if err := os.WriteFile(firstImagePath, []byte("first-bytes"), 0o644); err != nil {
		t.Fatalf("write first temp image: %v", err)
	}
	if err := os.WriteFile(secondImagePath, []byte("second-bytes"), 0o644); err != nil {
		t.Fatalf("write second temp image: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/mcp/v1/resource_store" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode resource_store request: %v", err)
		}
		items, ok := req["resource_items"].([]any)
		if !ok || len(items) != 2 {
			t.Fatalf("unexpected resource items: %#v", req["resource_items"])
		}
		writeJSON(t, w, map[string]any{
			"ret": "0",
			"resource_items": []any{
				map[string]any{
					"resource_id":   "stored-first-id",
					"resource_type": "image",
					"path":          firstImagePath,
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
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        1,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        1,
				AgentScene:   1,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadImageFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return &SingleUploadRes{
			ResourceID: "uploaded-" + base + "-id",
			StoreURI:   "image/" + filepath.Base(path),
		}, nil
	}

	_, err = client.UploadResource(context.Background(), &mcpclient.Session{}, "image", []string{firstImagePath, secondImagePath})
	if err == nil {
		t.Fatalf("expected UploadResource to fail on partial real resource_store result")
	}
	if !strings.Contains(err.Error(), "resource store returned partial stored result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadResourceFailsWhenInjectedResourceStoreReturnsPartialStoredItems(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	firstImagePath := filepath.Join(tmpDir, "first.png")
	secondImagePath := filepath.Join(tmpDir, "second.png")
	if err := os.WriteFile(firstImagePath, []byte("first-bytes"), 0o644); err != nil {
		t.Fatalf("write first temp image: %v", err)
	}
	if err := os.WriteFile(secondImagePath, []byte("second-bytes"), 0o644); err != nil {
		t.Fatalf("write second temp image: %v", err)
	}

	client := New()
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        1,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        1,
				AgentScene:   1,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadImageFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return &SingleUploadRes{
			ResourceID: "uploaded-" + base + "-id",
			StoreURI:   "image/" + filepath.Base(path),
		}, nil
	}
	client.resourceStoreFunc = func(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error) {
		return &resourceStoreResult{
			Stored: []*Resource{
				{
					ResourceID:   "stored-first-id",
					ResourceType: "image",
					Path:         firstImagePath,
				},
			},
		}, nil
	}

	_, err := client.UploadResource(context.Background(), &mcpclient.Session{}, "image", []string{firstImagePath, secondImagePath})
	if err == nil {
		t.Fatalf("expected UploadResource to fail on partial injected resource_store result")
	}
	if !strings.Contains(err.Error(), "resource store returned partial stored result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadResourceFailsWhenInjectedResourceStoreReturnsUnexpectedStoredCount(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "sample.png")
	if err := os.WriteFile(imagePath, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	client := New()
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        1,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        1,
				AgentScene:   1,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadImageFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		return &SingleUploadRes{
			ResourceID: "uploaded-image-id",
			StoreURI:   "image/sample.png",
		}, nil
	}
	client.resourceStoreFunc = func(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error) {
		return &resourceStoreResult{
			Stored: []*Resource{
				{
					ResourceID:   "stored-image-id-1",
					ResourceType: "image",
					Path:         imagePath,
				},
				{
					ResourceID:   "stored-image-id-2",
					ResourceType: "image",
					Path:         imagePath,
				},
			},
		}, nil
	}

	_, err := client.UploadResource(context.Background(), &mcpclient.Session{}, "image", []string{imagePath})
	if err == nil {
		t.Fatalf("expected UploadResource to fail on unexpected injected resource_store result count")
	}
	if !strings.Contains(err.Error(), "resource store returned unexpected stored result count") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetUploadTokenRejectsBackendFailure(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"ret": "1001",
			"msg": "denied",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	_, err = client.getUploadToken(context.Background(), &mcpclient.Session{}, "image")
	if err == nil {
		t.Fatalf("expected getUploadToken failure")
	}
	if !strings.Contains(err.Error(), "get upload token failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseUploadTokenDataCollectsStoreInfosAndAuthAliases(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"scene":         3,
		"resource_type": "audio",
		"extra": map[string]any{
			"upload_auth": "auth-from-extra",
			"StoreInfos": []any{
				map[string]any{
					"store_uri":     "nested/audio-track.mp3",
					"upload_domain": "https://upload.example.com",
				},
				map[string]any{
					"store_key": "nested/audio-track-backup.mp3",
				},
			},
			"tos_headers": `{"X-Test":"nested"}`,
			"region":      "cn-north-1",
		},
	}, "audio")

	if data == nil {
		t.Fatalf("expected parsed token data")
	}
	if data.UploadAuth != "auth-from-extra" {
		t.Fatalf("unexpected upload auth: %q", data.UploadAuth)
	}
	if data.SessionKey != "auth-from-extra" {
		t.Fatalf("unexpected session key fallback: %q", data.SessionKey)
	}
	if data.UploadDomain != "https://upload.example.com" {
		t.Fatalf("unexpected upload domain: %q", data.UploadDomain)
	}
	if data.StoreURI != "nested/audio-track.mp3" {
		t.Fatalf("unexpected store uri: %q", data.StoreURI)
	}
	if len(data.StoreInfos) != 2 {
		t.Fatalf("unexpected store infos count: %d", len(data.StoreInfos))
	}
	if len(data.StoreKeys) != 2 {
		t.Fatalf("unexpected store keys count: %d", len(data.StoreKeys))
	}
	if data.StoreKeys[0] != "nested/audio-track.mp3" || data.StoreKeys[1] != "nested/audio-track-backup.mp3" {
		t.Fatalf("unexpected store keys: %#v", data.StoreKeys)
	}
	if data.TosHeaders != `{"X-Test":"nested"}` {
		t.Fatalf("unexpected tos headers: %q", data.TosHeaders)
	}
	if data.Region != "cn-north-1" {
		t.Fatalf("unexpected region: %q", data.Region)
	}
}

func TestParseUploadTokenDataCollectsImageXSTSAndDefaults(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"resource_type":     "image",
		"region":            "cn",
		"access_key_id":     "ak-test",
		"secret_access_key": "sk-test",
		"session_token":     "sts-test",
		"space_name":        "",
		"upload_domain":     "",
	}, "image")

	if data == nil {
		t.Fatalf("expected parsed token data")
	}
	if data.AccessKeyID != "ak-test" || data.SecretAccessKey != "sk-test" || data.SessionToken != "sts-test" {
		t.Fatalf("unexpected imagex sts fields: %#v", data)
	}
	if data.UploadDomain != defaultDreaminaImageXHost {
		t.Fatalf("unexpected imagex upload domain: %q", data.UploadDomain)
	}
	if data.SpaceName != defaultDreaminaImageXSpaceName || data.ServiceID != defaultDreaminaImageXSpaceName {
		t.Fatalf("unexpected imagex service/space fallback: %#v", data)
	}
}

func TestParseUploadTokenDataCollectsVODSTSAndDefaults(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"resource_type":     "video",
		"region":            "cn",
		"access_key_id":     "ak-test",
		"secret_access_key": "sk-test",
		"session_token":     "sts-test",
		"store_uri":         "dreamina/demo.mp4",
		"space_name":        "",
		"upload_domain":     "",
	}, "video")

	if data == nil {
		t.Fatalf("expected parsed token data")
	}
	if data.AccessKeyID != "ak-test" || data.SecretAccessKey != "sk-test" || data.SessionToken != "sts-test" {
		t.Fatalf("unexpected vod sts fields: %#v", data)
	}
	if data.UploadDomain != defaultDreaminaVODHost {
		t.Fatalf("unexpected vod upload domain: %q", data.UploadDomain)
	}
	if data.SpaceName != defaultDreaminaVODSpaceName || data.ServiceID != defaultDreaminaVODSpaceName {
		t.Fatalf("unexpected vod service/space fallback: %#v", data)
	}
}

func TestBuildImageXUploadConfigSupportsStoreURIFallback(t *testing.T) {
	t.Helper()

	cfg, err := buildImageXUploadConfig(&uploadTokenData{
		ResourceType:    "image",
		Region:          "cn",
		AccessKeyID:     "ak-test",
		SecretAccessKey: "sk-test",
		SessionToken:    "sts-test",
		StoreURI:        "tos-cn-i-space-from-uri/example.png",
	}, "example.png")
	if err != nil {
		t.Fatalf("buildImageXUploadConfig failed: %v", err)
	}
	if cfg.Region != defaultDreaminaImageXRegion {
		t.Fatalf("unexpected region: %q", cfg.Region)
	}
	if cfg.APIHost != defaultDreaminaImageXHost {
		t.Fatalf("unexpected api host: %q", cfg.APIHost)
	}
	if cfg.UploadHost != "" {
		t.Fatalf("unexpected upload host: %q", cfg.UploadHost)
	}
	if cfg.ServiceID != "space-from-uri" {
		t.Fatalf("unexpected service id: %q", cfg.ServiceID)
	}
}

func TestUploadImageUsesImageXSDKFlow(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "sample.png")
	if err := os.WriteFile(imagePath, []byte("png-sdk-bytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	fakeClient := &fakeImageXClient{
		resp: &imageXCommitUploadResult{
			RequestID: "req-imagex-1",
			Results: []imageXCommitResult{
				{
					URI:       "tos-cn-i-tb4s082cfz/sdk-upload.png",
					URIStatus: 2000,
				},
			},
			ImageInfos: []imageXImageInfo{
				{
					ImageURI:    "tos-cn-i-tb4s082cfz/sdk-upload.png",
					ImageWidth:  1024,
					ImageHeight: 768,
					ImageFormat: "png",
				},
			},
		},
	}

	client := New()
	var gotRegion string
	client.newImageXClientFunc = func(region string) imageXUploader {
		gotRegion = region
		return fakeClient
	}

	got, err := client.uploadImage(context.Background(), &uploadTokenData{
		ResourceType:    "image",
		Region:          "cn",
		AccessKeyID:     "ak-test",
		SecretAccessKey: "sk-test",
		SessionToken:    "sts-test",
	}, imagePath)
	if err != nil {
		t.Fatalf("uploadImage failed: %v", err)
	}
	if gotRegion != defaultDreaminaImageXRegion {
		t.Fatalf("unexpected sdk region: %q", gotRegion)
	}
	if fakeClient.accessKey != "ak-test" || fakeClient.secretKey != "sk-test" || fakeClient.sessionToken != "sts-test" {
		t.Fatalf("unexpected sdk credentials: %#v", fakeClient)
	}
	if fakeClient.host != defaultDreaminaImageXHost {
		t.Fatalf("unexpected sdk api host: %q", fakeClient.host)
	}
	if fakeClient.params == nil {
		t.Fatalf("expected upload params to be captured")
	}
	if fakeClient.params.ServiceID != defaultDreaminaImageXSpaceName {
		t.Fatalf("unexpected service id: %q", fakeClient.params.ServiceID)
	}
	if fakeClient.params.UploadHost != "" {
		t.Fatalf("unexpected upload host: %q", fakeClient.params.UploadHost)
	}
	if !fakeClient.params.Overwrite {
		t.Fatalf("expected overwrite to be enabled")
	}
	if len(fakeClient.images) != 1 || string(fakeClient.images[0]) != "png-sdk-bytes" {
		t.Fatalf("unexpected upload payload: %#v", fakeClient.images)
	}
	if got == nil || got.StoreURI != "tos-cn-i-tb4s082cfz/sdk-upload.png" {
		t.Fatalf("unexpected upload result: %#v", got)
	}
	if got.UploadDomain != defaultDreaminaImageXHost {
		t.Fatalf("unexpected upload domain: %q", got.UploadDomain)
	}
}

func TestParseUploadTokenResponseSupportsUppercaseWrapperShapes(t *testing.T) {
	t.Helper()

	got, err := parseUploadTokenResponse([]byte(`{
		"Ret": "0",
		"Payload": {
			"Scene": 3,
			"resource_type": "video",
			"UploadDomain": "https://upload.example.com",
			"StoreInfos": [
				{
					"store_uri": "video/sample.mp4"
				}
			],
			"upload_token": "token-1"
		}
	}`), "video")
	if err != nil {
		t.Fatalf("parseUploadTokenResponse failed: %v", err)
	}
	if got == nil || got.Data == nil {
		t.Fatalf("expected parsed upload token response")
	}
	if got.Data.UploadDomain != "https://upload.example.com" {
		t.Fatalf("unexpected upload domain: %#v", got.Data.UploadDomain)
	}
	if got.Data.StoreURI != "video/sample.mp4" {
		t.Fatalf("unexpected store uri: %#v", got.Data.StoreURI)
	}
	if got.Data.UploadAuth != "token-1" || got.Data.SessionKey != "token-1" {
		t.Fatalf("unexpected auth/session key: %#v", got.Data)
	}
}

func TestParseUploadTokenResponseSupportsRootLevelAndSingularStoreInfo(t *testing.T) {
	t.Helper()

	got, err := parseUploadTokenResponse([]byte(`{
		"code": "0",
		"resourceType": "audio",
		"extra": {
			"StoreInfo": {
				"StoreURI": "audio/root-track.mp3",
				"UploadDomain": "https://upload-root.example.com"
			},
			"nested": {
				"sessionKey": "session-root-1",
				"ServiceID": "service-root-1",
				"SpaceName": "space-root-1"
			}
		},
		"UploadToken": "token-root-1"
	}`), "audio")
	if err != nil {
		t.Fatalf("parseUploadTokenResponse failed: %v", err)
	}
	if got == nil || got.Data == nil {
		t.Fatalf("expected parsed upload token response")
	}
	if got.Data.UploadAuth != "token-root-1" {
		t.Fatalf("unexpected upload auth: %#v", got.Data.UploadAuth)
	}
	if got.Data.SessionKey != "session-root-1" {
		t.Fatalf("unexpected session key: %#v", got.Data.SessionKey)
	}
	if got.Data.ServiceID != "service-root-1" || got.Data.SpaceName != "space-root-1" {
		t.Fatalf("unexpected service metadata: %#v", got.Data)
	}
	if got.Data.StoreURI != "audio/root-track.mp3" || got.Data.UploadDomain != "https://upload-root.example.com" {
		t.Fatalf("unexpected store info: %#v", got.Data)
	}
	if len(got.Data.StoreInfos) != 1 {
		t.Fatalf("unexpected store infos: %#v", got.Data.StoreInfos)
	}
}

func TestParseUploadTokenResponseSupportsNestedMetaMetadata(t *testing.T) {
	t.Helper()

	got, err := parseUploadTokenResponse([]byte(`{
		"Payload": {
			"Meta": {
				"Ret": "0",
				"Msg": "token-meta-ok"
			},
			"Data": {
				"UploadToken": "token-meta-1",
				"StoreInfo": {
					"StoreURI": "audio/meta-track.mp3",
					"UploadDomain": "https://upload-meta.example.com"
				}
			}
		}
	}`), "audio")
	if err != nil {
		t.Fatalf("parseUploadTokenResponse failed: %v", err)
	}
	if got == nil || got.Data == nil {
		t.Fatalf("expected parsed upload token response")
	}
	if got.Ret != "0" || got.Msg != "token-meta-ok" || got.Message != "token-meta-ok" {
		t.Fatalf("unexpected nested meta metadata: %#v", got)
	}
	if got.Data.UploadAuth != "token-meta-1" || got.Data.StoreURI != "audio/meta-track.mp3" || got.Data.UploadDomain != "https://upload-meta.example.com" {
		t.Fatalf("unexpected nested meta token data: %#v", got.Data)
	}
}

func TestParseUploadTokenDataSupportsUpperCamelExtraAliases(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"scene":         3,
		"resource_type": "audio",
		"extra": map[string]any{
			"UploadToken":    "token-extra-1",
			"TosHeaders":     `{"X-Test":"upper"}`,
			"TosMeta":        `{"trace_id":"trace-upper-1"}`,
			"UploadDomain":   "https://upload-upper.example.com",
			"StoreURI":       "audio/upper-track.mp3",
			"ServiceID":      "service-upper-1",
			"SpaceName":      "space-upper-1",
			"Bucket":         "bucket-upper-1",
			"Buckets":        []any{"bucket-upper-1", "bucket-upper-2"},
			"Region":         "cn-upper-1",
			"IDC":            "lf",
			"InVolcanoCloud": true,
		},
	}, "audio")

	if data == nil {
		t.Fatalf("expected parsed token data")
	}
	if data.UploadAuth != "token-extra-1" || data.SessionKey != "token-extra-1" {
		t.Fatalf("unexpected auth/session key: %#v", data)
	}
	if data.TosHeaders != `{"X-Test":"upper"}` {
		t.Fatalf("unexpected tos headers: %q", data.TosHeaders)
	}
	if data.TosMeta != `{"trace_id":"trace-upper-1"}` {
		t.Fatalf("unexpected tos meta: %q", data.TosMeta)
	}
	if data.UploadDomain != "https://upload-upper.example.com" || data.StoreURI != "audio/upper-track.mp3" {
		t.Fatalf("unexpected upload domain/store uri: %#v", data)
	}
	if data.ServiceID != "service-upper-1" || data.SpaceName != "space-upper-1" {
		t.Fatalf("unexpected service info: %#v", data)
	}
	if data.Bucket != "bucket-upper-1" || !reflect.DeepEqual(data.Buckets, []string{"bucket-upper-1", "bucket-upper-2"}) {
		t.Fatalf("unexpected bucket info: %#v", data)
	}
	if data.Region != "cn-upper-1" || data.IDC != "lf" || !data.InVolcanoCloud {
		t.Fatalf("unexpected region/idc cloud flags: %#v", data)
	}
}

func TestParseUploadTokenDataSupportsUppercaseExtraWrapper(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"scene":         1,
		"resource_type": "image",
		"Extra": map[string]any{
			"StoreInfo": map[string]any{
				"StoreURI":     "image/extra-wrapper.png",
				"UploadDomain": "https://upload-extra-wrapper.example.com",
			},
			"UploadToken": "token-extra-wrapper-1",
		},
	}, "image")

	if data == nil {
		t.Fatalf("expected parsed token data")
	}
	if data.UploadAuth != "token-extra-wrapper-1" || data.SessionKey != "token-extra-wrapper-1" {
		t.Fatalf("unexpected auth/session key: %#v", data)
	}
	if data.StoreURI != "image/extra-wrapper.png" || data.UploadDomain != "https://upload-extra-wrapper.example.com" {
		t.Fatalf("unexpected store info from Extra wrapper: %#v", data)
	}
	if len(data.StoreInfos) != 1 {
		t.Fatalf("unexpected store infos from Extra wrapper: %#v", data.StoreInfos)
	}
}

func TestParseUploadTokenDataSupportsNestedKeyedStoreInfosWrappers(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"scene":         3,
		"resource_type": "audio",
		"Extra": map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"StoreInfos": map[string]any{
						"primary": map[string]any{
							"StoreURI":     "audio/keyed-track.mp3",
							"UploadDomain": "https://upload-keyed.example.com",
						},
						"backup": map[string]any{
							"StoreKey": "audio/keyed-track-backup.mp3",
						},
					},
					"SessionKey": "session-keyed-1",
				},
			},
		},
	}, "audio")

	if data == nil {
		t.Fatalf("expected parsed token data")
	}
	if data.SessionKey != "session-keyed-1" {
		t.Fatalf("unexpected session key: %#v", data.SessionKey)
	}
	if data.UploadDomain != "https://upload-keyed.example.com" {
		t.Fatalf("unexpected upload domain: %#v", data.UploadDomain)
	}
	if data.StoreURI != "audio/keyed-track.mp3" {
		t.Fatalf("unexpected store uri: %#v", data.StoreURI)
	}
	if len(data.StoreInfos) != 2 {
		t.Fatalf("unexpected keyed store infos: %#v", data.StoreInfos)
	}
	storeKeys := append([]string(nil), data.StoreKeys...)
	slices.Sort(storeKeys)
	if !reflect.DeepEqual(storeKeys, []string{"audio/keyed-track-backup.mp3", "audio/keyed-track.mp3"}) {
		t.Fatalf("unexpected keyed store keys: %#v", data.StoreKeys)
	}
}

func TestParseUploadTokenDataKeyedStoreInfosOrderIsStable(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"scene":         1,
		"resource_type": "image",
		"Extra": map[string]any{
			"StoreInfos": map[string]any{
				"zeta": map[string]any{
					"StoreURI": "image/zeta.png",
				},
				"alpha": map[string]any{
					"StoreURI": "image/alpha.png",
				},
			},
		},
	}, "image")

	if data == nil || len(data.StoreInfos) != 2 {
		t.Fatalf("expected keyed store infos: %#v", data)
	}
	if data.StoreInfos[0] == nil || data.StoreInfos[1] == nil {
		t.Fatalf("unexpected keyed store infos payload: %#v", data.StoreInfos)
	}
	if data.StoreInfos[0].StoreURI != "image/alpha.png" || data.StoreInfos[1].StoreURI != "image/zeta.png" {
		t.Fatalf("expected keyed store infos order to be stable, got %#v", data.StoreInfos)
	}
}

func TestUploadTokenDataForUploadUsesStableKeyedStoreInfoOrder(t *testing.T) {
	t.Helper()

	data := parseUploadTokenData(map[string]any{
		"scene":         1,
		"resource_type": "image",
		"Extra": map[string]any{
			"StoreInfos": map[string]any{
				"b": map[string]any{
					"StoreURI":     "image/second.png",
					"UploadDomain": "https://upload-second.example.com",
				},
				"a": map[string]any{
					"StoreURI":     "image/first.png",
					"UploadDomain": "https://upload-first.example.com",
				},
			},
		},
	}, "image")

	if data == nil {
		t.Fatalf("expected parsed upload token data")
	}
	second := data.forUpload("/tmp/second.png", 1)
	if second == nil {
		t.Fatalf("expected narrowed token")
	}
	if second.StoreURI != "image/second.png" {
		t.Fatalf("unexpected narrowed store uri: %#v", second)
	}
	if second.UploadDomain != "https://upload-second.example.com" {
		t.Fatalf("unexpected narrowed upload domain: %#v", second)
	}
	if len(second.StoreInfos) != 1 || second.StoreInfos[0] == nil || second.StoreInfos[0].StoreURI != "image/second.png" {
		t.Fatalf("unexpected narrowed keyed store infos: %#v", second.StoreInfos)
	}
}

func TestParseResourceStoreResponseSupportsUppercaseShapes(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Result": {
			"Stored": [
				{
					"ResourceId": "rid-1",
					"ResourceType": "image",
					"StoreURI": "image/result.png",
					"MimeType": "image/png",
					"Name": "result.png"
				}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed resource store response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-1" {
		t.Fatalf("unexpected resource id: %#v", got.Data.Stored[0])
	}
	if got.Data.Stored[0].Path != "image/result.png" || got.Data.Stored[0].MimeType != "image/png" {
		t.Fatalf("unexpected stored resource payload: %#v", got.Data.Stored[0])
	}
}

func TestParseResourceStoreResponseSupportsRootLevelSingleResource(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Code": "0",
		"Resource": {
			"ResourceId": "rid-root-1",
			"ResourceType": "audio",
			"StoreURI": "audio/root.mp3",
			"MimeType": "audio/mpeg"
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed resource store response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-root-1" || got.Data.Stored[0].Path != "audio/root.mp3" {
		t.Fatalf("unexpected stored resource: %#v", got.Data.Stored[0])
	}
}

func TestParseResourceStoreResponseSupportsRootResourceItems(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"ret": "0",
		"msg": "success",
		"resource_items": [
			{
				"resource_id": "rid-root-items-1",
				"resource_type": "image",
				"resource_value": "tos-cn-i-space/root-items.png"
			}
		]
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed root resource_items response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-root-items-1" || got.Data.Stored[0].Path != "tos-cn-i-space/root-items.png" {
		t.Fatalf("unexpected root resource_items payload: %#v", got.Data.Stored[0])
	}
}

func TestParseResourceStoreResponseRejectsStoreURIOnlyResource(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Code": "0",
		"Resource": {
			"StoreURI": "audio/store-only.mp3",
			"MimeType": "audio/mpeg"
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil {
		t.Fatalf("expected parsed resource store response shell: %#v", got)
	}
	if len(got.Data.Stored) != 0 {
		t.Fatalf("expected store-uri-only resource to be rejected, got %#v", got.Data.Stored)
	}
}

func TestParseResourceStoreResponseSupportsNestedStoredWrappers(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Result": {
			"Data": {
				"Stored": [
					{
						"ResourceId": "rid-nested-1",
						"ResourceType": "image",
						"StoreURI": "image/nested.png",
						"MimeType": "image/png"
					}
				]
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed nested resource store response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-nested-1" || got.Data.Stored[0].Path != "image/nested.png" {
		t.Fatalf("unexpected nested stored resource: %#v", got.Data.Stored[0])
	}
}

func TestParseResourceStoreResponseSupportsWrappedStoredListItems(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Result": {
			"Stored": [
				{
					"Data": {
						"ResourceId": "rid-wrapped-list-1",
						"ResourceType": "image",
						"StoreURI": "image/wrapped-list.png",
						"MimeType": "image/png"
					}
				}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed wrapped stored list response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-wrapped-list-1" || got.Data.Stored[0].Path != "image/wrapped-list.png" {
		t.Fatalf("unexpected wrapped stored list resource: %#v", got.Data.Stored[0])
	}
}

func TestParseResourceStoreResponseSupportsKeyedStoredShapes(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Result": {
			"Stored": {
				"primary": {
					"ResourceID": "rid-keyed-1",
					"ResourceType": "image",
					"StoreURI": "image/keyed-1.png",
					"MimeType": "image/png"
				},
				"secondary": {
					"ResourceID": "rid-keyed-2",
					"ResourceType": "image",
					"StoreURI": "image/keyed-2.png",
					"MimeType": "image/png"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 2 {
		t.Fatalf("expected parsed keyed stored response: %#v", got)
	}
	ids := []string{got.Data.Stored[0].ResourceID, got.Data.Stored[1].ResourceID}
	slices.Sort(ids)
	if !reflect.DeepEqual(ids, []string{"rid-keyed-1", "rid-keyed-2"}) {
		t.Fatalf("unexpected keyed stored ids: %#v", got.Data.Stored)
	}
}

func TestParseResourceStoreResponseSupportsWrappedKeyedStoredItems(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Result": {
			"Stored": {
				"primary": {
					"Payload": {
						"ResourceID": "rid-keyed-wrapped-1",
						"ResourceType": "image",
						"StoreURI": "image/keyed-wrapped-1.png"
					}
				},
				"secondary": {
					"Data": {
						"ResourceID": "rid-keyed-wrapped-2",
						"ResourceType": "image",
						"StoreURI": "image/keyed-wrapped-2.png"
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 2 {
		t.Fatalf("expected parsed wrapped keyed stored response: %#v", got)
	}
	ids := []string{got.Data.Stored[0].ResourceID, got.Data.Stored[1].ResourceID}
	slices.Sort(ids)
	if !reflect.DeepEqual(ids, []string{"rid-keyed-wrapped-1", "rid-keyed-wrapped-2"}) {
		t.Fatalf("unexpected wrapped keyed stored ids: %#v", got.Data.Stored)
	}
}

func TestParseResourceStoreResponseKeyedStoredOrderIsStable(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Result": {
			"Stored": {
				"zeta": {
					"ResourceID": "rid-zeta",
					"ResourceType": "image",
					"StoreURI": "image/zeta.png"
				},
				"alpha": {
					"ResourceID": "rid-alpha",
					"ResourceType": "image",
					"StoreURI": "image/alpha.png"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 2 {
		t.Fatalf("expected parsed keyed stored response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-alpha" || got.Data.Stored[1].ResourceID != "rid-zeta" {
		t.Fatalf("expected keyed stored order to be stable, got %#v", got.Data.Stored)
	}
}

func TestMergeStoredResourcesMatchesByStoreURIWhenReturnedOutOfOrder(t *testing.T) {
	t.Helper()

	original := []*Resource{
		{
			ResourceID:   "uploaded-alpha-id",
			ResourceType: "image",
			Path:         "/tmp/alpha.png",
			Name:         "alpha.png",
			Size:         11,
			Kind:         "image",
			MimeType:     "image/png",
			UploadSummary: map[string]any{
				"store_uri": "image/alpha.png",
			},
		},
		{
			ResourceID:   "uploaded-beta-id",
			ResourceType: "image",
			Path:         "/tmp/beta.png",
			Name:         "beta.png",
			Size:         22,
			Kind:         "image",
			MimeType:     "image/png",
			UploadSummary: map[string]any{
				"store_uri": "image/beta.png",
			},
		},
	}
	stored := []*Resource{
		{
			ResourceID:   "stored-beta-id",
			ResourceType: "image",
			Path:         "image/beta.png",
		},
		{
			ResourceID:   "stored-alpha-id",
			ResourceType: "image",
			Path:         "image/alpha.png",
		},
	}

	got := mergeStoredResources(stored, original)
	if len(got) != 2 {
		t.Fatalf("unexpected merged count: %#v", got)
	}
	if got[0].Name != "beta.png" || got[1].Name != "alpha.png" {
		t.Fatalf("expected names to follow store uri match, got %#v", got)
	}
	if got[0].Size != 22 || got[1].Size != 11 {
		t.Fatalf("expected sizes to follow store uri match, got %#v", got)
	}
	if got[0].MimeType != "image/png" || got[1].MimeType != "image/png" {
		t.Fatalf("expected mime types to be merged from matching originals, got %#v", got)
	}
}

func TestBuildResourceStoreRequestUsesResourceItemsShape(t *testing.T) {
	t.Helper()

	got := buildResourceStoreRequest([]*Resource{
		{
			ResourceID:   "tos-cn-i-space/example.png",
			ResourceType: "image",
			Path:         "/tmp/example.png",
			Name:         "example.png",
		},
	})
	if got == nil || len(got.ResourceItems) != 1 {
		t.Fatalf("expected resource_items payload: %#v", got)
	}
	if got.ResourceItems[0].ResourceType != "image" || got.ResourceItems[0].ResourceValue != "tos-cn-i-space/example.png" {
		t.Fatalf("unexpected resource_items payload: %#v", got.ResourceItems[0])
	}
}

func TestParseResourceStoreResponseSupportsDeepSingleResourceWrapper(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Payload": {
			"Result": {
				"Resource": {
					"ResourceID": "rid-deep-1",
					"ResourceType": "image",
					"StoreURI": "image/deep.png",
					"MimeType": "image/png"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed deep resource store response: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-deep-1" || got.Data.Stored[0].Path != "image/deep.png" {
		t.Fatalf("unexpected deep stored resource: %#v", got.Data.Stored[0])
	}
}

func TestParseResourceStoreResponseSupportsNestedMetaMetadata(t *testing.T) {
	t.Helper()

	got, err := parseResourceStoreResponse([]byte(`{
		"Payload": {
			"Meta": {
				"Code": "0",
				"Message": "store-meta-ok"
			},
			"Result": {
				"Stored": [
					{
						"ResourceID": "rid-meta-1",
						"ResourceType": "image",
						"StoreURI": "image/meta.png",
						"MimeType": "image/png"
					}
				]
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseResourceStoreResponse failed: %v", err)
	}
	if got == nil || got.Data == nil || len(got.Data.Stored) != 1 {
		t.Fatalf("expected parsed resource store response: %#v", got)
	}
	if got.Code != "0" || got.Message != "store-meta-ok" || got.Msg != "store-meta-ok" {
		t.Fatalf("unexpected nested meta metadata: %#v", got)
	}
	if got.Data.Stored[0].ResourceID != "rid-meta-1" || got.Data.Stored[0].Path != "image/meta.png" {
		t.Fatalf("unexpected meta stored resource: %#v", got.Data.Stored[0])
	}
}

func TestUploadResourceAudioFlow(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	var (
		sawTransfer      bool
		serverURL        string
		preStoreResource string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mweb/v1/get_upload_token":
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"data": map[string]any{
					"scene":         3,
					"resource_type": "audio",
					"extra": map[string]any{
						"upload_auth": "vod-auth-token",
						"StoreInfos": []any{
							map[string]any{
								"store_uri":     "audio/sample.mp3",
								"upload_domain": serverURL,
							},
						},
						"tos_headers": `{"X-Test-Upload":"audio"}`,
					},
				},
			})
		case r.URL.Path == "/audio/sample.mp3":
			switch r.URL.Query().Get("phase") {
			case "init":
				writeJSON(t, w, map[string]any{"data": map[string]any{"uploadID": "audio-u-1"}})
			case "transfer":
				if got := r.Method; got != http.MethodPut {
					t.Fatalf("unexpected transfer method: %s", got)
				}
				if got := r.Header.Get("Content-Type"); got != "audio/mpeg" {
					t.Fatalf("unexpected transfer content-type: %q", got)
				}
				if got := r.Header.Get("X-Test-Upload"); got != "audio" {
					t.Fatalf("unexpected transfer header: %q", got)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read transfer body: %v", err)
				}
				if string(body) != "audio-bytes" {
					t.Fatalf("unexpected transfer body: %q", string(body))
				}
				sawTransfer = true
				w.WriteHeader(http.StatusOK)
			case "finish":
				writeJSON(t, w, map[string]any{
					"data": map[string]any{
						"resource_id": "vod-uploaded-audio-id",
						"store_uri":   "audio/sample.mp3",
					},
				})
			default:
				t.Fatalf("unexpected upload phase: %q", r.URL.RawQuery)
			}
		case r.URL.Path == "/dreamina/mcp/v1/resource_store":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resource_store request: %v", err)
			}
			resourceItems, ok := req["resource_items"].([]any)
			if !ok || len(resourceItems) != 1 {
				t.Fatalf("unexpected resource payload: %#v", req["resource_items"])
			}
			item, ok := resourceItems[0].(map[string]any)
			if !ok {
				t.Fatalf("unexpected resource item: %#v", resourceItems[0])
			}
			preStoreResource = strings.TrimSpace(item["resource_value"].(string))
			if preStoreResource != "vod-uploaded-audio-id" {
				t.Fatalf("unexpected pre-store resource_id: %q", preStoreResource)
			}
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"resource_items": []map[string]any{
					{
						"resource_id":   "stored-audio-id",
						"resource_type": "audio",
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)
	client.probeDurationFunc = func(ctx context.Context, path string) (float64, error) {
		return 5, nil
	}

	got, err := client.UploadResource(context.Background(), &mcpclient.Session{Cookie: "sid=test"}, "audio", []string{audioPath})
	if err != nil {
		t.Fatalf("UploadResource failed: %v", err)
	}
	if !sawTransfer {
		t.Fatalf("expected transfer phase to run")
	}
	if preStoreResource != "vod-uploaded-audio-id" {
		t.Fatalf("unexpected captured pre-store resource id: %q", preStoreResource)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected result count: %d", len(got))
	}
	if got[0].ResourceID != "stored-audio-id" {
		t.Fatalf("unexpected stored resource id: %q", got[0].ResourceID)
	}
	if got[0].MimeType != "audio/mpeg" {
		t.Fatalf("unexpected stored mime type: %q", got[0].MimeType)
	}
}

func TestUploadVideoAudioAnnotatesVODFallbackMetadata(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("phase") {
		case "init":
			writeJSON(t, w, map[string]any{"uploadID": "audio-u-1"})
		case "transfer":
			w.WriteHeader(http.StatusOK)
		case "finish":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"resource_id": "vod-uploaded-audio-id",
					"store_uri":   "audio/sample.mp3",
					"meta": map[string]any{
						"duration": 6.2,
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.uploadVideoAudio(context.Background(), &uploadTokenData{
		ResourceType:   "audio",
		UploadAuth:     "auth-token",
		SessionKey:     "session-key",
		ServiceID:      "service-id",
		SpaceName:      "space-name",
		Bucket:         "bucket-audio-primary",
		Buckets:        []string{"bucket-audio-primary", "bucket-audio-backup"},
		Region:         "cn-north-1",
		IDC:            "lf",
		InVolcanoCloud: true,
		UploadDomain:   serverURL,
		StoreInfos: []*uploadStoreInfo{
			{
				StoreURI:     "audio/sample.mp3",
				UploadDomain: serverURL,
			},
		},
	}, audioPath)
	if err != nil {
		t.Fatalf("uploadVideoAudio() error = %v", err)
	}
	if got == nil || got.Extra == nil {
		t.Fatalf("expected extra metadata")
	}
	if got.Extra["vod_fallback_mode"] != "phase_http" {
		t.Fatalf("unexpected fallback mode: %#v", got.Extra["vod_fallback_mode"])
	}
	vodMeta, ok := got.Extra["vod_upload"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected vod_upload payload: %#v", got.Extra["vod_upload"])
	}
	if vodMeta["file_path"] != audioPath || vodMeta["file_name"] != "sample.mp3" {
		t.Fatalf("unexpected file metadata: %#v", vodMeta)
	}
	if vodMeta["service_id"] != "service-id" || vodMeta["space_name"] != "space-name" {
		t.Fatalf("unexpected vod upload config: %#v", vodMeta)
	}
	if vodMeta["bucket"] != "bucket-audio-primary" || vodMeta["region"] != "cn-north-1" || vodMeta["idc"] != "lf" {
		t.Fatalf("unexpected bucket/region metadata: %#v", vodMeta)
	}
	if buckets, ok := vodMeta["buckets"].([]string); !ok || !reflect.DeepEqual(buckets, []string{"bucket-audio-primary", "bucket-audio-backup"}) {
		t.Fatalf("unexpected buckets metadata: %#v", vodMeta["buckets"])
	}
	if cloud, ok := vodMeta["in_volcano_cloud"].(bool); !ok || !cloud {
		t.Fatalf("unexpected in_volcano_cloud metadata: %#v", vodMeta["in_volcano_cloud"])
	}
	if seconds := extractUploadDurationSeconds(got.Extra); seconds != 6.2 {
		t.Fatalf("unexpected extracted duration: %v", seconds)
	}
}

func TestUploadVideoAudioUsesVODOpenAPIFlow(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "sample.mp4")
	videoBytes := []byte("video-openapi-bytes")
	if err := os.WriteFile(videoPath, videoBytes, 0o644); err != nil {
		t.Fatalf("write temp video: %v", err)
	}

	expectedCRC32 := fmt.Sprintf("%08x", crc32.ChecksumIEEE(videoBytes))
	var (
		sawApply  bool
		sawDirect bool
		sawCommit bool
		serverURL string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/top/v1" && r.URL.Query().Get("Action") == "ApplyUploadInner":
			sawApply = true
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected apply method: %s", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded; charset=utf-8" {
				t.Fatalf("unexpected apply content type: %q", got)
			}
			if got := r.Header.Get("X-Amz-Security-Token"); got != "sts-vod-test" {
				t.Fatalf("unexpected apply session token: %q", got)
			}
			if got := r.Header.Get("Authorization"); !strings.Contains(got, "AWS4-HMAC-SHA256 Credential=ak-vod-test/") || !strings.Contains(got, "/cn-north-1/vod/aws4_request") {
				t.Fatalf("unexpected apply authorization: %q", got)
			}
			writeJSON(t, w, map[string]any{
				"ResponseMetadata": map[string]any{
					"RequestId": "req-apply-1",
				},
				"Result": map[string]any{
					"RequestId": "req-apply-1",
					"UploadAddress": map[string]any{
						"UploadNodes": []any{
							map[string]any{
								"VID":        "vid-node-1",
								"UploadHost": serverURL,
								"SessionKey": "session-node-1",
								"StoreInfos": []any{
									map[string]any{
										"StoreUri": "tos-cn-v-148450/demo.mp4",
										"Auth":     "SpaceKey/dreamina/0/:version:v2:test-auth",
									},
								},
							},
						},
					},
				},
			})
		case r.URL.Path == "/tos-cn-v-148450/demo.mp4":
			sawDirect = true
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected direct upload method: %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "SpaceKey/dreamina/0/:version:v2:test-auth" {
				t.Fatalf("unexpected direct upload authorization: %q", got)
			}
			if got := r.Header.Get("Content-CRC32"); got != expectedCRC32 {
				t.Fatalf("unexpected direct upload crc32: %q", got)
			}
			writeJSON(t, w, map[string]any{
				"code":    0,
				"message": "ok",
				"data": map[string]any{
					"crc32": expectedCRC32,
				},
			})
		case r.URL.Path == "/top/v1" && r.URL.Query().Get("Action") == "CommitUploadInner":
			sawCommit = true
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected commit method: %s", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read commit body: %v", err)
			}
			payload := map[string]any{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode commit body: %v", err)
			}
			if got := strings.TrimSpace(payload["SessionKey"].(string)); got != "session-node-1" {
				t.Fatalf("unexpected commit session key: %q", got)
			}
			writeJSON(t, w, map[string]any{
				"ResponseMetadata": map[string]any{
					"RequestId": "req-commit-1",
				},
				"Result": map[string]any{
					"RequestId": "req-commit-1",
					"Results": []any{
						map[string]any{
							"Vid": "vid-commit-1",
							"Uri": "tos-cn-v-148450/demo.mp4",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.uploadVideoAudio(context.Background(), &uploadTokenData{
		ResourceType:    "video",
		AccessKeyID:     "ak-vod-test",
		SecretAccessKey: "sk-vod-test",
		SessionToken:    "sts-vod-test",
		UploadDomain:    server.URL,
		SpaceName:       "dreamina",
		ServiceID:       "dreamina",
		Region:          "cn",
	}, videoPath)
	if err != nil {
		t.Fatalf("uploadVideoAudio() error = %v", err)
	}
	if !sawApply || !sawDirect || !sawCommit {
		t.Fatalf("expected apply/direct/commit flow, got apply=%v direct=%v commit=%v", sawApply, sawDirect, sawCommit)
	}
	if got.ResourceID != "vid-commit-1" {
		t.Fatalf("unexpected resource id: %q", got.ResourceID)
	}
	if got.StoreURI != "tos-cn-v-148450/demo.mp4" {
		t.Fatalf("unexpected store uri: %q", got.StoreURI)
	}
	if got.UploadDomain != server.URL {
		t.Fatalf("unexpected upload domain: %q", got.UploadDomain)
	}
	if got.Extra["vod_upload_mode"] != "vod_openapi" {
		t.Fatalf("unexpected upload mode: %#v", got.Extra["vod_upload_mode"])
	}
	if got.Extra["request_id"] != "req-commit-1" {
		t.Fatalf("unexpected request id: %#v", got.Extra["request_id"])
	}
}

func TestUploadAudioUsesVODOpenAPIFlowWhenSTSPresent(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	audioBytes := []byte("audio-openapi-bytes")
	if err := os.WriteFile(audioPath, audioBytes, 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	expectedCRC32 := fmt.Sprintf("%08x", crc32.ChecksumIEEE(audioBytes))
	var (
		sawApply  bool
		sawDirect bool
		sawCommit bool
		serverURL string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/top/v1" && r.URL.Query().Get("Action") == "ApplyUploadInner":
			sawApply = true
			if got := r.URL.Query().Get("FileType"); got != "video" {
				t.Fatalf("unexpected apply file type: %q", got)
			}
			writeJSON(t, w, map[string]any{
				"ResponseMetadata": map[string]any{
					"RequestId": "req-apply-audio-1",
				},
				"Result": map[string]any{
					"RequestId": "req-apply-audio-1",
					"UploadAddress": map[string]any{
						"UploadNodes": []any{
							map[string]any{
								"VID":        "audio-node-1",
								"UploadHost": serverURL,
								"SessionKey": "audio-session-1",
								"StoreInfos": []any{
									map[string]any{
										"StoreUri": "tos-cn-v-148450/demo.mp3",
										"Auth":     "SpaceKey/dreamina/0/:version:v2:test-audio-auth",
									},
								},
							},
						},
					},
				},
			})
		case r.URL.Path == "/tos-cn-v-148450/demo.mp3":
			sawDirect = true
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected direct upload method: %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "SpaceKey/dreamina/0/:version:v2:test-audio-auth" {
				t.Fatalf("unexpected direct upload authorization: %q", got)
			}
			if got := r.Header.Get("Content-CRC32"); got != expectedCRC32 {
				t.Fatalf("unexpected direct upload crc32: %q", got)
			}
			if got := r.Header.Get("Specified-Content-Type"); got != "audio/mpeg" {
				t.Fatalf("unexpected direct upload specified content type: %q", got)
			}
			writeJSON(t, w, map[string]any{
				"code":    0,
				"message": "ok",
				"data": map[string]any{
					"crc32": expectedCRC32,
				},
			})
		case r.URL.Path == "/top/v1" && r.URL.Query().Get("Action") == "CommitUploadInner":
			sawCommit = true
			writeJSON(t, w, map[string]any{
				"ResponseMetadata": map[string]any{
					"RequestId": "req-commit-audio-1",
				},
				"Result": map[string]any{
					"RequestId": "req-commit-audio-1",
					"Results": []any{
						map[string]any{
							"Vid": "audio-commit-1",
							"Uri": "tos-cn-v-148450/demo.mp3",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.uploadVideoAudio(context.Background(), &uploadTokenData{
		ResourceType:    "audio",
		AccessKeyID:     "ak-audio-test",
		SecretAccessKey: "sk-audio-test",
		SessionToken:    "sts-audio-test",
		UploadDomain:    server.URL,
		SpaceName:       "dreamina",
		ServiceID:       "dreamina",
		Region:          "cn",
	}, audioPath)
	if err != nil {
		t.Fatalf("uploadVideoAudio() error = %v", err)
	}
	if !sawApply || !sawDirect || !sawCommit {
		t.Fatalf("expected apply/direct/commit flow, got apply=%v direct=%v commit=%v", sawApply, sawDirect, sawCommit)
	}
	if got.ResourceID != "audio-commit-1" {
		t.Fatalf("unexpected resource id: %q", got.ResourceID)
	}
	if got.StoreURI != "tos-cn-v-148450/demo.mp3" {
		t.Fatalf("unexpected store uri: %q", got.StoreURI)
	}
	if got.Extra["vod_upload_mode"] != "vod_openapi" {
		t.Fatalf("unexpected upload mode: %#v", got.Extra["vod_upload_mode"])
	}
}

func TestUploadResourceAudioFlowRejectsStoreURIFallbackResourceID(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mweb/v1/get_upload_token":
			writeJSON(t, w, map[string]any{
				"ret": "0",
				"data": map[string]any{
					"scene":         3,
					"resource_type": "audio",
					"upload_domain": serverURL,
					"store_infos": []any{
						map[string]any{
							"store_uri":     "audio/sample.mp3",
							"upload_domain": serverURL,
						},
					},
				},
			})
		case r.URL.Path == "/audio/sample.mp3":
			switch r.URL.Query().Get("phase") {
			case "init":
				writeJSON(t, w, map[string]any{"uploadID": "audio-fallback-u-1"})
			case "transfer":
				w.WriteHeader(http.StatusOK)
			case "finish":
				// 这里只返回 store_uri，不返回真实 resource_id/vid/file_id。
				writeJSON(t, w, map[string]any{
					"store_uri": "audio/sample.mp3",
				})
			default:
				t.Fatalf("unexpected upload phase: %q", r.URL.RawQuery)
			}
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)
	client.probeDurationFunc = func(ctx context.Context, path string) (float64, error) {
		return 5, nil
	}

	_, err = client.UploadResource(context.Background(), &mcpclient.Session{Cookie: "sid=test"}, "audio", []string{audioPath})
	if err == nil {
		t.Fatalf("expected UploadResource to reject store_uri fallback resource id")
	}
	if !strings.Contains(err.Error(), "missing remote resource_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadResourceAudioFlowCarriesDurationIntoUploadSummary(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "sample.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}

	client := New()
	client.getUploadTokenFunc = func(ctx context.Context, session any, resourceType string) (*uploadTokenResp, error) {
		return &uploadTokenResp{
			Scene:        3,
			ResourceType: resourceType,
			Data: &uploadTokenData{
				Scene:        3,
				AgentScene:   3,
				ResourceType: resourceType,
			},
		}, nil
	}
	client.uploadVideoAudioFunc = func(ctx context.Context, token *uploadTokenData, path string) (*SingleUploadRes, error) {
		return &SingleUploadRes{
			ResourceID:   "uploaded-audio-id",
			UploadDomain: "https://upload.example.com",
			StoreURI:     "audio/sample.mp3",
			UploadID:     "upload-id-1",
			Extra: map[string]any{
				"VideoMeta": map[string]any{
					"DurationSeconds": 6.5,
				},
				"VideoURL":    "https://cdn.example.com/audio.mp3",
				"SnapshotUri": "tos://snapshot/audio-cover.png",
				"cover_url":   "https://cdn.example.com/audio-cover.png",
				"ResponseMetadata": map[string]any{
					"RequestId": "req-001",
					"HostId":    "tos-host-001",
				},
				"statusCode":    200,
				"EC":            "tos-error-signature",
				"DetailErrCode": 10017,
				"ExpectedCodes": []any{200, 204},
				"ResponseErr": map[string]any{
					"Code":      "SignatureMismatch",
					"Message":   "signature mismatch",
					"RequestID": "req-001",
				},
				"vod_fallback_mode": "phase_http",
			},
		}, nil
	}
	client.resourceStoreFunc = func(ctx context.Context, session any, items []*Resource) (*resourceStoreResult, error) {
		if len(items) != 1 {
			t.Fatalf("unexpected item count: %d", len(items))
		}
		if items[0].UploadSummary["duration_seconds"] != 6.5 {
			t.Fatalf("unexpected duration summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["vod_fallback_mode"] != "phase_http" {
			t.Fatalf("unexpected fallback summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["media_url"] != "https://cdn.example.com/audio.mp3" {
			t.Fatalf("unexpected media url summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["cover_url"] != "https://cdn.example.com/audio-cover.png" {
			t.Fatalf("unexpected cover url summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["snapshot_uri"] != "tos://snapshot/audio-cover.png" {
			t.Fatalf("unexpected snapshot uri summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["request_id"] != "req-001" {
			t.Fatalf("unexpected request id summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["status_code"] != 200 {
			t.Fatalf("unexpected status code summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["host_id"] != "tos-host-001" {
			t.Fatalf("unexpected host id summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["ec"] != "tos-error-signature" {
			t.Fatalf("unexpected ec summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["detail_err_code"] != 10017 {
			t.Fatalf("unexpected detail err code summary: %#v", items[0].UploadSummary)
		}
		if items[0].UploadSummary["response_err"] != `{"Code":"SignatureMismatch","Message":"signature mismatch","RequestID":"req-001"}` {
			t.Fatalf("unexpected response err summary: %#v", items[0].UploadSummary)
		}
		if !reflect.DeepEqual(items[0].UploadSummary["expected_codes"], []string{"200", "204"}) {
			t.Fatalf("unexpected expected codes summary: %#v", items[0].UploadSummary)
		}
		return &resourceStoreResult{
			Stored: []*Resource{
				{
					ResourceID:   "stored-audio-id",
					ResourceType: "audio",
				},
			},
		}, nil
	}
	client.probeDurationFunc = func(ctx context.Context, path string) (float64, error) {
		t.Fatalf("probe should not be called when upload metadata contains duration")
		return 0, nil
	}

	got, err := client.UploadResource(ContextWithUploadModelVersion(context.Background(), "seedance2.0"), &mcpclient.Session{}, "audio", []string{audioPath})
	if err != nil {
		t.Fatalf("UploadResource failed: %v", err)
	}
	if len(got) != 1 || got[0].ResourceID != "stored-audio-id" {
		t.Fatalf("unexpected stored result: %#v", got)
	}
}

func TestValidateUploadedMediaDurationUsesUploadMetadataFirst(t *testing.T) {
	t.Helper()

	probeCalled := false
	err := validateUploadedMediaDuration(
		context.Background(),
		"audio",
		"/tmp/sample.mp3",
		"seedance2.0",
		&SingleUploadRes{
			ResourceID: "rid-1",
			Extra: map[string]any{
				"meta": map[string]any{
					"duration": 5.5,
				},
			},
		},
		func(ctx context.Context, path string) (float64, error) {
			probeCalled = true
			return 1, nil
		},
	)
	if err != nil {
		t.Fatalf("validateUploadedMediaDuration() error = %v", err)
	}
	if probeCalled {
		t.Fatalf("expected upload metadata to bypass local probe")
	}
}

func TestValidateUploadedMediaDurationFallsBackToProbe(t *testing.T) {
	t.Helper()

	err := validateUploadedMediaDuration(
		context.Background(),
		"audio",
		"/tmp/sample.mp3",
		"seedance2.0",
		&SingleUploadRes{ResourceID: "rid-1"},
		func(ctx context.Context, path string) (float64, error) {
			return 3.5, nil
		},
	)
	if err == nil {
		t.Fatalf("expected fallback probe validation to fail")
	}
}

func TestParseUploadedResourceSupportsVODWrapperShapes(t *testing.T) {
	t.Helper()

	got := parseUploadedResource([]byte(`{
		"ResponseMetadata": {
			"RequestId": "req-xyz"
		},
		"statusCode": 200,
		"Result": {
			"Vid": "vid-001",
			"SourceInfo": {
				"Duration": 7.25,
				"VideoURL": "https://cdn.example.com/video.mp4",
				"PosterURL": "https://cdn.example.com/video-cover.png",
				"SnapshotUri": "tos://snapshot/video-cover.png"
			},
			"VideoMeta": {
				"DurationSeconds": 7.25
			}
		},
		"UploadID": "upload-001"
	}`), "fallback-store-uri", "fallback-upload-id", "https://upload.example.com")

	if got == nil {
		t.Fatalf("expected parsed upload result")
	}
	if got.ResourceID != "vid-001" {
		t.Fatalf("unexpected resource id: %q", got.ResourceID)
	}
	if got.UploadID != "upload-001" {
		t.Fatalf("unexpected upload id: %q", got.UploadID)
	}
	if seconds := extractUploadDurationSeconds(got.Extra); seconds != 7.25 {
		t.Fatalf("unexpected duration seconds: %v", seconds)
	}
	if mediaURL := extractUploadMediaURL(got.Extra); mediaURL != "https://cdn.example.com/video.mp4" {
		t.Fatalf("unexpected media url: %q", mediaURL)
	}
	if coverURL := extractUploadCoverURL(got.Extra); coverURL != "https://cdn.example.com/video-cover.png" {
		t.Fatalf("unexpected cover url: %q", coverURL)
	}
	if snapshotURI := extractUploadSnapshotURI(got.Extra); snapshotURI != "tos://snapshot/video-cover.png" {
		t.Fatalf("unexpected snapshot uri: %q", snapshotURI)
	}
	if requestID := extractUploadRequestID(got.Extra); requestID != "req-xyz" {
		t.Fatalf("unexpected request id: %q", requestID)
	}
	if statusCode := extractUploadStatusCode(got.Extra); statusCode != 200 {
		t.Fatalf("unexpected status code: %d", statusCode)
	}
}

func TestExtractUploadMediaURLSupportsMediaURLAliases(t *testing.T) {
	t.Helper()

	got := extractUploadMediaURL(map[string]any{
		"Result": map[string]any{
			"SourceInfo": map[string]any{
				"MediaURL": "https://cdn.example.com/media-alias.mp4",
			},
		},
	})
	if got != "https://cdn.example.com/media-alias.mp4" {
		t.Fatalf("unexpected media url alias: %q", got)
	}
}

func TestExtractUploadCoverURLSupportsCoverURIAliases(t *testing.T) {
	t.Helper()

	got := extractUploadCoverURL(map[string]any{
		"Payload": map[string]any{
			"Result": map[string]any{
				"SourceInfo": map[string]any{
					"CoverUri": "https://cdn.example.com/cover-alias.png",
				},
			},
		},
	})
	if got != "https://cdn.example.com/cover-alias.png" {
		t.Fatalf("unexpected cover url alias: %q", got)
	}
}

func TestParseUploadedResourceSupportsErrorWrapperShapes(t *testing.T) {
	t.Helper()

	got := parseUploadedResource([]byte(`{
		"ResponseMetadata": {
			"RequestId": "req-err-1",
			"HostID": "tos-host-err"
		},
		"Error": {
			"ExtendedCode": "AccessDenied",
			"Detail": "signature expired",
			"EC": "tos-expired-signature",
			"DetailErrCode": 40317
		},
		"statusCode": 403
	}`), "fallback-store-uri", "fallback-upload-id", "https://upload.example.com")

	if got == nil || got.Extra == nil {
		t.Fatalf("expected parsed upload result with extra")
	}
	if requestID := extractUploadRequestID(got.Extra); requestID != "req-err-1" {
		t.Fatalf("unexpected request id: %q", requestID)
	}
	if statusCode := extractUploadStatusCode(got.Extra); statusCode != 403 {
		t.Fatalf("unexpected status code: %d", statusCode)
	}
	if errorCode := extractUploadErrorCode(got.Extra); errorCode != "AccessDenied" {
		t.Fatalf("unexpected error code: %q", errorCode)
	}
	if errorMessage := extractUploadErrorMessage(got.Extra); errorMessage != "signature expired" {
		t.Fatalf("unexpected error message: %q", errorMessage)
	}
	if hostID := extractUploadHostID(got.Extra); hostID != "tos-host-err" {
		t.Fatalf("unexpected host id: %q", hostID)
	}
	if ec := extractUploadEC(got.Extra); ec != "tos-expired-signature" {
		t.Fatalf("unexpected ec: %q", ec)
	}
	if detailErrCode := extractUploadDetailErrCode(got.Extra); detailErrCode != 40317 {
		t.Fatalf("unexpected detail err code: %d", detailErrCode)
	}
}

func TestParseUploadedResourceSupportsResponseErrWrapperShapes(t *testing.T) {
	t.Helper()

	got := parseUploadedResource([]byte(`{
		"ResponseMetadata": {
			"RequestId": "req-response-err"
		},
		"ExpectedCodes": [200, 204],
		"ResponseErr": {
			"Code": "InvalidSignature",
			"HostID": "tos-host-response",
			"EC": "tos-signature-mismatch"
		},
		"Error": {
			"Message": "wrapped error message"
		},
		"statusCode": 403
	}`), "fallback-store-uri", "fallback-upload-id", "https://upload.example.com")

	if got == nil || got.Extra == nil {
		t.Fatalf("expected parsed upload result with extra")
	}
	if responseErr := extractUploadResponseErr(got.Extra); responseErr != `{"Code":"InvalidSignature","EC":"tos-signature-mismatch","HostID":"tos-host-response"}` {
		t.Fatalf("unexpected response err: %q", responseErr)
	}
	if expectedCodes := extractUploadExpectedCodes(got.Extra); !reflect.DeepEqual(expectedCodes, []string{"200", "204"}) {
		t.Fatalf("unexpected expected codes: %#v", expectedCodes)
	}
	if errorCode := extractUploadErrorCode(got.Extra); errorCode != "InvalidSignature" {
		t.Fatalf("unexpected error code: %q", errorCode)
	}
	if errorMessage := extractUploadErrorMessage(got.Extra); errorMessage != "wrapped error message" {
		t.Fatalf("unexpected error message: %q", errorMessage)
	}
}

func TestParseUploadedResourceSupportsUpperCamelAliases(t *testing.T) {
	t.Helper()

	got := parseUploadedResource([]byte(`{
		"Payload": {
			"ResourceID": "rid-upper-1",
			"StoreURI": "resource/upper-store.png"
		},
		"UploadId": "upload-upper-1"
	}`), "fallback-store-uri", "fallback-upload-id", "https://upload.example.com")

	if got == nil {
		t.Fatalf("expected parsed upload result")
	}
	if got.ResourceID != "rid-upper-1" {
		t.Fatalf("unexpected resource id: %q", got.ResourceID)
	}
	if got.StoreURI != "resource/upper-store.png" {
		t.Fatalf("unexpected store uri: %q", got.StoreURI)
	}
	if got.UploadID != "upload-upper-1" {
		t.Fatalf("unexpected upload id: %q", got.UploadID)
	}
}

func TestParseUploadedResourceSupportsNestedUploadDomainAlias(t *testing.T) {
	t.Helper()

	got := parseUploadedResource([]byte(`{
		"Result": {
			"Payload": {
				"ResourceID": "rid-domain-1",
				"StoreURI": "resource/domain-store.png",
				"UploadDomain": "https://upload-domain.example.com"
			}
		}
	}`), "fallback-store-uri", "fallback-upload-id", "")

	if got == nil {
		t.Fatalf("expected parsed upload result")
	}
	if got.ResourceID != "rid-domain-1" {
		t.Fatalf("unexpected resource id: %q", got.ResourceID)
	}
	if got.StoreURI != "resource/domain-store.png" {
		t.Fatalf("unexpected store uri: %q", got.StoreURI)
	}
	if got.UploadDomain != "https://upload-domain.example.com" {
		t.Fatalf("unexpected upload domain: %q", got.UploadDomain)
	}
}

func TestValidateVideoAudioDurationHonorsModelRange(t *testing.T) {
	t.Helper()

	if err := validateVideoAudioDuration(
		context.Background(),
		"audio",
		"/tmp/sample.mp3",
		"seedance2.0",
		func(ctx context.Context, path string) (float64, error) {
			return 3.5, nil
		},
	); err == nil {
		t.Fatalf("expected duration range validation to fail")
	}

	if err := validateVideoAudioDuration(
		context.Background(),
		"audio",
		"/tmp/sample.mp3",
		"seedance2.0",
		func(ctx context.Context, path string) (float64, error) {
			return 4.5, nil
		},
	); err != nil {
		t.Fatalf("expected duration range validation to pass, got %v", err)
	}
}

func TestValidateVideoAudioDurationSkipsProbeFailure(t *testing.T) {
	t.Helper()

	if err := validateVideoAudioDuration(
		context.Background(),
		"video",
		"/tmp/sample.mp4",
		"seedance2.0",
		func(ctx context.Context, path string) (float64, error) {
			return 0, io.EOF
		},
	); err != nil {
		t.Fatalf("expected probe failure to be treated as skip, got %v", err)
	}
}

func TestMimeTypeForPathUsesStableFallbacks(t *testing.T) {
	t.Helper()

	if got := mimeTypeForPath("/tmp/image.png"); got != "image/png" {
		t.Fatalf("unexpected image mime type: %q", got)
	}
	if got := mimeTypeForPath("/tmp/audio.mp3"); got != "audio/mpeg" {
		t.Fatalf("unexpected audio mime type: %q", got)
	}
	if got := mimeTypeForPath("/tmp/video-without-extension"); got != "video/mp4" {
		t.Fatalf("unexpected fallback video mime type: %q", got)
	}
}

func TestUploadTokenDataForUploadNarrowsToIndexedStoreInfo(t *testing.T) {
	t.Helper()

	token := (&uploadTokenData{
		UploadDomain: "https://upload-root.example.com",
		StoreURI:     "resource/root.png",
		StoreKeys: []string{
			"resource/first.png",
			"resource/second.png",
		},
		StoreInfos: []*uploadStoreInfo{
			{
				StoreURI:     "resource/first.png",
				UploadDomain: "https://upload-first.example.com",
			},
			{
				StoreURI:     "resource/second.png",
				UploadDomain: "https://upload-second.example.com",
			},
		},
	}).forUpload("/tmp/ignored.png", 1)

	if token == nil {
		t.Fatalf("expected narrowed upload token")
	}
	if got := token.selectedStoreURI("/tmp/ignored.png"); got != "resource/second.png" {
		t.Fatalf("unexpected selected store uri: %q", got)
	}
	if token.StoreURI != "resource/second.png" {
		t.Fatalf("unexpected narrowed store uri: %q", token.StoreURI)
	}
	if token.UploadDomain != "https://upload-second.example.com" {
		t.Fatalf("unexpected narrowed upload domain: %q", token.UploadDomain)
	}
	if len(token.StoreInfos) != 1 || token.StoreInfos[0] == nil || token.StoreInfos[0].StoreURI != "resource/second.png" {
		t.Fatalf("unexpected narrowed store infos: %#v", token.StoreInfos)
	}
	if !reflect.DeepEqual(token.StoreKeys, []string{"resource/second.png"}) {
		t.Fatalf("unexpected narrowed store keys: %#v", token.StoreKeys)
	}
}

func TestPhaseHeadersPromotesTOSMetaHeaders(t *testing.T) {
	t.Helper()

	headers := phaseHeaders(&uploadTokenData{
		TosHeaders: `{"X-Test-Upload":"yes"}`,
		TosMeta:    `{"trace_id":"trace-001","x-tos-meta-scene":"dreamina"}`,
	}, "image/png")

	if headers["X-Test-Upload"] != "yes" {
		t.Fatalf("unexpected plain upload header: %#v", headers)
	}
	if headers["x-tos-meta-trace_id"] != "trace-001" {
		t.Fatalf("unexpected trace meta header: %#v", headers)
	}
	if headers["x-tos-meta-scene"] != "dreamina" {
		t.Fatalf("unexpected scene meta header: %#v", headers)
	}
	if headers["Content-Type"] != "image/png" {
		t.Fatalf("unexpected content-type: %#v", headers)
	}
}

func TestPhaseHeadersSupportsWrappedHeaderPayloads(t *testing.T) {
	t.Helper()

	headers := phaseHeaders(&uploadTokenData{
		TosHeaders: `{"headers":{"X-Test-Upload":"yes"}}`,
		TosMeta:    `{"payload":{"trace_id":"trace-wrapped-1"}}`,
	}, "image/png")

	if headers["X-Test-Upload"] != "yes" {
		t.Fatalf("unexpected wrapped upload header: %#v", headers)
	}
	if headers["x-tos-meta-trace_id"] != "trace-wrapped-1" {
		t.Fatalf("unexpected wrapped meta header: %#v", headers)
	}
	if headers["Content-Type"] != "image/png" {
		t.Fatalf("unexpected content-type: %#v", headers)
	}
}

func TestSignTOSPhaseRequestAtBuildsExpectedHeaders(t *testing.T) {
	t.Helper()

	httpCli, err := httpclient.New("https://jimeng.jianying.com")
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	req, err := httpCli.NewRequest(context.Background(), "PUT", "https://vod.bytedanceapi.com/dreamina/jpai.mp4?uploadid=u%2F1&phase=transfer&part_number=1", []byte("video-bytes"), map[string]string{
		"Content-Type":     "video/mp4",
		"x-tos-meta-scene": "dreamina",
	})
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	err = signTOSPhaseRequestAt(req, &uploadTokenData{
		AccessKeyID:     "ak-test",
		SecretAccessKey: "sk-test",
		SessionToken:    "sts-test",
		Region:          "cn",
	}, time.Date(2026, time.April, 5, 6, 7, 8, 0, time.UTC))
	if err != nil {
		t.Fatalf("signTOSPhaseRequestAt() error = %v", err)
	}

	if req.Headers["Host"] != "dreamina.vod.bytedanceapi.com" {
		t.Fatalf("unexpected host header: %#v", req.Headers)
	}
	if req.Headers["X-Tos-Date"] != "20260405T060708Z" {
		t.Fatalf("unexpected x-tos-date: %#v", req.Headers)
	}
	if req.Headers["X-Tos-Content-Sha256"] != unsignedTOSPayloadHash {
		t.Fatalf("unexpected x-tos-content-sha256: %#v", req.Headers)
	}
	if req.Headers["X-Tos-Security-Token"] != "sts-test" {
		t.Fatalf("unexpected x-tos-security-token: %#v", req.Headers)
	}
	if !strings.HasPrefix(req.Headers["Authorization"], "TOS4-HMAC-SHA256 Credential=ak-test/20260405/cn/tos/request,SignedHeaders=") {
		t.Fatalf("unexpected authorization prefix: %q", req.Headers["Authorization"])
	}
	if !strings.Contains(req.Headers["Authorization"], "content-type;host;x-tos-content-sha256;x-tos-date;x-tos-meta-scene;x-tos-security-token") {
		t.Fatalf("unexpected signed headers: %q", req.Headers["Authorization"])
	}
	if !strings.Contains(req.Headers["Authorization"], ",Signature=") {
		t.Fatalf("unexpected authorization format: %q", req.Headers["Authorization"])
	}
}

func TestSignAWSServiceRequestAtBuildsExpectedHeaders(t *testing.T) {
	t.Helper()

	httpCli, err := httpclient.New("https://jimeng.jianying.com")
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	req, err := httpCli.NewRequest(context.Background(), "GET", "http://vod.bytedanceapi.com/top/v1?Action=ApplyUploadInner&FileType=video&SessionKey=&SpaceName=dreamina&Version=2020-11-19", nil, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
	})
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	err = signAWSServiceRequestAt(req, "ak-test", "sk-test", "sts-test", "cn-north-1", "vod", time.Date(2026, time.April, 5, 6, 7, 8, 0, time.UTC))
	if err != nil {
		t.Fatalf("signAWSServiceRequestAt() error = %v", err)
	}

	if req.Headers["Host"] != "vod.bytedanceapi.com" {
		t.Fatalf("unexpected host header: %#v", req.Headers)
	}
	if req.Headers["X-Amz-Date"] != "20260405T060708Z" {
		t.Fatalf("unexpected x-amz-date: %#v", req.Headers)
	}
	if req.Headers["X-Amz-Content-Sha256"] != hashSHA256Hex(nil) {
		t.Fatalf("unexpected x-amz-content-sha256: %#v", req.Headers)
	}
	if req.Headers["X-Amz-Security-Token"] != "sts-test" {
		t.Fatalf("unexpected x-amz-security-token: %#v", req.Headers)
	}
	if !strings.HasPrefix(req.Headers["Authorization"], "AWS4-HMAC-SHA256 Credential=ak-test/20260405/cn-north-1/vod/aws4_request, SignedHeaders=") {
		t.Fatalf("unexpected authorization prefix: %q", req.Headers["Authorization"])
	}
	if !strings.Contains(req.Headers["Authorization"], "content-type;x-amz-content-sha256;x-amz-date;x-amz-security-token") {
		t.Fatalf("unexpected signed headers: %q", req.Headers["Authorization"])
	}
	if !strings.Contains(req.Headers["Authorization"], "Signature=") {
		t.Fatalf("unexpected authorization format: %q", req.Headers["Authorization"])
	}
}

func TestDoUploadPhaseRequestSignsTOSHeaders(t *testing.T) {
	t.Helper()

	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = true
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "TOS4-HMAC-SHA256 Credential=ak-live/") {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("X-Tos-Content-Sha256"); got != unsignedTOSPayloadHash {
			t.Fatalf("unexpected x-tos-content-sha256: %q", got)
		}
		if got := r.Header.Get("X-Tos-Security-Token"); got != "sts-live" {
			t.Fatalf("unexpected x-tos-security-token: %q", got)
		}
		if got := r.Header.Get("X-Use-Ppe"); got != "" {
			t.Fatalf("signed phase request should not carry backend header: %q", got)
		}
		writeJSON(t, w, map[string]any{"uploadID": "u-1"})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)
	_, _, err = client.doUploadPhaseRequest(context.Background(), "POST", server.URL+"/dreamina/jpai.mp4?phase=init", &uploadTokenData{
		AccessKeyID:     "ak-live",
		SecretAccessKey: "sk-live",
		SessionToken:    "sts-live",
		Region:          "cn",
	}, map[string]string{
		"x-tos-meta-scene": "dreamina",
	}, nil)
	if err != nil {
		t.Fatalf("doUploadPhaseRequest() error = %v", err)
	}
	if !sawAuth {
		t.Fatalf("expected signed request to reach server")
	}
}

func TestBuildUploadPhaseURLUsesEncodedQuery(t *testing.T) {
	t.Helper()

	got, err := buildUploadPhaseURL(&uploadTokenData{
		UploadDomain: "upload.example.com",
	}, "resource/test image.png", "transfer", "upload id/1")
	if err != nil {
		t.Fatalf("buildUploadPhaseURL() error = %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse built upload url: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "upload.example.com" {
		t.Fatalf("unexpected upload url host: %q", got)
	}
	if parsed.Query().Get("phase") != "transfer" || parsed.Query().Get("uploadid") != "upload id/1" {
		t.Fatalf("unexpected upload query: %q", parsed.RawQuery)
	}
	if parsed.Query().Get("part_number") != "1" {
		t.Fatalf("unexpected part_number query: %q", parsed.RawQuery)
	}
}

func TestBuildUploadPhaseURLUsesBucketHostForSignedTOS(t *testing.T) {
	t.Helper()

	got, err := buildUploadPhaseURL(&uploadTokenData{
		AccessKeyID:     "ak-test",
		SecretAccessKey: "sk-test",
		SessionToken:    "sts-test",
		UploadDomain:    "vod.bytedanceapi.com",
		SpaceName:       "dreamina",
	}, "dreamina/jpai.mp4", "transfer", "upload-1")
	if err != nil {
		t.Fatalf("buildUploadPhaseURL() error = %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse built upload url: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "vod.bytedanceapi.com" {
		t.Fatalf("unexpected signed upload host: %q", got)
	}
	if parsed.Path != "/jpai.mp4" {
		t.Fatalf("unexpected signed upload path: %q", parsed.Path)
	}
	if parsed.Query().Get("phase") != "transfer" || parsed.Query().Get("uploadid") != "upload-1" {
		t.Fatalf("unexpected signed upload query: %q", parsed.RawQuery)
	}
}

func TestParseUploadIDSupportsCaseVariants(t *testing.T) {
	t.Helper()

	if got := parseUploadID([]byte(`{"Data":{"UploadId":"upload-1"}}`)); got != "upload-1" {
		t.Fatalf("unexpected UploadId parse result: %q", got)
	}
	if got := parseUploadID([]byte(`{"payload":{"UploadID":"upload-2"}}`)); got != "upload-2" {
		t.Fatalf("unexpected UploadID parse result: %q", got)
	}
}

func TestParseFinishedStoreURISupportsWrapperAliases(t *testing.T) {
	t.Helper()

	if got := parseFinishedStoreURI([]byte(`{"Data":{"storeUri":"resource/data-store.png"}}`)); got != "resource/data-store.png" {
		t.Fatalf("unexpected wrapped store uri: %q", got)
	}
	if got := parseFinishedStoreURI([]byte(`{"payload":{"StoreURI":"resource/upper-store.png"}}`)); got != "resource/upper-store.png" {
		t.Fatalf("unexpected upper store uri: %q", got)
	}
	if got := parseFinishedStoreURI([]byte(`{"payload":{"resource_id":"resource/final-id"}}`)); got != "resource/final-id" {
		t.Fatalf("unexpected resource_id fallback: %q", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode json response: %v", err)
	}
}
