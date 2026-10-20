/**
 * [INPUT]: 依赖 cmd/app 的清单读取、cmd/client、cmd/output、internal/api，errors、fmt、regexp、strings、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppVariableCmd；包内 validVariable 共用输入校验、新增或更新及保存回执
 * [POS]: cmd/app 的 variable add 子命令，从 apps/dsl/app.yaml 定位应用；固定 runtime、默认 beta，仅保存配置
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

var variableKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

const appVariableScope = "runtime"

type appVariableOptions struct {
	env, output string
}

type appVariableReceipt struct {
	AppKey      string `json:"appKey"`
	Key         string `json:"key"`
	Scope       string `json:"scope"`
	Environment string `json:"environment"`
	Action      string `json:"action"`
	Message     string `json:"message"`
}

func newAppVariableCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "variable", Short: "Manage app variables"}
	cmd.AddCommand(newAppVariableAddCmd())
	return cmd
}

func newAppVariableAddCmd() *cobra.Command {
	var o appVariableOptions
	cmd := &cobra.Command{
		Use:          "add <KEY> <VALUE>",
		Short:        "Add or update an app variable",
		Long:         "Save a plain-text runtime variable for the app in apps/dsl/app.yaml. Existing variables are updated; secrets cannot be overwritten. Saving does not trigger a deployment. Empty values are supported.",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		Example: `  makecli app variable add BASE_URL https://api.example.com
  makecli app variable add BASE_URL https://api.example.com --env production
  makecli app variable add LOG_LEVEL debug --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppVariableAdd(args[0], args[1], o)
		},
	}
	cmd.Flags().StringVar(&o.env, "env", api.EnvBeta, "target environment: beta | production")
	addOutputFlag(cmd, &o.output)
	return cmd
}

func (o appVariableOptions) validate(key, value string) error {
	if o.env != api.EnvBeta && o.env != api.EnvProduction {
		return fmt.Errorf("--env must be beta or production")
	}
	return validVariable(key, value)
}

// validVariable 是命令参数与 app.yaml 变量声明共用的输入规则。
func validVariable(key, value string) error {
	if !variableKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid variable key %q: must match [A-Za-z_][A-Za-z0-9_]{0,127}", key)
	}
	if strings.HasPrefix(key, "MAKE_") {
		return fmt.Errorf("variable key %q uses the reserved MAKE_ prefix", key)
	}
	if len(value) > 32*1024 {
		return fmt.Errorf("variable value must not exceed 32 KiB")
	}
	return nil
}

func runAppVariableAdd(key, value string, o appVariableOptions) error {
	if err := o.validate(key, value); err != nil {
		return err
	}
	output, err := resolveOutputFormat(o.output)
	if err != nil {
		return err
	}
	appKey, err := appKeyFromManifest(appDSLPath)
	if err != nil {
		return fmt.Errorf("read app identity: %w; run from the app project root", err)
	}
	client, err := newClientFromProfile(api.WithAppRole(api.RoleForEnv(o.env)))
	if err != nil {
		return err
	}
	action, err := saveAppVariable(client, appKey, key, value)
	if err != nil {
		return err
	}
	message := "Configuration saved; applies on the next deployment."
	receipt := appVariableReceipt{AppKey: appKey, Key: key, Scope: appVariableScope, Environment: o.env, Action: action, Message: message}
	if output == outputJSON {
		return writeJSON(receipt)
	}
	fmt.Printf("Variable '%s' %s successfully\n", key, action)
	fmt.Printf("%-12s %s\n%-12s %s\n%-12s %s\n%s\n", "App", appKey, "Environment", o.env, "Scope", appVariableScope, message)
	return nil
}

// saveAppVariable 在同一应用、角色及 scope 内新增或更新，保留已有说明；查询失败不转创建。
func saveAppVariable(client *api.Client, appKey, key, value string) (string, error) {
	variable, err := client.GetVariable(key, appKey, appVariableScope)
	create := errors.Is(err, api.ErrNotFound)
	if err != nil && !create {
		return "", err
	}
	if create {
		variable = &api.Variable{Key: key, Type: "Make.Variable", AppKey: appKey, Properties: api.VariableProperties{Scope: appVariableScope}}
	}
	if variable.Type != "Make.Variable" {
		return "", fmt.Errorf("%q already exists as a secret in scope %q; cannot overwrite it with a variable", key, appVariableScope)
	}
	variable.Properties.Value = &value
	if create {
		return "created", client.CreateVariable(*variable)
	}
	return "updated", client.UpdateVariable(*variable)
}
