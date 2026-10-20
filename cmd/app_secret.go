/**
 * [INPUT]: 依赖 app_variable.go 的校验/目标解析/写入/列表/删除/回执、delete_confirm.go、internal/api、huh/v2、go-isatty，errors、fmt、io、os、strings、cobra
 * [OUTPUT]: 提供 newAppSecretCmd 的 add/put/list/delete 与掩码输入；--from-stdin 有界读取并去除一个末尾换行
 * [POS]: cmd/app 的密钥管理入口，复用 --app 优先/清单回退；固定 runtime、--appRole beta|prod 默认 beta，put 仅更新已有密钥，list 仅返回元数据
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

var confirmAppSecretDeleteFunc = func(key, appKey, appRole string) error {
	return confirmDeletionByTypingKey(key, fmt.Sprintf("secret %q from app %q (%s, runtime)", key, appKey, appRole))
}

func newAppSecretCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "secret", Short: "Manage app secrets"}
	cmd.AddCommand(newAppSecretAddCmd(), newAppSecretPutCmd(), newAppSecretListCmd(), newAppSecretDeleteCmd())
	return cmd
}

func newAppSecretAddCmd() *cobra.Command {
	return newAppSecretWriteCmd("add", "Add or update an app secret",
		"Existing secrets are updated; missing secrets are created.", saveAppSecret)
}

func newAppSecretPutCmd() *cobra.Command {
	return newAppSecretWriteCmd("put", "Update an existing app secret",
		"The secret must already exist; missing secrets return an error and are never created.", updateAppSecret)
}

func newAppSecretListCmd() *cobra.Command {
	cmd := newAppConfigListCmd("Secret", (*api.Client).ListSecrets)
	cmd.Long = "List runtime secret metadata for --app, or the app in apps/dsl/app.yaml. Secret values are never displayed."
	return cmd
}

func newAppSecretDeleteCmd() *cobra.Command {
	return newAppConfigDeleteCmd("Secret", (*api.Client).DeleteSecret, confirmAppSecretDeleteFunc)
}

func newAppSecretWriteCmd(verb, short, behavior string, write appVariableWriter) *cobra.Command {
	var o appVariableOptions
	var fromStdin bool
	cmd := &cobra.Command{
		Use: verb + " <KEY>", Short: short,
		Long: "Save a runtime secret for --app, or the app in apps/dsl/app.yaml. Prompts for a masked value, or reads it with --from-stdin. " + behavior + " Variables cannot be overwritten. Changes apply on the next deployment.",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		Example: fmt.Sprintf(`  makecli app secret %[1]s OPENAI_API_KEY
  makecli app secret %[1]s OPENAI_API_KEY --app demo --appRole prod
  printf '%%s' "$OPENAI_API_KEY" | makecli app secret %[1]s OPENAI_API_KEY --from-stdin`, verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppSecretWrite(cmd, args[0], o, fromStdin, write)
		},
	}
	o.flags(cmd)
	cmd.Flags().BoolVar(&fromStdin, "from-stdin", false, "read secret from stdin; remove one trailing LF or CRLF")
	return cmd
}

func runAppSecretWrite(cmd *cobra.Command, key string, o appVariableOptions, fromStdin bool, write appVariableWriter) error {
	if err := o.validate(key, ""); err != nil {
		return err
	}
	appKey, client, output, err := o.target()
	if err != nil {
		return err
	}
	value, err := readAppSecret(cmd.InOrStdin(), cmd.ErrOrStderr(), key, fromStdin)
	if err != nil {
		return err
	}
	action, err := write(client, appKey, key, value)
	if err != nil {
		return err
	}
	return writeAppConfigReceipt("Secret", appKey, key, action, o.appRole, output)
}

func saveAppSecret(client *api.Client, appKey, key, value string) (string, error) {
	return saveAppConfig(client, appKey, key, value, "Make.Secret")
}

func updateAppSecret(client *api.Client, appKey, key, value string) (string, error) {
	return updateAppConfig(client, appKey, key, value, "Make.Secret")
}

func readAppSecret(input io.Reader, output io.Writer, key string, fromStdin bool) (string, error) {
	if fromStdin {
		return readSecretStdin(input, key)
	}
	terminal, ok := input.(*os.File)
	if !ok || !isatty.IsTerminal(terminal.Fd()) {
		return "", fmt.Errorf("secret input requires a terminal; use --from-stdin in a non-interactive shell")
	}
	var value string
	field := huh.NewInput().Title("Enter a secret value:").EchoMode(huh.EchoModePassword).
		Value(&value).Validate(func(value string) error { return validVariable(key, value) })
	// 显式关闭 accessible 自动回退，避免其忽略读取错误后保存空值。
	err := huh.NewForm(huh.NewGroup(field)).WithAccessible(false).WithInput(input).WithOutput(output).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return "", fmt.Errorf("secret input cancelled")
	}
	return value, err
}

func readSecretStdin(input io.Reader, key string) (string, error) {
	// 预留 CRLF 两字节，再多读一个字节即可判断超限，不读取无限输入。
	data, err := io.ReadAll(io.LimitReader(input, 32*1024+3))
	if err != nil {
		return "", fmt.Errorf("could not read secret from stdin")
	}
	value := strings.TrimSuffix(string(data), "\n")
	if len(value) != len(data) {
		value = strings.TrimSuffix(value, "\r")
	}
	if err := validVariable(key, value); err != nil {
		return "", err
	}
	return value, nil
}
