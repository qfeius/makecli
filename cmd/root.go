/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 Execute 函数、rootCmd 根命令
 * [POS]: cmd 模块的入口，挂载 version / configure / app / entity 子命令
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "makecli",
	Short: "makecli — make your workflow faster",
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
	rootCmd.AddCommand(newVersionCmd(version, buildDate))
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newApplyCmd())
	rootCmd.AddCommand(newAppCmd())
	rootCmd.AddCommand(newEntityCmd())
	return rootCmd.Execute()
}
