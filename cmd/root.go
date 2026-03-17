/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 Execute 函数、rootCmd 根命令
 * [POS]: cmd 模块的入口，挂载 version / configure / app / entity / update 子命令
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/spf13/cobra"
)

// DebugMode 全局调试模式标志，从命令行读取
var DebugMode bool

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
{{end}}{{if .HasAvailableInheritedFlags}}
GLOBAL FLAGS
{{.InheritedFlags.FlagUsages | trimRightSpace}}
{{end}}{{if .HasExample}}
EXAMPLES
{{.Example}}
{{end}}{{if .HasAvailableSubCommands}}
Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`

// Execute 是程序入口，由 main.go 调用
func Execute(version, buildDate string) error {
	rootCmd.SetUsageTemplate(usageTemplate)
	rootCmd.SetErrPrefix("error:")
	rootCmd.PersistentFlags().BoolVar(&DebugMode, "debug", false, "enable debug mode to show curl output")
	_ = rootCmd.PersistentFlags().MarkHidden("debug")
	rootCmd.AddCommand(newVersionCmd(version, buildDate))
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newApplyCmd())
	rootCmd.AddCommand(newAppCmd())
	rootCmd.AddCommand(newEntityCmd())
	rootCmd.AddCommand(newUpdateCmd())
	return rootCmd.Execute()
}
