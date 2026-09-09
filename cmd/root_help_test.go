/**
 * [INPUT]: 依赖 bytes、strings、testing、github.com/spf13/cobra
 * [OUTPUT]: 验证根命令 --help 尾部印 skills 安装引导，子命令不印
 * [POS]: cmd/root.go usageTemplate 的回归测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const skillsSetupHint = "Skills setup (one-time, humans): makecli skills install --all --yes"

func renderHelp(t *testing.T, root *cobra.Command, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(append(args, "--help"))
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	return out.String()
}

func newHelpRoot() *cobra.Command {
	root := &cobra.Command{Use: "makecli"}
	installUsageTemplate(root)
	root.AddCommand(&cobra.Command{Use: "skills", Run: func(*cobra.Command, []string) {}})
	return root
}

func TestRootHelpShowsSkillsSetupHint(t *testing.T) {
	out := renderHelp(t, newHelpRoot())
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), skillsSetupHint) {
		t.Fatalf("root help should end with skills setup hint, got:\n%s", out)
	}
}

func TestSubcommandHelpOmitsSkillsSetupHint(t *testing.T) {
	out := renderHelp(t, newHelpRoot(), "skills")
	if strings.Contains(out, "Skills setup") {
		t.Fatalf("subcommand help should not carry skills setup hint, got:\n%s", out)
	}
}
