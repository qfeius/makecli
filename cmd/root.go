/**
 * [INPUT]: 依赖 github.com/spf13/cobra、github.com/spf13/pflag、os、strings、internal/config（EnvironmentNames/DefaultEnvironment）、internal/notifier
 * [OUTPUT]: 对外提供 Execute 函数、rootCmd 根命令、全局变量 Profile / MetaServerURL / RepoServerURL / Environment / DebugMode；包内 commandName 解析器
 * [POS]: cmd 模块的入口，挂载 version / configure / login / whoami / app / entity / relation / record / apply / diff / update / schema / integration / preflight 子命令；定义全局 --profile / --meta-server-url / --repo-server-url / --env / --debug PersistentFlag；后端 URL 兜底交给 config.Environment preset；错误呈现经 reportExecuteError 单一出口（SilenceErrors，见 errors.go）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"os"
	"strings"

	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/notifier"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// DebugMode 全局调试模式标志，从命令行读取
var DebugMode bool

// MetaServerURL Meta Server 基础 URL，从命令行读取
var MetaServerURL string

// RepoServerURL 代码仓库服务（make-repo）基础 URL，从命令行读取
var RepoServerURL string

// Profile 全局凭证 profile 名称，从命令行读取（--profile）。
// 默认值与 PersistentFlag 注册一致，确保未经过 cobra 解析时（如单元测试）也可用。
var Profile = "default"

// Environment 全局环境名（--env）。空串 = 回退 [settings] environment 或 config.DefaultEnvironment。
// 后端 URL 三件套由当前环境的 config.Environment preset 兜底（见 client.go resolveEnvironment）。
var Environment string

var rootCmd = &cobra.Command{
	Use:   "makecli",
	Short: "makecli — agentic development platform cli",
}

// usageTemplate 对齐 GitHub CLI 风格：段落标题全大写
// 不含命令描述——描述由 cobra 默认 HelpTemplate 的 (or .Long .Short) 负责，
// 此处再印一遍会与 --help 重复（曾因所有命令只设 Short 而未暴露）
const usageTemplate = `USAGE
  {{.UseLine}}{{if .HasAvailableSubCommands}} [command]{{end}}
{{if .HasAvailableSubCommands}}
AVAILABLE COMMANDS
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
FLAGS
{{.LocalFlags.FlagUsages | trimRightSpace}}
{{end}}{{if parentFlags .}}
INHERITED FLAGS
{{parentFlags . | trimRightSpace}}
{{end}}{{if globalFlags .}}
GLOBAL FLAGS
{{globalFlags . | trimRightSpace}}
{{end}}{{if .HasExample}}
EXAMPLES
{{.Example}}
{{end}}{{if .HasAvailableSubCommands}}
Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`

// Execute 是程序入口，由 main.go 调用
func Execute(version, buildDate string) error {
	// 注册模板函数：拆分 InheritedFlags 为 global（root 级）和 parent（中间命令级）
	cobra.AddTemplateFunc("globalFlags", func(cmd *cobra.Command) string {
		fs := pflag.NewFlagSet("global", pflag.ContinueOnError)
		cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
			if rootCmd.PersistentFlags().Lookup(f.Name) != nil {
				fs.AddFlag(f)
			}
		})
		return fs.FlagUsages()
	})
	cobra.AddTemplateFunc("parentFlags", func(cmd *cobra.Command) string {
		fs := pflag.NewFlagSet("parent", pflag.ContinueOnError)
		cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
			if rootCmd.PersistentFlags().Lookup(f.Name) == nil {
				fs.AddFlag(f)
			}
		})
		return fs.FlagUsages()
	})
	rootCmd.Version = formatVersion(version, buildDate)
	rootCmd.SetVersionTemplate(`{{.Version}}`)
	rootCmd.SetUsageTemplate(usageTemplate)
	// 错误呈现收口到 Execute 出口的 reportExecuteError 单一出口：
	// 抑制 cobra 自动打印，让鉴权失败能升级为引导、退出码哨兵能保持静默。
	rootCmd.SilenceErrors = true
	rootCmd.PersistentFlags().BoolVar(&DebugMode, "debug", false, "enable debug mode to show curl output")
	_ = rootCmd.PersistentFlags().MarkHidden("debug")
	rootCmd.PersistentFlags().StringVar(&MetaServerURL, "meta-server-url", "", "Meta Server base URL (overrides profile config and environment default)")
	rootCmd.PersistentFlags().StringVar(&RepoServerURL, "repo-server-url", "", "Code Repository Server base URL (overrides profile config and environment default)")
	rootCmd.PersistentFlags().StringVar(&Profile, "profile", "default", "credentials profile to use")
	rootCmd.PersistentFlags().StringVar(&Environment, "env", "", "backend environment "+strings.Join(config.EnvironmentNames(), "|")+" (overrides [settings] environment, default "+config.DefaultEnvironment+")")
	rootCmd.AddCommand(newVersionCmd(version, buildDate))
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newWhoamiCmd())
	rootCmd.AddCommand(newApplyCmd())
	rootCmd.AddCommand(newAppCmd())
	rootCmd.AddCommand(newEntityCmd())
	rootCmd.AddCommand(newRelationCmd())
	rootCmd.AddCommand(newRecordCmd())
	rootCmd.AddCommand(newUpdateCmd())
	rootCmd.AddCommand(newDiffCmd())
	rootCmd.AddCommand(newPreflightCmd())
	rootCmd.AddCommand(newSchemaCmd())
	rootCmd.AddCommand(newIntegrationCmd())
	n := notifier.Start()
	err := rootCmd.Execute()
	n.Finish(commandName(rootCmd, os.Args[1:]))
	reportExecuteError(os.Stderr, err)
	return err
}

// commandName 解析本次实际调用的顶级子命令名（version/update/app...）。
// 无子命令或解析失败时返回 ""（由判定链视为跳过）。
func commandName(root *cobra.Command, args []string) string {
	cmd, _, err := root.Find(args)
	if err != nil || cmd == nil || cmd == root {
		return ""
	}
	for cmd.Parent() != nil && cmd.Parent() != root {
		cmd = cmd.Parent()
	}
	return cmd.Name()
}
