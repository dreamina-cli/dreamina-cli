package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

func TestDoGeneratePreservesRemoteDataAndMovesRecoveredMetadata(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-text2image",
			"data": map[string]any{
				"submit_id":  "submit-1",
				"history_id": "hist-1",
				"task_id":    "task-1",
				"transport": map[string]any{
					"method": "POST",
					"path":   "/internal/submit",
					"headers": map[string]any{
						"Cookie":     "sid=secret",
						"X-Tt-Logid": "tt-log-id",
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie:  "sid=test",
		UserID:  "u-1",
		Headers: map[string]string{"X-Test": "1"},
	}, &Text2ImageRequest{
		Prompt: "a test image",
	})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if got := data["submit_id"]; got != "submit-1" {
		t.Fatalf("unexpected submit_id: %#v", got)
	}
	if got := data["history_id"]; got != "hist-1" {
		t.Fatalf("unexpected history_id: %#v", got)
	}
	for _, key := range []string{"request", "session", "submitted", "response"} {
		if _, exists := data[key]; exists {
			t.Fatalf("remote data should not contain recovered field %q: %#v", key, data[key])
		}
	}

	recovered, ok := resp.Recovered.(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered type: %T", resp.Recovered)
	}
	if got := recovered["history_id"]; got != "hist-1" {
		t.Fatalf("expected recovered history_id, got %#v", got)
	}
	if _, exists := recovered["submitted"]; exists {
		t.Fatalf("did not expect local submitted fallback: %#v", recovered["submitted"])
	}
	transport, ok := recovered["transport"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected transport type: %T", recovered["transport"])
	}
	headers, ok := transport["headers"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected transport headers type: %T", transport["headers"])
	}
	if got := headers["Cookie"]; got != "<redacted-cookie>" {
		t.Fatalf("unexpected redacted cookie: %#v", got)
	}
	if got := headers["X-Tt-Logid"]; got != "tt-log-id" {
		t.Fatalf("unexpected forwarded logid: %#v", got)
	}
}

func TestText2ImageRequestUsesSnakeCaseJSONFields(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "dreamina_cli" {
			t.Fatalf("unexpected from query: %q", got)
		}
		if got := r.URL.Query().Get("cli_version"); got != "4946b9d-dirty" {
			t.Fatalf("unexpected cli_version query: %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("unexpected accept header: %q", got)
		}
		if got := r.Header.Get("Appid"); got != "513695" {
			t.Fatalf("unexpected appid header: %q", got)
		}
		if got := r.Header.Get("Pf"); got != "7" {
			t.Fatalf("unexpected pf header: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["generate_type"]; got != "text2imageByConfig" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["prompt"]; got != "a test image" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["ratio"]; got != "1:1" {
			t.Fatalf("unexpected ratio: %#v", got)
		}
		if got := payload["resolution_type"]; got != "2k" {
			t.Fatalf("unexpected resolution_type: %#v", got)
		}
		if got := payload["model_key"]; got != "general_v2.1" {
			t.Fatalf("unexpected model_key: %#v", got)
		}
		submitID, _ := payload["submit_id"].(string)
		subjectID, _ := payload["subject_id"].(string)
		if submitID == "" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(submitID) {
			t.Fatalf("unexpected submit_id: %#v", payload["submit_id"])
		}
		if subjectID != submitID {
			t.Fatalf("expected subject_id to match submit_id, got subject=%q submit=%q", subjectID, submitID)
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-text2image-snake-case",
			"data": map[string]any{
				"submit_id": "submit-snake-case-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{
		Prompt:         "a test image",
		Ratio:          "1:1",
		ResolutionType: "2k",
		ModelVersion:   "general_v2.1",
	})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}
	if resp.LogID != "log-text2image-snake-case" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestImage2ImageRequestUsesOriginalImageGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "dreamina_cli" {
			t.Fatalf("unexpected from query: %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("unexpected accept header: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "editImageByConfig" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["prompt"]; got != "turn into watercolor" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["ratio"]; got != "16:9" {
			t.Fatalf("unexpected ratio: %#v", got)
		}
		if got := payload["resolution_type"]; got != "2k" {
			t.Fatalf("unexpected resolution_type: %#v", got)
		}
		resourceIDs, ok := payload["resource_id_list"].([]any)
		if !ok || len(resourceIDs) != 1 || resourceIDs[0] != "image-resource-1" {
			t.Fatalf("unexpected resource_id_list: %#v", payload["resource_id_list"])
		}
		submitID, _ := payload["submit_id"].(string)
		subjectID, _ := payload["subject_id"].(string)
		if submitID != "submit-image2image-1" {
			t.Fatalf("unexpected submit_id: %#v", payload["submit_id"])
		}
		if subjectID != submitID {
			t.Fatalf("expected subject_id to match submit_id, got subject=%q submit=%q", subjectID, submitID)
		}
		if _, exists := payload["media_resource_id_list"]; exists {
			t.Fatalf("did not expect media_resource_id_list in payload: %#v", payload["media_resource_id_list"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-image2image",
			"data": map[string]any{
				"submit_id": "submit-image2image-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Image2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Image2ImageRequest{
		ResourceIDList: []string{"image-resource-1"},
		Prompt:         "turn into watercolor",
		Ratio:          "16:9",
		ResolutionType: "2k",
		SubmitID:       "submit-image2image-1",
	})
	if err != nil {
		t.Fatalf("Image2Image failed: %v", err)
	}
	if resp.LogID != "log-image2image" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestUpscaleRequestUsesOriginalImageGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "dreamina_cli" {
			t.Fatalf("unexpected from query: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "imageSuperResolution" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["resource_id"]; got != "image-resource-upscale-1" {
			t.Fatalf("unexpected resource_id: %#v", got)
		}
		if got := payload["resolution_type"]; got != "2k" {
			t.Fatalf("unexpected resolution_type: %#v", got)
		}
		submitID, _ := payload["submit_id"].(string)
		subjectID, _ := payload["subject_id"].(string)
		if submitID == "" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(submitID) {
			t.Fatalf("unexpected submit_id: %#v", payload["submit_id"])
		}
		if subjectID != submitID {
			t.Fatalf("expected subject_id to match submit_id, got subject=%q submit=%q", subjectID, submitID)
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-upscale-1",
			"data": map[string]any{
				"submit_id": "submit-upscale-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Upscale(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &UpscaleRequest{
		ResourceID:     "image-resource-upscale-1",
		ResolutionType: "2k",
	})
	if err != nil {
		t.Fatalf("Upscale failed: %v", err)
	}
	if resp.LogID != "log-upscale-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestImage2VideoRequestUsesOriginalVideoGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "dreamina_cli" {
			t.Fatalf("unexpected from query: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "image2video" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["prompt"]; got != "camera push in" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["first_frame_resource_id"]; got != "image-resource-video-1" {
			t.Fatalf("unexpected first_frame_resource_id: %#v", got)
		}
		if got := payload["duration"]; got != float64(5) {
			t.Fatalf("unexpected duration: %#v", got)
		}
		submitID, _ := payload["submit_id"].(string)
		if submitID == "" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(submitID) {
			t.Fatalf("unexpected submit_id: %#v", payload["submit_id"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-image2video-1",
			"data": map[string]any{
				"submit_id": "submit-image2video-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Image2Video(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Image2VideoRequest{
		FirstFrameResourceID: "image-resource-video-1",
		Prompt:               "camera push in",
	})
	if err != nil {
		t.Fatalf("Image2Video failed: %v", err)
	}
	if resp.LogID != "log-image2video-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestImage2VideoByConfigRequestUsesDedicatedPayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "firstFrameVideoByConfig" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["model_key"]; got != "seedance2.0fast" {
			t.Fatalf("unexpected default model_key: %#v", got)
		}
		if got := payload["duration"]; got != float64(5) {
			t.Fatalf("unexpected duration: %#v", got)
		}
		if got := payload["first_frame_resource_id"]; got != "image-resource-video-2" {
			t.Fatalf("unexpected first_frame_resource_id: %#v", got)
		}
		if _, exists := payload["video_resolution"]; exists {
			t.Fatalf("did not expect unsupported default video_resolution: %#v", payload["video_resolution"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-image2video-by-config-1",
			"data": map[string]any{
				"submit_id": "submit-image2video-by-config-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Image2VideoByConfig(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Image2VideoRequest{
		FirstFrameResourceID: "image-resource-video-2",
		Prompt:               "camera push in",
		Duration:             5,
		VideoResolution:      "1080p",
	})
	if err != nil {
		t.Fatalf("Image2VideoByConfig failed: %v", err)
	}
	if resp.LogID != "log-image2video-by-config-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestText2VideoRequestUsesOriginalVideoGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "dreamina_cli" {
			t.Fatalf("unexpected from query: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "text2VideoByConfig" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["prompt"]; got != "a cat walks slowly" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["duration"]; got != float64(5) {
			t.Fatalf("unexpected duration: %#v", got)
		}
		if got := payload["ratio"]; got != "16:9" {
			t.Fatalf("unexpected ratio: %#v", got)
		}
		if got := payload["model_key"]; got != "seedance2.0fast" {
			t.Fatalf("unexpected default model_key: %#v", got)
		}
		if _, exists := payload["video_resolution"]; exists {
			t.Fatalf("did not expect default video_resolution: %#v", payload["video_resolution"])
		}
		submitID, _ := payload["submit_id"].(string)
		if submitID == "" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(submitID) {
			t.Fatalf("unexpected submit_id: %#v", payload["submit_id"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-text2video-1",
			"data": map[string]any{
				"submit_id": "submit-text2video-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Text2Video(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2VideoRequest{
		Prompt: "a cat walks slowly",
	})
	if err != nil {
		t.Fatalf("Text2Video failed: %v", err)
	}
	if resp.LogID != "log-text2video-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestFrames2VideoRequestUsesOriginalVideoGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "startEndFrameVideoByConfig" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["prompt"]; got != "season changes" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["duration"]; got != float64(5) {
			t.Fatalf("unexpected duration: %#v", got)
		}
		if got := payload["first_frame_resource_id"]; got != "image-first-1" {
			t.Fatalf("unexpected first_frame_resource_id: %#v", got)
		}
		if got := payload["last_frame_resource_id"]; got != "image-last-1" {
			t.Fatalf("unexpected last_frame_resource_id: %#v", got)
		}
		if _, exists := payload["video_resolution"]; exists {
			t.Fatalf("did not expect default video_resolution: %#v", payload["video_resolution"])
		}
		if _, exists := payload["model_key"]; exists {
			t.Fatalf("did not expect default model_key: %#v", payload["model_key"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-frames2video-1",
			"data": map[string]any{
				"submit_id": "submit-frames2video-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Frames2Video(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Frames2VideoRequest{
		FirstFrameResourceID: "image-first-1",
		LastFrameResourceID:  "image-last-1",
		Prompt:               "season changes",
	})
	if err != nil {
		t.Fatalf("Frames2Video failed: %v", err)
	}
	if resp.LogID != "log-frames2video-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestRef2VideoRequestUsesOriginalVideoGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "multiFrame2video" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		mediaIDs, _ := payload["media_resource_id_list"].([]any)
		if len(mediaIDs) != 2 || mediaIDs[0] != "image-1" || mediaIDs[1] != "image-2" {
			t.Fatalf("unexpected media_resource_id_list: %#v", payload["media_resource_id_list"])
		}
		mediaTypes, _ := payload["media_type_list"].([]any)
		if len(mediaTypes) != 2 || mediaTypes[0] != "图片" || mediaTypes[1] != "图片" {
			t.Fatalf("unexpected media_type_list: %#v", payload["media_type_list"])
		}
		promptList, _ := payload["prompt_list"].([]any)
		if len(promptList) != 1 || promptList[0] != "turn around" {
			t.Fatalf("unexpected prompt_list: %#v", payload["prompt_list"])
		}
		durationList, _ := payload["duration_list"].([]any)
		if len(durationList) != 1 || durationList[0] != float64(3) {
			t.Fatalf("unexpected duration_list: %#v", payload["duration_list"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-ref2video-1",
			"data": map[string]any{
				"submit_id": "submit-ref2video-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Ref2Video(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Ref2VideoRequest{
		MediaResourceIDList: []string{"image-1", "image-2"},
		PromptList:          []string{"turn around"},
		DurationList:        []float64{3},
	})
	if err != nil {
		t.Fatalf("Ref2Video failed: %v", err)
	}
	if resp.LogID != "log-ref2video-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestMultiModal2VideoUsesOriginalVideoGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "dreamina_cli" {
			t.Fatalf("unexpected from query: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["generate_type"]; got != "multiModal2VideoByConfig" {
			t.Fatalf("unexpected generate_type: %#v", got)
		}
		if got := payload["agent_scene"]; got != "workbench" {
			t.Fatalf("unexpected agent_scene: %#v", got)
		}
		if got := payload["creation_agent_version"]; got != "3.0.0" {
			t.Fatalf("unexpected creation_agent_version: %#v", got)
		}
		if got := payload["prompt"]; got != "保持主体不变，做自然镜头运动" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["ratio"]; got != "16:9" {
			t.Fatalf("unexpected ratio: %#v", got)
		}
		if got := payload["duration"]; got != float64(5) {
			t.Fatalf("unexpected duration: %#v", got)
		}
		if got := payload["model_key"]; got != "seedance2.0fast" {
			t.Fatalf("unexpected model_key: %#v", got)
		}
		imageIDs, _ := payload["image_resource_id_list"].([]any)
		if len(imageIDs) != 1 || imageIDs[0] != "image-resource-1" {
			t.Fatalf("unexpected image_resource_id_list: %#v", payload["image_resource_id_list"])
		}
		videoIDs, _ := payload["video_resource_id_list"].([]any)
		if len(videoIDs) != 1 || videoIDs[0] != "video-resource-1" {
			t.Fatalf("unexpected video_resource_id_list: %#v", payload["video_resource_id_list"])
		}
		audioIDs, _ := payload["audio_resource_id_list"].([]any)
		if len(audioIDs) != 1 || audioIDs[0] != "audio-resource-1" {
			t.Fatalf("unexpected audio_resource_id_list: %#v", payload["audio_resource_id_list"])
		}
		submitID, _ := payload["submit_id"].(string)
		if submitID == "" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(submitID) {
			t.Fatalf("unexpected submit_id: %#v", payload["submit_id"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-multimodal2video-1",
			"data": map[string]any{
				"submit_id": "submit-multimodal2video-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.MultiModal2Video(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &MultiModal2VideoRequest{
		ImageResourceIDList: []string{"image-resource-1"},
		VideoResourceIDList: []string{"video-resource-1"},
		AudioResourceIDList: []string{"audio-resource-1"},
		Prompt:              "保持主体不变，做自然镜头运动",
	})
	if err != nil {
		t.Fatalf("MultiModal2Video failed: %v", err)
	}
	if resp.LogID != "log-multimodal2video-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestDoPostUsesResponseHeaderLogIDFallback(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tt-Logid", "20260404234210192168001245723FD58")
		writeMCPJSON(t, w, map[string]any{
			"ret": "0",
			"msg": "success",
			"data": map[string]any{
				"submit_id": "submit-header-logid-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Text2Image(context.Background(), &Session{Cookie: "sid=test"}, &Text2ImageRequest{Prompt: "header logid"})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}
	if resp.LogID != "20260404234210192168001245723FD58" {
		t.Fatalf("unexpected header fallback log id: %#v", resp.LogID)
	}
}

func TestBuildMCPLogIDMatchesOriginalLengthShape(t *testing.T) {
	t.Helper()

	got := buildMCPLogID("Text2Image")
	if len(got) != 33 {
		t.Fatalf("unexpected log id length: %q (%d)", got, len(got))
	}
	if !regexp.MustCompile(`^[0-9]{14}[0-9A-F]{19}$`).MatchString(got) {
		t.Fatalf("unexpected log id shape: %q", got)
	}
}

func TestText2ImageRequestUsesOriginalDefaultGeneratePayload(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got := payload["prompt"]; got != "default image" {
			t.Fatalf("unexpected prompt: %#v", got)
		}
		if got := payload["ratio"]; got != "16:9" {
			t.Fatalf("unexpected default ratio: %#v", got)
		}
		if _, exists := payload["model_key"]; exists {
			t.Fatalf("did not expect model_key in default payload: %#v", payload["model_key"])
		}
		if _, exists := payload["resolution_type"]; exists {
			t.Fatalf("did not expect resolution_type in default payload: %#v", payload["resolution_type"])
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-text2image-default-payload",
			"data": map[string]any{
				"submit_id": "submit-default-payload-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{
		Prompt: "default image",
	})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}
	if resp.LogID != "log-text2image-default-payload" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
}

func TestDoGenerateSupportsUpperCamelResponseWrappers(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"Code":    "0",
			"Message": "ok",
			"LogID":   "log-text2image-upper-1",
			"Payload": map[string]any{
				"SubmitID":  "submit-upper-1",
				"HistoryID": "hist-upper-1",
				"TaskID":    "task-upper-1",
				"Transport": map[string]any{
					"Method": "POST",
					"Path":   "/internal/submit-upper",
					"Headers": map[string]any{
						"Cookie":     "sid=secret-upper",
						"X-Tt-Logid": "tt-logid-upper",
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{
		Prompt: "an upper camel image",
	})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}
	if resp.LogID != "log-text2image-upper-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if got := data["SubmitID"]; got != "submit-upper-1" {
		t.Fatalf("unexpected SubmitID: %#v", got)
	}
	if got := data["HistoryID"]; got != "hist-upper-1" {
		t.Fatalf("unexpected HistoryID: %#v", got)
	}

	recovered, ok := resp.Recovered.(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered type: %T", resp.Recovered)
	}
	if got := recovered["history_id"]; got != "hist-upper-1" {
		t.Fatalf("expected recovered history_id, got %#v", got)
	}
	transport, ok := recovered["transport"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected transport type: %T", recovered["transport"])
	}
	if sanitized, ok := transport["sanitized"].(bool); !ok || !sanitized {
		t.Fatalf("expected sanitized transport marker, got %#v", transport)
	}
	if got := transport["path"]; got != "/internal/submit-upper" {
		t.Fatalf("unexpected sanitized path: %#v", transport)
	}
	headers, ok := transport["headers"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected sanitized headers type: %T", transport["headers"])
	}
	if got := headers["Cookie"]; got != "<redacted-cookie>" {
		t.Fatalf("unexpected sanitized cookie: %#v", headers)
	}
}

func TestDoGenerateRecoversNestedWrapperHistoryAndTransport(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"Code": "0",
			"Payload": map[string]any{
				"Data": map[string]any{
					"HistoryID": "hist-nested-1",
					"Transport": map[string]any{
						"Method": "POST",
						"Path":   "/internal/nested",
						"Headers": map[string]any{
							"Cookie":        "sid=nested",
							"Authorization": "Bearer nested-secret-token",
						},
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{
		Prompt: "a nested image",
	})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if got := data["HistoryID"]; got != "hist-nested-1" {
		t.Fatalf("unexpected nested HistoryID: %#v", got)
	}

	recovered, ok := resp.Recovered.(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered type: %T", resp.Recovered)
	}
	if got := recovered["history_id"]; got != "hist-nested-1" {
		t.Fatalf("expected recovered nested history_id, got %#v", got)
	}
	transport, ok := recovered["transport"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered transport type: %T", recovered["transport"])
	}
	if got := transport["path"]; got != "/internal/nested" {
		t.Fatalf("unexpected sanitized path: %#v", got)
	}
	headers, ok := transport["headers"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected sanitized headers type: %T", transport["headers"])
	}
	if got := headers["Cookie"]; got != "<redacted-cookie>" {
		t.Fatalf("unexpected sanitized cookie: %#v", headers)
	}
	if _, exists := headers["Authorization"]; exists {
		t.Fatalf("authorization header should be filtered out: %#v", headers)
	}
}

func TestDoGenerateUnwrapsPayloadDataButKeepsSiblingTransportInRecovered(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"Payload": map[string]any{
				"Transport": map[string]any{
					"Method": "POST",
					"Path":   "/internal/payload-sibling",
					"Headers": map[string]any{
						"Cookie": "sid=sibling",
					},
				},
				"Data": map[string]any{
					"HistoryID": "hist-payload-sibling-1",
					"SubmitID":  "submit-payload-sibling-1",
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{Prompt: "payload sibling transport"})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if got := data["HistoryID"]; got != "hist-payload-sibling-1" {
		t.Fatalf("unexpected flattened HistoryID: %#v", got)
	}
	if _, exists := data["Data"]; exists {
		t.Fatalf("did not expect nested Data wrapper after unwrapping: %#v", data)
	}

	recovered, ok := resp.Recovered.(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered type: %T", resp.Recovered)
	}
	transport, ok := recovered["transport"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered transport type: %T", recovered["transport"])
	}
	if got := transport["path"]; got != "/internal/payload-sibling" {
		t.Fatalf("unexpected recovered transport path: %#v", transport)
	}
}

func TestDoGenerateUnwrapsResponseDataWrapper(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"Response": map[string]any{
				"Code":    "0",
				"Message": "ok",
				"LogID":   "log-response-wrapper-1",
				"Data": map[string]any{
					"HistoryID": "hist-response-wrapper-1",
					"SubmitID":  "submit-response-wrapper-1",
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{Prompt: "response wrapper"})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}
	if resp.Code != "0" || resp.Message != "ok" || resp.LogID != "log-response-wrapper-1" {
		t.Fatalf("unexpected response metadata: %#v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if got := data["HistoryID"]; got != "hist-response-wrapper-1" {
		t.Fatalf("unexpected flattened wrapper data: %#v", data)
	}
	if _, exists := data["Data"]; exists {
		t.Fatalf("did not expect nested Data wrapper after unwrapping: %#v", data)
	}
}

func TestDoGenerateSupportsNestedMetaResponseFields(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"Payload": map[string]any{
				"Meta": map[string]any{
					"Code":      "0",
					"Message":   "meta-ok",
					"RequestID": "log-meta-submit-1",
				},
				"Data": map[string]any{
					"HistoryID": "hist-meta-submit-1",
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{Prompt: "meta submit"})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}
	if resp.Code != "0" || resp.Message != "meta-ok" || resp.LogID != "log-meta-submit-1" {
		t.Fatalf("unexpected nested meta response: %#v", resp)
	}
}

func TestDoGeneratePrefersRemoteSubmittedTimestamp(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"HistoryID":   "hist-submit-time-1",
					"SubmittedAt": 1700002222,
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

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{Prompt: "submitted timestamp"})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}

	recovered, ok := resp.Recovered.(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered type: %T", resp.Recovered)
	}
	if submitted, ok := recovered["submitted"].(float64); !ok || int64(submitted) != 1700002222 {
		t.Fatalf("unexpected recovered submitted timestamp: %#v", recovered["submitted"])
	}
}

func TestDoGenerateDoesNotSynthesizeFallbackHistoryID(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"log_id":  "log-no-history-id-1",
			"data": map[string]any{
				"submit_id": "submit-no-history-id-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{Prompt: "no history id"})
	if err != nil {
		t.Fatalf("Text2Image failed: %v", err)
	}

	recovered, ok := resp.Recovered.(map[string]any)
	if !ok {
		t.Fatalf("unexpected recovered type: %T", resp.Recovered)
	}
	if got := recovered["history_id"]; got != "submit-no-history-id-1" {
		t.Fatalf("expected remote submit_id fallback, got %#v", got)
	}
	if strings.HasPrefix(fmt.Sprint(recovered["history_id"]), "hist_") {
		t.Fatalf("history_id should not be synthetic hash: %#v", recovered["history_id"])
	}
	if _, exists := recovered["submitted"]; exists {
		t.Fatalf("did not expect local submitted fallback: %#v", recovered["submitted"])
	}
}

func TestParseHistoryResponsePayloadAcceptsMediaURLAndCoverUriAliases(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"code": "0",
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"submit_id":    "submit-alias-media-1",
					"history_id":   "hist-alias-media-1",
					"queue_status": "done",
					"videos": []any{
						map[string]any{
							"MediaURL": "https://example.com/media-alias.mp4",
							"CoverUri": "https://example.com/media-cover-alias.png",
							"resource_list": []any{
								map[string]any{
									"FileURL": "https://example.com/media-alias-hd.mp4",
									"Type":    "hd",
								},
							},
						},
					},
					"details": []any{
						map[string]any{
							"HistoryRecordID": "record-alias-media-1",
							"MediaURL":        "https://example.com/detail-media-alias.mp4",
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-alias-media-1"]
	if item == nil {
		t.Fatalf("expected parsed item, got %#v", resp.Items)
	}
	if item.VideoURL != "https://example.com/media-alias.mp4" {
		t.Fatalf("unexpected video url alias: %#v", item)
	}
	if len(item.Videos) != 1 || item.Videos[0].CoverURL != "https://example.com/media-cover-alias.png" {
		t.Fatalf("unexpected video alias payload: %#v", item.Videos)
	}
	if len(item.Videos[0].Resources) != 1 || item.Videos[0].Resources[0].VideoURL != "https://example.com/media-alias-hd.mp4" {
		t.Fatalf("unexpected resource alias payload: %#v", item.Videos[0].Resources)
	}
	if len(item.Details) != 1 || item.Details[0].VideoURL != "https://example.com/detail-media-alias.mp4" {
		t.Fatalf("unexpected detail alias payload: %#v", item.Details)
	}
}

func TestParseHistoryResponsePayloadAcceptsSingularAndKeyedMediaCollections(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"code": "0",
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"submit_id":    "submit-singular-media-1",
					"history_id":   "hist-singular-media-1",
					"queue_status": "success",
					"images": map[string]any{
						"ImageURL": "https://example.com/singular-image.png",
						"Origin":   "result",
					},
					"videos": map[string]any{
						"primary": map[string]any{
							"MediaURL": "https://example.com/keyed-video.mp4",
							"CoverUri": "https://example.com/keyed-cover.png",
							"resources": map[string]any{
								"hd": map[string]any{
									"FileURL": "https://example.com/keyed-video-hd.mp4",
									"Type":    "hd",
								},
							},
						},
					},
					"details": map[string]any{
						"result": map[string]any{
							"HistoryRecordID": "record-singular-media-1",
							"ImageURL":        "https://example.com/detail-image.png",
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-singular-media-1"]
	if item == nil {
		t.Fatalf("expected parsed item, got %#v", resp.Items)
	}
	if len(item.Images) != 1 || item.Images[0].ImageURL != "https://example.com/singular-image.png" {
		t.Fatalf("unexpected singular image payload: %#v", item.Images)
	}
	if len(item.Videos) != 1 || item.Videos[0].VideoURL != "https://example.com/keyed-video.mp4" || item.Videos[0].CoverURL != "https://example.com/keyed-cover.png" {
		t.Fatalf("unexpected keyed video payload: %#v", item.Videos)
	}
	if len(item.Videos[0].Resources) != 1 || item.Videos[0].Resources[0].VideoURL != "https://example.com/keyed-video-hd.mp4" {
		t.Fatalf("unexpected keyed resource payload: %#v", item.Videos[0].Resources)
	}
	if len(item.Details) != 1 || item.Details[0].ImageURL != "https://example.com/detail-image.png" {
		t.Fatalf("unexpected singular detail payload: %#v", item.Details)
	}
}

func TestDoGenerateReturnsDecodeErrorForNonJSON200Response(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/image_generate/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json-success-body"))
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	_, err = client.Text2Image(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Text2ImageRequest{
		Prompt: "non-json response",
	})
	if err == nil {
		t.Fatalf("expected Text2Image failure")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "response_decode_error" {
		t.Fatalf("unexpected error code: %#v", apiErr)
	}
	if apiErr.Message != "not-json-success-body" {
		t.Fatalf("unexpected error message: %#v", apiErr)
	}
}

func TestDoGenerateReturnsAPIErrorWithRemoteSubmitID(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dreamina/cli/v1/video_generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "2061",
			"message": "模型已不可用，请刷新界面后再试试",
			"log_id":  "log-fail-submit-1",
			"data": map[string]any{
				"submit_id": "remote-fail-submit-1",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	_, err = client.Image2Video(context.Background(), &Session{
		Cookie: "sid=test",
		UserID: "u-1",
	}, &Image2VideoRequest{
		FirstFrameResourceID: "image-resource-1",
		Prompt:               "camera push in",
	})
	if err == nil {
		t.Fatalf("expected Image2Video failure")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.SubmitID != "remote-fail-submit-1" {
		t.Fatalf("unexpected remote submit id: %#v", apiErr.SubmitID)
	}
	if apiErr.LogID != "log-fail-submit-1" {
		t.Fatalf("unexpected log id: %#v", apiErr.LogID)
	}
}

func TestGetHistoryByIdsKeepsEmptyBackendSuccess(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mweb/v1/get_history_by_ids" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeMCPJSON(t, w, map[string]any{
			"code":    "0",
			"message": "ok",
			"data": map[string]any{
				"items": []any{},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.GetHistoryByIds(context.Background(), &Session{Cookie: "sid=test"}, &GetHistoryByIdsRequest{
		HistoryIDs: []string{"hist-1"},
	})
	if err != nil {
		t.Fatalf("GetHistoryByIds failed: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected empty items without fallback, got %#v", resp.Items)
	}
}

func TestGetHistoryByIdsReturnsBackendFailureWithoutFallback(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("backend exploded"))
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.GetHistoryByIds(context.Background(), &Session{Cookie: "sid=test", UserID: "u-1"}, &GetHistoryByIdsRequest{
		HistoryIDs: []string{"hist-1"},
	})
	if err != nil {
		t.Fatalf("GetHistoryByIds failed: %v", err)
	}
	if resp.Code != "500" {
		t.Fatalf("unexpected response code: %#v", resp.Code)
	}
	if resp.Message != "backend exploded" {
		t.Fatalf("unexpected response message: %#v", resp.Message)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected no synthetic fallback items, got %#v", resp.Items)
	}
}

func TestParseHistoryResponsePayloadAcceptsListShape(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"code": "0",
		"data": map[string]any{
			"list": []any{
				map[string]any{
					"history_id":   "hist-1",
					"submit_id":    "submit-1",
					"queue_status": "success",
					"video_url":    "https://example.com/video.mp4",
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-1"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if item.QueueStatus != "success" {
		t.Fatalf("unexpected queue status: %#v", item)
	}
	if item.VideoURL != "https://example.com/video.mp4" {
		t.Fatalf("unexpected video url: %#v", item)
	}
}

func TestParseHistoryResponsePayloadAcceptsNestedMediaAndAliasShapes(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"code": "0",
		"payload": map[string]any{
			"results": []any{
				map[string]any{
					"historyId": "hist-alias-1",
					"submitId":  "submit-alias-1",
					"taskId":    "task-alias-1",
					"status":    "running",
					"queue": map[string]any{
						"queueStatus": "processing",
						"queueLength": 9,
						"queueIdx":    2,
						"Progress":    "72%",
					},
					"images": []any{
						map[string]any{
							"imageUrl": "https://example.com/image-1.png",
							"origin":   "result",
						},
					},
					"video_list": []any{
						map[string]any{
							"videoUrl": "https://example.com/video-1.mp4",
							"coverUrl": "https://example.com/video-1-cover.png",
							"fps":      24,
							"width":    1470,
							"height":   630,
							"format":   "mp4",
							"duration": 5.042,
							"resource_list": []any{
								map[string]any{
									"url":  "https://example.com/video-1-hd.mp4",
									"type": "hd",
								},
							},
						},
					},
					"detail_list": []any{
						map[string]any{
							"historyRecordId": "record-1",
							"queueStatus":     "processing",
							"imageUrl":        "https://example.com/detail-image.png",
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-alias-1"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if item.SubmitID != "submit-alias-1" || item.TaskID != "task-alias-1" {
		t.Fatalf("unexpected item ids: %#v", item)
	}
	if item.QueueStatus != "processing" || item.QueueLength != 9 || item.QueueIdx != 2 {
		t.Fatalf("unexpected queue fields: %#v", item)
	}
	if item.QueueProgress != 72 {
		t.Fatalf("unexpected queue progress: %#v", item)
	}
	if item.ImageURL != "https://example.com/image-1.png" || len(item.Images) != 1 {
		t.Fatalf("unexpected image view: %#v", item)
	}
	if item.Images[0].Origin != "result" {
		t.Fatalf("unexpected image origin: %#v", item.Images[0])
	}
	if item.VideoURL != "https://example.com/video-1.mp4" || len(item.Videos) != 1 {
		t.Fatalf("unexpected video view: %#v", item)
	}
	if item.Videos[0].CoverURL != "https://example.com/video-1-cover.png" {
		t.Fatalf("unexpected video cover: %#v", item.Videos[0])
	}
	if item.Videos[0].FPS != 24 || item.Videos[0].Width != 1470 || item.Videos[0].Height != 630 {
		t.Fatalf("unexpected video geometry: %#v", item.Videos[0])
	}
	if item.Videos[0].Format != "mp4" || item.Videos[0].Duration != 5.042 {
		t.Fatalf("unexpected video metadata: %#v", item.Videos[0])
	}
	if len(item.Videos[0].Resources) != 1 || item.Videos[0].Resources[0].Type != "hd" {
		t.Fatalf("unexpected video resources: %#v", item.Videos[0].Resources)
	}
	if len(item.Details) != 1 || item.Details[0].HistoryRecordID != "record-1" {
		t.Fatalf("unexpected details: %#v", item.Details)
	}

	view := item.View()
	queue, ok := view["queue"].(map[string]any)
	if !ok || queue["queue_status"] != "processing" {
		t.Fatalf("unexpected queue view: %#v", view["queue"])
	}
	if queue["progress"] != 72 {
		t.Fatalf("unexpected queue progress view: %#v", queue)
	}
	details, ok := view["details"].([]map[string]any)
	if !ok || len(details) != 1 || details[0]["history_record_id"] != "record-1" {
		t.Fatalf("unexpected detail view: %#v", view["details"])
	}
}

func TestParseHistoryResponsePayloadPrefersOriginVideoOverPreviewVideo(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"code": "0",
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"history_id":   "hist-origin-video-1",
					"submit_id":    "submit-origin-video-1",
					"queue_status": "Finish",
					"images": []any{
						map[string]any{
							"image_url": "https://example.com/preview.png",
							"origin": map[string]any{
								"video_url": "https://example.com/final.mp4",
								"fps":       24,
								"width":     1470,
								"height":    630,
								"format":    "mp4",
							},
						},
					},
					"videos": []any{
						map[string]any{
							"video_url": "https://example.com/preview-low.mp4",
							"fps":       24,
							"width":     840,
							"height":    360,
							"format":    "webp",
							"duration":  6,
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-origin-video-1"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if item.VideoURL != "https://example.com/final.mp4" || len(item.Videos) != 1 {
		t.Fatalf("expected origin video to win, got %#v", item)
	}
	if item.Videos[0].Format != "mp4" || item.Videos[0].Width != 1470 || item.Videos[0].Height != 630 {
		t.Fatalf("unexpected preferred origin video: %#v", item.Videos[0])
	}
}

func TestParseHistoryResponsePayloadPrefersStringifiedOriginVideoOverPreviewVideo(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"code": "0",
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"history_id":   "hist-origin-video-2",
					"submit_id":    "submit-origin-video-2",
					"queue_status": "Finish",
					"images": []any{
						map[string]any{
							"image_url": "https://example.com/preview.png",
							"origin":    "map[duration:5.042 format:mp4 fps:24 height:630 video_url:https://example.com/final.mp4 width:1470]",
						},
					},
					"videos": []any{
						map[string]any{
							"video_url": "https://example.com/preview-low.mp4",
							"fps":       24,
							"width":     840,
							"height":    360,
							"format":    "webp",
							"duration":  6,
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-origin-video-2"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if item.VideoURL != "https://example.com/final.mp4" || len(item.Videos) != 1 {
		t.Fatalf("expected stringified origin video to win, got %#v", item)
	}
	if item.Videos[0].Format != "mp4" || item.Videos[0].Duration != 5.042 {
		t.Fatalf("unexpected stringified origin video metadata: %#v", item.Videos[0])
	}
}

func TestParseHistoryResponsePayloadAcceptsRealKeyedDataShape(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"ret":    "0",
		"errmsg": "success",
		"logid":  "history-real-shape-1",
		"data": map[string]any{
			"submit-real-1": map[string]any{
				"history_record_id": "hist-real-1",
				"item_list": []any{
					map[string]any{
						"common_attr": map[string]any{
							"cover_url": "https://example.com/cover.png",
						},
						"image": map[string]any{
							"large_images": []any{
								map[string]any{
									"image_url": "https://example.com/result.png",
									"width":     5404,
									"height":    3040,
								},
							},
						},
					},
				},
				"task": map[string]any{
					"task_id":    "hist-real-1",
					"submit_id":  "submit-real-1",
					"history_id": "hist-real-1",
					"queue_info": map[string]any{
						"queue_idx":    0,
						"priority":     1,
						"queue_status": 3,
						"queue_length": 0,
						"debug_info":   "{\"stage\":\"done\"}",
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	if resp.LogID != "history-real-shape-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
	item := resp.Items["submit-real-1"]
	if item == nil {
		t.Fatalf("expected parsed keyed history item, got %#v", resp.Items)
	}
	if item.SubmitID != "submit-real-1" || item.HistoryID != "hist-real-1" || item.TaskID != "hist-real-1" {
		t.Fatalf("unexpected real keyed ids: %#v", item)
	}
	if item.QueueStatus != "Finish" || item.QueueIdx != 0 || item.QueuePriority != 1 || item.QueueLength != 0 {
		t.Fatalf("unexpected real keyed queue fields: %#v", item)
	}
	if item.QueueDebugInfo != "{\"stage\":\"done\"}" {
		t.Fatalf("unexpected real keyed debug info: %#v", item.QueueDebugInfo)
	}
	if len(item.Images) != 1 || item.Images[0].ImageURL != "https://example.com/result.png" {
		t.Fatalf("unexpected real keyed images: %#v", item.Images)
	}
	if item.Images[0].Width != 5404 || item.Images[0].Height != 3040 {
		t.Fatalf("unexpected real keyed image size: %#v", item.Images[0])
	}
	view := item.View()
	queue, ok := view["queue"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected real keyed queue view: %#v", view["queue"])
	}
	if queue["queue_idx"] != 0 || queue["queue_length"] != 0 || queue["priority"] != 1 {
		t.Fatalf("unexpected real keyed queue view fields: %#v", queue)
	}
}

func TestParseHistoryResponsePayloadAcceptsUpperCamelWrapperShapes(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"Code":    "0",
		"Message": "ok",
		"LogID":   "log-history-upper-1",
		"Payload": map[string]any{
			"Results": []any{
				map[string]any{
					"HistoryID": "hist-upper-1",
					"SubmitID":  "submit-upper-1",
					"TaskID":    "task-upper-1",
					"Status":    "success",
					"Queue": map[string]any{
						"QueueStatus": "done",
						"QueueLength": 1,
						"QueueIdx":    0,
					},
					"Images": []any{
						map[string]any{
							"ImageURL": "https://example.com/upper-image.png",
							"Origin":   "result",
						},
					},
					"Videos": []any{
						map[string]any{
							"VideoURL": "https://example.com/upper-video.mp4",
							"CoverURL": "https://example.com/upper-cover.png",
							"Resources": []any{
								map[string]any{
									"VideoURL": "https://example.com/upper-video-4k.mp4",
									"Type":     "4k",
								},
							},
						},
					},
					"Details": []any{
						map[string]any{
							"HistoryRecordID": "record-upper-1",
							"QueueStatus":     "done",
							"ImageURL":        "https://example.com/detail-upper-image.png",
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	if resp.LogID != "log-history-upper-1" {
		t.Fatalf("unexpected log id: %#v", resp.LogID)
	}
	item := resp.Items["hist-upper-1"]
	if item == nil {
		t.Fatalf("expected parsed upper camel history item, got %#v", resp.Items)
	}
	if item.SubmitID != "submit-upper-1" || item.TaskID != "task-upper-1" {
		t.Fatalf("unexpected item ids: %#v", item)
	}
	if item.QueueStatus != "done" || item.QueueLength != 1 || item.QueueIdx != 0 {
		t.Fatalf("unexpected queue fields: %#v", item)
	}
	if item.ImageURL != "https://example.com/upper-image.png" || len(item.Images) != 1 {
		t.Fatalf("unexpected image fields: %#v", item)
	}
	if item.VideoURL != "https://example.com/upper-video.mp4" || len(item.Videos) != 1 {
		t.Fatalf("unexpected video fields: %#v", item)
	}
	if item.Videos[0].CoverURL != "https://example.com/upper-cover.png" {
		t.Fatalf("unexpected cover url: %#v", item.Videos[0])
	}
	if len(item.Videos[0].Resources) != 1 || item.Videos[0].Resources[0].Type != "4k" {
		t.Fatalf("unexpected video resources: %#v", item.Videos[0].Resources)
	}
	if len(item.Details) != 1 || item.Details[0].HistoryRecordID != "record-upper-1" {
		t.Fatalf("unexpected details: %#v", item.Details)
	}
}

func TestParseHistoryResponsePayloadAcceptsDeepNestedWrapperShapes(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"Code": "0",
		"Data": map[string]any{
			"Payload": map[string]any{
				"Items": map[string]any{
					"hist-deep-1": map[string]any{
						"SubmitID": "submit-deep-1",
						"TaskID":   "task-deep-1",
						"Queue": map[string]any{
							"QueueStatus": "processing",
							"QueueLength": 4,
							"QueueIdx":    1,
						},
						"Payload": map[string]any{
							"Videos": []any{
								map[string]any{
									"VideoURL": "https://example.com/deep-video.mp4",
									"CoverURL": "https://example.com/deep-cover.png",
								},
							},
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-deep-1"]
	if item == nil {
		t.Fatalf("expected parsed deep wrapper history item, got %#v", resp.Items)
	}
	if item.SubmitID != "submit-deep-1" || item.TaskID != "task-deep-1" {
		t.Fatalf("unexpected deep wrapper ids: %#v", item)
	}
	if item.QueueStatus != "processing" || item.QueueLength != 4 || item.QueueIdx != 1 {
		t.Fatalf("unexpected deep wrapper queue: %#v", item)
	}
	if item.VideoURL != "https://example.com/deep-video.mp4" || len(item.Videos) != 1 {
		t.Fatalf("unexpected deep wrapper video fields: %#v", item)
	}
	if item.Videos[0].CoverURL != "https://example.com/deep-cover.png" {
		t.Fatalf("unexpected deep wrapper cover: %#v", item.Videos[0])
	}
}

func TestParseHistoryResponsePayloadAcceptsNestedMetaMetadata(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"Payload": map[string]any{
			"Meta": map[string]any{
				"Ret":       "0",
				"Message":   "history-meta-ok",
				"RequestID": "log-history-meta-1",
			},
			"Data": map[string]any{
				"Items": []any{
					map[string]any{
						"HistoryID": "hist-meta-1",
						"SubmitID":  "submit-meta-1",
						"Status":    "success",
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	if resp.Code != "0" || resp.Message != "history-meta-ok" || resp.LogID != "log-history-meta-1" {
		t.Fatalf("unexpected nested meta metadata: %#v", resp)
	}
	item := resp.Items["hist-meta-1"]
	if item == nil || item.SubmitID != "submit-meta-1" || item.Status != "success" {
		t.Fatalf("unexpected parsed history item: %#v", resp.Items)
	}
}

func TestParseHistoryResponsePayloadKeyedVideosOrderIsStable(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"Data": map[string]any{
			"Items": []any{
				map[string]any{
					"HistoryID": "hist-video-order-1",
					"Videos": map[string]any{
						"zeta": map[string]any{
							"VideoURL": "https://cdn.example.com/zeta.mp4",
						},
						"alpha": map[string]any{
							"VideoURL": "https://cdn.example.com/alpha.mp4",
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-video-order-1"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if len(item.Videos) != 2 {
		t.Fatalf("unexpected parsed videos: %#v", item.Videos)
	}
	if item.Videos[0].VideoURL != "https://cdn.example.com/alpha.mp4" || item.Videos[1].VideoURL != "https://cdn.example.com/zeta.mp4" {
		t.Fatalf("expected keyed videos order to be stable, got %#v", item.Videos)
	}
	if item.VideoURL != "https://cdn.example.com/alpha.mp4" {
		t.Fatalf("expected primary video url to follow stable order, got %#v", item.VideoURL)
	}
}

func TestParseHistoryResponsePayloadKeyedImagesOrderIsStable(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"Data": map[string]any{
			"Items": []any{
				map[string]any{
					"HistoryID": "hist-image-order-1",
					"Images": map[string]any{
						"zeta": map[string]any{
							"ImageURL": "https://cdn.example.com/zeta.png",
						},
						"alpha": map[string]any{
							"ImageURL": "https://cdn.example.com/alpha.png",
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-image-order-1"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if len(item.Images) != 2 {
		t.Fatalf("unexpected parsed images: %#v", item.Images)
	}
	if item.Images[0].ImageURL != "https://cdn.example.com/alpha.png" || item.Images[1].ImageURL != "https://cdn.example.com/zeta.png" {
		t.Fatalf("expected keyed images order to be stable, got %#v", item.Images)
	}
	if item.ImageURL != "https://cdn.example.com/alpha.png" {
		t.Fatalf("expected primary image url to follow stable order, got %#v", item.ImageURL)
	}
}

func TestParseHistoryResponsePayloadPrefersTopLevelImagesOverNestedItemList(t *testing.T) {
	t.Helper()

	resp := parseHistoryResponsePayload(map[string]any{
		"Data": map[string]any{
			"Items": []any{
				map[string]any{
					"HistoryID": "hist-image-prefer-top-1",
					"Images": []any{
						map[string]any{
							"ImageURL": "https://cdn.example.com/final-top.png",
							"Width":    5404,
							"Height":   3040,
						},
					},
					"ItemList": []any{
						map[string]any{
							"Image": map[string]any{
								"LargeImages": []any{
									map[string]any{
										"ImageURL": "https://cdn.example.com/nested-fallback.png",
										"Width":    2048,
										"Height":   2048,
									},
								},
							},
						},
					},
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected parsed response")
	}
	item := resp.Items["hist-image-prefer-top-1"]
	if item == nil {
		t.Fatalf("expected parsed history item, got %#v", resp.Items)
	}
	if len(item.Images) != 1 {
		t.Fatalf("unexpected parsed images: %#v", item.Images)
	}
	if item.Images[0].ImageURL != "https://cdn.example.com/final-top.png" {
		t.Fatalf("expected top-level final image to win, got %#v", item.Images)
	}
	if item.ImageURL != "https://cdn.example.com/final-top.png" {
		t.Fatalf("expected primary image url to follow top-level images, got %#v", item.ImageURL)
	}
}

func TestGetHistoryByIdsReturnsTransportFailureWithoutFallback(t *testing.T) {
	t.Helper()

	httpCli, err := httpclient.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.GetHistoryByIds(context.Background(), &Session{Cookie: "sid=test", UserID: "u-1"}, &GetHistoryByIdsRequest{
		HistoryIDs: []string{"hist-1"},
	})
	if err != nil {
		t.Fatalf("GetHistoryByIds failed: %v", err)
	}
	if resp.Code != "transport_error" {
		t.Fatalf("unexpected transport response code: %#v", resp.Code)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected no fallback items, got %#v", resp.Items)
	}
	if resp.Message == "" {
		t.Fatalf("expected transport error message")
	}
}

func TestGetHistoryByIdsReturnsDecodeFailureWithoutFallback(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	resp, err := client.GetHistoryByIds(context.Background(), &Session{Cookie: "sid=test"}, &GetHistoryByIdsRequest{
		HistoryIDs: []string{"hist-1"},
	})
	if err != nil {
		t.Fatalf("GetHistoryByIds failed: %v", err)
	}
	if resp.Code != "response_decode_error" {
		t.Fatalf("unexpected response code: %#v", resp.Code)
	}
	if resp.Message != "not-json" {
		t.Fatalf("unexpected response message: %#v", resp.Message)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected no synthetic fallback items, got %#v", resp.Items)
	}
}

func TestFormatHistoryQueueStatusMatchesOriginalLabels(t *testing.T) {
	t.Helper()

	if got := formatHistoryQueueStatus("1"); got != "Queueing" {
		t.Fatalf("unexpected queue status for code 1: %q", got)
	}
	if got := formatHistoryQueueStatus("2"); got != "Generating" {
		t.Fatalf("unexpected queue status for code 2: %q", got)
	}
	if got := formatHistoryQueueStatus("3"); got != "Finish" {
		t.Fatalf("unexpected queue status for code 3: %q", got)
	}
}

func writeMCPJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode json response: %v", err)
	}
}
