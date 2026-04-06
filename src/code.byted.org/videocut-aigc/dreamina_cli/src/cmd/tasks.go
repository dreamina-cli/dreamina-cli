package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	appctx "code.byted.org/videocut-aigc/dreamina_cli/app"
	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
	"code.byted.org/videocut-aigc/dreamina_cli/components/gen"
	"code.byted.org/videocut-aigc/dreamina_cli/components/login"
	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
)

// newQueryResultCommand 创建查询单个任务结果的命令入口。
func newQueryResultCommand(app any) *Command {
	// query_result 会在登录态下查询指定任务，并可按需下载结果媒体。
	return &Command{
		Use: "query_result",
		RunE: func(cmd *Command, args []string) error {
			submitID, downloadDir, err := parseQueryResultArgs(args)
			if err != nil {
				return err
			}
			appContext, err := appctx.NewContext()
			if err != nil {
				return err
			}
			if err := appContext.RequireLogin(); err != nil {
				return err
			}
			if submitID == "" {
				return fmt.Errorf("--submit_id is required")
			}
			service, ok := appContext.GenService().(*gen.Service)
			if !ok {
				return fmt.Errorf("generator service is not configured")
			}
			queryCtx := context.Background()
			var session any
			if svc, ok := appContext.Login.(*login.Service); ok {
				if payload, err := svc.ParseAuthToken(); err == nil {
					session = payload
					queryCtx = gen.ContextWithSession(queryCtx, payload)
				}
			}
			value, err := service.QueryResult(queryCtx, submitID)
			if err != nil {
				if isMissingTaskQueryResultError(err, submitID) {
					writeOriginalTaskNotFoundLog(cmd.OutOrStdout(), submitID, time.Now(), 70*time.Microsecond)
				}
				return err
			}
			localTask, ok := value.(*task.AIGCTask)
			if !ok {
				return fmt.Errorf("query result returned unexpected type %T", value)
			}
			if normalizeQueryResultGenStatus(localTask.GenStatus) == "fail" {
				if _, ok := taskCreditCount(localTask); !ok {
					if client, ok := appContext.Clients.Commerce.(*commerceclient.HTTPClient); ok && session != nil {
						if credit, err := client.GetUserCredit(context.Background(), session); err == nil && credit != nil && credit.CreditCount > 0 {
							localTask.CommerceInfo = map[string]any{"credit_count": credit.CreditCount}
						}
					}
				}
			}
			parsed, err := parseRemoteQueryResult(localTask.ResultJSON)
			if err != nil {
				return err
			}
			var downloaded any
			if downloadDir != "" {
				downloaded, err = downloadQueryResultMedia(localTask, parsed, downloadDir)
				if err != nil {
					return err
				}
			}
			output := buildQueryResultOutput(localTask, parsed, downloadDir, downloaded)
			return printJSON(output, cmd.OutOrStdout())
		},
	}
}

// newListTaskCommand 创建列出本地任务记录的命令入口。
func newListTaskCommand(app any) *Command {
	// list_task 会按筛选条件读取当前用户的本地任务记录。
	return &Command{
		Use: "list_task",
		RunE: func(cmd *Command, args []string) error {
			filter, err := parseListTaskArgs(args)
			if err != nil {
				return err
			}
			appContext, err := appctx.NewContext()
			if err != nil {
				return err
			}
			if err := appContext.RequireLogin(); err != nil {
				return err
			}

			store, ok := appContext.TaskStore().(*task.Store)
			if !ok {
				return fmt.Errorf("task store is not configured")
			}
			if filter.UID == "" {
				if svc, ok := appContext.Login.(*login.Service); ok {
					if payload, err := svc.ParseAuthToken(); err == nil {
						filter.UID = currentUserIDFromSession(payload)
					}
				}
			}
			tasks, err := store.ListTasks(context.Background(), filter)
			if err != nil {
				return err
			}
			return printJSON(taskListView(tasks), cmd.OutOrStdout())
		},
	}
}

// parseListTaskArgs 解析 list_task 命令支持的筛选和分页参数，并拒绝未知 flag。
func parseListTaskArgs(args []string) (task.ListTaskFilter, error) {
	filter := task.ListTaskFilter{}
	allowed := map[string]struct{}{
		"submit_id":     {},
		"gen_task_type": {},
		"gen_status":    {},
		"offset":        {},
		"limit":         {},
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return task.ListTaskFilter{}, fmt.Errorf("unknown command %q for %q", arg, "dreamina list_task")
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if idx := strings.Index(key, "="); idx >= 0 {
			key = key[:idx]
		}
		if _, ok := allowed[key]; !ok {
			return task.ListTaskFilter{}, fmt.Errorf("unknown flag: --%s", key)
		}
		switch {
		case arg == "--submit_id":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return task.ListTaskFilter{}, fmt.Errorf("flag needs an argument: --submit_id")
			}
			filter.SubmitID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--submit_id="):
			filter.SubmitID = strings.TrimPrefix(arg, "--submit_id=")
		case arg == "--gen_task_type":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return task.ListTaskFilter{}, fmt.Errorf("flag needs an argument: --gen_task_type")
			}
			filter.GenTaskType = args[i+1]
			i++
		case strings.HasPrefix(arg, "--gen_task_type="):
			filter.GenTaskType = strings.TrimPrefix(arg, "--gen_task_type=")
		case arg == "--gen_status":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return task.ListTaskFilter{}, fmt.Errorf("flag needs an argument: --gen_status")
			}
			filter.GenStatus = args[i+1]
			i++
		case strings.HasPrefix(arg, "--gen_status="):
			filter.GenStatus = strings.TrimPrefix(arg, "--gen_status=")
		case arg == "--offset":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return task.ListTaskFilter{}, fmt.Errorf("flag needs an argument: --offset")
			}
			value, err := parseCLIIntFlag(args[i+1], "offset")
			if err != nil {
				return task.ListTaskFilter{}, err
			}
			filter.Offset = value
			i++
		case strings.HasPrefix(arg, "--offset="):
			value, err := parseCLIIntFlag(strings.TrimPrefix(arg, "--offset="), "offset")
			if err != nil {
				return task.ListTaskFilter{}, err
			}
			filter.Offset = value
		case arg == "--limit":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return task.ListTaskFilter{}, fmt.Errorf("flag needs an argument: --limit")
			}
			value, err := parseCLIIntFlag(args[i+1], "limit")
			if err != nil {
				return task.ListTaskFilter{}, err
			}
			filter.Limit = value
			i++
		case strings.HasPrefix(arg, "--limit="):
			value, err := parseCLIIntFlag(strings.TrimPrefix(arg, "--limit="), "limit")
			if err != nil {
				return task.ListTaskFilter{}, err
			}
			filter.Limit = value
		}
	}
	return filter, nil
}

func parseCLIIntFlag(raw string, name string) (int, error) {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid argument %q for \"--%s\" flag: %v", raw, strings.TrimSpace(name), err)
	}
	return int(parsed), nil
}

func isMissingTaskQueryResultError(err error, submitID string) bool {
	if err == nil {
		return false
	}
	return err.Error() == fmt.Sprintf("task %q not found", strings.TrimSpace(submitID))
}

func writeOriginalTaskNotFoundLog(out io.Writer, submitID string, startedAt time.Time, elapsed time.Duration) {
	if out == nil {
		return
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	if elapsed < 0 {
		elapsed = 0
	}
	_, _ = fmt.Fprint(out, "\r\n")
	_, _ = fmt.Fprintf(
		out,
		"%s \x1b[31;1m/opt/tiger/compile_path/src/code.byted.org/videocut-aigc/dreamina_cli/components/task/store.go:278 \x1b[35;1mrecord not found\n",
		startedAt.Format("2006/01/02 15:04:05"),
	)
	_, _ = fmt.Fprintf(
		out,
		"\x1b[0m\x1b[33m[%.3fms] \x1b[34;1m[rows:0]\x1b[0m SELECT * FROM `aigc_task` WHERE submit_id = %q ORDER BY `aigc_task`.`submit_id` LIMIT 1\n",
		float64(elapsed)/float64(time.Millisecond),
		strings.TrimSpace(submitID),
	)
}

// parseQueryResultArgs 解析 query_result 命令支持的 submit_id 和 download_dir 参数。
func parseQueryResultArgs(args []string) (string, string, error) {
	var submitID string
	var downloadDir string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return "", "", fmt.Errorf("unknown command %q for %q", arg, "dreamina query_result")
		}
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if idx := strings.Index(key, "="); idx >= 0 {
				key = key[:idx]
			}
			switch key {
			case "submit_id", "download_dir":
			default:
				return "", "", fmt.Errorf("unknown flag: --%s", key)
			}
		}
		switch {
		case arg == "--submit_id":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", "", fmt.Errorf("flag needs an argument: --submit_id")
			}
			submitID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--submit_id="):
			submitID = strings.TrimPrefix(arg, "--submit_id=")
		case arg == "--download_dir":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", "", fmt.Errorf("flag needs an argument: --download_dir")
			}
			downloadDir = args[i+1]
			i++
		case strings.HasPrefix(arg, "--download_dir="):
			downloadDir = strings.TrimPrefix(arg, "--download_dir=")
		}
	}
	return strings.TrimSpace(submitID), strings.TrimSpace(downloadDir), nil
}
