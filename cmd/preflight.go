/**
 * [INPUT]: 依赖 errors、fmt、os、path/filepath、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newPreflightCmd 函数、errPreflightFailed 哨兵错误
 * [POS]: cmd 模块的顶层 preflight 命令，按 --type（fullstack/service/ui，默认 fullstack）
 *        校验工作目录是否具备对应形态的 Make app 必需工程骨架——apps/dsl 是身份核心三形态必查，
 *        service / ui 按形态增减；任一缺失返回 errPreflightFailed（由 main.go 转译为退出码 1），
 *        作 CI / deploy 前置门禁
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// ---------------------------------- 哨兵错误 ----------------------------------

// errPreflightFailed 表示工程骨架检查未通过。沿 cobra RunE 链向上返回，
// 由 main.go 转译为退出码 1，使 CI / deploy 能据此门禁；它不是执行失败，
// 故 preflight 命令静默其错误消息（SilenceErrors），避免污染 stderr。
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
for the chosen project type (--type, default fullstack):

  fullstack  apps/dsl/ + apps/service/package.json + apps/ui/package.json
  service    apps/dsl/ + apps/service/package.json          (backend only)
  ui         apps/dsl/ + apps/ui/package.json               (frontend only)

apps/dsl/ holds the DSL definitions and is required in every type.
Any missing entry fails the check (exit code 1), so it can gate CI or deploy.
The directory defaults to the current working directory.`,
		Example: `  makecli preflight
  makecli preflight ./my-app
  makecli preflight --type service`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true, // 检查未过返回 errPreflightFailed 仅作退出码信号，不打印 error: 行
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return reportPreflightError(cmd, runPreflight(root, projectType))
		},
	}
	cmd.Flags().StringVar(&projectType, "type", "fullstack", "project type: fullstack (ui+service), service (service only), ui (ui only)")
	return cmd
}

// reportPreflightError 在命令开启 SilenceErrors 的前提下亲自打印真实错误到 stderr，
// 但放过 errPreflightFailed 哨兵——它仅用于把「检查未过」翻译成非零退出码。
func reportPreflightError(cmd *cobra.Command, err error) error {
	if err != nil && !errors.Is(err, errPreflightFailed) {
		cmd.PrintErrln(cmd.ErrPrefix(), err.Error())
	}
	return err
}

// runPreflight 按 projectType 选定必需骨架，逐项 stat 打印 ✓ / ✗ 清单。
// 未知形态返回普通错误；任一项缺失或类型不符 → 返回 errPreflightFailed（退出码 1）。
func runPreflight(root, projectType string) error {
	layout, ok := layoutByType[projectType]
	if !ok {
		return fmt.Errorf("invalid --type %q: must be fullstack, service, or ui", projectType)
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
