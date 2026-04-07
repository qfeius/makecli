/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 Execute 函数、rootCmd 根命令
 * [POS]: cmd 模块的入口，挂载 version / configure / app / entity / relation / record / apply / diff / update / schema 子命令
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// DebugMode 全局调试模式标志，从命令行读取
var DebugMode bool

// ServerURL Meta Server 基础 URL，从命令行读取
var ServerURL string

const defaultMetaServer = "https://dev-make.qtech.cn/api/make"

var rootCmd = &cobra.Command{
	Use:   "makecli",
	Short: "makecli — agentic development platform cli",
}

// usageTemplate 对齐 GitHub CLI 风格：段落标题全大写
const usageTemplate = `{{with .Long}}{{. | trimRightSpace}}

{{end}}USAGE
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
	rootCmd.SetErrPrefix("error:")
	rootCmd.PersistentFlags().BoolVar(&DebugMode, "debug", false, "enable debug mode to show curl output")
	_ = rootCmd.PersistentFlags().MarkHidden("debug")
	rootCmd.PersistentFlags().StringVar(&ServerURL, "server-url", "", "Meta Server base URL (default: config or "+defaultMetaServer+")")
	rootCmd.AddCommand(newVersionCmd(version, buildDate))
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newApplyCmd())
	rootCmd.AddCommand(newAppCmd())
	rootCmd.AddCommand(newEntityCmd())
	rootCmd.AddCommand(newRelationCmd())
	rootCmd.AddCommand(newRecordCmd())
	rootCmd.AddCommand(newUpdateCmd())
	rootCmd.AddCommand(newDiffCmd())
	rootCmd.AddCommand(newSchemaCmd())
	return rootCmd.Execute()
}
