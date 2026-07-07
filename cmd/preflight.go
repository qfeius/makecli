/**
 * [INPUT]: 依赖 errors、fmt、os、path/filepath、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newPreflightCmd 函数、errPreflightFailed 哨兵错误
 * [POS]: cmd 模块的顶层 preflight 命令，按 --app-type（fullstack/service/ui，默认 fullstack）
 *        校验工作目录是否具备对应形态的 Make app 必需工程骨架——apps/dsl 是身份核心三形态必查，
 *        service / ui 按形态增减；任一缺失返回 errPreflightFailed（由 main.go 转译为退出码 1），
 *        作 CI / deploy 前置门禁
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---------------------------------- 哨兵错误 ----------------------------------

// errPreflightFailed 表示工程骨架检查未通过。沿 cobra RunE 链向上返回，
// 由 main.go 转译为退出码 1，使 CI / deploy 能据此门禁；它不是执行失败，
// 故 reportExecuteError（单一错误出口）放过它不打印 error: 行。
var errPreflightFailed = errors.New("preflight: project layout check failed")

// ---------------------------------- 必需骨架 ----------------------------------

// layoutEntry 描述一项必需的工程结构条目：path 相对工程根，dir 区分目录/文件。
type layoutEntry struct {
	path string
	dir  bool
}

// 三块工程骨架的基本单元：dsl 目录承载 DSL 定义（app.yaml 身份核心驻于此），
// service / ui 各自是带 package.json 的 Node 子工程。
var (
	layoutDSL     = layoutEntry{"apps/dsl", true}
	layoutService = layoutEntry{"apps/service/package.json", false}
	layoutUI      = layoutEntry{"apps/ui/package.json", false}
)

// layoutByType 把工程形态映射到必需骨架——deploy 前置门禁。
// apps/dsl 是 Make app 身份核心，三形态都必查；service / ui 按形态增减。
//   fullstack: dsl + service + ui    service: dsl + service    ui: dsl + ui
var layoutByType = map[string][]layoutEntry{
	"fullstack": {layoutDSL, layoutService, layoutUI},
	"service":   {layoutDSL, layoutService},
	"ui":        {layoutDSL, layoutUI},
}

// ---------------------------------- 命令定义 ----------------------------------

func newPreflightCmd() *cobra.Command {
	var projectType string
	cmd := &cobra.Command{
		Use:   "preflight [dir]",
		Short: "Check the directory has a valid Make app project layout",
		Long: `Preflight verifies the directory contains the required Make app skeleton
for the chosen project type (--app-type, default fullstack):

  fullstack  apps/dsl/ + apps/service/package.json + apps/ui/package.json
  service    apps/dsl/ + apps/service/package.json          (backend only)
  ui         apps/dsl/ + apps/ui/package.json               (frontend only)

apps/dsl/ holds the DSL definitions and is required in every type.
Any missing entry fails the check (exit code 1), so it can gate CI or deploy.
The directory defaults to the current working directory.`,
		Example: `  makecli preflight
  makecli preflight ./my-app
  makecli preflight --app-type service`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return runPreflight(root, projectType)
		},
	}
	cmd.Flags().StringVar(&projectType, "app-type", "fullstack", "project type: fullstack (ui+service), service (service only), ui (ui only)")
	return cmd
}

// runPreflight 按 projectType 选定必需骨架，逐项 stat 打印 ✓ / ✗ 清单。
// 未知形态返回普通错误；任一项缺失或类型不符 → 返回 errPreflightFailed（退出码 1）。
func runPreflight(root, projectType string) error {
	layout, ok := layoutByType[projectType]
	if !ok {
		return fmt.Errorf("invalid --app-type %q: must be fullstack, service, or ui", projectType)
	}

	display := root
	if abs, err := filepath.Abs(root); err == nil {
		display = abs
	}
	fmt.Printf("%-10s %s\n", "Project:", display)
	fmt.Printf("%-10s %s\n\n", "Type:", projectType)

	failed := 0
	for _, e := range layout {
		if err := checkLayoutEntry(root, e); err != nil {
			fmt.Printf("✗ %-26s %s\n", e.path, err)
			failed++
		} else {
			fmt.Printf("✓ %s\n", e.path)
		}
	}

	if failed > 0 {
		fmt.Printf("\nFAIL: %d/%d checks failed — not a valid Make app project\n", failed, len(layout))
		return errPreflightFailed
	}
	fmt.Printf("\nOK: project layout looks good\n")
	return nil
}

// checkLayoutEntry 校验单项；通过返回 nil，否则返回失败原因（直接用于输出）。
func checkLayoutEntry(root string, e layoutEntry) error {
	info, err := os.Stat(filepath.Join(root, e.path))
	if err != nil {
		return errors.New("missing")
	}
	if e.dir && !info.IsDir() {
		return errors.New("expected directory, found file")
	}
	if !e.dir && info.IsDir() {
		return errors.New("expected file, found directory")
	}
	return nil
}

// ================================ build spec 检查 ================================
// 以 make-build-service build_spec.md 第 5 节检查清单为实现依据。

// ---------------------------------- 文件投影 ----------------------------------

// workspacesField 兼容 package.json workspaces 的两种形态：
// ["ui"] 数组、{"packages": ["ui"]} 对象。
type workspacesField []string

func (w *workspacesField) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*w = arr
		return nil
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*w = obj.Packages
	return nil
}

// packageJSON 是 package.json 面向检查的最小投影。
type packageJSON struct {
	Name       string            `json:"name"`
	Scripts    map[string]string `json:"scripts"`
	Workspaces workspacesField   `json:"workspaces"`
}

// pkgFile 是一次 package.json 读取的三态结果：不存在 / 存在但坏 / 存在且可用。
// 存在性与内容可用性分离——A1 只问存在，A2/A5/A6 才问内容，坏 JSON 归为内容检查的失败原因。
type pkgFile struct {
	path   string // 相对工程根，输出用
	exists bool
	err    error
	pkg    packageJSON
}

func loadPackageJSON(root, rel string) *pkgFile {
	p := &pkgFile{path: rel}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		p.exists = !errors.Is(err, os.ErrNotExist)
		if p.exists {
			p.err = err
		}
		return p
	}
	p.exists = true
	if err := json.Unmarshal(data, &p.pkg); err != nil {
		p.err = fmt.Errorf("invalid JSON: %v", err)
	}
	return p
}

// pnpmWorkspaceFile 是 apps/pnpm-workspace.yaml 的读取结果，三态同 pkgFile。
type pnpmWorkspaceFile struct {
	exists   bool
	err      error
	packages []string
}

func loadPnpmWorkspace(root string) *pnpmWorkspaceFile {
	w := &pnpmWorkspaceFile{}
	data, err := os.ReadFile(filepath.Join(root, "apps", "pnpm-workspace.yaml"))
	if err != nil {
		w.exists = !errors.Is(err, os.ErrNotExist)
		if w.exists {
			w.err = err
		}
		return w
	}
	w.exists = true
	var doc struct {
		Packages []string `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		w.err = fmt.Errorf("invalid YAML: %v", err)
		return w
	}
	w.packages = doc.Packages
	return w
}

// ---------------------------------- 判定原语 ----------------------------------

// lockfilePriority 按 spec 第 1 节优先级排列：命中即定包管理器，其余被忽略。
var lockfilePriority = []struct{ file, pm string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"yarn.lock", "yarn"},
	{"package-lock.json", "npm"},
}

// detectLockfiles 返回 dir 下按优先级排列的 lockfile 清单与判定出的包管理器。
// 无 lockfile 时包管理器回退 npm（spec 第 1 节：模式 A 走 npm install 兜底）。
func detectLockfiles(dir string) (files []string, pm string) {
	for _, lf := range lockfilePriority {
		if info, err := os.Stat(filepath.Join(dir, lf.file)); err == nil && !info.IsDir() {
			files = append(files, lf.file)
			if pm == "" {
				pm = lf.pm
			}
		}
	}
	if pm == "" {
		pm = "npm"
	}
	return files, pm
}

// workspaceCovers 判断 workspace 声明（pnpm packages / yarn+npm workspaces）是否覆盖
// 组件目录名。条目按 glob 语义匹配（path.Match），归一化 "./ui"、"ui/" 等写法；
// "!" 排除条目跳过——只判覆盖不判排除，排除误伤交构建期暴露。
func workspaceCovers(patterns []string, component string) bool {
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			continue
		}
		p = strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
		if ok, err := path.Match(p, component); err == nil && ok {
			return true
		}
	}
	return false
}
