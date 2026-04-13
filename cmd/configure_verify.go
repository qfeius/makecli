/**
 * [INPUT]: 依赖 internal/config（Load/LoadConfig）、internal/api（New/WithHeaders）、cmd/client（defaultMetaServer）、cmd/output（outputJSON/validateOutputFormat/writeJSON）、fmt、os
 * [OUTPUT]: 对外提供 newConfigureVerifyCmd 函数
 * [POS]: cmd/configure 的 verify 子命令，在线验证 token 有效性并输出 profile 状态
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/api"
	"github.com/qfeius/makecli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigureVerifyCmd(profile *string) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:          "verify",
		Short:        "Verify that the current profile has a valid token",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := runConfigureVerify(*profile, output)
			if err != nil {
				return err
			}
			if !r.Valid {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	return cmd
}

// verifyResult 承载验证结果，复用于 table 和 JSON 输出
type verifyResult struct {
	Profile    string `json:"profile"`
	Valid      bool   `json:"valid"`
	Token      string `json:"token"`
	ServerURL  string `json:"server_url"`
	TenantID   string `json:"tenant_id"`
	OperatorID string `json:"operator_id"`
	Message    string `json:"message"`
}

func runConfigureVerify(profile, output string) (*verifyResult, error) {
	if err := validateOutputFormat(output); err != nil {
		return nil, err
	}

	result := verifyResult{Profile: profile}

	// 加载凭证
	creds, err := config.Load()
	if err != nil {
		return nil, err
	}

	// 加载配置（server-url / tenant / operator）
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cp, ok := cfg[profile]; ok {
		result.ServerURL = cp.ServerURL
		result.TenantID = cp.XTenantID
		result.OperatorID = cp.OperatorID
	}

	// 检查 token 是否存在
	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		result.Message = "token not configured"
		outputVerifyResult(result, output)
		return &result, nil
	}
	result.Token = mask(p.AccessToken)

	// JWT 格式校验
	if err := validateJWT(p.AccessToken); err != nil {
		result.Message = "token invalid (malformed JWT)"
		outputVerifyResult(result, output)
		return &result, nil
	}

	// 在线验证：调用 app list(page=1, size=1)
	server := defaultMetaServer
	headers := map[string]string{}
	if result.ServerURL != "" {
		server = result.ServerURL
	}
	if ServerURL != "" {
		server = ServerURL
	}
	if result.TenantID != "" {
		headers["X-Tenant-ID"] = result.TenantID
	}
	if result.OperatorID != "" {
		headers["X-Operator-ID"] = result.OperatorID
	}

	client := api.New(server, p.AccessToken, api.WithHeaders(headers))
	_, _, err = client.ListApps(1, 1, nil)
	if err != nil {
		result.Message = fmt.Sprintf("token invalid (%s)", err)
		outputVerifyResult(result, output)
		return &result, nil
	}

	result.Valid = true
	result.Message = "ok"
	outputVerifyResult(result, output)
	return &result, nil
}

func outputVerifyResult(r verifyResult, output string) {
	if output == outputJSON {
		_ = writeJSON(r)
	} else if r.Valid {
		fmt.Printf("Profile [%s]: ok\n", r.Profile)
	} else {
		fmt.Printf("Profile [%s]: %s\n", r.Profile, r.Message)
		fmt.Fprintf(os.Stderr, "\nRun \"makecli configure --profile %s\" to set access token.\n", r.Profile)
	}
}
