package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

// TestParseGeneratorArgsAcceptsSingularRepeatedResourceFlags 确认 multimodal2video 与原程序一致，只接受重复 --image/--video/--audio。
func TestParseGeneratorArgsAcceptsSingularRepeatedResourceFlags(t *testing.T) {
	t.Helper()

	input, pollSeconds, err := parseGeneratorArgs("multimodal2video", []string{
		"--image", "./a.png",
		"--image", "./b.png",
		"--video", "./c.mp4",
		"--audio", "./d.mp3",
		"--poll", "30",
	})
	if err != nil {
		t.Fatalf("parseGeneratorArgs failed: %v", err)
	}
	if pollSeconds != 30 {
		t.Fatalf("unexpected pollSeconds: %d", pollSeconds)
	}

	imagePaths := stringSliceInput(input, "image_paths")
	if len(imagePaths) != 2 || imagePaths[0] != "./a.png" || imagePaths[1] != "./b.png" {
		t.Fatalf("unexpected image_paths: %#v", imagePaths)
	}

	videoPaths := stringSliceInput(input, "video_paths")
	if len(videoPaths) != 1 || videoPaths[0] != "./c.mp4" {
		t.Fatalf("unexpected video_paths: %#v", videoPaths)
	}

	audioPaths := stringSliceInput(input, "audio_paths")
	if len(audioPaths) != 1 || audioPaths[0] != "./d.mp3" {
		t.Fatalf("unexpected audio_paths: %#v", audioPaths)
	}
}

func TestParseGeneratorArgsRejectsPluralMultimodalResourceFlags(t *testing.T) {
	t.Helper()

	_, _, err := parseGeneratorArgs("multimodal2video", []string{"--images", "./a.png"})
	if err == nil {
		t.Fatalf("expected plural multimodal flag to be rejected")
	}
	if got := err.Error(); got != "unknown flag: --images" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestParseGeneratorArgsRejectsUnknownFlagBeforeLogin(t *testing.T) {
	t.Helper()

	_, _, err := parseGeneratorArgs("text2image", []string{"--badflag"})
	if err == nil {
		t.Fatalf("expected unknown flag error")
	}
	if got := err.Error(); got != "unknown flag: --badflag" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestParseGeneratorArgsRejectsUnexpectedPositionalArgBeforeRequiredFlags(t *testing.T) {
	t.Helper()

	cases := []struct {
		genTaskType string
		args        []string
		want        string
	}{
		{genTaskType: "text2image", args: []string{"foo"}, want: `unknown command "foo" for "dreamina text2image"`},
		{genTaskType: "text2video", args: []string{"foo"}, want: `unknown command "foo" for "dreamina text2video"`},
		{genTaskType: "image2video", args: []string{"foo"}, want: `unknown command "foo" for "dreamina image2video"`},
		{genTaskType: "frames2video", args: []string{"foo"}, want: `unknown command "foo" for "dreamina frames2video"`},
		{genTaskType: "multiframe2video", args: []string{"foo"}, want: `unknown command "foo" for "dreamina multiframe2video"`},
		{genTaskType: "image_upscale", args: []string{"foo"}, want: `unknown command "foo" for "dreamina image_upscale"`},
	}

	for _, tc := range cases {
		_, _, err := parseGeneratorArgs(tc.genTaskType, tc.args)
		if err == nil {
			t.Fatalf("expected positional-arg error for %s", tc.genTaskType)
		}
		if got := err.Error(); got != tc.want {
			t.Fatalf("unexpected positional-arg error for %s: %q", tc.genTaskType, got)
		}
	}
}

func TestParseGeneratorArgsRejectsMissingValueBeforeValidation(t *testing.T) {
	t.Helper()

	cases := []struct {
		genTaskType string
		args        []string
		want        string
	}{
		{genTaskType: "text2image", args: []string{"--prompt"}, want: `flag needs an argument: --prompt`},
		{genTaskType: "image2video", args: []string{"--image"}, want: `flag needs an argument: --image`},
		{genTaskType: "frames2video", args: []string{"--first"}, want: `flag needs an argument: --first`},
		{genTaskType: "text2video", args: []string{"--poll"}, want: `flag needs an argument: --poll`},
	}

	for _, tc := range cases {
		_, _, err := parseGeneratorArgs(tc.genTaskType, tc.args)
		if err == nil {
			t.Fatalf("expected missing-value error for %s", tc.genTaskType)
		}
		if got := err.Error(); got != tc.want {
			t.Fatalf("unexpected missing-value error for %s: %q", tc.genTaskType, got)
		}
	}
}

func TestParseGeneratorArgsRejectsInvalidPollValue(t *testing.T) {
	t.Helper()

	_, _, err := parseGeneratorArgs("text2image", []string{"--prompt=hello", "--poll=abc"})
	if err == nil {
		t.Fatalf("expected invalid poll error")
	}
	if got := err.Error(); got != `invalid argument "abc" for "--poll" flag: strconv.ParseInt: parsing "abc": invalid syntax` {
		t.Fatalf("unexpected poll error: %q", got)
	}

	_, _, err = parseGeneratorArgs("text2image", []string{"--prompt=hello", "--poll="})
	if err == nil {
		t.Fatalf("expected empty poll error")
	}
	if got := err.Error(); got != `invalid argument "" for "--poll" flag: strconv.ParseInt: parsing "": invalid syntax` {
		t.Fatalf("unexpected empty poll error: %q", got)
	}
}

func TestParseGeneratorArgsTreatsExplicitEmptyPromptAsProvided(t *testing.T) {
	t.Helper()

	input, _, err := parseGeneratorArgs("text2image", []string{"--prompt="})
	if err != nil {
		t.Fatalf("parseGeneratorArgs failed: %v", err)
	}
	if got, ok := input["prompt"]; !ok || got != "" {
		t.Fatalf("expected explicit empty prompt to be preserved, got %#v", input["prompt"])
	}
}

func TestValidateGeneratorInputAllowsExplicitEmptyPrompt(t *testing.T) {
	t.Helper()

	for _, genTaskType := range []string{"text2image", "text2video", "image2image", "image2video"} {
		input := GenerateInput{"prompt": ""}
		if genTaskType == "image2image" {
			input["image_paths"] = []string{"/tmp/does-not-matter.png"}
		}
		if genTaskType == "image2video" {
			input["image_path"] = "/tmp/does-not-matter.png"
		}
		err := validateGeneratorInput(genTaskType, input)
		if err == nil {
			continue
		}
		if err.Error() == "prompt is required" {
			t.Fatalf("expected explicit empty prompt to bypass local prompt validation for %s", genTaskType)
		}
	}
}

func TestValidateGeneratorInputAllowsExplicitEmptySingleResourcePath(t *testing.T) {
	t.Helper()

	cases := []struct {
		genTaskType string
		input       GenerateInput
	}{
		{
			genTaskType: "image2video",
			input:       GenerateInput{"image_path": "", "prompt": "hello"},
		},
		{
			genTaskType: "image_upscale",
			input:       GenerateInput{"image_path": "", "resolution_type": "2k"},
		},
		{
			genTaskType: "frames2video",
			input:       GenerateInput{"first_path": "", "last_path": ""},
		},
	}

	for _, tc := range cases {
		if err := validateGeneratorInput(tc.genTaskType, tc.input); err != nil {
			t.Fatalf("expected explicit empty resource path to bypass local required-flag validation for %s, got %v", tc.genTaskType, err)
		}
	}
}

func TestParseGeneratorArgsReportsMissingPromptAsRequiredFlag(t *testing.T) {
	t.Helper()

	_, _, err := parseGeneratorArgs("text2image", []string{"--ratio", "1:1"})
	if err == nil {
		t.Fatalf("expected missing prompt error")
	}
	if got := err.Error(); got != `required flag(s) "prompt" not set` {
		t.Fatalf("unexpected error: %q", got)
	}

	_, _, err = parseGeneratorArgs("text2video", []string{"--duration", "5"})
	if err == nil {
		t.Fatalf("expected missing prompt error")
	}
	if got := err.Error(); got != `required flag(s) "prompt" not set` {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestParseGeneratorArgsReportsMissingImageFlagsBeforePromptValidation(t *testing.T) {
	t.Helper()

	_, _, err := parseGeneratorArgs("image2image", nil)
	if err == nil {
		t.Fatalf("expected missing image2image flags error")
	}
	if got := err.Error(); got != `required flag(s) "images", "prompt" not set` {
		t.Fatalf("unexpected image2image error: %q", got)
	}

	_, _, err = parseGeneratorArgs("image2video", nil)
	if err == nil {
		t.Fatalf("expected missing image2video flags error")
	}
	if got := err.Error(); got != `required flag(s) "image", "prompt" not set` {
		t.Fatalf("unexpected image2video error: %q", got)
	}
}

func TestParseGeneratorArgsMapsSingleImageAndFrameFlagsToOriginalInternalFields(t *testing.T) {
	t.Helper()

	upscaleInput, _, err := parseGeneratorArgs("image_upscale", []string{"--image", "./input.png", "--resolution_type", "2k"})
	if err != nil {
		t.Fatalf("parseGeneratorArgs(image_upscale) failed: %v", err)
	}
	if got := upscaleInput["image_path"]; got != "./input.png" {
		t.Fatalf("unexpected image_upscale image_path: %#v", got)
	}

	framesInput, _, err := parseGeneratorArgs("frames2video", []string{"--first", "./start.png", "--last", "./end.png"})
	if err != nil {
		t.Fatalf("parseGeneratorArgs(frames2video) failed: %v", err)
	}
	if got := framesInput["first_path"]; got != "./start.png" {
		t.Fatalf("unexpected first_path: %#v", got)
	}
	if got := framesInput["last_path"]; got != "./end.png" {
		t.Fatalf("unexpected last_path: %#v", got)
	}
}

func TestPrintPolledGeneratorOutputKeepsCompactSubmitViewWhenStillQuerying(t *testing.T) {
	t.Helper()

	cmd := &Command{}
	var out bytes.Buffer
	cmd.out = &out

	submitted := &task.AIGCTask{
		SubmitID:  "submit-querying-1",
		GenStatus: "querying",
		LogID:     "log-querying-1",
		ResultJSON: `{
			"gen_status":"querying",
			"log_id":"log-querying-1",
			"response":{
				"data":{
					"submit_id":"submit-querying-1",
					"commerce_info":{"credit_count":1}
				}
			}
		}`,
	}
	finalTask := map[string]any{
		"SubmitID":  "submit-querying-1",
		"GenStatus": "querying",
	}

	if err := printPolledGeneratorOutput(cmd, submitted, finalTask); err != nil {
		t.Fatalf("printPolledGeneratorOutput failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"submit_id": "submit-querying-1"`) {
		t.Fatalf("missing compact submit_id: %q", text)
	}
	if !strings.Contains(text, `"credit_count": 1`) {
		t.Fatalf("missing compact credit_count: %q", text)
	}
	if strings.Contains(text, `"task"`) {
		t.Fatalf("did not expect query_result task wrapper for querying timeout: %q", text)
	}
}

func TestPrintPolledGeneratorOutputUsesFinalQueryingTaskContextWhenAvailable(t *testing.T) {
	t.Helper()

	cmd := &Command{}
	var out bytes.Buffer
	cmd.out = &out

	submitted := &task.AIGCTask{
		SubmitID:    "submit-querying-ctx-1",
		GenTaskType: "image2image",
		GenStatus:   "querying",
		LogID:       "log-submit-ctx-1",
		ResultJSON: `{
			"gen_status":"querying",
			"response":{"data":{"submit_id":"submit-querying-ctx-1","commerce_info":{"credit_count":1}}}
		}`,
	}
	finalTask := &task.AIGCTask{
		SubmitID:    "submit-querying-ctx-1",
		GenTaskType: "image2image",
		GenStatus:   "querying",
		LogID:       "log-final-ctx-1",
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "改成水彩风格"},
		},
		ResultJSON: `{
			"gen_status":"querying",
			"log_id":"log-final-ctx-1",
			"queue_info":{"queue_idx":0,"priority":1,"queue_status":"Generating","queue_length":0,"debug_info":"{}"},
			"response":{"data":{"submit_id":"submit-querying-ctx-1","commerce_info":{"credit_count":3}}}
		}`,
	}

	if err := printPolledGeneratorOutput(cmd, submitted, finalTask); err != nil {
		t.Fatalf("printPolledGeneratorOutput failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"prompt": "改成水彩风格"`) {
		t.Fatalf("missing prompt from final querying task: %q", text)
	}
	if !strings.Contains(text, `"queue_info"`) {
		t.Fatalf("missing queue_info from final querying task: %q", text)
	}
	if !strings.Contains(text, `"credit_count": 3`) {
		t.Fatalf("expected credit_count from final querying task: %q", text)
	}
}

func TestPrintPolledGeneratorOutputOmitsZeroCreditCountForQueryingTask(t *testing.T) {
	t.Helper()

	cmd := &Command{}
	var out bytes.Buffer
	cmd.out = &out

	finalTask := &task.AIGCTask{
		SubmitID:    "submit-querying-zero-credit-1",
		GenTaskType: "image2image",
		GenStatus:   "querying",
		LogID:       "log-querying-zero-credit-1",
		Request: &task.TaskRequestPayload{
			Values: map[string]any{"prompt": "改成水彩风格"},
		},
		ResultJSON: `{
			"gen_status":"querying",
			"log_id":"log-querying-zero-credit-1",
			"queue_info":{"queue_idx":0,"priority":1,"queue_status":"Generating","queue_length":0,"debug_info":"{}"},
			"response":{"data":{"submit_id":"submit-querying-zero-credit-1","commerce_info":{"credit_count":0}}}
		}`,
	}

	if err := printPolledGeneratorOutput(cmd, finalTask, finalTask); err != nil {
		t.Fatalf("printPolledGeneratorOutput failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"submit_id": "submit-querying-zero-credit-1"`) {
		t.Fatalf("missing submit_id: %q", text)
	}
	if strings.Contains(text, `"credit_count"`) {
		t.Fatalf("did not expect zero credit_count in compact querying output: %q", text)
	}
}

func TestPrintPolledGeneratorOutputKeepsUpscaleCreditCountButOmitsQueueInfo(t *testing.T) {
	t.Helper()

	cmd := &Command{}
	var out bytes.Buffer
	cmd.out = &out

	finalTask := &task.AIGCTask{
		SubmitID:    "submit-upscale-querying-1",
		GenTaskType: "image_upscale",
		GenStatus:   "querying",
		LogID:       "log-upscale-querying-1",
		ResultJSON: `{
			"gen_status":"querying",
			"log_id":"log-upscale-querying-1",
			"queue_info":{"queue_idx":0,"priority":1,"queue_status":"Generating","queue_length":0,"debug_info":"{}"},
			"response":{"data":{"submit_id":"submit-upscale-querying-1","commerce_info":{"credit_count":2}}}
		}`,
	}

	if err := printPolledGeneratorOutput(cmd, finalTask, finalTask); err != nil {
		t.Fatalf("printPolledGeneratorOutput failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"submit_id": "submit-upscale-querying-1"`) {
		t.Fatalf("missing submit_id: %q", text)
	}
	if strings.Contains(text, `"queue_info"`) {
		t.Fatalf("did not expect queue_info for image_upscale querying output: %q", text)
	}
	if !strings.Contains(text, `"credit_count": 2`) {
		t.Fatalf("expected credit_count for image_upscale querying output: %q", text)
	}
}

func TestCompactGeneratorFailureViewMatchesOriginalFailedSubmitShape(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-fail-1",
		GenTaskType: "image2video",
		GenStatus:   "failed",
		LogID:       "20260405031911192168001245212AD12",
		FailReason:  "api error: ret=2061, message=模型已不可用，请刷新界面后再试试, logid=20260405031911192168001245212AD12",
		ResultJSON: `{
			"input": {
				"prompt": "camera push in"
			},
			"log_id": "20260405031911192168001245212AD12"
		}`,
	}

	got := compactGeneratorFailureView(taskValue)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal compact failed output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal compact failed output: %v", err)
	}
	if root["submit_id"] != "submit-fail-1" {
		t.Fatalf("unexpected submit_id: %#v", root["submit_id"])
	}
	if root["prompt"] != "camera push in" {
		t.Fatalf("unexpected prompt: %#v", root["prompt"])
	}
	if root["logid"] != "20260405031911192168001245212AD12" {
		t.Fatalf("unexpected logid: %#v", root["logid"])
	}
	if root["gen_status"] != "fail" {
		t.Fatalf("unexpected gen_status: %#v", root["gen_status"])
	}
	if root["fail_reason"] != "api error: ret=2061, message=模型已不可用，请刷新界面后再试试, logid=20260405031911192168001245212AD12" {
		t.Fatalf("unexpected fail_reason: %#v", root["fail_reason"])
	}
}

func TestCompactGeneratorFailureViewOmitsEmptyLogID(t *testing.T) {
	t.Helper()

	taskValue := &task.AIGCTask{
		SubmitID:    "submit-fail-2",
		GenTaskType: "image_upscale",
		GenStatus:   "failed",
		FailReason:  `upload resource "": read file : open : no such file or directory`,
		ResultJSON: `{
			"gen_status": "fail",
			"fail_reason": "upload resource \"\": read file : open : no such file or directory"
		}`,
	}

	got := compactGeneratorFailureView(taskValue)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal compact failed output: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal compact failed output: %v", err)
	}
	if _, ok := root["logid"]; ok {
		t.Fatalf("expected empty logid to be omitted, got %#v", root["logid"])
	}
}

func TestPrintGeneratorQueryResultOutputUsesQueryResultTopLevelShape(t *testing.T) {
	t.Helper()

	cmd := &Command{}
	var out bytes.Buffer
	cmd.out = &out

	taskValue := &task.AIGCTask{
		SubmitID:  "submit-success-1",
		GenStatus: "success",
		ResultJSON: `{
			"images": [{"image_url":"https://example.com/result.png","width":5404,"height":3040}],
			"videos": [],
			"queue_info": {"queue_status":"Finish"},
			"response": {"data": {"commerce_info": {"credit_count": 1}}}
		}`,
		CommerceInfo: map[string]any{"credit_count": 1},
	}

	if err := printGeneratorQueryResultOutput(cmd, taskValue); err != nil {
		t.Fatalf("printGeneratorQueryResultOutput failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"submit_id": "submit-success-1"`) {
		t.Fatalf("missing top-level submit_id: %q", text)
	}
	if !strings.Contains(text, `"gen_status": "success"`) {
		t.Fatalf("missing top-level gen_status: %q", text)
	}
	if strings.Contains(text, `"task"`) {
		t.Fatalf("did not expect task wrapper: %q", text)
	}
}
