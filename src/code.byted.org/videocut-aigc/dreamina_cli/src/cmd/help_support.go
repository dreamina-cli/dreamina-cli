package cmd

import (
	"fmt"
	"io"
	"strings"
)

type helpFlag struct {
	Name        string
	Description string
}

type helpRow struct {
	Name        string
	Description string
}

type commandHelpSpec struct {
	UseLine     string
	Paragraphs  []string
	Flags       []helpFlag
	Examples    []string
	RootSummary string
	Raw         string
}

var rootBuiltInHelpRows = []helpRow{
	{Name: "help", Description: "Help about any command"},
	{Name: "import_login_response", Description: "Import a copied dreamina_cli_login JSON response into the local credential store"},
	{Name: "list_task", Description: "List saved tasks with status and result summary"},
	{Name: "login", Description: "Log in locally before using task and account commands; use --headless for agent or remote login"},
	{Name: "logout", Description: "Clear the local login session"},
	{Name: "query_result", Description: "Query the current result of an async generation task"},
	{Name: "relogin", Description: "Clear the local login session and force a fresh login; use --headless for agent or remote login"},
	{Name: "user_credit", Description: "Show the current user's remaining credit balance"},
	{Name: "version", Description: "Print build version and commit information"},
}

var rootGeneratorHelpRows = []helpRow{
	{Name: "frames2video", Description: "Submit a Dreamina first-last-frames video task"},
	{Name: "image2image", Description: "Submit a Dreamina image-to-image task"},
	{Name: "image2video", Description: "Animate one image into video; use multiframe2video for multi-image stories"},
	{Name: "image_upscale", Description: "Submit a Dreamina image upscale task"},
	{Name: "multiframe2video", Description: "Create a coherent video story from multiple images"},
	{Name: "multimodal2video", Description: "Dreamina flagship video mode with all-around references and Seedance 2.0"},
	{Name: "text2image", Description: "Submit a Dreamina text-to-image task"},
	{Name: "text2video", Description: "Submit a Dreamina text-to-video task"},
}

var commandHelpSpecs = map[string]commandHelpSpec{
	"help": {
		Raw: `Usage:
  dreamina help [command] [flags]

Help provides help for any command in the application.
Simply type dreamina help [path to command] for full details.


Flags:
  -h, --help   help for help

Global Flags:
      --version   print build version information
`,
	},
	"completion": {
		Raw: `Usage:
  dreamina completion [flags]

Generate the autocompletion script for dreamina for the specified shell.
See each sub-command's help for details on how to use the generated script.



Flags:
  -h, --help   help for completion

Global Flags:
      --version   print build version information
`,
	},
	"login": {
		Raw: `Usage:
  dreamina login [flags]

Reuse the current local login session when it is still valid; otherwise open the browser and continue login.
When an AI or agent invokes login, prefer --headless first rather than the default local callback browser flow.
Use --headless to keep the login page inside a headless browser and render the intercepted Douyin QR code in the terminal.
Agents automating login in headless or remote environments should have the user install Google Chrome first (Linux: google-chrome or google-chrome-stable; the binary must be available as google-chrome), because --headless drives headless Chrome.
The CLI will wait until the local credential is saved successfully.
If the current machine cannot open a browser, follow the printed manual import steps and then use dreamina import_login_response to save the copied login JSON.
For that JSON, paste the full body in a local terminal or use --file with a saved JSON file; pasting long JSON in chat channels often truncates the payload.


Flags:
      --debug      show manual-import details plus extra callback troubleshooting output
      --headless   headless Google Chrome (install google-chrome); load the login page and render the QR code in the terminal
  -h, --help       help for login

Global Flags:
      --version   print build version information

Examples:
  dreamina login
  dreamina login --headless
  dreamina login --debug
`,
	},
	"relogin": {
		Raw: `Usage:
  dreamina relogin [flags]

Remove the local login credential first, then force the browser login flow.
When an AI or agent invokes relogin, prefer --headless first rather than the default local callback browser flow.
Use --headless to keep the login page inside a headless browser and render the intercepted Douyin QR code in the terminal.
Agents automating relogin in headless or remote environments should have the user install Google Chrome first (Linux: google-chrome or google-chrome-stable; the binary must be available as google-chrome), because --headless drives headless Chrome.
If the current machine cannot open a browser, follow the printed manual import steps and then use dreamina import_login_response to save the copied login JSON.
For that JSON, paste the full body in a local terminal or use --file with a saved JSON file; pasting long JSON in chat channels often truncates the payload.


Flags:
      --debug      show manual-import details plus extra callback troubleshooting output
      --headless   headless Google Chrome (install google-chrome); load the login page and render the QR code in the terminal
  -h, --help       help for relogin

Global Flags:
      --version   print build version information

Examples:
  dreamina relogin
  dreamina relogin --headless
  dreamina relogin --debug
`,
	},
	"text2image": {
		Raw: `Usage:
  dreamina text2image [flags]

Submit a Dreamina text-to-image task. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- model_version: 3.0, 3.1, 4.0, 4.1, 4.5, 4.6, 5.0, lab
- ratio: 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16
- 3.0/3.1 -> resolution_type 1k or 2k
- 4.0/4.1/4.5/4.6/5.0 -> resolution_type 2k or 4k
- lab -> resolution_type 2k or 4k; VIP only

Notes:
- omit --model_version to use the default model
- omit --resolution_type to use the model default


Flags:
      --prompt string            generation prompt
      --ratio string             supported values: 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16
      --resolution_type string   supported values by model: 3.0/3.1 -> 1k or 2k; 4.0/4.1/4.5/4.6/5.0/lab -> 2k or 4k; omit to use the model default
      --model_version string     supported values: 3.0, 3.1, 4.0, 4.1, 4.5, 4.6, 5.0, lab; lab requires VIP
      --poll int                 submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                     help for text2image

Global Flags:
      --version   print build version information

Examples:
  dreamina text2image --prompt="a cat portrait" --ratio=1:1 --resolution_type=2k
`,
	},
	"text2video": {
		Raw: `Usage:
  dreamina text2video [flags]

Submit a Dreamina text-to-video task. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- model_version: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip
- ratio: 1:1, 3:4, 16:9, 4:3, 9:16, 21:9
- seedance2.0 family -> video_resolution 720p; duration 4-15s

Notes:
- default model_version: seedance2.0fast
- omit --video_resolution to use the model default
- omit --ratio to use the default ratio
- 部分高内容安全风险模型在首次使用前，可能需要先在 Dreamina Web 端完成授权确认。若返回 AigcComplianceConfirmationRequired，请先完成授权后重试。


Flags:
      --prompt string             generation prompt
      --duration int              video duration in seconds; supported range: 4-15 (default 5)
      --ratio string              supported values: 1:1, 3:4, 16:9, 4:3, 9:16, 21:9
      --video_resolution string   supported values: 720p
      --model_version string      supported values: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip; default: seedance2.0fast
      --poll int                  submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                      help for text2video

Global Flags:
      --version   print build version information

Examples:
  dreamina text2video --prompt="a cat running" --duration=5
`,
	},
	"image2image": {
		Raw: `Usage:
  dreamina image2image [flags]

Upload one or more local images, then submit a Dreamina image-to-image task. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- model_version: 4.0, 4.1, 4.5, 4.6, 5.0, lab
- ratio: 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16
- resolution_type: 2k, 4k
- lab requires VIP

Notes:
- 1k is not supported for image2image
- omit --model_version to use the default model
- omit --resolution_type to use the model default


Flags:
      --images strings           local input image paths
      --prompt string            edit prompt
      --ratio string             supported values: 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16
      --resolution_type string   supported values: 2k, 4k; omit to use the model default
      --model_version string     supported values: 4.0, 4.1, 4.5, 4.6, 5.0, lab; lab requires VIP
      --poll int                 submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                     help for image2image

Global Flags:
      --version   print build version information

Examples:
  dreamina image2image --images ./input.png --prompt="turn into watercolor"
`,
	},
	"image_upscale": {
		Raw: `Usage:
  dreamina image_upscale [flags]

Upload one local image, then submit a Dreamina image upscale task. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- resolution_type: 2k, 4k, 8k
- 2k is available to non-VIP users
- 4k and 8k require VIP


Flags:
      --image string             local input image path
      --resolution_type string   supported values: 2k, 4k, 8k; 4k and 8k require VIP
      --poll int                 submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                     help for image_upscale

Global Flags:
      --version   print build version information

Examples:
  dreamina image_upscale --image=./input.png --resolution_type=4k
`,
	},
	"image2video": {
		Raw: `Usage:
  dreamina image2video [flags]

Upload one local image, then submit a Dreamina image-to-video task. For multi-image storytelling, use multiframe2video; for full-reference mixed-media generation, use multimodal2video. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- basic usage: --image + --prompt
- advanced controls: set any of --duration, --video_resolution, or --model_version
- advanced model_version values: 3.0, 3.0fast, 3.0pro, 3.0_fast, 3.0_pro, 3.5pro, 3.5_pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip
- ratio is inferred from the input image and is not set on this command

Notes:
- omit advanced controls to use the default image-to-video path
- duration, model_version, and video_resolution must be provided in a supported combination
- 部分高内容安全风险模型在首次使用前，可能需要先在 Dreamina Web 端完成授权确认。若返回 AigcComplianceConfirmationRequired，请先完成授权后重试。


Flags:
      --image string              local first-frame image path
      --prompt string             generation prompt
      --duration int              advanced controls only; supported duration ranges by model: 3.0/3.0fast/3.0pro -> 3-10, 3.5pro -> 4-12, seedance2.0 family -> 4-15 (default 5)
      --video_resolution string   advanced controls only; supported values by model: 3.0/3.0fast/3.5pro -> 720p or 1080p, 3.0pro -> 1080p, seedance2.0 family -> 720p
      --model_version string      advanced controls only; supported values: 3.0, 3.0fast, 3.0pro, 3.0_fast, 3.0_pro, 3.5pro, 3.5_pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip
      --poll int                  submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                      help for image2video

Global Flags:
      --version   print build version information

Examples:
  dreamina image2video --image=./first.png --prompt="camera push in"
`,
	},
	"frames2video": {
		Raw: `Usage:
  dreamina frames2video [flags]

Upload two local images as first and last frames, then submit a Dreamina video generation task. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- model_version: 3.0, 3.5pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip
- 3.0 -> video_resolution 720p or 1080p; duration 3-10s
- 3.5pro -> video_resolution 720p or 1080p; duration 4-12s
- seedance2.0 family -> video_resolution 720p; duration 4-15s

Notes:
- ratio is inferred from the first frame image size
- default model_version: seedance2.0fast
- omit --video_resolution to use the model default
- 部分高内容安全风险模型在首次使用前，可能需要先在 Dreamina Web 端完成授权确认。若返回 AigcComplianceConfirmationRequired，请先完成授权后重试。


Flags:
      --first string              local first-frame image path
      --last string               local last-frame image path
      --prompt string             generation prompt
      --duration int              video duration in seconds; supported ranges: 3.0 -> 3-10, 3.5pro -> 4-12, seedance2.0 family -> 4-15 (default 5)
      --video_resolution string   supported values by model: 3.0/3.5pro -> 720p or 1080p; seedance2.0 family -> 720p
      --model_version string      supported values: 3.0, 3.5pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip; default: seedance2.0fast
      --poll int                  submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                      help for frames2video

Global Flags:
      --version   print build version information

Examples:
  dreamina frames2video --first=./start.png --last=./end.png --prompt="season changes"
`,
	},
	"multiframe2video": {
		Raw: `Usage:
  dreamina multiframe2video [flags]

Upload multiple local images, then submit a Dreamina intelligent multi-frame video task for coherent visual storytelling. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- inputs: 2-20 images
- exactly 2 images: use shorthand --prompt and optional --duration
- 3+ images: repeat --transition-prompt once per transition segment to describe how one frame evolves into the next
- repeat --transition-duration once per transition segment, or omit it to default each segment to 3 seconds

Notes:
- designed for multi-image story generation, not full multimodal editing
- for N images, the transition count is N-1
- ratio is inferred from the first image
- model_version and video_resolution overrides are not supported by this command
- each duration segment is limited to [0.5, 8] seconds and total duration must be >= 2


Flags:
      --images strings                    local reference image paths
      --prompt string                     shorthand prompt for exactly 2 images
      --duration float                    shorthand transition duration in seconds for exactly 2 images; backend clamps each segment to [0.5, 8] and requires total duration >= 2 (default 3)
      --transition-prompt stringArray     repeat once per transition segment; for N images provide N-1 prompts
      --transition-duration stringArray   repeat once per transition segment in seconds; for N images provide N-1 durations, or omit to default each segment to 3
      --poll int                          submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                              help for multiframe2video

Global Flags:
      --version   print build version information

Examples:
  dreamina multiframe2video --images ./a.png,./b.png --prompt="character turns around"
  dreamina multiframe2video --images ./a.png,./b.png,./c.png --transition-prompt="turn from A to B" --transition-prompt="turn from B to C"
`,
	},
	"ref2video": {
		Raw: `Usage:
  dreamina ref2video [flags]

Upload multiple local images, then submit a Dreamina intelligent multi-frame video task for coherent visual storytelling. The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- inputs: 2-20 images
- exactly 2 images: use shorthand --prompt and optional --duration
- 3+ images: repeat --transition-prompt once per transition segment to describe how one frame evolves into the next
- repeat --transition-duration once per transition segment, or omit it to default each segment to 3 seconds

Notes:
- designed for multi-image story generation, not full multimodal editing
- for N images, the transition count is N-1
- ratio is inferred from the first image
- model_version and video_resolution overrides are not supported by this command
- each duration segment is limited to [0.5, 8] seconds and total duration must be >= 2


Flags:
      --images strings                    local reference image paths
      --prompt string                     shorthand prompt for exactly 2 images
      --duration float                    shorthand transition duration in seconds for exactly 2 images; backend clamps each segment to [0.5, 8] and requires total duration >= 2 (default 3)
      --transition-prompt stringArray     repeat once per transition segment; for N images provide N-1 prompts
      --transition-duration stringArray   repeat once per transition segment in seconds; for N images provide N-1 durations, or omit to default each segment to 3
      --poll int                          submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                              help for ref2video

Global Flags:
      --version   print build version information

Examples:
  dreamina ref2video --images ./a.png,./b.png --prompt="character turns around"
  dreamina ref2video --images ./a.png,./b.png,./c.png --transition-prompt="turn from A to B" --transition-prompt="turn from B to C"
`,
	},
	"multimodal2video": {
		Raw: `Usage:
  dreamina multimodal2video [flags]

Upload local images, videos, and audio, then submit Dreamina's flagship multimodal video generation mode. This is the strongest video generation mode currently exposed in the CLI, supports all-around references, and supports the Seedance 2.0 family (flag values: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip). The task is asynchronous, but --poll can wait briefly before falling back to query_result.

Supported combinations:
- inputs: any mix of --image, --video, --audio
- at least one --image or --video is required
- audio inputs must be 2-15 seconds
- model_version: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip
- ratio: 1:1, 3:4, 16:9, 4:3, 9:16, 21:9
- video_resolution: 720p
- duration: 4-15s

Notes:
- local files are uploaded automatically before submit
- input limits: image<=9, video<=3, audio<=3
- 部分高内容安全风险模型在首次使用前，可能需要先在 Dreamina Web 端完成授权确认。若返回 AigcComplianceConfirmationRequired，请先完成授权后重试。


Flags:
      --image stringArray         repeat for each local input image path
      --video stringArray         repeat for each local input video path
      --audio stringArray         repeat for each local input audio path
      --prompt string             optional multimodal edit prompt
      --duration int              video duration in seconds; supported range: 4-15 (default 5)
      --ratio string              supported values: 1:1, 3:4, 16:9, 4:3, 9:16, 21:9
      --video_resolution string   supported values: 720p
      --model_version string      supported values: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip
      --poll int                  submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)
  -h, --help                      help for multimodal2video

Global Flags:
      --version   print build version information

Examples:
  dreamina multimodal2video --image ./input.png --prompt="turn this into a cinematic shot"
  dreamina multimodal2video --image ./input.png --audio ./music.mp3 --model_version=seedance2.0fast --duration=5
  dreamina multimodal2video --image ./input.png --video ./ref.mp4 --audio ./music.mp3 --model_version=seedance2.0fast --duration=5
`,
	},
	"import_login_response": {
		Raw: `Usage:
  dreamina import_login_response [flags]

Import the full JSON body returned by /dreamina/cli/v1/dreamina_cli_login.
Use this when an agent or another machine finishes the browser login and needs to save the login response back into the local CLI credential store.
You can either pass --file or pipe the JSON through stdin.
If a user shares this JSON through a chat channel, ask them to paste the full JSON locally or attach a JSON file instead, because long pastes in channels are often truncated.


Flags:
      --file string   read the copied login JSON from a file instead of stdin
  -h, --help          help for import_login_response

Global Flags:
      --version   print build version information

Examples:
  dreamina import_login_response --file /tmp/dreamina-login.json
  cat /tmp/dreamina-login.json | dreamina import_login_response
`,
	},
	"query_result": {
		Raw: `Usage:
  dreamina query_result [flags]

Query one async task by submit_id.


Flags:
      --download_dir string   download result media into the target directory
  -h, --help                  help for query_result
      --submit_id string      task submit_id

Global Flags:
      --version   print build version information

Examples:
  dreamina query_result --submit_id=3f6eb41f425d23a3
`,
	},
	"list_task": {
		Raw: `Usage:
  dreamina list_task [flags]

List tasks saved for the current logged-in user.


Flags:
      --gen_status string      filter by gen_status
      --gen_task_type string   filter by gen_task_type
  -h, --help                   help for list_task
      --limit int              max number of tasks to return (default 20)
      --offset int             offset for pagination
      --submit_id string       filter by submit_id

Global Flags:
      --version   print build version information

Examples:
  dreamina list_task
  dreamina list_task --gen_status=success
`,
	},
	"logout": {
		Raw: `Usage:
  dreamina logout [flags]

Remove the local login credential without touching tasks or config.


Flags:
  -h, --help   help for logout

Global Flags:
      --version   print build version information

Examples:
  dreamina logout
`,
	},
	"user_credit": {
		Raw: `Usage:
  dreamina user_credit [flags]

Query the current logged-in user's remaining Dreamina credits.


Flags:
  -h, --help   help for user_credit

Global Flags:
      --version   print build version information

Examples:
  dreamina user_credit
`,
	},
	"validate-auth-token": {
		Raw: `Usage:
  dreamina validate-auth-token [flags]

Debug: validate the local credential with the backend


Flags:
  -h, --help   help for validate-auth-token

Global Flags:
      --version   print build version information

Examples:
  dreamina validate-auth-token
`,
	},
	"version": {
		Raw: `Usage:
  dreamina version [flags]

Print build version and commit information


Flags:
  -h, --help   help for version

Global Flags:
      --version   print build version information

Examples:
  dreamina version
`,
	},
}

func newHelpCommand(root *Command) *Command {
	return &Command{
		Use: "help",
		RunE: func(cmd *Command, args []string) error {
			if len(args) == 0 {
				return writeCommandHelp(cmd.OutOrStdout(), root)
			}
			target, ok := findChildCommand(root, args[0])
			if !ok {
				return writeUnknownHelpTopic(cmd.OutOrStdout(), args[0])
			}
			return writeCommandHelp(cmd.OutOrStdout(), target)
		},
	}
}

func isHelpFlag(arg string) bool {
	arg = strings.TrimSpace(arg)
	return arg == "-h" || arg == "--help"
}

func isVersionFlag(arg string) bool {
	return strings.TrimSpace(arg) == "--version"
}

func findChildCommand(root *Command, name string) (*Command, bool) {
	name = strings.TrimSpace(name)
	for _, child := range root.Children {
		if child != nil && strings.TrimSpace(child.Use) == name {
			return child, true
		}
	}
	return nil, false
}

func writeCommandHelp(out io.Writer, cmd *Command) error {
	if out == nil {
		return nil
	}
	if cmd == nil || strings.TrimSpace(cmd.Use) == "" || strings.TrimSpace(cmd.Use) == "dreamina" {
		return writeRootHelp(out)
	}
	spec, ok := commandHelpSpecs[strings.TrimSpace(cmd.Use)]
	if !ok {
		spec = defaultCommandHelpSpec(strings.TrimSpace(cmd.Use))
	}
	if strings.TrimSpace(spec.Raw) != "" {
		_, _ = io.WriteString(out, spec.Raw)
		return nil
	}
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintf(out, "  %s\n", spec.UseLine)
	_, _ = fmt.Fprintln(out)
	for _, paragraph := range spec.Paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintln(out, paragraph)
	}
	if len(spec.Flags) > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Flags:")
		for _, flag := range spec.Flags {
			_, _ = fmt.Fprintf(out, "  %-28s %s\n", flag.Name, flag.Description)
		}
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Global Flags:")
	_, _ = fmt.Fprintln(out, "      --version   print build version information")
	if len(spec.Examples) > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Examples:")
		for _, example := range spec.Examples {
			_, _ = fmt.Fprintf(out, "  %s\n", example)
		}
	}
	return nil
}

func writeRootHelp(out io.Writer) error {
	if out == nil {
		return nil
	}
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  dreamina [flags]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "即梦 official AIGC CLI tool for login, account, and generation workflows")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "About:")
	_, _ = fmt.Fprintln(out, "  dreamina is the 即梦 official AIGC CLI tool.")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Quick start:")
	_, _ = fmt.Fprintln(out, `  1. Run "dreamina login" to save a local login session.`)
	_, _ = fmt.Fprintln(out, `  2. Run a generator command such as "dreamina text2image --prompt=\"a cat portrait\"".`)
	_, _ = fmt.Fprintln(out, `  3. Use "dreamina query_result --submit_id=<id>" for async tasks, or "dreamina list_task" to review saved tasks.`)
	_, _ = fmt.Fprintln(out, `  4. Use "dreamina user_credit" to check the current account credit balance.`)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Tips:")
	_, _ = fmt.Fprintln(out, `  Run "dreamina <subcommand> -h" to view detailed help for any subcommand.`)
	_, _ = fmt.Fprintln(out, `  When an AI or agent drives login, prefer "dreamina login --headless" (or "dreamina relogin --headless") over the default browser callback flow; have the user install Google Chrome (google-chrome / google-chrome-stable on Linux) first.`)
	_, _ = fmt.Fprintln(out, `  When sharing the manual-import login JSON with an agent, paste the full JSON in a local terminal or send a JSON file; long pastes in chat channels are often truncated.`)
	_, _ = fmt.Fprintln(out, "  All generation operations consume credits.")
	_, _ = fmt.Fprintln(out, "  Seedance 2.0 family is a flagship video generation model family and is a strong choice when output quality matters most.")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Built-in Commands:")
	for _, row := range rootBuiltInHelpRows {
		_, _ = fmt.Fprintf(out, "  %-20s %s\n", row.Name, row.Description)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Generator Commands:")
	for _, row := range rootGeneratorHelpRows {
		_, _ = fmt.Fprintf(out, "  %-20s %s\n", row.Name, row.Description)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Examples:")
	_, _ = fmt.Fprintln(out, "  dreamina login")
	_, _ = fmt.Fprintln(out, "  dreamina login --headless")
	_, _ = fmt.Fprintln(out, "  dreamina logout")
	_, _ = fmt.Fprintln(out, "  dreamina relogin")
	_, _ = fmt.Fprintln(out, "  dreamina user_credit")
	_, _ = fmt.Fprintln(out, "  dreamina list_task --gen_status=success")
	_, _ = fmt.Fprintln(out, "  dreamina query_result --submit_id=3f6eb41f425d23a3")
	_, _ = fmt.Fprintln(out, `  dreamina text2image --prompt="a cat portrait" --ratio=1:1 --resolution_type=2k`)
	return nil
}

// writeUnknownHelpTopic 对齐原程序在未知帮助主题下的输出：先提示 unknown topic，再回退到根帮助摘要。
func writeUnknownHelpTopic(out io.Writer, name string) error {
	if out == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	_, _ = fmt.Fprintf(out, "Unknown help topic [`%s`]\n", name)
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  dreamina [flags]")
	_, _ = fmt.Fprintln(out, "  dreamina [command]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Examples:")
	_, _ = fmt.Fprintln(out, "  dreamina login")
	_, _ = fmt.Fprintln(out, "  dreamina login --headless")
	_, _ = fmt.Fprintln(out, "  dreamina logout")
	_, _ = fmt.Fprintln(out, "  dreamina relogin")
	_, _ = fmt.Fprintln(out, "  dreamina user_credit")
	_, _ = fmt.Fprintln(out, "  dreamina list_task --gen_status=success")
	_, _ = fmt.Fprintln(out, "  dreamina query_result --submit_id=3f6eb41f425d23a3")
	_, _ = fmt.Fprintln(out, `  dreamina text2image --prompt="a cat portrait" --ratio=1:1 --resolution_type=2k`)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Built-in Commands")
	for _, row := range rootBuiltInHelpRows {
		_, _ = fmt.Fprintf(out, "  %-21s %s\n", row.Name, row.Description)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Generator Commands")
	for _, row := range rootGeneratorHelpRows {
		_, _ = fmt.Fprintf(out, "  %-21s %s\n", row.Name, row.Description)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Debug Commands")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Additional Commands:")
	_, _ = fmt.Fprintln(out, "  completion            Generate the autocompletion script for the specified shell")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Flags:")
	_, _ = fmt.Fprintln(out, "      --version   print build version information")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, `Use "dreamina [command] --help" for more information about a command.`)
	return nil
}

func defaultCommandHelpSpec(use string) commandHelpSpec {
	return commandHelpSpec{
		UseLine: fmt.Sprintf("dreamina %s [flags]", use),
		Paragraphs: []string{
			defaultCommandSummary(use),
		},
		Flags: []helpFlag{
			{Name: "-h, --help", Description: fmt.Sprintf("help for %s", use)},
		},
	}
}

func defaultCommandSummary(use string) string {
	for _, row := range rootBuiltInHelpRows {
		if row.Name == use {
			return row.Description
		}
	}
	for _, row := range rootGeneratorHelpRows {
		if row.Name == use {
			return row.Description
		}
	}
	if use == "validate-auth-token" {
		return "Validate the current auth token and print the normalized session payload."
	}
	if use == "ref2video" {
		return "Legacy alias of multiframe2video."
	}
	return fmt.Sprintf("Show help for %s.", use)
}
