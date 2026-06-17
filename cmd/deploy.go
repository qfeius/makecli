/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile/newRepoClientFromProfile）、cmd/app（loadAppManifestFromFile/validResourceKey）、cmd/app_create（appDSLPath）、cmd/git（openRepo/assertDeployable）、internal/api（ErrNotFound 哨兵）、errors、fmt、os、slices、strings、charm.land/huh/v2（production 确认表单）、github.com/mattn/go-isatty（TTY 检测）、github.com/go-git/go-git/v5（及 config/plumbing/transport/http 子包）、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newDeployCmd 函数；包内 assertAppRegistered（push 前 Meta 注册门控）、confirmProductionDeploy（production 部署确认）；包级 gitPushFunc / confirmDeployFunc 可打桩变量（测试替换推送 / 终端交互，参照 update.go applyFunc、app_delete.go confirmDeleteFunc 模式）；envPreview/envProduction 环境常量
 * [POS]: cmd 模块 app 命令组的 deploy 子命令——「纯 push 已提交状态」。--env 默认 envPreview（安全），production 须显式 opt-in 且 push 前过 continue/abort 确认（--yes/-y 跳过，非交互终端拒绝并指引 --yes）。从 apps/dsl/app.yaml 读 app key，
 *        本地先行门控（openRepo 要求已 init、assertDeployable 要求有 commit 且工作树干净，脏/无仓库/无提交即报错，
 *        全在网络调用之前 fail-fast），再经 assertAppRegistered 用 Meta GetApp 把关 app 已注册（不存在即指引 app create -f，
 *        避免「有仓库、无 app」孤儿状态；在建仓库/推送之前短路），production 确认通过后再幂等准备 preview/production 仓库（MakeService.CreateResource）取 cloneUrl，
 *        用 go-git（纯 Go，不 shell-out）把当前 HEAD 推到固定分支（deployBranch，webhook 约定）；token 走 HTTP BasicAuth(make:<token>)。
 *        提交时机交还用户——deploy 不再自动 add/commit（建仓+ignore 由 `makecli app init` 负责）。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

// 部署目标环境——preview 是安全默认，production 是不可逆线上部署。
const (
	envPreview    = "preview"
	envProduction = "production"
)

// deployEnvs 是合法的部署环境集合，与服务端双仓库约定一一对应
var deployEnvs = []string{envPreview, envProduction}

// confirmDeployFunc 为包级可打桩变量，单测替换以隔离真实终端交互（参照 app_delete.go confirmDeleteFunc 模式）
var confirmDeployFunc = confirmProductionDeploy

// deployBranch 是构建流水线 webhook 监听的固定远端分支。
// 部署只推送到此分支——分支名是服务端约定，不是用户可调旋钮。
const deployBranch = "dev"

// anonymousRemote 是 go-git 临时 remote 的固定名（CreateRemoteAnonymous 约定值），
// 仅存在于内存、不写进 .git/config，用完即弃——cloneUrl 每次部署才解析，不该污染用户仓库配置。
const anonymousRemote = "anonymous"

// gitPushFunc 为包级可打桩变量，单测替换以隔离真实网络推送（本地仓库门控不打桩，跑真 go-git）
var gitPushFunc = pushCurrentHead

func newDeployCmd() *cobra.Command {
	var env string
	var force bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an app to Make Platform",
		Example: `  makecli app deploy                       # 默认部署到 preview
  makecli app deploy --env production      # 部署到 production（需确认）
  makecli app deploy --env production -y   # 跳过确认（CI / 非交互）`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(env, force, yes)
		},
	}

	cmd.Flags().StringVar(&env, "env", envPreview, "target environment: preview | production")
	cmd.Flags().BoolVar(&force, "force", false, "force push")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the production deploy confirmation prompt")
	return cmd
}

// runDeploy 编排「纯 push 部署」：env 校验 → 读 app key → 本地 git 门控（fail-fast）→ 注册门控 → production 确认 → 备仓库 → 推 HEAD。
// 本地门控刻意在网络调用之前：脏工作树 / 无仓库 / 无提交都不该先白跑一趟仓库准备，
// 提示用户先 commit 即可，零网络往返。skipConfirm（--yes）仅对 production 确认生效。
func runDeploy(env string, force, skipConfirm bool) error {
	if !slices.Contains(deployEnvs, env) {
		return fmt.Errorf("invalid --env %q: must be one of %s", env, strings.Join(deployEnvs, " | "))
	}

	appKey, err := appKeyFromDSL()
	if err != nil {
		return err
	}

	// 本地先行门控：仓库须已 init、有提交、工作树干净——否则报错让用户先 commit（网络之前）
	repo, err := openRepo()
	if err != nil {
		return err
	}
	if err := assertDeployable(repo); err != nil {
		return err
	}

	// app 身份真相在 Meta Server——push 之前确认已注册，否则只 init 过的本地工程也能
	// 推成功，留下「有仓库、无 app」的孤儿状态（app list 看不到）。网络门控，但刻意在
	// 建仓库/推送之前：既不为不存在的 app 建孤儿仓库，也不白推一趟。
	if err := assertAppRegistered(appKey); err != nil {
		return err
	}

	fmt.Printf("%-12s %s\n", "App:", appKey)
	fmt.Printf("%-12s %s\n", "Environment:", env)

	// production 是不可逆的线上部署——push 前要求显式 continue/abort 确认，--yes 跳过。
	// 确认刻意在建仓库之前：abort 时连幂等的仓库准备都不白跑。preview 是安全默认，不拦。
	if env == envProduction && !skipConfirm {
		if err := confirmDeployFunc(appKey); err != nil {
			return err
		}
	}

	client, token, err := newRepoClientFromProfile()
	if err != nil {
		return err
	}

	// CreateResource 幂等：组织/仓库不存在则创建，存在则复用，成功即可推送
	repoInfo, err := client.CreateRepository(appKey)
	if err != nil {
		return fmt.Errorf("准备代码仓库失败: %w", err)
	}

	// cloneURL 含内部组织 id 与仓库主机，是部署实现细节——只用于 push，不向用户展示
	cloneURL := repoInfo.CloneURLFor(env)
	if cloneURL == "" {
		return fmt.Errorf("服务端未返回 %s 环境的仓库地址", env)
	}

	if err := gitPushFunc(repo, cloneURL, token, force); err != nil {
		return err
	}

	fmt.Printf("Deployed '%s' to %s\n", appKey, env)
	return nil
}

// appKeyFromDSL 从工程内 apps/dsl/app.yaml 读取 app key。
// app.yaml 是 app 身份的单一真相源（create 写出、apply/diff 读回），
// deploy 据此定位部署目标——目录可随意改名而部署仓库稳定，无需 --app 旋钮。
// 文件缺失给可操作错误：要么不在 app 工程根目录，要么尚未 makecli app create。
func appKeyFromDSL() (string, error) {
	if _, err := os.Stat(appDSLPath); err != nil {
		return "", fmt.Errorf("%s not found: run deploy from the app project root (or create it with `makecli app create`)", appDSLPath)
	}
	manifest, err := loadAppManifestFromFile(appDSLPath)
	if err != nil {
		return "", err
	}
	if err := validResourceKey(manifest.Key); err != nil {
		return "", fmt.Errorf("invalid app key in %s: %w", appDSLPath, err)
	}
	return manifest.Key, nil
}

// assertAppRegistered 确认 appKey 已在 Meta Server 注册为 App。
// deploy 推的是代码仓库，但 app 身份的真相在 Meta Server——跳过此关，
// 一个只 `app init` 过、从未 `app create` 的 key 也能 push 成功，留下「有仓库、无 app」
// 的孤儿状态。故 push 之前先 GetApp 把关：不存在给可操作错误指引按 app.yaml 注册远端
// （-f 形式取 app.yaml 里的精确 key，不像裸 key 会误建子目录），其余错误（网络/服务端）
// 原样上抛、绝不放行。
func assertAppRegistered(appKey string) error {
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}
	if _, err := client.GetApp(appKey); err != nil {
		if errors.Is(err, api.ErrNotFound) {
			return fmt.Errorf("app '%s' 尚未在 Make 平台注册，请先创建: makecli app create -f %s", appKey, appDSLPath)
		}
		return fmt.Errorf("校验 app 是否存在失败: %w", err)
	}
	return nil
}

// confirmProductionDeploy 在 production 部署前要求 continue/abort 确认（与 app delete 同款 huh 护栏）。
// 非交互终端（CI / 管道）无法应答，直接拒绝并指引 --yes，杜绝挂起。
// confirmed 初值 false → 表单默认停在 Abort，用户须显式选 Continue 才放行；
// ErrUserAborted（Ctrl-C）与选 Abort 都视为取消。
func confirmProductionDeploy(appKey string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("refusing to deploy %q to production without confirmation: re-run with --yes in a non-interactive shell", appKey)
	}

	confirmed := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Deploy %q to PRODUCTION?", appKey)).
				Description("This pushes to the live production environment.").
				Affirmative("Continue").
				Negative("Abort").
				Value(&confirmed),
		),
	).Run()

	if errors.Is(err, huh.ErrUserAborted) || (err == nil && !confirmed) {
		return fmt.Errorf("production deploy of %q cancelled", appKey)
	}
	return err
}

// pushCurrentHead 把仓库当前 HEAD 推送到部署分支。
// 调用前 assertDeployable 已确认 HEAD 存在且工作树干净，故此处 Head() 失败属防御性错误。
func pushCurrentHead(repo *git.Repository, cloneURL, token string, force bool) error {
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("仓库无可推送的提交: %w", err)
	}
	fmt.Printf("Pushing %s -> %s ...\n", head.Hash().String()[:7], deployBranch)
	return pushHead(repo, head, cloneURL, token, force)
}

// pushHead 把 head 指向的提交推送到临时 remote 的固定部署分支。
// 用匿名 remote 承载 cloneUrl（不落 .git/config）；token 走 HTTP BasicAuth(make:<token>)；
// up-to-date（远端已是该提交）视为成功，不当错误。
func pushHead(repo *git.Repository, head *plumbing.Reference, cloneURL, token string, force bool) error {
	remote, err := repo.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: anonymousRemote,
		URLs: []string{cloneURL},
	})
	if err != nil {
		return fmt.Errorf("准备推送目标失败: %w", err)
	}

	refspec := config.RefSpec(fmt.Sprintf("%s:refs/heads/%s", head.Name().String(), deployBranch))
	err = remote.Push(&git.PushOptions{
		RemoteName: anonymousRemote,
		RefSpecs:   []config.RefSpec{refspec},
		Auth:       &http.BasicAuth{Username: "make", Password: token},
		Force:      force,
		Progress:   os.Stdout,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git push 失败: %w", err)
	}
	return nil
}
