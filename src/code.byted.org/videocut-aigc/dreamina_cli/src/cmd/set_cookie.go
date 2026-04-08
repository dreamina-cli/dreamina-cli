package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CookieData struct {
	Cookie  string            `json:"cookie"`
	Headers map[string]string `json:"headers"`
	UID     any               `json:"uid"`
}

func newSetCookieCommand(app any) *Command {
	return &Command{
		Use: "set_cookie",
		RunE: func(cmd *Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: dreamina set_cookie <uid> <cookie>")
			}

			uid := args[0]
			cookie := args[1]

			// 保存cookie到 ~/.dreamina_cli/cookie.json
			err := saveCookieToFile(uid, cookie)
			if err != nil {
				return fmt.Errorf("failed to save cookie: %w", err)
			}

			fmt.Printf("Cookie saved successfully to ~/.dreamina_cli/cookie.json\n")
			fmt.Printf("UID: %s\n", uid)
			fmt.Printf("Cookie length: %d\n", len(cookie))
			return nil
		},
	}
}

func saveCookieToFile(uid, cookie string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	// 确保 .dreamina_cli 目录存在
	dreaminaDir := filepath.Join(home, ".dreamina_cli")
	if err := os.MkdirAll(dreaminaDir, 0755); err != nil {
		return fmt.Errorf("create dreamina dir: %w", err)
	}

	// 获取参考headers
	headers := buildHeadersFromReference()

	// 过滤cookie：移除不需要的cookies
	filteredCookie := filterCookies(cookie)

	// 创建cookie数据，参考auth_token_session_payload.json格式
	cookieData := CookieData{
		Cookie:  filteredCookie,
		Headers: headers,
		UID:     uid,
	}

	// 保存到cookie.json
	cookiePath := filepath.Join(dreaminaDir, "cookie.json")
	data, err := json.MarshalIndent(cookieData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cookie data: %w", err)
	}

	if err := os.WriteFile(cookiePath, data, 0644); err != nil {
		return fmt.Errorf("write cookie file: %w", err)
	}

	return nil
}

func filterCookies(cookie string) string {
	// 移除不需要的cookies
	unwantedCookies := []string{
		"passport_csrf_token_wap_state",
		"passport_mfa_token",
	}

	// 解析cookie
	cookieMap := parseCookies(cookie)

	// 移除不需要的cookies
	for _, unwanted := range unwantedCookies {
		delete(cookieMap, unwanted)
	}

	// 重新组合cookie
	return cookiesToString(cookieMap)
}

func parseCookies(cookie string) map[string]string {
	cookies := make(map[string]string)
	parts := strings.Split(cookie, ";")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			cookies[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	return cookies
}

func cookiesToString(cookies map[string]string) string {
	var parts []string
	for k, v := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, "; ")
}

func buildHeadersFromReference() map[string]string {
	// 返回完整的headers，参考auth_token_session_payload.json格式
	return map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"Accept-Language":    "zh-CN,zh;q=0.9",
		"Appvr":              "8.4.0",
		"Device-Time":        "1775553769",
		"Lan":                "zh-Hans",
		"Pf":                 "7",
		"Priority":           "u=1, i",
		"Referer":            "https://jimeng.jianying.com/ai-tool/login?callback=http%3A%2F%2F127.0.0.1%3A60713%2Fdreamina%2Fcallback%2Fsave_session&from=cli&random_secret_key=5eed0d046793f6970c085cc01cd23cfc",
		"Sec-Ch-Ua":          `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": `"macOS"`,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-origin",
		"Sign":               "4f5904c56a2ab7ffeab1c4bfd50df0d2",
		"Sign-Ver":           "1",
		"User-Agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	}
}
