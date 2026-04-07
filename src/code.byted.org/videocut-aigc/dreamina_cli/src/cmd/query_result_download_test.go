package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

func TestParseRemoteQueryResultNormalizesMediaPayload(t *testing.T) {
	t.Helper()

	raw := `{
		"images": ["file:///tmp/a.png"],
		"videos": [{"url":"file:///tmp/b.mp4","cover_url":"file:///tmp/b-cover.png"}]
	}`

	got, err := parseRemoteQueryResult(raw)
	if err != nil {
		t.Fatalf("parseRemoteQueryResult failed: %v", err)
	}

	media, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", got)
	}
	images, ok := media["images"].([]map[string]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected images payload: %#v", media["images"])
	}
	if images[0]["type"] != "image" {
		t.Fatalf("unexpected image item: %#v", images[0])
	}
	videos, ok := media["videos"].([]map[string]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("unexpected videos payload: %#v", media["videos"])
	}
	if videos[0]["cover_url"] != "file:///tmp/b-cover.png" {
		t.Fatalf("unexpected video item: %#v", videos[0])
	}
}

func TestParseRemoteQueryResultDeduplicatesDuplicateImages(t *testing.T) {
	t.Helper()

	raw := `{
		"images": [
			{
				"image_uri": "tos-cn-i/example-1",
				"image_url": "https://example.com/tos-cn-i/example-1~tplv-aigc_resize:0:0.png?format=.png",
				"url": "https://example.com/tos-cn-i/example-1~tplv-aigc_resize:0:0.png?format=.png",
				"width": 2048,
				"height": 2048
			},
			{
				"image_url": "https://example.com/tos-cn-i/example-1~tplv-aigc_resize:0:0.png?format=.png",
				"url": "https://example.com/tos-cn-i/example-1~tplv-aigc_resize:0:0.png?format=.png",
				"width": 2048,
				"height": 2048
			}
		],
		"videos": []
	}`

	got, err := parseRemoteQueryResult(raw)
	if err != nil {
		t.Fatalf("parseRemoteQueryResult failed: %v", err)
	}

	media, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", got)
	}
	images, ok := media["images"].([]map[string]any)
	if !ok || len(images) != 1 {
		t.Fatalf("expected duplicate images to be collapsed, got %#v", media["images"])
	}
}

func TestDownloadQueryResultMediaWritesExpectedPaths(t *testing.T) {
	t.Helper()

	srcDir := t.TempDir()
	imageSrc := filepath.Join(srcDir, "source-image.png")
	videoSrc := filepath.Join(srcDir, "source-video.mp4")
	if err := os.WriteFile(imageSrc, []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("write source image: %v", err)
	}
	if err := os.WriteFile(videoSrc, []byte("video-bytes"), 0o644); err != nil {
		t.Fatalf("write source video: %v", err)
	}

	downloadDir := t.TempDir()
	got, err := downloadQueryResultMedia(&task.AIGCTask{
		SubmitID: "submit-1",
	}, map[string]any{
		"images": []any{map[string]any{"url": "file://" + imageSrc}},
		"videos": []any{map[string]any{"url": "file://" + videoSrc}},
	}, downloadDir)
	if err != nil {
		t.Fatalf("downloadQueryResultMedia failed: %v", err)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal downloaded payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal downloaded payload: %v", err)
	}

	imageItems, ok := payload["images"].([]any)
	if !ok || len(imageItems) != 1 {
		t.Fatalf("unexpected image downloads: %#v", payload["images"])
	}
	imageItem := imageItems[0].(map[string]any)
	if gotPath := imageItem["path"]; gotPath != filepath.Join(downloadDir, "submit-1_image_1.png") {
		t.Fatalf("unexpected image path: %#v", gotPath)
	}
	imageBody, err := os.ReadFile(filepath.Join(downloadDir, "submit-1_image_1.png"))
	if err != nil {
		t.Fatalf("read downloaded image: %v", err)
	}
	if string(imageBody) != "image-bytes" {
		t.Fatalf("unexpected image content: %q", string(imageBody))
	}

	videoItems, ok := payload["videos"].([]any)
	if !ok || len(videoItems) != 1 {
		t.Fatalf("unexpected video downloads: %#v", payload["videos"])
	}
	videoItem := videoItems[0].(map[string]any)
	if gotPath := videoItem["path"]; gotPath != filepath.Join(downloadDir, "submit-1_video_1.mp4") {
		t.Fatalf("unexpected video path: %#v", gotPath)
	}
	videoBody, err := os.ReadFile(filepath.Join(downloadDir, "submit-1_video_1.mp4"))
	if err != nil {
		t.Fatalf("read downloaded video: %v", err)
	}
	if string(videoBody) != "video-bytes" {
		t.Fatalf("unexpected video content: %q", string(videoBody))
	}
}

func TestDownloadQueryResultMediaDeduplicatesDuplicateImages(t *testing.T) {
	t.Helper()

	srcDir := t.TempDir()
	imageSrc := filepath.Join(srcDir, "source-image.png")
	if err := os.WriteFile(imageSrc, []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("write source image: %v", err)
	}

	downloadDir := t.TempDir()
	got, err := downloadQueryResultMedia(&task.AIGCTask{
		SubmitID: "submit-dedupe-1",
	}, map[string]any{
		"images": []any{
			map[string]any{"image_uri": "tos-cn-i/example-1", "url": "file://" + imageSrc},
			map[string]any{"url": "file://" + imageSrc},
		},
		"videos": []any{},
	}, downloadDir)
	if err != nil {
		t.Fatalf("downloadQueryResultMedia failed: %v", err)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal downloaded payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal downloaded payload: %v", err)
	}
	imageItems, ok := payload["images"].([]any)
	if !ok || len(imageItems) != 1 {
		t.Fatalf("expected duplicate image downloads to be collapsed, got %#v", payload["images"])
	}
}

func TestBuildDownloadedMediaFilenameMatchesRemoteExtensionBehavior(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{SubmitID: "submit-1"}
	imageName := buildDownloadedMediaFilename(
		taskValue,
		"image",
		1,
		"https://example.com/media/result.image?x=1",
		".png",
	)
	if imageName != "submit-1_image_1.png" {
		t.Fatalf("unexpected remote image filename: %q", imageName)
	}

	videoName := buildDownloadedMediaFilename(
		taskValue,
		"video",
		1,
		"https://example.com/media/result_without_ext?x=1",
		".mp4",
	)
	if videoName != "submit-1_video_1.mp4" {
		t.Fatalf("unexpected remote video filename: %q", videoName)
	}

	videoMIMEName := buildDownloadedMediaFilename(
		taskValue,
		"video",
		1,
		"https://example.com/media/result_without_ext?mime_type=video_mp4",
		".mp4",
	)
	if videoMIMEName != "submit-1_video_1.mp4" {
		t.Fatalf("unexpected remote video filename with mime_type: %q", videoMIMEName)
	}
}

func TestBuildQueryResultOutputRewritesDownloadedMediaIntoResultJSON(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-1",
			GenStatus: "success",
			CommerceInfo: map[string]any{
				"credit_count": 4,
			},
			ResultJSON: `{
				"images": [{"image_url":"file:///tmp/a.png","width": 100, "height": 200}],
				"videos": [],
				"queue_info": {"queue_status":"Finish"}
			}`,
		},
		map[string]any{"images": []any{"file:///tmp/a.png"}, "videos": []any{}},
		map[string]any{"images": []map[string]any{{"path": "/tmp/out/image-1.png"}}},
		"/tmp/out",
	)

	root, ok := got.(map[string]any)
	if !ok {
		body, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal query_result output: %v", err)
		}
		if err := json.Unmarshal(body, &root); err != nil {
			t.Fatalf("unmarshal query_result output: %v", err)
		}
	}
	if root["submit_id"] != "submit-1" {
		t.Fatalf("unexpected submit_id: %#v", root["submit_id"])
	}
	if root["credit_count"] != float64(4) {
		t.Fatalf("unexpected credit_count: %#v", root["credit_count"])
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result_json payload: %#v", root["result_json"])
	}
	images, ok := resultJSON["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected result_json images: %#v", resultJSON["images"])
	}
	imageItem, ok := images[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected image result item: %#v", images[0])
	}
	if imageItem["path"] != "/tmp/out/image-1.png" {
		t.Fatalf("unexpected image path: %#v", imageItem["path"])
	}
	if imageItem["width"] != float64(100) || imageItem["height"] != float64(200) {
		t.Fatalf("unexpected downloaded image dimensions: %#v", imageItem)
	}
	if _, exists := root["download_dir"]; exists {
		t.Fatalf("did not expect download_dir in output: %#v", root["download_dir"])
	}
	if _, exists := root["downloaded"]; exists {
		t.Fatalf("did not expect downloaded payload in output: %#v", root["downloaded"])
	}
}

func TestBuildQueryResultOutputPreservesDownloadedImageFieldOrder(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-1",
			GenStatus: "success",
			ResultJSON: `{
				"images": [{"image_url":"file:///tmp/a.png","width": 100, "height": 200}],
				"videos": []
			}`,
		},
		map[string]any{"images": []any{"file:///tmp/a.png"}, "videos": []any{}},
		map[string]any{"images": []map[string]any{{"path": "/tmp/out/image-1.png"}}},
		"/tmp/out",
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	if !strings.Contains(string(body), `"images":[{"path":"/tmp/out/image-1.png","width":100,"height":200}]`) {
		t.Fatalf("unexpected downloaded image field order: %s", string(body))
	}
}

func TestBuildQueryResultOutputDeduplicatesStoredDuplicateImagesWhenDownloaded(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-1",
			GenStatus: "success",
			ResultJSON: `{
				"images": [
					{"image_uri":"tos-cn-i/example-1","image_url":"https://example.com/final.png","width": 100, "height": 200},
					{"image_url":"https://example.com/final.png","width": 100, "height": 200}
				],
				"videos": []
			}`,
		},
		map[string]any{"images": []any{map[string]any{"url": "https://example.com/final.png"}}, "videos": []any{}},
		map[string]any{"images": []map[string]any{{"path": "/tmp/out/image-1.png"}}},
		"/tmp/out",
	)

	root, ok := got.(map[string]any)
	if !ok {
		body, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal query_result output: %v", err)
		}
		if err := json.Unmarshal(body, &root); err != nil {
			t.Fatalf("unmarshal query_result output: %v", err)
		}
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result_json payload: %#v", root["result_json"])
	}
	images, ok := resultJSON["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("expected duplicate stored images to be collapsed, got %#v", resultJSON["images"])
	}
}

func TestBuildQueryResultOutputOmitsEmptyResultJSONButKeepsLogIDWhileQuerying(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:     "submit-1",
			GenStatus:    "querying",
			LogID:        "20260405004412192168001245420E622",
			CommerceInfo: map[string]any{"credit_count": 4},
			ResultJSON:   `{"images":[],"videos":[]}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["logid"] != "20260405004412192168001245420E622" {
		t.Fatalf("unexpected logid: %#v", root["logid"])
	}
	if _, ok := root["result_json"]; ok {
		t.Fatalf("expected empty querying result_json to be omitted: %#v", root["result_json"])
	}
	if root["credit_count"] != float64(4) {
		t.Fatalf("unexpected credit_count: %#v", root["credit_count"])
	}
}

func TestBuildQueryResultOutputKeepsRawFailedLikeResultJSONWhileTopLevelStillQuerying(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-1",
			GenStatus: "querying",
			LogID:     "log-failed-like-1",
			ResultJSON: `{
				"gen_status":"failed",
				"gen_task_type":"image2video",
				"first_frame":[{"path":"/tmp/a.png","type":"image"}],
				"queue_info":{"queue_status":"failed","history_id":"submit-1"}
			}`,
		},
		nil,
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["gen_status"] != "querying" {
		t.Fatalf("unexpected top-level gen_status: %#v", root["gen_status"])
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw failed-like result_json: %#v", root["result_json"])
	}
	if resultJSON["gen_status"] != "failed" {
		t.Fatalf("unexpected failed-like result_json: %#v", resultJSON)
	}
	if _, ok := resultJSON["first_frame"].([]any); !ok {
		t.Fatalf("expected first_frame to be preserved: %#v", resultJSON)
	}
}

func TestBuildQueryResultOutputFallsBackToResultJSONLogIDAndCreditCount(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:   "submit-remote-only-1",
			GenStatus:  "failed",
			FailReason: "generation failed: final generation failed",
			ResultJSON: `{
				"response": {
					"data": {
						"history": {
							"log_id": "log-history-1",
							"commerce_info": {
								"credit_count": 50
							}
						}
					}
				},
				"queue_info": {
					"queue_status": "Finish"
				}
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["logid"] != "log-history-1" {
		t.Fatalf("unexpected fallback logid: %#v", root["logid"])
	}
	if root["credit_count"] != float64(50) {
		t.Fatalf("unexpected fallback credit_count: %#v", root["credit_count"])
	}
}

func TestBuildQueryResultOutputFallsBackToQueueHistoryLogIDAndCreditCount(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:   "submit-remote-only-2",
			GenStatus:  "failed",
			FailReason: "generation failed: final generation failed",
			ResultJSON: `{
				"queue_info": {
					"queue_status": "Finish",
					"history": {
						"log_id": "log-history-2",
						"commerce_info": {
							"credit_count": 50
						}
					}
				}
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["logid"] != "log-history-2" {
		t.Fatalf("unexpected queue-history logid: %#v", root["logid"])
	}
	if root["credit_count"] != float64(50) {
		t.Fatalf("unexpected queue-history credit_count: %#v", root["credit_count"])
	}
}

func TestBuildQueryResultOutputUsesQueueHistoryGenerateIDAsLogID(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:   "submit-remote-only-3",
			GenStatus:  "failed",
			FailReason: "generation failed: final generation failed",
			ResultJSON: `{
				"queue_info": {
					"queue_status": "Finish",
					"history": {
						"generate_id": "history-generate-3"
					}
				}
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["logid"] != "history-generate-3" {
		t.Fatalf("unexpected queue-history generate_id logid: %#v", root["logid"])
	}
}

func TestBuildQueryResultOutputOmitsZeroCreditCountOnSuccess(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-zero-credit-1",
			GenStatus: "success",
			CommerceInfo: map[string]any{
				"credit_count": 0,
			},
			ResultJSON: `{
				"images": [{"image_url":"https://example.com/a.png","width": 100, "height": 200}],
				"videos": [],
				"queue_info": {"queue_status":"Finish"}
			}`,
		},
		map[string]any{"images": []any{"https://example.com/a.png"}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["submit_id"] != "submit-zero-credit-1" {
		t.Fatalf("unexpected submit_id: %#v", root["submit_id"])
	}
	if _, exists := root["credit_count"]; exists {
		t.Fatalf("did not expect zero credit_count in success output: %#v", root["credit_count"])
	}
}

func TestBuildQueryResultOutputKeepsCompactQueueInfoWhileQueryingWithRecoveredContext(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:     "submit-1",
			GenStatus:    "querying",
			LogID:        "mcp-text2image-88dabe98",
			CommerceInfo: map[string]any{"credit_count": 4},
			ResultJSON: `{
				"backend":"dreamina",
				"gen_status":"querying",
				"gen_task_type":"text2image",
				"input":{"prompt":"test"},
				"log_id":"mcp-text2image-88dabe98",
				"queue_info":{"queue_status":"submitted"},
				"recovered":true,
				"request":{"prompt":"test"},
				"response":{"code":"0","message":"ok"}
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected compact querying result_json, got: %#v", root["result_json"])
	}
	if _, ok := root["queue_info"]; ok {
		t.Fatalf("expected top-level queue_info to be omitted when querying result_json is present: %#v", root["queue_info"])
	}
	if _, exists := resultJSON["backend"]; exists {
		t.Fatalf("did not expect backend in querying result_json: %#v", resultJSON)
	}
	queueInfo, ok := resultJSON["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("expected queue_info in querying result_json: %#v", resultJSON)
	}
	if queueInfo["queue_status"] != "submitted" {
		t.Fatalf("unexpected queue status in querying result_json: %#v", queueInfo)
	}
}

func TestBuildQueryResultOutputCollapsesRecoveredFailedPlaceholderWhileQuerying(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-failed-placeholder-1",
			GenStatus: "querying",
			LogID:     "log-failed-placeholder-1",
			ResultJSON: `{
				"gen_status":"failed",
				"gen_task_type":"image2video",
				"queue_info":{"queue_status":"submitted"},
				"videos":[],
				"images":[]
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected compact result_json, got %#v", root["result_json"])
	}
	if _, ok := resultJSON["queue_info"]; ok {
		t.Fatalf("did not expect queue_info in failed placeholder querying result_json: %#v", resultJSON["queue_info"])
	}
	images, ok := resultJSON["images"].([]any)
	if !ok || len(images) != 0 {
		t.Fatalf("unexpected images payload: %#v", resultJSON["images"])
	}
	videos, ok := resultJSON["videos"].([]any)
	if !ok || len(videos) != 0 {
		t.Fatalf("unexpected videos payload: %#v", resultJSON["videos"])
	}
}

func TestBuildQueryResultOutputDoesNotKeepVerboseQueryingForTimestampOnly(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-query-timestamp-only-1",
			GenStatus: "querying",
			LogID:     "log-query-timestamp-only-1",
			ResultJSON: `{
				"gen_status":"querying",
				"gen_task_type":"text2video",
				"input":{"prompt":"hello"},
				"images":[],
				"queue_info":{
					"queue_status":"submitted",
					"last_queried_at":1775371433
				},
				"recovered":true
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if _, ok := root["result_json"]; ok {
		t.Fatalf("did not expect verbose querying result_json for timestamp-only queue_info: %#v", root["result_json"])
	}
}

func TestBuildQueryResultOutputKeepsVerboseQueryingResultJSONAfterHistoryQuery(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:  "submit-query-verbose-1",
			GenStatus: "querying",
			LogID:     "log-query-verbose-1",
			ResultJSON: `{
				"gen_status":"querying",
				"gen_task_type":"image2video",
				"first_frame":[{"path":"/tmp/a.png","type":"image"}],
				"input":{"prompt":"hello"},
				"queue_info":{
					"history_id":"submit-query-verbose-1",
					"history_query":{"code":"0"},
					"last_queried_at":1775371433,
					"queue_status":"submitted"
				},
				"recovered":true
			}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected verbose querying result_json, got %#v", root["result_json"])
	}
	if _, ok := resultJSON["first_frame"].([]any); !ok {
		t.Fatalf("expected first_frame to be preserved: %#v", resultJSON)
	}
	queueInfo, ok := resultJSON["queue_info"].(map[string]any)
	if !ok || queueInfo["history_id"] != "submit-query-verbose-1" {
		t.Fatalf("unexpected queue_info: %#v", resultJSON["queue_info"])
	}
}

func TestBuildQueryResultOutputStripsRecoveredFieldsWhenMediaPresent(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:     "submit-1",
			GenStatus:    "failed",
			LogID:        "log-fail-1",
			FailReason:   "generation failed: final generation failed",
			CommerceInfo: map[string]any{"credit_count": 50},
			ResultJSON: `{
				"backend":"dreamina",
				"gen_status":"success",
				"gen_task_type":"multimodal2video",
				"images":[{"image_url":"https://example.com/image.png","type":"image"}],
				"videos":[{"video_url":"https://example.com/video.mp4","type":"video"}],
				"uploaded_images":[{"resource_id":"image-1"}],
				"uploaded_videos":[{"resource_id":"video-1"}],
				"queue_info":{"queue_status":"Finish","queue_idx":0,"priority":1,"queue_length":0},
				"recovered":{"history_id":"hist-1"},
				"input":{"prompt":"test"},
				"submit_info":{"code":0}
			}`,
		},
		map[string]any{
			"images": []any{map[string]any{"url": "https://example.com/image.png"}},
			"videos": []any{map[string]any{"url": "https://example.com/video.mp4"}},
		},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result_json payload: %#v", root["result_json"])
	}
	if _, exists := resultJSON["uploaded_images"]; exists {
		t.Fatalf("did not expect uploaded_images in result_json: %#v", resultJSON)
	}
	if _, exists := resultJSON["recovered"]; exists {
		t.Fatalf("did not expect recovered in result_json: %#v", resultJSON)
	}
	if _, exists := resultJSON["input"]; exists {
		t.Fatalf("did not expect input in result_json: %#v", resultJSON)
	}
	images, ok := resultJSON["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected compact images: %#v", resultJSON["images"])
	}
	videos, ok := resultJSON["videos"].([]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("unexpected compact videos: %#v", resultJSON["videos"])
	}
}

func TestBuildQueryResultOutputKeepsVideoMetadataInResultJSON(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:   "submit-video-1",
			GenStatus:  "success",
			ResultJSON: `{"images":[],"videos":[{"video_url":"https://example.com/result.mp4","fps":24,"width":1470,"height":630,"format":"mp4","duration":5.042}]}`,
		},
		map[string]any{
			"images": []any{},
			"videos": []any{map[string]any{
				"url":      "https://example.com/result.mp4",
				"fps":      24,
				"width":    1470,
				"height":   630,
				"format":   "mp4",
				"duration": 5.042,
			}},
		},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result_json payload: %#v", root["result_json"])
	}
	videos, ok := resultJSON["videos"].([]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("unexpected videos payload: %#v", resultJSON["videos"])
	}
	video, ok := videos[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected video item: %#v", videos[0])
	}
	if video["fps"] != float64(24) || video["width"] != float64(1470) || video["height"] != float64(630) {
		t.Fatalf("unexpected video dimensions: %#v", video)
	}
	if video["format"] != "mp4" || video["duration"] != 5.042 {
		t.Fatalf("unexpected video format payload: %#v", video)
	}
}

func TestQueryResultQueueInfoUsesOriginalFieldOrder(t *testing.T) {
	t.Helper()

	got := queryResultQueueInfo(`{
		"queue_info": {
			"queue_status": "Finish",
			"queue_idx": 0,
			"priority": 1,
			"queue_length": 0,
			"debug_info": "debug-1"
		}
	}`)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal queue_info: %v", err)
	}
	if string(body) != `{"queue_idx":0,"priority":1,"queue_status":"Finish","queue_length":0,"debug_info":"debug-1"}` {
		t.Fatalf("unexpected queue_info json: %s", string(body))
	}
}

func TestBuildQueryResultOutputKeepsRawResultJSONWhileFailed(t *testing.T) {
	t.Helper()

	got := buildQueryResultOutput(
		&task.AIGCTask{
			SubmitID:     "submit-1",
			RequestRaw:   `{"method":"POST","url":"/dreamina/cli/v1/video_generate","body":"{\"prompt\":\"保持主体不变，做自然镜头运动\"}"}`,
			GenStatus:    "failed",
			LogID:        "20260405004412192168001245420E622",
			FailReason:   "generation failed: final generation failed",
			CommerceInfo: map[string]any{"credit_count": 4},
			ResultJSON:   `{"gen_status":"success","queue_info":{"queue_status":"Finish"},"failed_item_list":[{"id":"1"}]}`,
		},
		map[string]any{"images": []any{}, "videos": []any{}},
	)

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query_result output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal query_result output: %v", err)
	}
	if root["gen_status"] != "fail" {
		t.Fatalf("unexpected gen_status: %#v", root["gen_status"])
	}
	if root["prompt"] != "保持主体不变，做自然镜头运动" {
		t.Fatalf("unexpected prompt: %#v", root["prompt"])
	}
	resultJSON, ok := root["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected raw result_json: %#v", root["result_json"])
	}
	if _, ok := resultJSON["failed_item_list"].([]any); !ok {
		t.Fatalf("expected failed_item_list in raw result_json: %#v", resultJSON)
	}
}

func TestTaskListViewMatchesOriginalShape(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-1",
			GenTaskType: "text2image",
			GenStatus:   "success",
			ResultJSON: `{
				"images": [{"width": 5404, "height": 3040}],
				"videos": [],
				"response": {
					"data": {
						"commerce_info": {
							"credit_count": 4
						}
					}
				}
			}`,
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	if _, exists := items[0]["uid"]; exists {
		t.Fatalf("expected uid to be omitted: %#v", items[0])
	}
	if items[0]["submit_id"] != "submit-1" {
		t.Fatalf("unexpected submit_id: %#v", items[0]["submit_id"])
	}
	if _, ok := items[0]["result_json"].(map[string]any); !ok {
		t.Fatalf("unexpected result_json: %#v", items[0]["result_json"])
	}
	resultJSON := items[0]["result_json"].(map[string]any)
	response, ok := resultJSON["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw response payload in list_task success result_json: %#v", resultJSON)
	}
	images, ok := resultJSON["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected raw success images: %#v", resultJSON["images"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response.data payload: %#v", response)
	}
	embeddedCommerce, ok := data["commerce_info"].(map[string]any)
	if !ok || embeddedCommerce["credit_count"] != float64(4) {
		t.Fatalf("unexpected embedded commerce_info: %#v", data["commerce_info"])
	}
	if _, ok := items[0]["commerce_info"].(map[string]any); !ok {
		t.Fatalf("unexpected commerce_info: %#v", items[0]["commerce_info"])
	}
	commerce := items[0]["commerce_info"].(map[string]any)
	triplet, ok := commerce["triplet"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected triplet: %#v", commerce["triplet"])
	}
	if triplet["resource_type"] != "" || triplet["resource_id"] != "" || triplet["benefit_type"] != "" {
		t.Fatalf("unexpected normalized triplet: %#v", triplet)
	}
}

func TestTaskListViewKeepsRawSuccessResultJSON(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-1",
			GenTaskType: "text2image",
			GenStatus:   "success",
			ResultJSON: `{
				"gen_status":"success",
				"images":[{"image_url":"https://example.com/a.png","width":5404,"height":3040}],
				"queue_info":{"queue_status":"Finish","queue_idx":0}
			}`,
			CommerceInfo: map[string]any{"credit_count": 4},
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	resultJSON, ok := items[0]["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result_json: %#v", items[0]["result_json"])
	}
	if resultJSON["gen_status"] != "success" {
		t.Fatalf("expected raw success gen_status in list_task result_json: %#v", resultJSON)
	}
	queueInfo, ok := resultJSON["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw success queue_info in list_task result_json: %#v", resultJSON)
	}
	if queueInfo["queue_status"] != "Finish" {
		t.Fatalf("unexpected raw success queue_info: %#v", queueInfo)
	}
	images, ok := resultJSON["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected raw success images: %#v", resultJSON["images"])
	}
}

func TestTaskListViewKeepsDefaultCommerceInfoWhileQuerying(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-1",
			GenTaskType: "text2image",
			GenStatus:   "querying",
			ResultJSON: `{
				"gen_status":"querying",
				"gen_task_type":"text2image",
				"images":[],
				"queue_info":{"queue_status":"submitted"}
			}`,
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	commerce, ok := items[0]["commerce_info"].(map[string]any)
	if !ok {
		t.Fatalf("expected default commerce_info, got: %#v", items[0]["commerce_info"])
	}
	if commerce["credit_count"] != float64(0) {
		t.Fatalf("unexpected default credit_count: %#v", commerce["credit_count"])
	}
	triplet, ok := commerce["triplet"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected default triplet: %#v", commerce["triplet"])
	}
	if triplet["resource_type"] != "" || triplet["resource_id"] != "" || triplet["benefit_type"] != "" {
		t.Fatalf("unexpected default triplet payload: %#v", triplet)
	}
}

func TestTaskListViewKeepsSkeletonResultJSONWhileQuerying(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-1",
			GenTaskType: "text2image",
			GenStatus:   "querying",
			ResultJSON: `{
				"gen_status":"querying",
				"gen_task_type":"text2image",
				"input":{"prompt":"test"},
				"images":[],
				"queue_info":{"queue_status":"submitted","progress":0,"query_count":0},
				"recovered":true
			}`,
			CommerceInfo: map[string]any{
				"credit_count": 4,
			},
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	resultJSON, ok := items[0]["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw skeleton querying result_json: %#v", items[0]["result_json"])
	}
	if resultJSON["gen_status"] != "querying" {
		t.Fatalf("unexpected raw skeleton gen_status: %#v", resultJSON)
	}
}

func TestTaskListViewOmitsEmptyResultJSONWhileQuerying(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-1",
			GenTaskType: "text2image",
			GenStatus:   "querying",
			ResultJSON:  "",
			CommerceInfo: map[string]any{
				"credit_count": 4,
			},
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	if _, ok := items[0]["result_json"]; ok {
		t.Fatalf("expected empty querying result_json to be omitted: %#v", items[0]["result_json"])
	}
}

func TestTaskListViewKeepsRawResultJSONWhileFailed(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-fail-1",
			GenTaskType: "text2image",
			GenStatus:   "failed",
			FailReason:  "generation failed: final generation failed",
			ResultJSON: `{
				"gen_status": "success",
				"failed_item_list": [{"id":"1"}],
				"queue_info": {"queue_status": "Finish"}
			}`,
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	resultJSON, ok := items[0]["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected raw result_json: %#v", items[0]["result_json"])
	}
	if _, ok := resultJSON["failed_item_list"].([]any); !ok {
		t.Fatalf("expected failed_item_list in raw result_json: %#v", resultJSON)
	}
}

func TestTaskListViewKeepsRawResultJSONWhileQuerying(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-query-1",
			GenTaskType: "text2image",
			GenStatus:   "querying",
			ResultJSON: `{
				"log_id": "20260405004412192168001245420E622",
				"queue_info": {"queue_status": "Generating"}
			}`,
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	resultJSON, ok := items[0]["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected raw result_json: %#v", items[0]["result_json"])
	}
	queueInfo, ok := resultJSON["queue_info"].(map[string]any)
	if !ok || queueInfo["queue_status"] != "Generating" {
		t.Fatalf("unexpected querying result_json: %#v", resultJSON)
	}
}

func TestTaskListViewKeepsVerboseRecoveredResultJSONWhileQuerying(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-query-verbose-1",
			GenTaskType: "text2video",
			GenStatus:   "querying",
			ResultJSON: `{
				"gen_status":"querying",
				"gen_task_type":"text2video",
				"input":null,
				"queue_info":{
					"queue_status":"submitted",
					"history":{"history_id":"hist-1"},
					"history_query":{"code":"0"}
				},
				"response":{"data":{"history":{"history_id":"hist-1"}}}
			}`,
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	resultJSON, ok := items[0]["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw verbose recovered querying result_json: %#v", items[0]["result_json"])
	}
	queueInfo, ok := resultJSON["queue_info"].(map[string]any)
	if !ok || queueInfo["queue_status"] != "submitted" {
		t.Fatalf("unexpected verbose recovered querying result_json: %#v", resultJSON)
	}
}

func TestTaskListViewKeepsVerboseRecoveredMediaResultJSONWhileQuerying(t *testing.T) {
	t.Helper()

	got := taskListView([]*task.AIGCTask{
		{
			SubmitID:    "submit-query-verbose-media-1",
			GenTaskType: "text2video",
			GenStatus:   "querying",
			ResultJSON: `{
				"backend":"dreamina",
				"gen_status":"querying",
				"gen_task_type":"text2video",
				"input":{"prompt":"让云海翻涌","duration":"5"},
				"log_id":"20260405090332782BF23F279943A6F65",
				"queue_info":{
					"queue_status":"submitted",
					"debug_info":"{}",
					"history":{"history_id":"hist-1"}
				},
				"videos":[{"url":"https://example.com/high.mp4","width":1470,"height":630,"format":"mp4"}]
			}`,
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list_task payload: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list_task payload: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected list_task payload: %#v", got)
	}
	resultJSON, ok := items[0]["result_json"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw verbose recovered querying media result_json: %#v", items[0]["result_json"])
	}
	videos, ok := resultJSON["videos"].([]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("unexpected verbose recovered querying media result_json: %#v", resultJSON)
	}
}

func TestCurrentUserIDFromSessionAcceptsUpperCamelAliases(t *testing.T) {
	t.Helper()

	got := currentUserIDFromSession(map[string]any{
		"Payload": map[string]any{
			"Session": map[string]any{
				"UserID": "user-upper-1",
			},
		},
	})
	if got != "user-upper-1" {
		t.Fatalf("unexpected user id: %q", got)
	}

	got = currentUserIDFromSession(map[string]any{
		"profile": map[string]any{
			"UID": 12345,
		},
	})
	if got != "12345" {
		t.Fatalf("unexpected uid alias: %q", got)
	}

	got = currentUserIDFromSession(map[string]any{
		"user_id": "4.091737426886912e+15",
	})
	if got != "4091737426886912" {
		t.Fatalf("unexpected normalized scientific-notation user id: %q", got)
	}
}

func TestCompactGeneratorSubmitViewMatchesOriginalSubmitShape(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:  "local-submit-1",
		GenStatus: "querying",
		LogID:     "20260404234210192168001245723FD58",
		ResultJSON: `{
			"gen_status": "querying",
			"log_id": "20260404234210192168001245723FD58",
			"response": {
				"data": {
					"submit_id": "remote-submit-1",
					"commerce_info": {
						"credit_count": 4
					}
				}
			}
		}`,
	}

	got := compactGeneratorSubmitView(taskValue)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal compact output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal compact output: %v", err)
	}
	if root["submit_id"] != "remote-submit-1" {
		t.Fatalf("unexpected submit_id: %#v", root["submit_id"])
	}
	if root["logid"] != "20260404234210192168001245723FD58" {
		t.Fatalf("unexpected logid: %#v", root["logid"])
	}
	if root["gen_status"] != "querying" {
		t.Fatalf("unexpected gen_status: %#v", root["gen_status"])
	}
	if root["credit_count"] != float64(4) {
		t.Fatalf("unexpected credit_count: %#v", root["credit_count"])
	}
}
