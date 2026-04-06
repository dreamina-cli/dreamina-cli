package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewStoreRecreatesSQLiteTaskDBWhenTaskDBIsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatalf("seed sqlite-like file: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	wantPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if store.path != wantPath {
		t.Fatalf("store.path = %q, want %q", store.path, wantPath)
	}
	list, err := store.ListTasks(context.Background(), ListTaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("unexpected recreated sqlite contents: %#v", list)
	}
}

func TestNewStoreMigratesLegacyRecoveredJSONStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatalf("seed sqlite-like file: %v", err)
	}
	legacyPath := filepath.Join(home, ".dreamina_cli", "tasks.recovered.json")
	legacyBody := []byte(`[{"submit_id":"legacy-json-1","uid":"u1"}]`)
	if err := os.WriteFile(legacyPath, legacyBody, 0o600); err != nil {
		t.Fatalf("seed legacy recovered json: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	wantPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if store.path != wantPath {
		t.Fatalf("store.path = %q, want %q", store.path, wantPath)
	}
	got, err := store.GetTask(context.Background(), "legacy-json-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.UID != "u1" {
		t.Fatalf("unexpected migrated task: %#v", got)
	}
}

func TestNewStoreMigratesLegacyJSONTaskDBToNeutralJSONStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	legacyJSONBody := []byte(`[{"submit_id":"legacy-db-json-1","uid":"u-db-json"}]`)
	if err := os.WriteFile(dbPath, legacyJSONBody, 0o600); err != nil {
		t.Fatalf("seed legacy json task db: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	wantPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if store.path != wantPath {
		t.Fatalf("store.path = %q, want %q", store.path, wantPath)
	}
	got, err := store.GetTask(context.Background(), "legacy-db-json-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.UID != "u-db-json" {
		t.Fatalf("unexpected migrated task: %#v", got)
	}
}

func TestNewStoreUsesExistingJSONStoreWhenTaskDBIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	jsonPath := filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	jsonBody := []byte(`[{"submit_id":"json-existing-1","uid":"u-json"}]`)
	if err := os.WriteFile(jsonPath, jsonBody, 0o600); err != nil {
		t.Fatalf("seed json store: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	dbPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if store.path != dbPath {
		t.Fatalf("store.path = %q, want %q", store.path, dbPath)
	}
	got, err := store.GetTask(context.Background(), "json-existing-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.UID != "u-json" {
		t.Fatalf("unexpected migrated task: %#v", got)
	}
}

func TestNewStoreMigratesLegacyRecoveredJSONWhenTaskDBIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacyPath := filepath.Join(home, ".dreamina_cli", "tasks.recovered.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	legacyBody := []byte(`[{"submit_id":"legacy-missing-db-1","uid":"u-legacy"}]`)
	if err := os.WriteFile(legacyPath, legacyBody, 0o600); err != nil {
		t.Fatalf("seed legacy recovered json: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	wantPath := filepath.Join(home, ".dreamina_cli", "tasks.db")
	if store.path != wantPath {
		t.Fatalf("store.path = %q, want %q", store.path, wantPath)
	}
	got, err := store.GetTask(context.Background(), "legacy-missing-db-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.UID != "u-legacy" {
		t.Fatalf("unexpected migrated task: %#v", got)
	}
}

func TestStoreCreateAndGetTaskRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	input := &AIGCTask{
		SubmitID:    "submit-1",
		UID:         "uid-1",
		GenTaskType: "text2video",
		Request: &TaskRequestPayload{
			Values: map[string]any{
				"prompt": "a cat",
				"seed":   float64(7),
			},
		},
		GenStatus:  "submitted",
		ResultJSON: `{"queue_status":"submitted"}`,
		CreateTime: 1700000001,
		UpdateTime: 1700000002,
		LogID:      "log-1",
		CommerceInfo: map[string]any{
			"is_free": true,
		},
	}
	if err := store.CreateTask(context.Background(), input); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "submit-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if got.SubmitID != input.SubmitID || got.UID != input.UID || got.GenTaskType != input.GenTaskType {
		t.Fatalf("unexpected task identity: %#v", got)
	}
	if got.Request == nil || got.Request.Values["prompt"] != "a cat" {
		t.Fatalf("unexpected request payload: %#v", got.Request)
	}
	if got.GenStatus != "submitted" || got.LogID != "log-1" {
		t.Fatalf("unexpected task status fields: %#v", got)
	}
	commerce, ok := got.CommerceInfo.(map[string]any)
	if !ok || commerce["is_free"] != true {
		t.Fatalf("unexpected commerce payload: %#v", got.CommerceInfo)
	}
}

func TestAIGCTaskListPromptOnlyUsesLegacyRequestBody(t *testing.T) {
	legacy := &taskRecord{
		SubmitID:    "legacy-1",
		UID:         "uid-1",
		GenTaskType: "image2image",
		Request:     `{"method":"POST","url":"/dreamina/cli/v1/image_generate","body":"{\"prompt\":\"改成水彩风格\"}"}`,
		GenStatus:   "querying",
	}
	legacyTask, err := legacy.toAIGCTask()
	if err != nil {
		t.Fatalf("legacy toAIGCTask() error = %v", err)
	}
	if got := legacyTask.ListPrompt(); got != "改成水彩风格" {
		t.Fatalf("unexpected legacy list prompt: %#v", got)
	}

	current := &taskRecord{
		SubmitID:    "current-1",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request:     `{"values":{"prompt":"一只猫"}}`,
		GenStatus:   "success",
	}
	currentTask, err := current.toAIGCTask()
	if err != nil {
		t.Fatalf("current toAIGCTask() error = %v", err)
	}
	if got := currentTask.ListPrompt(); got != nil {
		t.Fatalf("did not expect list prompt for values request: %#v", got)
	}
}

func TestStoreRenameTaskSubmitID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	input := &AIGCTask{
		SubmitID:    "local-submit-1",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request: &TaskRequestPayload{
			Values: map[string]any{
				"prompt": "a cat",
			},
		},
		GenStatus:  "querying",
		ResultJSON: `{"gen_status":"querying"}`,
		CreateTime: 1700000001,
		UpdateTime: 1700000002,
	}
	if err := store.CreateTask(context.Background(), input); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := store.RenameTaskSubmitID(context.Background(), "local-submit-1", "remote-submit-1"); err != nil {
		t.Fatalf("RenameTaskSubmitID() error = %v", err)
	}

	if _, err := store.GetTask(context.Background(), "local-submit-1"); err == nil {
		t.Fatalf("expected old submit_id lookup to fail")
	}

	got, err := store.GetTask(context.Background(), "remote-submit-1")
	if err != nil {
		t.Fatalf("GetTask(new submit_id) error = %v", err)
	}
	if got.SubmitID != "remote-submit-1" {
		t.Fatalf("unexpected submit_id after rename: %#v", got.SubmitID)
	}
	if got.Request == nil || got.Request.Values["prompt"] != "a cat" {
		t.Fatalf("unexpected request payload after rename: %#v", got.Request)
	}
}

func TestStoreUpdateTaskAndListTasks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	mustCreate := func(task *AIGCTask) {
		t.Helper()
		if err := store.CreateTask(context.Background(), task); err != nil {
			t.Fatalf("CreateTask(%q) error = %v", task.SubmitID, err)
		}
	}

	mustCreate(&AIGCTask{
		SubmitID:    "submit-1",
		UID:         "uid-1",
		GenTaskType: "text2video",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "first"}},
		GenStatus:   "submitted",
		CreateTime:  10,
		UpdateTime:  10,
	})
	mustCreate(&AIGCTask{
		SubmitID:    "submit-2",
		UID:         "uid-1",
		GenTaskType: "text2img",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "second"}},
		GenStatus:   "submitted",
		CreateTime:  20,
		UpdateTime:  20,
	})
	mustCreate(&AIGCTask{
		SubmitID:    "submit-3",
		UID:         "uid-2",
		GenTaskType: "text2img",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "third"}},
		GenStatus:   "failed",
		CreateTime:  30,
		UpdateTime:  30,
	})

	failReason := "remote failed"
	resultJSON := `{"queue_status":"success"}`
	logID := "log-2"
	if err := store.UpdateTask(context.Background(), UpdateTaskInput{
		SubmitID:   "submit-2",
		GenStatus:  "success",
		FailReason: &failReason,
		ResultJSON: &resultJSON,
		UpdateTime: 99,
		LogID:      &logID,
		CommerceInfo: map[string]any{
			"charge_type": "vip",
		},
	}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "submit-2")
	if err != nil {
		t.Fatalf("GetTask(updated) error = %v", err)
	}
	if got.GenStatus != "success" || got.FailReason != failReason || got.ResultJSON != resultJSON || got.UpdateTime != 99 || got.LogID != logID {
		t.Fatalf("unexpected updated task: %#v", got)
	}

	list, err := store.ListTasks(context.Background(), ListTaskFilter{
		UID:    "uid-1",
		Offset: -5,
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("ListTasks(uid-1) error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(ListTasks(uid-1)) = %d, want 2", len(list))
	}
	if list[0].SubmitID != "submit-2" || list[1].SubmitID != "submit-1" {
		t.Fatalf("unexpected list order: %#v", []string{list[0].SubmitID, list[1].SubmitID})
	}

	filtered, err := store.ListTasks(context.Background(), ListTaskFilter{
		UID:       "uid-1",
		GenStatus: "success",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListTasks(filtered) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].SubmitID != "submit-2" {
		t.Fatalf("unexpected filtered tasks: %#v", filtered)
	}
}

func TestStoreListTasksUsesUpdateTimeThenCreateTimeAndInsertionOrderTiebreaker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	mustCreate := func(task *AIGCTask) {
		t.Helper()
		if err := store.CreateTask(context.Background(), task); err != nil {
			t.Fatalf("CreateTask(%q) error = %v", task.SubmitID, err)
		}
	}

	mustCreate(&AIGCTask{
		SubmitID:    "submit-b",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "b"}},
		GenStatus:   "submitted",
		CreateTime:  30,
		UpdateTime:  0,
	})
	mustCreate(&AIGCTask{
		SubmitID:    "submit-a",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "a"}},
		GenStatus:   "submitted",
		CreateTime:  30,
		UpdateTime:  0,
	})
	mustCreate(&AIGCTask{
		SubmitID:    "submit-newest",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "newest"}},
		GenStatus:   "submitted",
		CreateTime:  40,
		UpdateTime:  0,
	})

	list, err := store.ListTasks(context.Background(), ListTaskFilter{
		UID:   "uid-1",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(ListTasks()) = %d, want 3", len(list))
	}
	if got := []string{list[0].SubmitID, list[1].SubmitID, list[2].SubmitID}; fmt.Sprint(got) != "[submit-newest submit-b submit-a]" {
		t.Fatalf("unexpected fallback ordering: %#v", got)
	}
}

func TestStoreListTasksIgnoresCreateTimeWhenUpdateTimeMatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	mustCreate := func(task *AIGCTask) {
		t.Helper()
		if err := store.CreateTask(context.Background(), task); err != nil {
			t.Fatalf("CreateTask(%q) error = %v", task.SubmitID, err)
		}
	}

	mustCreate(&AIGCTask{
		SubmitID:    "submit-older-create",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "older"}},
		GenStatus:   "submitted",
		CreateTime:  10,
		UpdateTime:  20,
	})
	mustCreate(&AIGCTask{
		SubmitID:    "submit-newer-create",
		UID:         "uid-1",
		GenTaskType: "text2image",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "newer"}},
		GenStatus:   "submitted",
		CreateTime:  11,
		UpdateTime:  20,
	})

	list, err := store.ListTasks(context.Background(), ListTaskFilter{
		UID:   "uid-1",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(ListTasks()) = %d, want 2", len(list))
	}
	if got := []string{list[0].SubmitID, list[1].SubmitID}; fmt.Sprint(got) != "[submit-older-create submit-newer-create]" {
		t.Fatalf("unexpected update-time ordering: %#v", got)
	}
}

func TestStoreRejectsDuplicateCreateAndMissingTask(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	task := &AIGCTask{
		SubmitID:    "submit-1",
		UID:         "uid-1",
		GenTaskType: "text2video",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "first"}},
		CreateTime:  1,
		UpdateTime:  1,
	}
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := store.CreateTask(context.Background(), task); err == nil {
		t.Fatalf("expected duplicate create to fail")
	}
	if err := store.UpdateTask(context.Background(), UpdateTaskInput{SubmitID: "missing", GenStatus: "success"}); err == nil {
		t.Fatalf("expected missing update to fail")
	}
	if _, err := store.GetTask(context.Background(), "missing"); err == nil {
		t.Fatalf("expected missing get to fail")
	}
}

func TestStoreReadsLegacyWrappedRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	jsonPath := filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(`{
		"tasks": [
			{
				"submitId": "legacy-1",
				"uid": "uid-legacy",
				"genTaskType": "text2image",
				"request": {"values":{"prompt":"legacy prompt"}},
				"genStatus": "success",
				"resultJson": {"queue_status":"success"},
				"log_id": "legacy-log",
				"commerceInfo": {"charge_type":"vip"},
				"createTime": 11,
				"updateTime": 12
			}
		]
	}`), 0o600); err != nil {
		t.Fatalf("seed legacy store: %v", err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "legacy-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.GenTaskType != "text2image" || got.LogID != "legacy-log" {
		t.Fatalf("unexpected legacy task identity: %#v", got)
	}
	if got.Request == nil || got.Request.Values["prompt"] != "legacy prompt" {
		t.Fatalf("unexpected legacy request payload: %#v", got.Request)
	}
	if got.ResultJSON != `{"queue_status":"success"}` {
		t.Fatalf("unexpected legacy result json: %q", got.ResultJSON)
	}
	commerce, ok := got.CommerceInfo.(map[string]any)
	if !ok || commerce["charge_type"] != "vip" {
		t.Fatalf("unexpected legacy commerce payload: %#v", got.CommerceInfo)
	}
}

func TestStoreReadsLegacyArrayRecordsWithUserIDAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	jsonPath := filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(`[
		{
			"submit_id": "legacy-user-id-1",
			"user_id": "uid-from-user-id",
			"gen_task_type": "text2image",
			"request": {"values":{"prompt":"legacy user id prompt"}},
			"gen_status": "success",
			"result_json": {"queue_status":"success"},
			"update_time": 41
		}
	]`), 0o600); err != nil {
		t.Fatalf("seed legacy alias store: %v", err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "legacy-user-id-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.UID != "uid-from-user-id" {
		t.Fatalf("unexpected uid alias recovery: %#v", got)
	}

	list, err := store.ListTasks(context.Background(), ListTaskFilter{
		UID:   "uid-from-user-id",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 1 || list[0].SubmitID != "legacy-user-id-1" {
		t.Fatalf("unexpected uid alias filtered list: %#v", list)
	}
}

func TestStoreReadsLegacyKeyedObjectRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	jsonPath := filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(`{
		"legacy-key-1": {
			"uid": "uid-keyed",
			"genTaskType": "text2video",
			"request": {"values":{"prompt":"keyed prompt"}},
			"genStatus": "success",
			"resultJson": {"queue_status":"success"},
			"logId": "keyed-log",
			"commerceInfo": {"charge_type":"pro"},
			"createTime": 21,
			"updateTime": 22
		}
	}`), 0o600); err != nil {
		t.Fatalf("seed legacy keyed store: %v", err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "legacy-key-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.SubmitID != "legacy-key-1" || got.LogID != "keyed-log" || got.GenTaskType != "text2video" {
		t.Fatalf("unexpected keyed task identity: %#v", got)
	}
	if got.Request == nil || got.Request.Values["prompt"] != "keyed prompt" {
		t.Fatalf("unexpected keyed request payload: %#v", got.Request)
	}
	commerce, ok := got.CommerceInfo.(map[string]any)
	if !ok || commerce["charge_type"] != "pro" {
		t.Fatalf("unexpected keyed commerce payload: %#v", got.CommerceInfo)
	}
}

func TestStoreReadsLegacyNestedKeyedObjectRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	jsonPath := filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(`{
		"tasks": {
			"data": {
				"legacy-nested-1": {
					"uid": "uid-nested",
					"genTaskType": "text2image",
					"Request": "{\"values\":{\"prompt\":\"nested prompt\"}}",
					"genStatus": "success",
					"resultJson": "{\"queue_status\":\"success\"}",
					"LogID": "nested-log",
					"CommerceInfo": "{\"charge_type\":\"plus\"}",
					"createTime": 31,
					"updateTime": 32
				}
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("seed nested keyed store: %v", err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "legacy-nested-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.SubmitID != "legacy-nested-1" || got.LogID != "nested-log" || got.GenTaskType != "text2image" {
		t.Fatalf("unexpected nested keyed task identity: %#v", got)
	}
	if got.Request == nil || got.Request.Values["prompt"] != "nested prompt" {
		t.Fatalf("unexpected nested keyed request payload: %#v", got.Request)
	}
	commerce, ok := got.CommerceInfo.(map[string]any)
	if !ok || commerce["charge_type"] != "plus" {
		t.Fatalf("unexpected nested keyed commerce payload: %#v", got.CommerceInfo)
	}
}

func TestStoreSkipsNonJSONSeedButRejectsBrokenJSONStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	jsonPath := filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatalf("seed non-json store: %v", err)
	}
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	list, err := store.ListTasks(context.Background(), ListTaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks(non-json) error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected non-json seed to be ignored, got %#v", list)
	}

	home = t.TempDir()
	t.Setenv("HOME", home)
	jsonPath = filepath.Join(home, ".dreamina_cli", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(`{"tasks":[`), 0o600); err != nil {
		t.Fatalf("seed broken json store: %v", err)
	}
	if _, err := NewStore(); err == nil {
		t.Fatalf("expected broken json store to fail")
	}
}

func TestStoreUpdateTaskPreservesExistingFieldsWhenInputOmitted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	original := &AIGCTask{
		SubmitID:    "submit-keep-1",
		UID:         "uid-1",
		GenTaskType: "text2video",
		Request:     &TaskRequestPayload{Values: map[string]any{"prompt": "keep me"}},
		GenStatus:   "submitted",
		FailReason:  "old fail reason",
		ResultJSON:  `{"queue_status":"submitted"}`,
		CreateTime:  11,
		UpdateTime:  12,
		LogID:       "log-old",
		CommerceInfo: map[string]any{
			"charge_type": "vip",
		},
	}
	if err := store.CreateTask(context.Background(), original); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := store.UpdateTask(context.Background(), UpdateTaskInput{
		SubmitID:   "submit-keep-1",
		GenStatus:  "success",
		UpdateTime: 20,
	}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}

	got, err := store.GetTask(context.Background(), "submit-keep-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.GenStatus != "success" || got.UpdateTime != 20 {
		t.Fatalf("unexpected updated fields: %#v", got)
	}
	if got.FailReason != "old fail reason" || got.ResultJSON != `{"queue_status":"submitted"}` || got.LogID != "log-old" {
		t.Fatalf("unexpected overwritten scalar fields: %#v", got)
	}
	if got.Request == nil || got.Request.Values["prompt"] != "keep me" {
		t.Fatalf("unexpected overwritten request payload: %#v", got.Request)
	}
	commerce, ok := got.CommerceInfo.(map[string]any)
	if !ok || commerce["charge_type"] != "vip" {
		t.Fatalf("unexpected overwritten commerce payload: %#v", got.CommerceInfo)
	}
}

func TestStoreConcurrentCreateKeepsSQLiteStoreReadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 12)
	for i := 0; i < 12; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- store.CreateTask(context.Background(), &AIGCTask{
				SubmitID:    fmt.Sprintf("submit-%02d", i),
				UID:         "uid-1",
				GenTaskType: "text2image",
				Request:     &TaskRequestPayload{Values: map[string]any{"index": i}},
				GenStatus:   "submitted",
				CreateTime:  int64(100 + i),
				UpdateTime:  int64(100 + i),
			})
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent CreateTask() error = %v", err)
		}
	}

	list, err := store.ListTasks(context.Background(), ListTaskFilter{
		UID:   "uid-1",
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 12 {
		t.Fatalf("len(ListTasks()) = %d, want 12", len(list))
	}

	body, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatalf("read task store: %v", err)
	}
	if len(body) < len("SQLite format 3\x00") || string(body[:len("SQLite format 3\x00")]) != "SQLite format 3\x00" {
		previewLen := len(body)
		if previewLen > 32 {
			previewLen = 32
		}
		t.Fatalf("task store is not sqlite: %q", string(body[:previewLen]))
	}
}
