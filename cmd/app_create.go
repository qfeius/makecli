/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile/newRepoClientFromProfile）、cmd/app（loadAppManifestFromFile、validResourceKey、defaultName）、cmd/apply（ResourceManifest）、cmd/git（initGitRepo/ensureGitignore/stageAndCommit）、agents（embed 模板）、bytes、fmt、os、path/filepath、github.com/go-git/go-git/v5、gopkg.in/yaml.v3、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppCreateCmd 函数；包内 runAppCreate / assertScaffoldClear / writeScaffold / scaffoldGit / renderAppDSL / newAppManifest / deriveAppKey
 * [POS]: cmd/app 的 create 子命令——一条命令完成 本地脚手架 + 远端 App + git 仓库 + 代码仓库。
 *        位置参数 <appKey> 同时是「目录名 + key」（filepath.Base(filepath.Abs(arg)) 推导，`.`/`..` 隐藏便利），
 *        validResourceKey 把关；写 CLAUDE.md/AGENTS.md（embed 模板，scaffoldFile 映射 embed→out 名）+ apps/dsl/app.yaml（ResourceManifest 序列化，与 apply/diff 同结构往返）；
 *        执行序「远端先行」：存在性预检(assertScaffoldClear,只读)→CreateApp→writeScaffold→scaffoldGit(init+.gitignore+initial commit)→prepareCodeRepos，远端失败时本地零残留，重跑干净；本地/远端冲突即拒绝（提示删除重建）；
 *        scaffoldGit 复用 app init 内核再加一次 initial commit，使 create 产物即干净可 deploy；git 失败降级 stderr 警告（不阻断已成功的远端创建）；
 *        prepareCodeRepos 成功静默（仅 deploy 关心仓库地址），失败降级为 stderr 警告；-f 文件模式仅建远端不脚手架
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/qfeius/makecli/agents"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// scaffoldFile 把 embed 模板名映射到写出文件名（同名去 .tmpl 后缀）。
type scaffoldFile struct{ embed, out string }

// scaffoldTemplates 是脚手架从 embed 写出的引导文档。
// .gitignore 不在此列——它由 scaffoldGit 经 ensureGitignore 幂等管理（与 app init 同一真相源）。
var scaffoldTemplates = []scaffoldFile{
	{"CLAUDE.md.tmpl", "CLAUDE.md"},
	{"AGENTS.md.tmpl", "AGENTS.md"},
}

// appDSLPath 是 App DSL 种子在工程内的相对路径（对齐 preflight 骨架）
var appDSLPath = filepath.Join("apps", "dsl", "app.yaml")

func newAppCreateCmd() *cobra.Command {
	var description string
	var displayName string
	var file string

	cmd := &cobra.Command{
		Use:   "create <appKey>",
		Short: "Create a new Make app (scaffolds <appKey>/ and creates it on Make)",
		Example: `  makecli app create shop
  makecli app create shop --name "我的商城"
  makecli app create shop --name "My Shop" --description "demo shop"
  makecli app create -f apps/dsl/app.yaml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppCreateFromFile(file)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires <appKey> (or '.') or -f flag")
			}
			return runAppCreate(args[0], displayName, description)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "app description")
	cmd.Flags().StringVar(&displayName, "name", "", "app display name (defaults to appKey)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to YAML file containing Make.App resource (remote only, no scaffold)")
	return cmd
}

// ---------------------------------- 脚手架模式：本地 + 远端 + 仓库 ----------------------------------

// runAppCreate 执行合并后的 create：解析 appKey → 加载凭证 → 本地存在性预检 → 远端创建 → 写本地脚手架 → 准备仓库。
// 顺序刻意「远端先行」：token 失效 / 冲突 / 网络故障都在写任何本地文件之前报错，
// 这样换 profile 或修复 token 后重跑是干净的，不会被上一次的半成品工程拦住。
// 存在性预检（只读 os.Stat）又放在远端之前——目标已存在就尽早拒绝，避免白白创建远端 App。
func runAppCreate(folder, displayName, description string) error {
	appKey, err := deriveAppKey(folder)
	if err != nil {
		return err
	}
	manifest := newAppManifest(appKey, defaultName(displayName, appKey), description)

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	if err := assertScaffoldClear(folder); err != nil {
		return err
	}

	if apiErr := client.CreateApp(manifest.Key, manifest.Name, manifest.Properties); apiErr != nil {
		return apiErr
	}

	if err := writeScaffold(folder, manifest); err != nil {
		return err
	}

	scaffoldGit(folder, appKey)

	fmt.Printf("App '%s' created successfully\n", appKey)
	prepareCodeRepos(appKey)
	return nil
}

// scaffoldGit 把脚手架目录变成可立即部署的 git 仓库：init（幂等）+ .gitignore + 一次 initial commit。
// 与 app init 共享内核（initGitRepo / ensureGitignore），但额外做 commit——使 create 产物即干净、可直接 deploy。
// 失败仅降级为 stderr 警告（同 prepareCodeRepos 档）：远端 App 与本地脚手架均已就绪，
// git 没起来属可单独补救（重跑 `makecli app init` + 手动 commit），不该让已成功的 create 报全败。
// 全程不写 stdout——保持成功输出仅 `App 'X' created successfully` 一行。
func scaffoldGit(folder, appKey string) {
	if _, err := initGitRepo(folder); err != nil {
		warnGit(err)
		return
	}
	if _, err := ensureGitignore(folder); err != nil {
		warnGit(err)
		return
	}
	repo, err := git.PlainOpen(folder)
	if err != nil {
		warnGit(err)
		return
	}
	if _, err := stageAndCommit(repo, fmt.Sprintf("Initial scaffold for %s", appKey)); err != nil {
		warnGit(err)
	}
}

func warnGit(err error) {
	fmt.Fprintf(os.Stderr, "warning: git not initialized: %v (run 'makecli app init')\n", err)
}

// deriveAppKey 从目录参数推导 appKey：取绝对路径的 basename，统一覆盖 `shop` / `.` / `..`。
func deriveAppKey(folder string) (string, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", fmt.Errorf("resolve '%s': %w", folder, err)
	}
	appKey := filepath.Base(abs)
	if err := validResourceKey(appKey); err != nil {
		return "", fmt.Errorf("directory name %q can't be an app key: %w", appKey, err)
	}
	return appKey, nil
}

// newAppManifest 构造 Make.App 清单（脚手架写文件与远端 CreateApp 共用，单一真相源）。
// 空 description 不进 properties——保证 app.yaml 与远端无即时 diff 漂移。
func newAppManifest(appKey, name, description string) ResourceManifest {
	props := map[string]any{}
	if description != "" {
		props["description"] = description
	}
	return ResourceManifest{
		Key:        appKey,
		Name:       name,
		Type:       "Make.App",
		Meta:       map[string]any{"version": "1.0.0"},
		Properties: props,
	}
}

// assertScaffoldClear 前置检查脚手架目标文件均不存在——任一已存在即拒绝（提示删除重建）。
// 与写出分离：在远端创建之前调用（只读 os.Stat，不动文件系统），
// 避免「远端建好但本地拒绝」的反向半成品。
func assertScaffoldClear(folder string) error {
	targets := []string{appDSLPath}
	for _, f := range scaffoldTemplates {
		targets = append(targets, f.out)
	}
	for _, name := range targets {
		target := filepath.Join(folder, name)
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("'%s' already exists; remove it and re-run", target)
		}
	}
	return nil
}

// writeScaffold 写出本地工程骨架：CLAUDE.md / AGENTS.md（embed 模板）+ apps/dsl/app.yaml（DSL 种子）。
// .gitignore 不在此——由 scaffoldGit 经 ensureGitignore 管理。假定目标已由 assertScaffoldClear 确认为空；仅在远端 App 创建成功后调用。
func writeScaffold(folder string, manifest ResourceManifest) error {
	if err := os.MkdirAll(folder, 0755); err != nil {
		return fmt.Errorf("create '%s': %w", folder, err)
	}
	for _, f := range scaffoldTemplates {
		data, err := agents.Templates.ReadFile(f.embed)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", f.embed, err)
		}
		if err := os.WriteFile(filepath.Join(folder, f.out), data, 0644); err != nil {
			return err
		}
	}

	dsl, err := renderAppDSL(manifest)
	if err != nil {
		return err
	}
	dslFull := filepath.Join(folder, appDSLPath)
	if err := os.MkdirAll(filepath.Dir(dslFull), 0755); err != nil {
		return fmt.Errorf("create '%s': %w", filepath.Dir(dslFull), err)
	}
	return os.WriteFile(dslFull, dsl, 0644)
}

// renderAppDSL 把清单序列化为人类可编辑的 app.yaml（2 空格缩进对齐 DSL 例子 + 顶部用法注释）。
// 复用 ResourceManifest，使 create 写出的就是 apply/diff 能原样读回的 manifest。
func renderAppDSL(manifest ResourceManifest) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# Make App DSL · edit then `makecli apply -f apps/dsl/app.yaml`\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(manifest); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

// ---------------------------------- 文件模式：仅远端 ----------------------------------

func runAppCreateFromFile(path string) error {
	manifest, err := loadAppManifestFromFile(path)
	if err != nil {
		return err
	}

	if err := validResourceKey(manifest.Key); err != nil {
		return err
	}

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	props := manifest.Properties
	if props == nil {
		props = map[string]any{}
	}

	// 展示名缺省时回退用 key
	displayName := defaultName(manifest.Name, manifest.Key)

	if apiErr := client.CreateApp(manifest.Key, displayName, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", manifest.Key)
	prepareCodeRepos(manifest.Key)
	return nil
}

// ---------------------------------- 代码仓库准备 ----------------------------------

// prepareCodeRepos 在 App 创建成功后幂等准备 preview/production 代码仓库。
// 成功静默——仓库地址只在 deploy 时才有意义，create 成功只打印一行 created。
// 失败仅降级为 stderr 警告：deploy 走同一个幂等接口会自动重试，不该把已成功的
// App 创建报成失败，但准备失败属于值得告知的错误信息，故仍输出到 stderr。
func prepareCodeRepos(appKey string) {
	client, _, err := newRepoClientFromProfile()
	if err == nil {
		if _, err = client.CreateRepository(appKey); err == nil {
			return
		}
	}
	fmt.Fprintf(os.Stderr, "warning: code repositories not ready: %v (deploy will retry automatically)\n", err)
}
