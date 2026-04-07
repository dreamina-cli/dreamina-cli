package gen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

func TestBuildSkeletonResultJSONIncludesRecoveredTaskDetails(t *testing.T) {
	t.Helper()

	raw := buildSkeletonResultJSON("image2video", map[string]any{
		"image_path":    "/tmp/first.png",
		"use_by_config": "legacy",
	})

	root := parseRecoveredResultRoot(raw)
	if root["gen_task_type"] != "image2video" {
		t.Fatalf("unexpected gen_task_type: %#v", root["gen_task_type"])
	}
	if root["gen_status"] != "querying" {
		t.Fatalf("unexpected gen_status: %#v", root["gen_status"])
	}
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["queue_status"] != "submitted" {
		t.Fatalf("unexpected queue status: %#v", queueInfo["queue_status"])
	}
	firstFrame, ok := root["first_frame"].([]any)
	if !ok || len(firstFrame) != 1 {
		t.Fatalf("unexpected first_frame payload: %#v", root["first_frame"])
	}
	frame, ok := firstFrame[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first frame item: %#v", firstFrame[0])
	}
	if frame["path"] != "/tmp/first.png" {
		t.Fatalf("unexpected first frame path: %#v", frame["path"])
	}
	if frame["type"] != "image" {
		t.Fatalf("unexpected first frame type: %#v", frame["type"])
	}
	if root["use_by_config"] != "legacy" {
		t.Fatalf("unexpected use_by_config: %#v", root["use_by_config"])
	}
}

func TestBuildDreaminaResultJSONCarriesRecoveredSubmitMetadata(t *testing.T) {
	t.Helper()

	raw := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		map[string]any{"uploaded_images": []string{"rid-1"}},
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-1",
			Data: map[string]any{
				"submit_id": "submit-1",
				"task_id":   "task-1",
			},
			Recovered: map[string]any{
				"history_id": "hist-1",
				"submitted":  int64(1700000000),
			},
		},
	)

	root := parseRecoveredResultRoot(raw)
	if root["backend"] != "dreamina" {
		t.Fatalf("unexpected backend: %#v", root["backend"])
	}
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["history_id"] != "hist-1" {
		t.Fatalf("unexpected history_id: %#v", queueInfo["history_id"])
	}
	if got := anyInt64(queueInfo["submitted_at"]); got != 1700000000 {
		t.Fatalf("unexpected submitted_at: %d", got)
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	if response["log_id"] != "log-1" {
		t.Fatalf("unexpected response log_id: %#v", response["log_id"])
	}
	recovered, ok := response["recovered"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response recovered payload: %#v", response["recovered"])
	}
	if recovered["history_id"] != "hist-1" {
		t.Fatalf("unexpected recovered history id: %#v", recovered["history_id"])
	}
	if root["uploaded_images"] == nil {
		t.Fatalf("expected uploaded payload to be preserved")
	}
}

func TestBuildDreaminaResultJSONAcceptsUpperCamelRecoveredMetadata(t *testing.T) {
	t.Helper()

	raw := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-upper-1",
			Data: map[string]any{
				"SubmitID": "submit-upper-1",
				"TaskID":   "task-upper-1",
			},
			Recovered: map[string]any{
				"HistoryID":   "hist-upper-1",
				"SubmittedAt": int64(1700001234),
			},
		},
	)

	root := parseRecoveredResultRoot(raw)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["history_id"] != "hist-upper-1" {
		t.Fatalf("unexpected history_id: %#v", queueInfo["history_id"])
	}
	if got := anyInt64(queueInfo["submitted_at"]); got != 1700001234 {
		t.Fatalf("unexpected submitted_at: %d", got)
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response data: %#v", response["data"])
	}
	if data["SubmitID"] != "submit-upper-1" {
		t.Fatalf("unexpected SubmitID: %#v", data["SubmitID"])
	}
}

func TestBuildDreaminaResultJSONPrefersRemoteQueueStatusAndProgress(t *testing.T) {
	t.Helper()

	raw := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-queue-1",
			Data: map[string]any{
				"HistoryID": "hist-queue-1",
				"Queue": map[string]any{
					"QueueStatus": "processing",
					"Progress":    "72%",
				},
			},
		},
	)

	root := parseRecoveredResultRoot(raw)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["history_id"] != "hist-queue-1" {
		t.Fatalf("unexpected history_id: %#v", queueInfo["history_id"])
	}
	if queueInfo["queue_status"] != "running" {
		t.Fatalf("unexpected queue status: %#v", queueInfo["queue_status"])
	}
	if progress := anyInt(queueInfo["progress"]); progress != 72 {
		t.Fatalf("unexpected queue progress: %#v", queueInfo["progress"])
	}
}

func TestBuildDreaminaResultJSONDoesNotCarrySyntheticHistoryID(t *testing.T) {
	t.Helper()

	raw := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-no-history-1",
			Data: map[string]any{
				"submit_id": "submit-no-history-1",
			},
			Recovered: map[string]any{
				"submitted": int64(1700003333),
			},
		},
	)

	root := parseRecoveredResultRoot(raw)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if got := queueInfo["history_id"]; got != "submit-no-history-1" {
		t.Fatalf("expected remote submit_id fallback, got %#v", got)
	}
	if got := anyInt64(queueInfo["submitted_at"]); got != 1700003333 {
		t.Fatalf("unexpected submitted_at: %d", got)
	}
}

func TestExtractLogIDReadsDreaminaAPIError(t *testing.T) {
	t.Helper()

	got := extractLogID(&mcpclient.APIError{
		Code:    "2061",
		Message: "模型已不可用，请刷新界面后再试试",
		LogID:   "log-api-1",
	})
	if got != "log-api-1" {
		t.Fatalf("unexpected extracted log id: %q", got)
	}
}

func TestDreaminaFailureReasonKeepsFullAPIErrorText(t *testing.T) {
	t.Helper()

	got := dreaminaFailureReason(&mcpclient.APIError{
		Code:    "2061",
		Message: "模型已不可用，请刷新界面后再试试",
		LogID:   "log-api-2",
	})
	if !strings.Contains(got, "api error: ret=2061") {
		t.Fatalf("unexpected failure reason: %q", got)
	}
	if !strings.Contains(got, "message=模型已不可用，请刷新界面后再试试") {
		t.Fatalf("unexpected failure reason: %q", got)
	}
	if !strings.Contains(got, "logid=log-api-2") {
		t.Fatalf("unexpected failure reason: %q", got)
	}
}

func TestContextWithSessionAcceptsUpperCamelUserIDAliases(t *testing.T) {
	t.Helper()

	ctx := ContextWithSession(context.Background(), map[string]any{
		"cookie": "sid=test",
		"headers": map[string]any{
			"User-Agent": "ua-test",
		},
		"Payload": map[string]any{
			"Session": map[string]any{
				"UserID": "user-upper-1",
			},
		},
	})

	session := sessionFromContext(ctx)
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.UserID != "user-upper-1" {
		t.Fatalf("unexpected session user id: %#v", session)
	}
	if session.Cookie != "sid=test" {
		t.Fatalf("unexpected session cookie: %#v", session)
	}
	if session.Headers["User-Agent"] != "ua-test" {
		t.Fatalf("unexpected session headers: %#v", session.Headers)
	}
}

func TestContextWithSessionNormalizesScientificNotationUserID(t *testing.T) {
	t.Helper()

	ctx := ContextWithSession(context.Background(), map[string]any{
		"cookie":  "sid=test",
		"user_id": "4.091737426886912e+15",
	})

	session := sessionFromContext(ctx)
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.UserID != "4091737426886912" {
		t.Fatalf("unexpected normalized session user id: %#v", session.UserID)
	}
}

func TestContextWithSessionPrefersExplicitCookiePayloadOverCookieFile(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".dreamina_cli"), 0o755); err != nil {
		t.Fatalf("mkdir cookie dir: %v", err)
	}
	body := []byte("{\n  \"cookie\": \"sid=file-cookie\"\n}\n")
	if err := os.WriteFile(filepath.Join(home, ".dreamina_cli", "cookie.json"), body, 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	ctx := ContextWithSession(context.Background(), map[string]any{
		"cookie": "sid=payload-cookie",
		"headers": map[string]any{
			"User-Agent": "ua-payload",
			"X-Test":     "payload",
		},
		"uid": "user-payload-1",
	})

	session := sessionFromContext(ctx)
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.Cookie != "sid=payload-cookie" {
		t.Fatalf("unexpected session cookie: %#v", session)
	}
	if session.Headers["User-Agent"] != "ua-payload" || session.Headers["X-Test"] != "payload" {
		t.Fatalf("unexpected session headers: %#v", session.Headers)
	}
	if session.UserID != "user-payload-1" {
		t.Fatalf("unexpected session user id: %#v", session.UserID)
	}
}

func TestBuildDreaminaResultJSONDoesNotCarryLocalSubmittedFallback(t *testing.T) {
	t.Helper()

	raw := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-no-submitted-1",
			Data: map[string]any{
				"submit_id": "submit-no-submitted-1",
			},
			Recovered: map[string]any{
				"history_id": "submit-no-submitted-1",
			},
		},
	)

	root := parseRecoveredResultRoot(raw)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if _, exists := queueInfo["submitted_at"]; exists {
		t.Fatalf("did not expect local submitted_at fallback: %#v", queueInfo["submitted_at"])
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	recovered, ok := response["recovered"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response recovered payload: %#v", response["recovered"])
	}
	if _, exists := recovered["submitted"]; exists {
		t.Fatalf("did not expect local submitted fallback: %#v", recovered["submitted"])
	}
}

func TestDeriveRemoteHistoryStateMappings(t *testing.T) {
	t.Helper()

	tests := []struct {
		name         string
		historyItem  map[string]any
		wantStatus   int
		wantQueue    string
		wantProgress int
		wantMatched  bool
	}{
		{
			name: "success",
			historyItem: map[string]any{
				"queue_status": "success",
			},
			wantStatus:   2,
			wantQueue:    "success",
			wantProgress: 100,
			wantMatched:  true,
		},
		{
			name: "failed",
			historyItem: map[string]any{
				"queue_status": "failed",
				"status":       "failed",
			},
			wantStatus:   3,
			wantQueue:    "failed",
			wantProgress: 100,
			wantMatched:  true,
		},
		{
			name: "submitted",
			historyItem: map[string]any{
				"queue_status": "queued",
			},
			wantStatus:   1,
			wantQueue:    "submitted",
			wantProgress: 8,
			wantMatched:  true,
		},
		{
			name: "queueing maps to submitted even when preview image exists",
			historyItem: map[string]any{
				"queue_status": "Queueing",
				"images": []any{
					map[string]any{"image_url": "https://example.com/preview.png"},
				},
			},
			wantStatus:   1,
			wantQueue:    "submitted",
			wantProgress: 8,
			wantMatched:  true,
		},
		{
			name: "running uses remote progress",
			historyItem: map[string]any{
				"queue_status": "processing",
				"progress":     72,
			},
			wantStatus:   1,
			wantQueue:    "running",
			wantProgress: 72,
			wantMatched:  true,
		},
		{
			name: "submitted uses nested queue progress",
			historyItem: map[string]any{
				"queue_status": "pending",
				"queue": map[string]any{
					"progress": 18,
				},
			},
			wantStatus:   1,
			wantQueue:    "submitted",
			wantProgress: 18,
			wantMatched:  true,
		},
		{
			name: "running uses nested queue aliases and percent string",
			historyItem: map[string]any{
				"queue": map[string]any{
					"QueueStatus": "processing",
					"Progress":    "72%",
				},
			},
			wantStatus:   1,
			wantQueue:    "running",
			wantProgress: 72,
			wantMatched:  true,
		},
		{
			name: "status code 45 with generating queue stays querying",
			historyItem: map[string]any{
				"status":       "45",
				"queue_status": "Generating",
			},
			wantStatus:   1,
			wantQueue:    "running",
			wantProgress: 55,
			wantMatched:  true,
		},
	}

	for _, tt := range tests {
		gotStatus, gotQueue, gotProgress, gotMatched := deriveRemoteHistoryState(tt.historyItem)
		if gotStatus != tt.wantStatus || gotQueue != tt.wantQueue || gotProgress != tt.wantProgress || gotMatched != tt.wantMatched {
			t.Fatalf("%s: got (%d, %q, %d, %t), want (%d, %q, %d, %t)",
				tt.name,
				gotStatus, gotQueue, gotProgress, gotMatched,
				tt.wantStatus, tt.wantQueue, tt.wantProgress, tt.wantMatched,
			)
		}
	}
}

func TestRecoveredHistoryFailReasonUsesGenericFailureForStatus30(t *testing.T) {
	t.Helper()

	got := recoveredHistoryFailReason(map[string]any{
		"status":       "30",
		"queue_status": "Finish",
	}, "")
	if got != "generation failed: final generation failed" {
		t.Fatalf("unexpected fail reason: %q", got)
	}
}

func TestHistoryIndicatesFailureUsesFailFieldsAndStatus30(t *testing.T) {
	t.Helper()

	if !historyIndicatesFailure(map[string]any{
		"status":   "30",
		"fail_msg": "OutputImageRisk",
	}) {
		t.Fatalf("expected failure to be detected")
	}
}

func TestRemoteHistoryProgressSupportsPercentStringAliases(t *testing.T) {
	t.Helper()

	got := remoteHistoryProgress(map[string]any{
		"queue": map[string]any{
			"Percent": "18%",
		},
	})
	if got != 18 {
		t.Fatalf("unexpected percent progress: %d", got)
	}
}

func TestUpdateRecoveredQueryResultJSONMergesHistoryMedia(t *testing.T) {
	t.Helper()

	base := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-1",
			Data:    map[string]any{"submit_id": "submit-1"},
			Recovered: map[string]any{
				"history_id": "hist-1",
				"submitted":  int64(1700000000),
			},
		},
	)

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON:  base,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "a cat"},
		},
	}

	updated := updateRecoveredQueryResultJSON(base, taskValue, "hist-1", "success", 100, 2, map[string]any{
		"queue_status": "success",
		"videos": []any{
			map[string]any{
				"url":       "https://example.com/video.mp4",
				"cover_url": "https://example.com/cover.png",
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	if root["gen_status"] != "success" {
		t.Fatalf("unexpected gen_status: %#v", root["gen_status"])
	}
	videos, ok := root["videos"].([]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("unexpected videos payload: %#v", root["videos"])
	}
	video, ok := videos[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected video item: %#v", videos[0])
	}
	if video["url"] != "https://example.com/video.mp4" {
		t.Fatalf("unexpected merged video url: %#v", video["url"])
	}
	if video["cover_url"] != "https://example.com/cover.png" {
		t.Fatalf("unexpected merged cover url: %#v", video["cover_url"])
	}
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["history_id"] != "hist-1" {
		t.Fatalf("unexpected queue history_id: %#v", queueInfo["history_id"])
	}
	if anyInt(queueInfo["query_count"]) != 2 {
		t.Fatalf("unexpected queue query_count: %#v", queueInfo["query_count"])
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response data payload: %#v", response["data"])
	}
	query, ok := data["query"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response query payload: %#v", data["query"])
	}
	if query["queue_status"] != "success" {
		t.Fatalf("unexpected response query status: %#v", query["queue_status"])
	}
	if _, exists := query["query_count"]; exists {
		t.Fatalf("did not expect duplicated response query_count: %#v", query["query_count"])
	}
	if !taskResultHasMedia(updated) {
		t.Fatalf("expected merged result to contain media")
	}
}

func TestUpdateRecoveredQueryResultJSONKeepsGenStatusQueryingWhileQueueRuns(t *testing.T) {
	t.Helper()

	base := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-1",
			Data:    map[string]any{"submit_id": "submit-1"},
			Recovered: map[string]any{
				"history_id": "hist-1",
				"submitted":  int64(1700000000),
			},
		},
	)

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON:  base,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "a cat"},
		},
	}

	updated := updateRecoveredQueryResultJSON(base, taskValue, "hist-1", "running", 55, 1, nil, nil)
	root := parseRecoveredResultRoot(updated)
	if root["gen_status"] != "querying" {
		t.Fatalf("unexpected gen_status while running: %#v", root["gen_status"])
	}
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["queue_status"] != "running" {
		t.Fatalf("unexpected queue status: %#v", queueInfo["queue_status"])
	}
}

func TestUpdateRecoveredQueryResultJSONVideoSuccessDropsPreviewImagesAndKeepsMetadata(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-video-1",
		GenTaskType: "image2video",
		GenStatus:   "querying",
		ResultJSON: `{
			"gen_task_type":"image2video",
			"gen_status":"querying",
			"images":[],
			"videos":[{"fps":24,"width":1470,"height":630,"format":"mp4","duration":5.042}],
			"queue_info":{"queue_status":"submitted"}
		}`,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "让画面慢慢动起来"},
		},
	}

	updated := updateRecoveredQueryResultJSON(taskValue.ResultJSON, taskValue, "hist-video-1", "success", 100, 3, map[string]any{
		"queue_status": "success",
		"images": []any{
			map[string]any{
				"image_url": "https://example.com/preview.png",
				"width":     1120,
				"height":    630,
			},
		},
		"videos": []any{
			map[string]any{
				"video_url": "https://example.com/result.mp4",
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	if rawImages, exists := root["images"]; exists {
		images, ok := rawImages.([]map[string]any)
		if !ok || len(images) != 0 {
			t.Fatalf("expected preview images to be stripped, got %#v", root["images"])
		}
	}
	videoItems, ok := root["videos"].([]any)
	if !ok || len(videoItems) != 1 {
		t.Fatalf("unexpected merged videos: %#v", root["videos"])
	}
	video, ok := videoItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected merged video item: %#v", videoItems[0])
	}
	if video["video_url"] != "https://example.com/result.mp4" {
		t.Fatalf("unexpected merged video url: %#v", video["video_url"])
	}
	if anyInt(video["fps"]) != 24 || anyInt(video["width"]) != 1470 || anyInt(video["height"]) != 630 {
		t.Fatalf("expected local video metadata to be preserved, got %#v", video)
	}
	if anyString(video["format"]) != "mp4" {
		t.Fatalf("unexpected merged format: %#v", video["format"])
	}
	if got := anyFloat64(video["duration"]); got != 5.042 {
		t.Fatalf("unexpected merged duration: %#v", got)
	}
}

func TestUpdateRecoveredQueryResultJSONPrefersOriginVideoURL(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-video-2",
		GenTaskType: "frames2video",
		GenStatus:   "querying",
		ResultJSON:  `{"images":[],"videos":[{"fps":24,"width":1470,"height":630,"format":"mp4","duration":5.042}],"queue_info":{"queue_status":"submitted"}}`,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "让首尾画面自然过渡"},
		},
	}

	updated := updateRecoveredQueryResultJSON(taskValue.ResultJSON, taskValue, "hist-video-2", "success", 100, 4, map[string]any{
		"queue_status": "success",
		"images": []any{
			map[string]any{
				"image_url": "https://example.com/preview.png",
				"origin":    "map[duration:0 format:mp4 fps:24 height:630 video_url:https://example.com/final.mp4 width:1470]",
			},
		},
		"videos": []any{
			map[string]any{
				"video_url": "https://example.com/preview-low.mp4",
				"width":     840,
				"height":    360,
				"format":    "webp",
				"duration":  6,
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	videoItems, ok := root["videos"].([]any)
	if !ok || len(videoItems) != 1 {
		t.Fatalf("unexpected merged videos: %#v", root["videos"])
	}
	video, ok := videoItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected merged video item: %#v", videoItems[0])
	}
	if video["video_url"] != "https://example.com/final.mp4" {
		t.Fatalf("expected origin video url to win, got %#v", video["video_url"])
	}
	if anyString(video["format"]) != "mp4" || anyFloat64(video["duration"]) != 5.042 {
		t.Fatalf("expected local metadata to backfill origin video, got %#v", video)
	}
}

func TestUpdateRecoveredQueryResultJSONPrefersFinalGeneratedImageOverReferenceImage(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-image-1",
		GenTaskType: "image2image",
		GenStatus:   "querying",
		ResultJSON:  `{"images":[{"width":5404,"height":3040}],"videos":[],"queue_info":{"queue_status":"submitted"}}`,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "改成夜景霓虹风格"},
		},
	}

	updated := updateRecoveredQueryResultJSON(taskValue.ResultJSON, taskValue, "hist-image-1", "success", 100, 4, map[string]any{
		"queue_status": "success",
		"images": []any{
			map[string]any{
				"image_url": "https://example.com/reference~tplv-resize:640:640.image?format=.",
				"width":     640,
				"height":    640,
				"origin":    "smart_reference",
			},
		},
		"item_list": []any{
			map[string]any{
				"image": map[string]any{
					"large_images": []any{
						map[string]any{
							"image_url": "https://example.com/final~tplv-aigc_resize:0:0.png?format=.png",
							"width":     5404,
							"height":    3040,
							"format":    "png",
						},
					},
				},
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	imageItems, ok := root["images"].([]any)
	if !ok || len(imageItems) != 1 {
		t.Fatalf("unexpected merged images: %#v", root["images"])
	}
	image, ok := imageItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected merged image item: %#v", imageItems[0])
	}
	if image["image_url"] != "https://example.com/final~tplv-aigc_resize:0:0.png?format=.png" {
		t.Fatalf("expected final generated image url to win, got %#v", image["image_url"])
	}
	if anyInt(image["width"]) != 5404 || anyInt(image["height"]) != 3040 {
		t.Fatalf("expected local output dimensions to be preserved, got %#v", image)
	}
}

func TestUpdateRecoveredQueryResultJSONPrefersTranscodedOriginVideoURL(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-video-3",
		GenTaskType: "image2video",
		GenStatus:   "querying",
		ResultJSON:  `{"images":[],"videos":[{"fps":24,"width":1470,"height":630,"format":"mp4","duration":5.042}],"queue_info":{"queue_status":"submitted"}}`,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "让画面慢慢动起来"},
		},
	}

	updated := updateRecoveredQueryResultJSON(taskValue.ResultJSON, taskValue, "hist-video-3", "success", 100, 4, map[string]any{
		"queue_status": "success",
		"videos": []any{
			map[string]any{
				"video_url": "https://example.com/preview-low.mp4?ds=3",
				"width":     840,
				"height":    360,
				"format":    "webp",
				"duration":  6,
			},
		},
		"item_list": []any{
			map[string]any{
				"video": map[string]any{
					"video_model": `{"big_thumbs":[{"duration":5.041667}],"video_duration":5.062}`,
					"transcoded_video": map[string]any{
						"origin": map[string]any{
							"video_url": "https://example.com/final.mp4?ds=12&mime_type=video_mp4&br=6697",
							"fps":       24,
							"width":     1470,
							"height":    630,
							"format":    "mp4",
							"duration":  0,
						},
					},
				},
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	videoItems, ok := root["videos"].([]any)
	if !ok || len(videoItems) != 1 {
		t.Fatalf("unexpected merged videos: %#v", root["videos"])
	}
	video, ok := videoItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected merged video item: %#v", videoItems[0])
	}
	if video["video_url"] != "https://example.com/final.mp4?ds=12&mime_type=video_mp4&br=6697" {
		t.Fatalf("expected transcoded origin video url to win, got %#v", video["video_url"])
	}
	if anyInt(video["width"]) != 1470 || anyInt(video["height"]) != 630 || anyString(video["format"]) != "mp4" {
		t.Fatalf("expected transcoded origin metadata to be preserved, got %#v", video)
	}
	if anyFloat64(video["duration"]) != 5.042 {
		t.Fatalf("expected local duration to backfill transcoded origin video, got %#v", video)
	}
}

func TestUpdateRecoveredQueryResultJSONMergesRealHistoryQueueFieldsAndImageSize(t *testing.T) {
	t.Helper()

	base := buildDreaminaResultJSON(
		"text2image",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-queue-merge-1",
			Data:    map[string]any{"submit_id": "submit-1"},
			Recovered: map[string]any{
				"history_id": "hist-1",
			},
		},
	)

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2image",
		GenStatus:   "querying",
		ResultJSON:  base,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "a cat"},
		},
	}

	updated := updateRecoveredQueryResultJSON(base, taskValue, "hist-1", "success", 100, 2, map[string]any{
		"queue_status": "Finish",
		"queue_idx":    0,
		"priority":     1,
		"queue_length": 0,
		"debug_info":   "{\"stage\":\"done\"}",
		"images": []any{
			map[string]any{
				"image_url": "https://example.com/result.png",
				"url":       "https://example.com/result.png",
				"width":     5404,
				"height":    3040,
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["queue_status"] != "Finish" || anyInt(queueInfo["queue_idx"]) != 0 || anyInt(queueInfo["queue_length"]) != 0 {
		t.Fatalf("unexpected merged queue fields: %#v", queueInfo)
	}
	if anyInt(queueInfo["priority"]) != 1 || queueInfo["debug_info"] != "{\"stage\":\"done\"}" {
		t.Fatalf("unexpected merged queue details: %#v", queueInfo)
	}
	images, ok := root["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected images payload: %#v", root["images"])
	}
	image, ok := images[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected image item: %#v", images[0])
	}
	if image["image_url"] != "https://example.com/result.png" || anyInt(image["width"]) != 5404 || anyInt(image["height"]) != 3040 {
		t.Fatalf("unexpected merged image payload: %#v", image)
	}
}

func TestUpdateRecoveredQueryResultJSONPreservesBaseImageShapeWhileRefreshingURL(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-image-1",
		GenTaskType: "image2image",
		GenStatus:   "success",
		ResultJSON: `{
			"images": [
				{"image_url":"https://example.com/local.png","url":"https://example.com/local.png","width":5404,"height":3040}
			],
			"queue_info":{"queue_status":"submitted"}
		}`,
	}

	updated := updateRecoveredQueryResultJSON(taskValue.ResultJSON, taskValue, "hist-image-1", "success", 100, 2, map[string]any{
		"queue_status": "success",
		"images": []any{
			map[string]any{
				"image_url": "https://example.com/remote-1.png",
				"url":       "https://example.com/remote-1.png",
				"width":     5404,
				"height":    300,
			},
			map[string]any{
				"image_url": "https://example.com/remote-2.png",
				"url":       "https://example.com/remote-2.png",
			},
		},
	}, nil)

	root := parseRecoveredResultRoot(updated)
	images, ok := root["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("unexpected merged images: %#v", root["images"])
	}
	image, ok := images[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected merged image item: %#v", images[0])
	}
	if image["image_url"] != "https://example.com/remote-1.png" || image["url"] != "https://example.com/remote-1.png" {
		t.Fatalf("expected remote url refresh, got %#v", image)
	}
	if anyInt(image["width"]) != 5404 || anyInt(image["height"]) != 3040 {
		t.Fatalf("expected local dimensions to be preserved, got %#v", image)
	}
}

func TestBuildDreaminaResultJSONRoundTripIsValidJSON(t *testing.T) {
	t.Helper()

	raw := buildDreaminaResultJSON("text2image", map[string]any{"prompt": "a cat"}, nil, nil, nil)
	if !json.Valid([]byte(raw)) {
		t.Fatalf("expected valid json: %q", raw)
	}
}

func TestSubmitTaskRenamesLocalSubmitIDToRemoteSubmitID(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	store, err := task.NewStore()
	if err != nil {
		t.Fatalf("task.NewStore() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(&HandlerEntry{
		GenTaskType: "text2image",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			return &PreparedSubmit{
				Commit: func(ctx context.Context) (*SubmitOutcome, error) {
					return &SubmitOutcome{
						ResultJSON: `{
							"response": {
								"data": {
									"submit_id": "remote-submit-1"
								}
							},
							"queue_info": {
								"history_id": "remote-submit-1"
							}
						}`,
					}, nil
				},
			}, nil
		},
		Query: func(ctx context.Context, localTask any) (*RemoteQueryResult, error) {
			return &RemoteQueryResult{
				Status:       1,
				ResultJSON:   localTask.(*task.AIGCTask).ResultJSON,
				CommerceInfo: nil,
			}, nil
		},
	}); err != nil {
		t.Fatalf("registry.Register() error = %v", err)
	}

	service, err := NewService(store, registry)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	rawTask, err := service.SubmitTask(context.Background(), "uid-1", "text2image", map[string]any{
		"prompt": "a cat",
	})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	got, ok := rawTask.(*task.AIGCTask)
	if !ok {
		t.Fatalf("unexpected task type: %#v", rawTask)
	}
	if got.SubmitID != "remote-submit-1" {
		t.Fatalf("unexpected returned submit_id: %#v", got.SubmitID)
	}

	listed, err := store.ListTasks(context.Background(), task.ListTaskFilter{UID: "uid-1"})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("unexpected task count: %d", len(listed))
	}
	if listed[0].SubmitID != "remote-submit-1" {
		t.Fatalf("unexpected stored submit_id: %#v", listed[0].SubmitID)
	}
}

func TestSubmitTaskRenamesLocalSubmitIDToRemoteSubmitIDOnAPIError(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	store, err := task.NewStore()
	if err != nil {
		t.Fatalf("task.NewStore() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(&HandlerEntry{
		GenTaskType: "image2video",
		PrepareSubmit: func(ctx context.Context, uid string, input any) (*PreparedSubmit, error) {
			return &PreparedSubmit{
				Commit: func(ctx context.Context) (*SubmitOutcome, error) {
					return nil, &mcpclient.APIError{
						Code:     "2061",
						Message:  "模型已不可用，请刷新界面后再试试",
						LogID:    "log-fail-1",
						SubmitID: "remote-fail-1",
					}
				},
			}, nil
		},
		Query: func(ctx context.Context, localTask any) (*RemoteQueryResult, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("registry.Register() error = %v", err)
	}

	service, err := NewService(store, registry)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	rawTask, err := service.SubmitTask(context.Background(), "uid-1", "image2video", map[string]any{
		"prompt": "camera push in",
	})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	got, ok := rawTask.(*task.AIGCTask)
	if !ok {
		t.Fatalf("unexpected task type: %#v", rawTask)
	}
	if got.SubmitID != "remote-fail-1" {
		t.Fatalf("unexpected returned submit_id: %#v", got.SubmitID)
	}
	if got.GenStatus != "failed" {
		t.Fatalf("unexpected returned gen_status: %#v", got.GenStatus)
	}

	listed, err := store.ListTasks(context.Background(), task.ListTaskFilter{UID: "uid-1"})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("unexpected task count: %d", len(listed))
	}
	if listed[0].SubmitID != "remote-fail-1" {
		t.Fatalf("unexpected stored submit_id: %#v", listed[0].SubmitID)
	}
}

func TestQueryResultReturnsNotFoundWithoutLocalTask(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	store, err := task.NewStore()
	if err != nil {
		t.Fatalf("task.NewStore() error = %v", err)
	}

	registry := NewRegistry()
	called := false
	if err := registry.Register(&HandlerEntry{
		GenTaskType: "text2image",
		Query: func(ctx context.Context, localTask any) (*RemoteQueryResult, error) {
			called = true
			return &RemoteQueryResult{Status: 2}, nil
		},
	}); err != nil {
		t.Fatalf("registry.Register() error = %v", err)
	}

	service, err := NewService(store, registry)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.QueryResult(context.Background(), "remote-only-1")
	if err == nil {
		t.Fatalf("expected not found error")
	}
	if got := err.Error(); got != `task "remote-only-1" not found` {
		t.Fatalf("unexpected error: %q", got)
	}
	if called {
		t.Fatalf("unexpected remote query handler call without local task")
	}
}

func TestQueryResultFallsBackToGenericQueryHandlerForUnknownLocalTaskType(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	store, err := task.NewStore()
	if err != nil {
		t.Fatalf("task.NewStore() error = %v", err)
	}

	now := time.Now().Unix()
	if err := store.CreateTask(context.Background(), &task.AIGCTask{
		SubmitID:    "legacy-unknown-1",
		UID:         "uid-1",
		GenTaskType: "legacy_unknown",
		GenStatus:   "querying",
		ResultJSON:  buildSkeletonResultJSON("", nil),
		CreateTime:  now,
		UpdateTime:  now,
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(&HandlerEntry{
		GenTaskType: "text2image",
		Query: func(ctx context.Context, localTask any) (*RemoteQueryResult, error) {
			taskValue, ok := localTask.(*task.AIGCTask)
			if !ok {
				t.Fatalf("unexpected local task type: %T", localTask)
			}
			if taskValue.SubmitID != "legacy-unknown-1" {
				t.Fatalf("unexpected submit_id passed to query handler: %#v", taskValue.SubmitID)
			}
			return &RemoteQueryResult{
				Status: 2,
				ResultJSON: `{
					"videos":[{"video_url":"https://example.com/legacy-unknown-1.mp4"}],
					"queue_info":{"queue_status":"Finish"}
				}`,
			}, nil
		},
	}); err != nil {
		t.Fatalf("registry.Register() error = %v", err)
	}

	service, err := NewService(store, registry)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	value, err := service.QueryResult(context.Background(), "legacy-unknown-1")
	if err != nil {
		t.Fatalf("QueryResult() error = %v", err)
	}
	got, ok := value.(*task.AIGCTask)
	if !ok {
		t.Fatalf("unexpected query result type: %T", value)
	}
	if got.GenStatus != "success" {
		t.Fatalf("unexpected returned gen_status: %#v", got.GenStatus)
	}
	root := parseRecoveredResultRoot(got.ResultJSON)
	videos, ok := root["videos"].([]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("unexpected remote videos payload: %#v", root["videos"])
	}
}

func TestQueryHistoryIDFromTaskSupportsUpperCamelAliases(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID: "submit-fallback-1",
		ResultJSON: `{
			"response": {
				"recovered": {
					"HistoryID": "hist-upper-query-1"
				},
				"data": {
					"SubmitID": "submit-upper-query-1"
				}
			},
			"queue_info": {
				"HistoryID": "hist-queue-1"
			}
		}`,
	}

	if got := queryHistoryIDFromTask(taskValue); got != "hist-upper-query-1" {
		t.Fatalf("unexpected history id: %q", got)
	}
}

func TestPickRecoveredHistoryItemMatchesHistoryIDFromViewFields(t *testing.T) {
	t.Helper()

	resp := &mcpclient.GetHistoryByIdsResponse{
		Items: map[string]*mcpclient.HistoryItem{
			"submit-key-1": {
				SubmitID:    "submit-key-1",
				HistoryID:   "hist-match-1",
				QueueStatus: "success",
				Raw: map[string]any{
					"submit_id":    "submit-key-1",
					"history_id":   "hist-match-1",
					"queue_status": "success",
				},
			},
		},
	}

	got := pickRecoveredHistoryItem(resp, "hist-match-1")
	if got == nil {
		t.Fatalf("expected matched history item")
	}
	if got["history_id"] != "hist-match-1" {
		t.Fatalf("unexpected history item: %#v", got)
	}
}

func TestPickRecoveredHistoryItemKeepsQueueProgressFromNormalizedView(t *testing.T) {
	t.Helper()

	resp := &mcpclient.GetHistoryByIdsResponse{
		Items: map[string]*mcpclient.HistoryItem{
			"submit-progress-1": {
				SubmitID:      "submit-progress-1",
				HistoryID:     "hist-progress-1",
				QueueStatus:   "processing",
				QueueProgress: 72,
				Queue: &mcpclient.QueueInfo{
					QueueStatus: "processing",
					Progress:    72,
				},
				Raw: map[string]any{
					"submit_id":  "submit-progress-1",
					"history_id": "hist-progress-1",
				},
			},
		},
	}

	got := pickRecoveredHistoryItem(resp, "hist-progress-1")
	if got == nil {
		t.Fatalf("expected matched history item")
	}
	queue, ok := got["queue"].(map[string]any)
	if !ok {
		t.Fatalf("expected queue view, got %#v", got["queue"])
	}
	if anyInt(queue["progress"]) != 72 {
		t.Fatalf("unexpected queue progress: %#v", queue["progress"])
	}
}

func TestPickRecoveredHistoryItemDoesNotReturnUnrelatedItem(t *testing.T) {
	t.Helper()

	resp := &mcpclient.GetHistoryByIdsResponse{
		Items: map[string]*mcpclient.HistoryItem{
			"submit-other-1": {
				SubmitID:    "submit-other-1",
				HistoryID:   "hist-other-1",
				QueueStatus: "success",
				Raw: map[string]any{
					"submit_id":    "submit-other-1",
					"history_id":   "hist-other-1",
					"queue_status": "success",
				},
			},
		},
	}

	if got := pickRecoveredHistoryItem(resp, "hist-target-1"); got != nil {
		t.Fatalf("expected unrelated history item to be ignored, got %#v", got)
	}
}

func TestDeriveRecoveredQueryStateKeepsMinimalSubmittedFallbackWithoutHistoryEvidence(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		CreateTime:  1,
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, nil)
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "submitted" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 8 {
		t.Fatalf("unexpected fallback progress: %d", progress)
	}
}

func TestDeriveRecoveredQueryStateKeepsExistingQueueStateWhenHistoryQuerySucceededButEmpty(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON: `{
			"queue_info": {
				"queue_status": "running",
				"progress": 33
			}
		}`,
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, map[string]any{
		"code":    "0",
		"message": "ok",
		"log_id":  "history-ok-1",
	})
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "running" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 33 {
		t.Fatalf("unexpected progress: %d", progress)
	}
}

func TestDeriveRecoveredQueryStateDoesNotKeepRunningWithoutProgressWhenHistoryQuerySucceededButEmpty(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON: `{
			"queue_info": {
				"queue_status": "running",
				"progress": 0
			}
		}`,
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, map[string]any{
		"code":    "0",
		"message": "ok",
		"log_id":  "history-ok-1",
	})
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "submitted" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 0 {
		t.Fatalf("unexpected progress: %d", progress)
	}
}

func TestDeriveRecoveredQueryStateDoesNotPromoteLocalMediaWhenHistoryQuerySucceededButEmpty(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "image_upscale",
		GenStatus:   "success",
		ResultJSON: `{
			"images": [
				{"url":"https://example.com/result.png","image_url":"https://example.com/result.png"}
			]
		}`,
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, map[string]any{
		"code":    "0",
		"message": "ok",
		"log_id":  "history-ok-1",
	})
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "submitted" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 0 {
		t.Fatalf("unexpected progress: %d", progress)
	}
}

func TestHistoryQueryMetadataKeepsSuccessSentinelWithoutNoise(t *testing.T) {
	t.Helper()

	got := historyQueryMetadata(&mcpclient.GetHistoryByIdsResponse{
		Code:    "0",
		Message: "ok",
	})
	if len(got) != 1 || got["code"] != "0" {
		t.Fatalf("unexpected history query metadata: %#v", got)
	}
}

func TestDeriveRecoveredQueryStateKeepsMinimalSubmittedFallbackWhenHistoryQueryFailed(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, map[string]any{
		"code":    "transport_error",
		"message": "dial tcp 127.0.0.1:1: connect: connection refused",
		"log_id":  "history-transport-1",
	})
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "submitted" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 8 {
		t.Fatalf("unexpected fallback progress: %d", progress)
	}
}

func TestDeriveRecoveredQueryStateDoesNotRegressExistingProgressWhenHistoryQueryFailed(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON: `{
			"queue_info": {
				"queue_status": "running",
				"progress": 72
			}
		}`,
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, map[string]any{
		"code":    "transport_error",
		"message": "dial tcp 127.0.0.1:1: connect: connection refused",
		"log_id":  "history-transport-1",
	})
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "running" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 72 {
		t.Fatalf("unexpected progress regression: %d", progress)
	}
}

func TestDeriveRecoveredQueryStateDoesNotKeepRunningWithoutProgressWhenHistoryQueryFailed(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON: `{
			"queue_info": {
				"queue_status": "running",
				"progress": 0
			}
		}`,
	}

	status, queueStatus, progress := deriveRecoveredQueryState(taskValue, nil, map[string]any{
		"code":    "transport_error",
		"message": "dial tcp 127.0.0.1:1: connect: connection refused",
		"log_id":  "history-transport-1",
	})
	if status != 1 {
		t.Fatalf("unexpected status: %d", status)
	}
	if queueStatus != "submitted" {
		t.Fatalf("unexpected queue status: %q", queueStatus)
	}
	if progress != 8 {
		t.Fatalf("unexpected fallback progress: %d", progress)
	}
}

func TestUpdateRecoveredQueryResultJSONCarriesHistoryQueryDiagnostics(t *testing.T) {
	t.Helper()

	base := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-1",
			Data:    map[string]any{"submit_id": "submit-1"},
			Recovered: map[string]any{
				"history_id": "hist-1",
				"submitted":  int64(1700000000),
			},
		},
	)

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON:  base,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "a cat"},
		},
	}

	updated := updateRecoveredQueryResultJSON(base, taskValue, "hist-1", "running", 33, 1, nil, map[string]any{
		"code":    "transport_error",
		"message": "dial tcp 127.0.0.1:1: connect: connection refused",
		"log_id":  "history-transport-1",
	})

	root := parseRecoveredResultRoot(updated)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	historyQuery, ok := queueInfo["history_query"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue history_query: %#v", queueInfo["history_query"])
	}
	if historyQuery["code"] != "transport_error" || historyQuery["log_id"] != "history-transport-1" {
		t.Fatalf("unexpected history query metadata: %#v", historyQuery)
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response data payload: %#v", response["data"])
	}
	if _, exists := data["history_query"]; exists {
		t.Fatalf("did not expect duplicated response data history_query: %#v", data["history_query"])
	}
	query, ok := data["query"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected query payload: %#v", data["query"])
	}
	if _, ok := query["history_query"].(map[string]any); !ok {
		t.Fatalf("unexpected response query history_query: %#v", query["history_query"])
	}
	if _, exists := query["query_count"]; exists {
		t.Fatalf("did not expect duplicated response query_count in diagnostics query: %#v", query["query_count"])
	}
	if _, ok := queueInfo["last_queried_at"]; !ok {
		t.Fatalf("expected queue last_queried_at when history diagnostics persist: %#v", queueInfo)
	}
	if _, ok := query["last_queried_at"]; !ok {
		t.Fatalf("expected response query last_queried_at when history diagnostics persist: %#v", query)
	}
}

func TestUpdateRecoveredQueryResultJSONDoesNotWriteEmptyHistoryID(t *testing.T) {
	t.Helper()

	base := `{
		"gen_task_type":"text2video",
		"gen_status":"querying",
		"queue_info":{"queue_status":"submitted","history_id":"hist-keep-1"},
		"response":{"data":{"submit_id":"submit-1"}}
	}`

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON:  base,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "a cat"},
		},
	}

	updated := updateRecoveredQueryResultJSON(base, taskValue, "", "submitted", 8, 1, nil, nil)

	root := parseRecoveredResultRoot(updated)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if queueInfo["history_id"] != "hist-keep-1" {
		t.Fatalf("unexpected queue history_id overwrite: %#v", queueInfo["history_id"])
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response data payload: %#v", response["data"])
	}
	if _, exists := data["history_id"]; exists {
		t.Fatalf("did not expect empty response data history_id: %#v", data["history_id"])
	}
}

func TestUpdateRecoveredQueryResultJSONDoesNotPersistPureHistorySuccessMarker(t *testing.T) {
	t.Helper()

	base := buildDreaminaResultJSON(
		"text2video",
		map[string]any{"prompt": "a cat"},
		map[string]any{"prompt": "a cat"},
		nil,
		&mcpclient.BaseResponse{
			Code:    "0",
			Message: "ok",
			LogID:   "log-1",
			Data:    map[string]any{"submit_id": "submit-1"},
			Recovered: map[string]any{
				"history_id": "hist-1",
			},
		},
	)

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-1",
		GenTaskType: "text2video",
		GenStatus:   "querying",
		ResultJSON:  base,
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "a cat"},
		},
	}

	updated := updateRecoveredQueryResultJSON(base, taskValue, "hist-1", "submitted", 0, 1, nil, map[string]any{
		"code": "0",
	})

	root := parseRecoveredResultRoot(updated)
	queueInfo, ok := root["queue_info"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected queue_info: %#v", root["queue_info"])
	}
	if _, exists := queueInfo["history_query"]; exists {
		t.Fatalf("did not expect pure success marker in queue_info: %#v", queueInfo["history_query"])
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", root["response"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response data payload: %#v", response["data"])
	}
	if _, exists := data["history_query"]; exists {
		t.Fatalf("did not expect pure success marker in response data: %#v", data["history_query"])
	}
	query, ok := data["query"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected query payload: %#v", data["query"])
	}
	if _, exists := query["history_query"]; exists {
		t.Fatalf("did not expect pure success marker in response query: %#v", query["history_query"])
	}
	if _, exists := query["query_count"]; exists {
		t.Fatalf("did not expect duplicated response query_count for pure success marker: %#v", query["query_count"])
	}
	if _, exists := queueInfo["last_queried_at"]; exists {
		t.Fatalf("did not expect queue last_queried_at for pure success marker: %#v", queueInfo["last_queried_at"])
	}
	if _, exists := query["last_queried_at"]; exists {
		t.Fatalf("did not expect response query last_queried_at for pure success marker: %#v", query["last_queried_at"])
	}
}

func TestResolveRef2VideoTransitionsDefaultsDurationPerSegment(t *testing.T) {
	t.Helper()

	prompts, durations, err := resolveRef2VideoTransitions(
		[]string{"a.png", "b.png"},
		"角色缓慢转身",
		"",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("resolveRef2VideoTransitions failed: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != "角色缓慢转身" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}
	if len(durations) != 1 || durations[0] != 3 {
		t.Fatalf("unexpected durations: %#v", durations)
	}
}
