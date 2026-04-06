package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	appctx "code.byted.org/videocut-aigc/dreamina_cli/app"
	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
	"code.byted.org/videocut-aigc/dreamina_cli/components/login"
	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

// 该文件收敛账号相关命令入口。

type originalUserCreditOutput struct {
	VIPCredit      int `json:"vip_credit"`
	GiftCredit     int `json:"gift_credit"`
	PurchaseCredit int `json:"purchase_credit"`
	TotalCredit    int `json:"total_credit"`
}

// newUserCreditCommand 创建查询账号额度的命令入口。
func newUserCreditCommand(app any) *Command {
	// user_credit 命令会在已登录前提下查询当前账号额度并输出 JSON。
	return &Command{
		Use: "user_credit",
		RunE: func(cmd *Command, args []string) error {
			if err := rejectUnexpectedCommandArgs("user_credit", args); err != nil {
				return err
			}
			ctx, err := appctx.NewContext()
			if err != nil {
				return err
			}
			if err := ctx.RequireLogin(); err != nil {
				return err
			}

			svc, ok := ctx.Login.(*login.Service)
			if !ok {
				return fmt.Errorf("login service is not configured")
			}
			session, err := svc.ParseAuthToken()
			if err != nil {
				return err
			}

			client, ok := ctx.Clients.Commerce.(*commerceclient.HTTPClient)
			if !ok {
				return fmt.Errorf("commerce client is not configured")
			}
			credit, err := client.GetUserCredit(context.Background(), session)
			if err != nil {
				return err
			}
			return printJSON(buildUserCreditOutput(credit), cmd.OutOrStdout())
		},
	}
}

// buildUserCreditOutput 把内部额度结构收紧成原程序 user_credit 命令公开输出的四个字段。
func buildUserCreditOutput(credit *commerceclient.UserCredit) *originalUserCreditOutput {
	if credit == nil {
		return &originalUserCreditOutput{}
	}
	return &originalUserCreditOutput{
		VIPCredit:      credit.VIPCredit,
		GiftCredit:     credit.GiftCredit,
		PurchaseCredit: credit.PurchaseCredit,
		TotalCredit:    credit.TotalCredit,
	}
}

// newLogoutCommand 创建清理本地登录态的命令入口。
func newLogoutCommand(app any) *Command {
	// logout 命令会清理本地登录态，并输出是否真的移除了可用凭证。
	return &Command{
		Use: "logout",
		RunE: func(cmd *Command, args []string) error {
			if err := rejectUnexpectedCommandArgs("logout", args); err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, err := os.Stat(cfg.CredentialPath); err != nil && !os.IsNotExist(err) {
				return err
			}

			svc, err := login.NewService()
			if err != nil {
				return err
			}
			hadUsableCredential := svc.RequireUsableCredential() == nil
			if err := svc.Logout(); err != nil {
				return err
			}
			if hadUsableCredential {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "已清除本地登录态。")
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "当前没有本地登录态。")
			return nil
		},
	}
}

// newValidateAuthCommand 创建校验当前 auth token 并输出整理后会话信息的命令入口。
func newValidateAuthCommand(app any) *Command {
	// validate-auth-token 按原程序行为只解析本地 auth_token，并输出整理后的会话信息。
	return &Command{
		Use: "validate-auth-token",
		RunE: func(cmd *Command, args []string) error {
			if err := rejectUnexpectedCommandArgs("validate-auth-token", args); err != nil {
				return err
			}
			ctx, err := appctx.NewContext()
			if err != nil {
				return err
			}
			svc, ok := ctx.Login.(*login.Service)
			if !ok {
				return fmt.Errorf("login service is not configured")
			}
			payload, err := svc.ParseAuthToken()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), login.FormatSessionPayload(payload))
			return err
		},
	}
}

func rejectUnexpectedCommandArgs(use string, args []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			name := arg
			if idx := strings.Index(name, "="); idx >= 0 {
				name = name[:idx]
			}
			return fmt.Errorf("unknown flag: %s", name)
		}
		return fmt.Errorf("unknown command %q for %q", arg, "dreamina "+strings.TrimSpace(use))
	}
	return nil
}
