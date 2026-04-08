package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const shellCompDirectiveNoFileComp = 4

type completionEntry struct {
	Value       string
	Description string
}

func newCompletionCommand() *Command {
	return &Command{
		Use: "completion",
		RunE: func(cmd *Command, args []string) error {
			if len(args) == 0 {
				return writeCommandHelp(cmd.OutOrStdout(), cmd)
			}
			switch strings.TrimSpace(args[0]) {
			case "bash":
				_, _ = io.WriteString(cmd.OutOrStdout(), bashCompletionScript)
				return nil
			case "zsh":
				_, _ = io.WriteString(cmd.OutOrStdout(), zshCompletionScript)
				return nil
			case "fish":
				_, _ = io.WriteString(cmd.OutOrStdout(), fishCompletionScript)
				return nil
			case "powershell":
				_, _ = io.WriteString(cmd.OutOrStdout(), powershellCompletionScript)
				return nil
			default:
				return writeCommandHelp(cmd.OutOrStdout(), cmd)
			}
		},
	}
}

func newCompleteCommand() *Command {
	return &Command{
		Use: "__complete",
		RunE: func(cmd *Command, args []string) error {
			writeCompletionOutput(cmd.OutOrStdout(), args)
			_, _ = fmt.Fprintln(os.Stderr, "Completion ended with directive: ShellCompDirectiveNoFileComp")
			return nil
		},
	}
}

func writeCompletionOutput(out io.Writer, args []string) {
	if out == nil {
		return
	}
	items := completeArgs(args)
	for _, item := range items {
		if item.Description == "" {
			_, _ = fmt.Fprintln(out, item.Value)
			continue
		}
		_, _ = fmt.Fprintf(out, "%s\t%s\n", item.Value, item.Description)
	}
	_, _ = fmt.Fprintf(out, ":%d\n", shellCompDirectiveNoFileComp)
}

func completeArgs(args []string) []completionEntry {
	if len(args) == 0 {
		return rootCompletionEntries("")
	}
	current := strings.TrimSpace(args[len(args)-1])

	if len(args) == 1 {
		switch {
		case strings.HasPrefix(current, "-"):
			return rootFlagEntries(current)
		default:
			return rootCompletionEntries(current)
		}
	}

	commandName := strings.TrimSpace(args[0])
	rest := args[1:]
	current = strings.TrimSpace(rest[len(rest)-1])

	switch commandName {
	case "help":
		if len(rest) == 1 && !strings.HasPrefix(current, "-") {
			return rootCompletionEntries(current)
		}
		if len(rest) == 1 && strings.HasPrefix(current, "-") {
			return subcommandFlagEntries("help", current)
		}
		return nil
	case "completion":
		if len(rest) == 1 && !strings.HasPrefix(current, "-") {
			return filterCompletionEntries(completionShellEntries(), current)
		}
		if len(rest) == 1 && strings.HasPrefix(current, "-") {
			return subcommandFlagEntries("completion", current)
		}
		return nil
	default:
		if len(rest) == 1 && strings.HasPrefix(current, "-") {
			return subcommandFlagEntries(commandName, current)
		}
		if len(rest) == 1 && current == "" {
			return subcommandFlagEntries(commandName, "")
		}
		return nil
	}
}

func rootCompletionEntries(prefix string) []completionEntry {
	items := make([]completionEntry, 0, len(rootBuiltInHelpRows)+len(rootGeneratorHelpRows)+1)
	for _, row := range rootBuiltInHelpRows {
		items = append(items, completionEntry{Value: row.Name, Description: row.Description})
	}
	for _, row := range rootGeneratorHelpRows {
		items = append(items, completionEntry{Value: row.Name, Description: row.Description})
	}
	items = append(items, completionEntry{
		Value:       "completion",
		Description: "Generate the autocompletion script for the specified shell",
	})
	sort.Slice(items, func(i, j int) bool {
		return items[i].Value < items[j].Value
	})
	return filterCompletionEntries(items, prefix)
}

func rootFlagEntries(prefix string) []completionEntry {
	return filterCompletionEntries([]completionEntry{
		{Value: "--help", Description: "help for dreamina"},
		{Value: "-h", Description: "help for dreamina"},
		{Value: "--version", Description: "print build version information"},
	}, prefix)
}

func subcommandFlagEntries(use string, prefix string) []completionEntry {
	longFlags := []completionEntry{
		{Value: "--version", Description: "print build version information"},
	}
	shortFlags := []completionEntry{}
	for _, flag := range completionFlagsForCommand(use) {
		if strings.HasPrefix(flag.Value, "--") {
			longFlags = append(longFlags, flag)
			continue
		}
		shortFlags = append(shortFlags, flag)
	}
	items := append(longFlags, shortFlags...)
	return filterCompletionEntries(items, prefix)
}

func completionFlagsForCommand(use string) []completionEntry {
	switch strings.TrimSpace(use) {
	case "help":
		return []completionEntry{
			{Value: "--help", Description: "help for help"},
			{Value: "-h", Description: "help for help"},
		}
	case "import_login_response":
		return []completionEntry{
			{Value: "--file", Description: "read the copied login JSON from a file instead of stdin"},
			{Value: "--help", Description: "help for import_login_response"},
			{Value: "-h", Description: "help for import_login_response"},
		}
	case "query_result":
		return []completionEntry{
			{Value: "--download_dir", Description: "download result media into the target directory"},
			{Value: "--help", Description: "help for query_result"},
			{Value: "-h", Description: "help for query_result"},
			{Value: "--submit_id", Description: "task submit_id"},
		}
	case "list_task":
		return []completionEntry{
			{Value: "--gen_status", Description: "filter by gen_status"},
			{Value: "--gen_task_type", Description: "filter by gen_task_type"},
			{Value: "--help", Description: "help for list_task"},
			{Value: "-h", Description: "help for list_task"},
			{Value: "--limit", Description: "max number of tasks to return"},
			{Value: "--offset", Description: "offset for pagination"},
			{Value: "--submit_id", Description: "filter by submit_id"},
		}
	case "user_credit":
		return []completionEntry{
			{Value: "--help", Description: "help for user_credit"},
			{Value: "-h", Description: "help for user_credit"},
		}
	case "validate-auth-token":
		return []completionEntry{
			{Value: "--help", Description: "help for validate-auth-token"},
			{Value: "-h", Description: "help for validate-auth-token"},
		}
	case "version":
		return []completionEntry{
			{Value: "--help", Description: "help for version"},
			{Value: "-h", Description: "help for version"},
		}
	case "completion":
		return []completionEntry{
			{Value: "--help", Description: "help for completion"},
			{Value: "-h", Description: "help for completion"},
		}
	case "text2image":
		return []completionEntry{
			{Value: "--prompt", Description: "generation prompt"},
			{Value: "--ratio", Description: "supported values: 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16"},
			{Value: "--resolution_type", Description: "supported values by model: 3.0/3.1 -> 1k or 2k; 4.0/4.1/4.5/4.6/5.0/lab -> 2k or 4k; omit to use the model default"},
			{Value: "--model_version", Description: "supported values: 3.0, 3.1, 4.0, 4.1, 4.5, 4.6, 5.0, lab; lab requires VIP"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for text2image"},
			{Value: "-h", Description: "help for text2image"},
		}
	case "text2video":
		return []completionEntry{
			{Value: "--prompt", Description: "generation prompt"},
			{Value: "--duration", Description: "video duration in seconds; supported range: 4-15"},
			{Value: "--ratio", Description: "supported values: 1:1, 3:4, 16:9, 4:3, 9:16, 21:9"},
			{Value: "--video_resolution", Description: "supported values: 720p"},
			{Value: "--model_version", Description: "supported values: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip; default: seedance2.0fast"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for text2video"},
			{Value: "-h", Description: "help for text2video"},
		}
	case "image2image":
		return []completionEntry{
			{Value: "--images", Description: "local input image paths"},
			{Value: "--prompt", Description: "edit prompt"},
			{Value: "--ratio", Description: "supported values: 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16"},
			{Value: "--resolution_type", Description: "supported values: 2k, 4k; omit to use the model default"},
			{Value: "--model_version", Description: "supported values: 4.0, 4.1, 4.5, 4.6, 5.0, lab; lab requires VIP"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for image2image"},
			{Value: "-h", Description: "help for image2image"},
		}
	case "image_upscale":
		return []completionEntry{
			{Value: "--image", Description: "local input image path"},
			{Value: "--resolution_type", Description: "supported values: 2k, 4k, 8k; 4k and 8k require VIP"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for image_upscale"},
			{Value: "-h", Description: "help for image_upscale"},
		}
	case "image2video":
		return []completionEntry{
			{Value: "--image", Description: "local first-frame image path"},
			{Value: "--prompt", Description: "generation prompt"},
			{Value: "--duration", Description: "advanced controls only; supported duration ranges by model: 3.0/3.0fast/3.0pro -> 3-10, 3.5pro -> 4-12, seedance2.0 family -> 4-15"},
			{Value: "--video_resolution", Description: "advanced controls only; supported values by model: 3.0/3.0fast/3.5pro -> 720p or 1080p, 3.0pro -> 1080p, seedance2.0 family -> 720p"},
			{Value: "--model_version", Description: "advanced controls only; supported values: 3.0, 3.0fast, 3.0pro, 3.0_fast, 3.0_pro, 3.5pro, 3.5_pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for image2video"},
			{Value: "-h", Description: "help for image2video"},
		}
	case "frames2video":
		return []completionEntry{
			{Value: "--first", Description: "local first-frame image path"},
			{Value: "--last", Description: "local last-frame image path"},
			{Value: "--prompt", Description: "generation prompt"},
			{Value: "--duration", Description: "video duration in seconds; supported ranges: 3.0 -> 3-10, 3.5pro -> 4-12, seedance2.0 family -> 4-15"},
			{Value: "--video_resolution", Description: "supported values by model: 3.0/3.5pro -> 720p or 1080p; seedance2.0 family -> 720p"},
			{Value: "--model_version", Description: "supported values: 3.0, 3.5pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip; default: seedance2.0fast"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for frames2video"},
			{Value: "-h", Description: "help for frames2video"},
		}
	case "multiframe2video":
		return []completionEntry{
			{Value: "--images", Description: "local reference image paths"},
			{Value: "--prompt", Description: "shorthand prompt for exactly 2 images"},
			{Value: "--duration", Description: "shorthand transition duration in seconds for exactly 2 images; backend clamps each segment to [0.5, 8] and requires total duration >= 2"},
			{Value: "--transition-prompt", Description: "repeat once per transition segment; for N images provide N-1 prompts"},
			{Value: "--transition-duration", Description: "repeat once per transition segment in seconds; for N images provide N-1 durations, or omit to default each segment to 3"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for multiframe2video"},
			{Value: "-h", Description: "help for multiframe2video"},
		}
	case "ref2video":
		return []completionEntry{
			{Value: "--images", Description: "local reference image paths"},
			{Value: "--prompt", Description: "shorthand prompt for exactly 2 images"},
			{Value: "--duration", Description: "shorthand transition duration in seconds for exactly 2 images; backend clamps each segment to [0.5, 8] and requires total duration >= 2"},
			{Value: "--transition-prompt", Description: "repeat once per transition segment; for N images provide N-1 prompts"},
			{Value: "--transition-duration", Description: "repeat once per transition segment in seconds; for N images provide N-1 durations, or omit to default each segment to 3"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for ref2video"},
			{Value: "-h", Description: "help for ref2video"},
		}
	case "multimodal2video":
		return []completionEntry{
			{Value: "--image", Description: "repeat for each local input image path"},
			{Value: "--video", Description: "repeat for each local input video path"},
			{Value: "--audio", Description: "repeat for each local input audio path"},
			{Value: "--prompt", Description: "optional multimodal edit prompt"},
			{Value: "--duration", Description: "video duration in seconds; supported range: 4-15"},
			{Value: "--ratio", Description: "supported values: 1:1, 3:4, 16:9, 4:3, 9:16, 21:9"},
			{Value: "--video_resolution", Description: "supported values: 720p"},
			{Value: "--model_version", Description: "supported values: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip"},
			{Value: "--poll", Description: "submit then poll query_result for up to N seconds at 1s intervals (0 disables polling)"},
			{Value: "--help", Description: "help for multimodal2video"},
			{Value: "-h", Description: "help for multimodal2video"},
		}
	default:
		return nil
	}
}

func completionShellEntries() []completionEntry {
	return []completionEntry{
		{Value: "bash", Description: "Generate the autocompletion script for bash"},
		{Value: "fish", Description: "Generate the autocompletion script for fish"},
		{Value: "powershell", Description: "Generate the autocompletion script for powershell"},
		{Value: "zsh", Description: "Generate the autocompletion script for zsh"},
	}
}

func filterCompletionEntries(items []completionEntry, prefix string) []completionEntry {
	if prefix == "" {
		return items
	}
	filtered := make([]completionEntry, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.Value, prefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

const bashCompletionScript = `# bash completion for dreamina                             -*- shell-script -*-

__dreamina_complete()
{
    local cur words cword line out directive
    COMPREPLY=()
    _get_comp_words_by_ref -n =: cur words cword || return
    line=("${words[@]:1}")
    if [[ -z ${cur} ]]; then
        line+=("")
    fi
    out=$("${words[0]}" __complete "${line[@]}" 2>/dev/null) || return
    directive=${out##*$'\n':}
    out=${out%$'\n':*}
    while IFS=$'\n' read -r entry; do
        [[ -z ${entry} ]] && continue
        COMPREPLY+=("${entry%%$'\t'*}")
    done <<<"${out}"
}

complete -o default -F __dreamina_complete dreamina
`

const zshCompletionScript = `#compdef dreamina
compdef _dreamina dreamina

_dreamina() {
  local -a completions
  local out line
  out=$(dreamina __complete ${words[2,-1]} 2>/dev/null)
  while IFS=$'\n' read -r line; do
    [[ -z "$line" || "$line" == :* ]] && continue
    completions+=("${line%%$'\t'*}")
  done <<< "$out"
  _describe 'values' completions
}

if [ "$funcstack[1]" = "_dreamina" ]; then
  _dreamina
fi
`

const fishCompletionScript = `function __dreamina_complete
    set -l args (commandline -opc)
    set -l last (commandline -ct)
    if test -n "$last"
        dreamina __complete $args[2..-1] $last 2>/dev/null | string match -v ':*' | string replace -r '\t.*$' ''
    else
        dreamina __complete $args[2..-1] '' 2>/dev/null | string match -v ':*' | string replace -r '\t.*$' ''
    end
end

complete -c dreamina -f -a '(__dreamina_complete)'
`

const powershellCompletionScript = `Register-ArgumentCompleter -CommandName 'dreamina' -ScriptBlock {
    param($WordToComplete, $CommandAst, $CursorPosition)

    $command = "$CommandAst"
    if ($command.Length -gt $CursorPosition) {
        $command = $command.Substring(0, $CursorPosition)
    }
    $program, $arguments = $command.Split(" ", 2)
    $request = "$program __complete $arguments"
    if ([string]::IsNullOrEmpty($WordToComplete)) {
        $request = "$request ''"
    }
    Invoke-Expression $request 2>$null | Where-Object { $_ -and $_ -notmatch '^:' } | ForEach-Object {
        $name, $description = $_ -split [char]9, 2
        [System.Management.Automation.CompletionResult]::new($name, $name, 'ParameterValue', $description)
    }
}
`
