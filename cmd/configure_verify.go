/**
 * [INPUT]: 依赖 internal/config（Load/LoadConfig）、internal/api（New/WithHeaders）、cmd/client（resolveEnvironment）、cmd/output（outputJSON/validateOutputFormat/writeJSON）、encoding/base64、encoding/json、fmt、os、strings、time
 * [OUTPUT]: 对外提供 newConfigureVerifyCmd 函数；包内 parseJWTTimeClaims 免验签提取 iat/exp
 * [POS]: cmd/configure 的 verify 子命令，本地 exp fail-closed 判定 + 在线验证 token 有效性并输出 profile 状态
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/qfeius/makecli/internal/api"
	"github.com/qfeius/makecli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigureVerifyCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:          "verify",
		Short:        "Verify that the current profile has a valid token",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := runConfigureVerify(output)
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
	Profile       string `json:"profile"`
	Valid         bool   `json:"valid"`
	Token         string `json:"token"`
	IssuedAt      string `json:"issued_at"`
	ExpiresAt     string `json:"expires_at"`
	MetaServerURL string `json:"meta_server_url"`
	TenantID      string `json:"tenant_id"`
	OperatorID    string `json:"operator_id"`
	Message       string `json:"message"`
}

// parseJWTTimeClaims 免验签提取 JWT payload 中的 iat/exp——过期判定不依赖签名真伪：
// exp 已过则无论签名是否有效 token 都不可用，这正是 fail-closed 的依据；
// 缺失的 claim 返回零值 time.Time，由调用方按「无法判定」处理
func parseJWTTimeClaims(token string) (issuedAt, expiresAt time.Time, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, time.Time{}, fmt.Errorf("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	var claims struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if claims.Iat > 0 {
		issuedAt = time.Unix(claims.Iat, 0)
	}
	if claims.Exp > 0 {
		expiresAt = time.Unix(claims.Exp, 0)
	}
	return issuedAt, expiresAt, nil
}

func runConfigureVerify(output string) (*verifyResult, error) {
	if err := validateOutputFormat(output); err != nil {
		return nil, err
	}

	result := verifyResult{Profile: Profile}

	// 加载凭证
	creds, err := config.Load()
	if err != nil {
		return nil, err
	}

	// 加载配置（meta-server-url / tenant / operator）
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cp, ok := cfg[Profile]; ok {
		result.MetaServerURL = cp.MetaServerURL
		result.TenantID = cp.XTenantID
		result.OperatorID = cp.OperatorID
	}

	// 检查 token 是否存在
	p, ok := creds[Profile]
	if !ok || p.AccessToken == "" {
		result.Message = "token not configured"
		outputVerifyResult(&result, output)
		return &result, nil
	}
	result.Token = mask(p.AccessToken)

	// JWT 格式校验
	if err := validateJWT(p.AccessToken); err != nil {
		result.Message = "token invalid (malformed JWT)"
		outputVerifyResult(&result, output)
		return &result, nil
	}

	// 本地 exp 判定（fail-closed）：已过期不触网直接判无效；
	// payload 不可解析或无 exp claim 时不下本地结论，交给在线验证
	if issuedAt, expiresAt, err := parseJWTTimeClaims(p.AccessToken); err == nil {
		if !issuedAt.IsZero() {
			result.IssuedAt = issuedAt.Format(time.RFC3339)
		}
		if !expiresAt.IsZero() {
			result.ExpiresAt = expiresAt.Format(time.RFC3339)
			if time.Now().After(expiresAt) {
				result.Message = fmt.Sprintf("token expired. `makecli login --profile %s` to renew token", Profile)
				outputVerifyResult(&result, output)
				return &result, nil
			}
		}
	}

	// 在线验证：调用 app list(page=1, size=1)；server 取值链 flag > profile config > 环境 preset
	env, err := resolveEnvironment()
	if err != nil {
		return nil, err
	}
	server := env.MetaServerURL
	headers := map[string]string{}
	if result.MetaServerURL != "" {
		server = result.MetaServerURL
	}
	if MetaServerURL != "" {
		server = MetaServerURL
	}
	if result.TenantID != "" {
		headers["X-Tenant-ID"] = result.TenantID
	}
	if result.OperatorID != "" {
		headers["X-Operator-ID"] = result.OperatorID
	}

	client := api.New(withGateway(server), p.AccessToken, api.WithDebug(DebugMode), api.WithHeaders(headers))
	_, _, err = client.ListApps(1, 1, "")
	if err != nil {
		result.Message = fmt.Sprintf("token invalid (%s)", err)
		outputVerifyResult(&result, output)
		return &result, nil
	}

	result.Valid = true
	result.Message = "ok"
	outputVerifyResult(&result, output)
	return &result, nil
}

func outputVerifyResult(r *verifyResult, output string) {
	switch {
	case output == outputJSON:
		_ = writeJSON(r)
	case r.Valid:
		fmt.Printf("Profile [%s]: ok\n", r.Profile)
	default:
		fmt.Printf("Profile [%s]: %s\n", r.Profile, r.Message)
		fmt.Fprintf(os.Stderr, "\nRun \"makecli configure --profile %s\" to set access token.\n", r.Profile)
	}
}
