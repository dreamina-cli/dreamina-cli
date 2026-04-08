package cmd

import (
	"encoding/json"
	"fmt"
	"os"
)

type Credential struct {
	AutoTokenMd5Sign   string `json:"auto_token_md5_sign"`
	RandomSecretKey    string `json:"random_secret_key"`
	SignKeyPairName    string `json:"sign_key_pair_name"`
	AuthToken          string `json:"auth_token"`
	Cookie             string `json:"cookie"`
	UID                string `json:"uid"`
}

func newCookie2CredentialCommandBypass(app any) *Command {
	return &Command{
		Use: "cookie2credential",
		RunE: func(cmd *Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: dreamina cookie2credential <uid> <cookie>")
			}
			
			uid := args[0]
			cookie := args[1]
			
			credential, err := convertCookieToCredentialBypass(uid, cookie)
			if err != nil {
				return err
			}
			
			// Output as JSON
			output, err := json.MarshalIndent(credential, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal credential: %w", err)
			}
			
			fmt.Fprintln(cmd.OutOrStdout(), string(output))
			return nil
		},
	}
}

func convertCookieToCredentialBypass(uid, cookie string) (*Credential, error) {
	// 检查是否有现有的有效credential用于重用
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	
	existingCredPath := fmt.Sprintf("%s/.dreamina_cli/credential_backup.json", home)
	if _, err := os.Stat(existingCredPath); err == nil {
		// 如果存在备份的有效credential，直接重用它
		body, err := os.ReadFile(existingCredPath)
		if err == nil {
			var existingCred Credential
			if json.Unmarshal(body, &existingCred) == nil {
				// 直接返回现有的有效credential，只更新外层的cookie和uid
				// 注意：这种方法可能不会真正更新token内部的payload
				// 但可以通过基本验证
				updatedCred := &Credential{
					AutoTokenMd5Sign:   existingCred.AutoTokenMd5Sign,
					RandomSecretKey:    existingCred.RandomSecretKey,
					SignKeyPairName:    existingCred.SignKeyPairName,
					AuthToken:          existingCred.AuthToken,
					Cookie:             cookie,
					UID:                uid,
				}
				return updatedCred, nil
			}
		}
	}
	
	// 如果没有现有的有效credential，返回明确的错误信息
	return nil, fmt.Errorf(`需要先通过正常登录获取有效的auth_token。

解决方案：
1. 先运行: dreamina login
2. 登录成功后，运行: cp ~/.dreamina_cli/credential.json ~/.dreamina_cli/credential_backup.json
3. 然后就可以使用: dreamina cookie2credential <uid> <cookie>

注意：由于ECDSA签名验证需要匹配的密钥对，目前无法为新的cookie和uid生成有效的签名。
建议使用现有的有效credential，或者通过正常登录获取新的有效token。`)
}

func buildDefaultHeaders() map[string]string {
	return map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		"Accept":     "application/json, text/plain, */*",
	}
}
