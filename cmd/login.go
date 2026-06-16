/**
 * [INPUT]: 依赖 internal/oauth（Discover/RegisterClient/StartCallbackServer/PKCE/BuildAuthorizationURL/ExchangeAuthorizationCode/OpenBrowser）、internal/config（Load/Save/LoadConfig/CredentialsPath）、context、fmt、net/http、os、strings、time、github.com/spf13/cobra；从 root.go 读取全局 Profile / Environment 与 client.go 的 firstNonEmpty / resolveEnvironment
 * [OUTPUT]: 对外提供 newLoginCmd 函数
 * [POS]: cmd 模块的 login 顶级命令，编排浏览器 OAuth 登陆，把 access_token 写入 ~/.make/credentials[Profile]
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/oauth"
	"github.com/spf13/cobra"
)

// ---------------------------------- make preset ----------------------------------

const (
	authBusinessType = "make"
	authResource     = "" // 留空：授权/换 token 不带 resource 参数
	authClientName   = "makecli"
)

var authScopes = []string{"make:resources"}

// openBrowserFunc 为包级可打桩变量，单测替换以免真浏览器（参照 deploy.go gitPushFunc 模式）。
var openBrowserFunc = oauth.OpenBrowser

// authMetadataURL 把身份服务器基址拼成 .well-known 元数据地址（基址已由调用方按 flag>profile>env 解析）。
func authMetadataURL(authBase string) string {
	return strings.TrimRight(authBase, "/") + "/.well-known/oauth-authorization-server/" + authBusinessType
}

// ---------------------------------- 命令 ----------------------------------

func newLoginCmd() *cobra.Command {
	var timeout time.Duration
	var noOpenBrowser bool

	cmd := &cobra.Command{
		Use:          "login",
		Short:        "Log in via browser and save the access token to ~/.make/credentials",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLogin(timeout, noOpenBrowser)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Minute, "authorization timeout")
	cmd.Flags().BoolVar(&noOpenBrowser, "no-open-browser", false, "print the authorization URL instead of opening a browser")
	return cmd
}

// runLogin 执行完整登陆流程：discover → 起回调 server → 注册 client → PKCE →
// 浏览器 → 等回调 → 换 token → 写 credentials。
func runLogin(timeout time.Duration, noOpenBrowser bool) error {
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	env, err := resolveEnvironment()
	if err != nil {
		return err
	}
	// 身份服务器基址：profile 的 auth-server-url 覆盖当前环境 preset
	authBase := firstNonEmpty(cfg[Profile].AuthServerURL, env.AuthServerURL)

	meta, err := oauth.Discover(ctx, httpClient, authMetadataURL(authBase))
	if err != nil {
		return err
	}
	if meta.RegistrationEndpoint == "" {
		return fmt.Errorf("authorization server does not advertise registration_endpoint; a fixed client_id is required")
	}

	// 先起回调 server 拿动态端口，redirectURL 要带进注册与授权 URL。
	callback, redirectURL, err := oauth.StartCallbackServer()
	if err != nil {
		return err
	}
	defer callback.Close()

	registration, err := oauth.RegisterClient(ctx, httpClient, meta.RegistrationEndpoint, oauth.ClientRegistrationRequest{
		ClientName:    authClientName,
		RedirectURIs:  []string{redirectURL},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	if err != nil {
		return err
	}

	verifier, err := oauth.NewCodeVerifier(nil)
	if err != nil {
		return err
	}
	state, err := oauth.NewState(nil)
	if err != nil {
		return err
	}

	authURL, err := oauth.BuildAuthorizationURL(oauth.AuthorizationRequest{
		AuthorizationEndpoint: meta.AuthorizationEndpoint,
		BusinessType:          authBusinessType,
		ClientID:              registration.ClientID,
		RedirectURL:           redirectURL,
		Resource:              authResource,
		Scopes:                authScopes,
		State:                 state,
		CodeChallenge:         oauth.S256Challenge(verifier),
	})
	if err != nil {
		return err
	}

	if noOpenBrowser {
		fmt.Printf("Open this URL and finish authorization:\n%s\n", authURL)
	} else if err := openBrowserFunc(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to open browser: %v\n", err)
		fmt.Printf("Open this URL manually:\n%s\n", authURL)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	code, err := callback.Wait(waitCtx, state)
	if err != nil {
		return err
	}

	token, err := oauth.ExchangeAuthorizationCode(ctx, httpClient, oauth.TokenExchangeRequest{
		TokenEndpoint: meta.TokenEndpoint,
		ClientID:      registration.ClientID,
		Code:          code,
		CodeVerifier:  verifier,
		RedirectURL:   redirectURL,
		Resource:      authResource,
	})
	if err != nil {
		return err
	}

	creds, err := config.Load()
	if err != nil {
		return err
	}
	p := creds[Profile]
	p.AccessToken = token.AccessToken
	creds[Profile] = p
	if err := config.Save(creds); err != nil {
		return err
	}

	path, _ := config.CredentialsPath()
	fmt.Printf("\nLogin succeeded for profile [%s].\n", Profile)
	fmt.Printf("Access token saved to %s\n", path)
	if !token.Expiry.IsZero() {
		fmt.Printf("Access token expires at: %s\n", token.Expiry.Format(time.RFC3339))
	}
	return nil
}
