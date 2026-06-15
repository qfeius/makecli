/**
 * [INPUT]: 依赖 cmd/client（newRepoClientFromProfile）、cmd/app（validResourceKey）、encoding/base64、fmt、os、os/exec、path/filepath、slices、strings、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newDeployCmd 函数；包级 gitOutputFunc / gitPushFunc 可打桩变量（测试替换，参照 update.go applyFunc 模式）
 * [POS]: cmd 模块 app 命令组的 deploy 子命令：调用代码仓库服务幂等准备 preview/production 双环境仓库（MakeService.CreateResource），
 *        按 --env 选取 cloneUrl 后 git push 当前 HEAD 到固定分支（deployBranch，webhook 约定）触发构建；token 经 GIT_CONFIG_* 环境变量注入，不进程序参数
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// deployEnvs 是合法的部署环境集合，与服务端双仓库约定一一对应
var deployEnvs = []string{"preview", "production"}

// deployBranch 是构建流水线 webhook 监听的固定远端分支。
// 部署只推送到此分支——分支名是服务端约定，不是用户可调旋钮。
const deployBranch = "dev"

// gitOutputFunc / gitPushFunc 为包级可打桩变量，单测替换以隔离真实 git 进程
var (
	gitOutputFunc = gitOutput
	gitPushFunc   = gitPush
)

func newDeployCmd() *cobra.Command {
	var env, appKey string
	var force bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy current git HEAD to a Make environment",
		Long: `Deploy pushes the current git HEAD to the app's code repository on Make.

The repository for each environment is prepared automatically (idempotent):
preview code goes to {appKey}-preview, production code to {appKey}-production.
A successful push triggers the build pipeline via webhook.`,
		Example: `  makecli app deploy --env preview
  makecli app deploy --env production
  makecli app deploy --env preview --app myapp`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(env, appKey, force)
		},
	}

	cmd.Flags().StringVar(&env, "env", "", "target environment: preview | production (required)")
	_ = cmd.MarkFlagRequired("env")
	cmd.Flags().StringVar(&appKey, "app", "", "app key (default: current git repository directory name)")
	cmd.Flags().BoolVar(&force, "force", false, "force push, overwriting the remote branch")
	return cmd
}

func runDeploy(env, appKey string, force bool) error {
	if !slices.Contains(deployEnvs, env) {
		return fmt.Errorf("invalid --env %q: must be one of %s", env, strings.Join(deployEnvs, " | "))
	}

	// appKey 缺省时取 git 仓库根目录名（用户环境推断），不合法则要求显式 --app
	if appKey == "" {
		top, err := gitOutputFunc("rev-parse", "--show-toplevel")
		if err != nil {
			return fmt.Errorf("当前目录不是 git 仓库，无法推断 app key，请用 --app 指定: %w", err)
		}
		appKey = filepath.Base(top)
	}
	if err := validResourceKey(appKey); err != nil {
		return fmt.Errorf("无法作为 app key，请用 --app 显式指定: %w", err)
	}

	head, err := gitOutputFunc("rev-parse", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("读取 HEAD 失败（仓库是否有提交？）: %w", err)
	}

	client, token, err := newRepoClientFromProfile()
	if err != nil {
		return err
	}

	// CreateResource 幂等：组织/仓库不存在则创建，存在则复用，成功即可推送
	repo, err := client.CreateRepository(appKey)
	if err != nil {
		return fmt.Errorf("准备代码仓库失败: %w", err)
	}

	cloneURL := repo.CloneURLFor(env)
	if cloneURL == "" {
		return fmt.Errorf("服务端未返回 %s 环境的仓库地址", env)
	}

	fmt.Printf("%-12s %s\n", "App:", appKey)
	fmt.Printf("%-12s %s\n", "Environment:", env)
	fmt.Printf("%-12s %s\n", "Repository:", cloneURL)
	fmt.Printf("Pushing %s -> %s ...\n", head, deployBranch)

	if err := gitPushFunc(cloneURL, token, force); err != nil {
		return fmt.Errorf("git push 失败: %w", err)
	}

	fmt.Printf("Deployed '%s' to %s\n", appKey, env)
	return nil
}

// gitOutput 在当前目录执行 git 子命令，返回 trim 后的 stdout
func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitPush 把当前 HEAD 推送到 cloneURL 的固定部署分支（deployBranch），git 输出直通终端。
// 认证经 GIT_CONFIG_* 环境变量注入（与 -c 等价，但 token 不出现在进程参数里，避免 ps 泄露）；
// credential.helper 置空，防止系统 keychain 介入或缓存凭证。
func gitPush(cloneURL, token string, force bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	// 用完整 refname：远端分支不存在时（如新建仓库）HEAD:<short> 无法推断目标
	args = append(args, cloneURL, "HEAD:refs/heads/"+deployBranch)

	basic := base64.StdEncoding.EncodeToString([]byte("make:" + token))
	gitCmd := exec.Command("git", args...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=http.extraHeader", "GIT_CONFIG_VALUE_1=Authorization: Basic "+basic,
	)
	return gitCmd.Run()
}
