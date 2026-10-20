/**
 * [INPUT]: 依赖 cmd/app 的清单读取、cmd/client、cmd/output、delete_confirm.go、internal/api，errors、fmt、os、regexp、strings、tablewriter、cobra
 * [OUTPUT]: 对外提供 newAppVariableCmd；包内共享 --app/--appRole/--output、目标解析、配置 CRUD-L 与回执供 secret/apply 消费
 * [POS]: cmd/app 的配置公共流程，--app 优先，否则从 apps/dsl/app.yaml 定位；固定 runtime，--appRole beta|prod 默认 beta，按类型限制更新和删除
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

var variableKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

const appVariableScope = "runtime"

var confirmAppVariableDeleteFunc = func(key, appKey, appRole string) error {
	return confirmDeletionByTypingKey(key, fmt.Sprintf("variable %q from app %q (%s, runtime)", key, appKey, appRole))
}

type appVariableOptions struct {
	appKey, appRole, output string
}

func (o *appVariableOptions) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.appKey, "app", "", "app key (defaults to apps/dsl/app.yaml)")
	cmd.Flags().StringVar(&o.appRole, "appRole", api.RoleBeta, "target app role: beta | prod")
	addOutputFlag(cmd, &o.output)
}

type appVariableWriter func(client *api.Client, appKey, key, value string) (string, error)
type appConfigLister func(client *api.Client, appKey, scope string, page, size int) ([]api.Variable, int, error)
type appConfigDeleter func(client *api.Client, key, appKey, scope string) error
type appConfigConfirmer func(key, appKey, appRole string) error

type appVariableReceipt struct {
	AppKey  string `json:"appKey"`
	Key     string `json:"key"`
	Scope   string `json:"scope"`
	AppRole string `json:"appRole"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

func newAppVariableCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "variable", Short: "Manage app variables"}
	cmd.AddCommand(newAppVariableAddCmd(), newAppVariablePutCmd(), newAppVariableListCmd(), newAppVariableDeleteCmd())
	return cmd
}

func newAppVariableAddCmd() *cobra.Command {
	return newAppVariableWriteCmd("add", "Add or update an app variable",
		"Existing variables are updated; missing variables are created.", saveAppVariable)
}

func newAppVariablePutCmd() *cobra.Command {
	return newAppVariableWriteCmd("put", "Update an existing app variable",
		"The variable must already exist; missing variables return an error and are never created.", updateAppVariable)
}

func newAppVariableWriteCmd(verb, short, behavior string, write appVariableWriter) *cobra.Command {
	var o appVariableOptions
	cmd := &cobra.Command{
		Use:          verb + " <KEY> <VALUE>",
		Short:        short,
		Long:         "Save a plain-text runtime variable for --app, or the app in apps/dsl/app.yaml. " + behavior + " Secrets cannot be overwritten. Changes apply on the next deployment. Empty values are supported.",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		Example: fmt.Sprintf(`  makecli app variable %[1]s BASE_URL https://api.example.com
  makecli app variable %[1]s BASE_URL https://api.example.com --appRole prod
  makecli app variable %[1]s LOG_LEVEL debug --app demo --output json`, verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppVariableWrite(args[0], args[1], o, write)
		},
	}
	o.flags(cmd)
	return cmd
}

func (o appVariableOptions) validate(key, value string) error {
	if err := o.validateRole(); err != nil {
		return err
	}
	return validVariable(key, value)
}

func (o appVariableOptions) validateRole() error {
	if o.appRole != api.RoleBeta && o.appRole != api.RoleProd {
		return fmt.Errorf("--appRole must be beta or prod")
	}
	return nil
}

// target 统一变量命令的角色、输出、应用身份与客户端解析，不发起请求。
func (o appVariableOptions) target() (string, *api.Client, string, error) {
	if err := o.validateRole(); err != nil {
		return "", nil, "", err
	}
	output, err := resolveOutputFormat(o.output)
	if err != nil {
		return "", nil, "", err
	}
	appKey, err := o.resolveAppKey()
	if err != nil {
		return "", nil, "", err
	}
	client, err := newClientFromProfile(api.WithAppRole(o.appRole))
	return appKey, client, output, err
}

func (o appVariableOptions) resolveAppKey() (string, error) {
	if o.appKey != "" {
		return o.appKey, validResourceKey(o.appKey)
	}
	appKey, err := appKeyFromManifest(appDSLPath)
	if err != nil {
		return "", fmt.Errorf("read app identity: %w; use --app <appKey> or run from the app project root", err)
	}
	return appKey, nil
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

func runAppVariableWrite(key, value string, o appVariableOptions, write appVariableWriter) error {
	if err := o.validate(key, value); err != nil {
		return err
	}
	appKey, client, output, err := o.target()
	if err != nil {
		return err
	}
	action, err := write(client, appKey, key, value)
	if err != nil {
		return err
	}
	return writeAppConfigReceipt("Variable", appKey, key, action, o.appRole, output)
}

func newAppVariableListCmd() *cobra.Command {
	return newAppConfigListCmd("Variable", (*api.Client).ListVariables)
}

func newAppConfigListCmd(kind string, list appConfigLister) *cobra.Command {
	var o appVariableOptions
	var page, size int
	cmd := &cobra.Command{
		Use: "list", Short: "List runtime app " + strings.ToLower(kind) + "s",
		Long: "List runtime configuration for --app, or the app in apps/dsl/app.yaml. Plain-text values are visible; secret values are never displayed.",
		Args: cobra.NoArgs, SilenceUsage: true,
		Example: fmt.Sprintf(`  makecli app %[1]s list
  makecli app %[1]s list --appRole prod --output json
  makecli app %[1]s list --app demo --page 2 --size 20`, strings.ToLower(kind)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppConfigList(o, page, size, kind, list)
		},
	}
	o.flags(cmd)
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of entries per page (1–100)")
	return cmd
}

func runAppConfigList(o appVariableOptions, page, size int, kind string, list appConfigLister) error {
	if page < 1 || size < 1 || size > 100 {
		return fmt.Errorf("--page must be at least 1 and --size must be between 1 and 100")
	}
	appKey, client, output, err := o.target()
	if err != nil {
		return err
	}
	variables, total, err := list(client, appKey, appVariableScope, page, size)
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(map[string]any{
			"data":       variables,
			"pagination": map[string]int{"count": len(variables), "page": page, "size": size, "total": total},
		})
	}
	return printAppConfigs(variables, total, appKey, o.appRole, kind)
}

func printAppConfigs(variables []api.Variable, total int, appKey, appRole, kind string) error {
	plural := strings.ToLower(kind) + "s"
	if len(variables) == 0 {
		fmt.Printf("No runtime %s found in app '%s' (%s).\n", plural, appKey, appRole)
		return nil
	}
	rows := make([][]string, len(variables))
	for i, variable := range variables {
		value := ""
		if variable.Properties.Value != nil {
			value = *variable.Properties.Value
		}
		if variable.Type == "Make.Secret" {
			value = "(encrypted)"
		}
		rows[i] = []string{variable.Key, variable.Type, variable.Properties.Scope, value, variable.Properties.Description}
	}
	fmt.Printf("%-12s %s\n%-12s %s\n", "App", appKey, "App role", appRole)
	table := tablewriter.NewTable(os.Stdout)
	table.Header("KEY", "TYPE", "SCOPE", "VALUE", "DESCRIPTION")
	if err := table.Bulk(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}
	fmt.Printf("\nShowing %d of %d %s\n", len(variables), total, plural)
	return nil
}

func newAppVariableDeleteCmd() *cobra.Command {
	return newAppConfigDeleteCmd("Variable", (*api.Client).DeleteVariable, confirmAppVariableDeleteFunc)
}

func newAppConfigDeleteCmd(kind string, remove appConfigDeleter, confirm appConfigConfirmer) *cobra.Command {
	var o appVariableOptions
	var yes bool
	cmd := &cobra.Command{
		Use: "delete <KEY>", Short: "Delete a runtime app " + strings.ToLower(kind),
		Long: "Delete a runtime " + strings.ToLower(kind) + " for --app, or the app in apps/dsl/app.yaml. Requires typing the key to confirm, or --yes. The change applies on the next deployment.",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		Example: fmt.Sprintf(`  makecli app %[1]s delete CONFIG_KEY
  makecli app %[1]s delete CONFIG_KEY --app demo --appRole prod --yes`, strings.ToLower(kind)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppConfigDelete(args[0], o, yes, kind, remove, confirm)
		},
	}
	o.flags(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the deletion confirmation prompt")
	return cmd
}

func runAppConfigDelete(key string, o appVariableOptions, skipConfirm bool, kind string, remove appConfigDeleter, confirm appConfigConfirmer) error {
	if err := o.validate(key, ""); err != nil {
		return err
	}
	appKey, client, output, err := o.target()
	if err != nil {
		return err
	}
	if !skipConfirm {
		if err := confirm(key, appKey, o.appRole); err != nil {
			return err
		}
	}
	if err := remove(client, key, appKey, appVariableScope); err != nil {
		return err
	}
	return writeAppConfigReceipt(kind, appKey, key, "deleted", o.appRole, output)
}

func writeAppConfigReceipt(kind, appKey, key, action, appRole, output string) error {
	message := "Configuration saved; applies on the next deployment."
	receipt := appVariableReceipt{AppKey: appKey, Key: key, Scope: appVariableScope, AppRole: appRole, Action: action, Message: message}
	if output == outputJSON {
		return writeJSON(receipt)
	}
	fmt.Printf("%s '%s' %s successfully\n", kind, key, action)
	fmt.Printf("%-12s %s\n%-12s %s\n%-12s %s\n%s\n", "App", appKey, "App role", appRole, "Scope", appVariableScope, message)
	return nil
}

// saveAppVariable 在同一应用、角色及 scope 内新增或更新，保留已有说明；查询失败不转创建。
func saveAppVariable(client *api.Client, appKey, key, value string) (string, error) {
	return saveAppConfig(client, appKey, key, value, "Make.Variable")
}

// updateAppVariable 先读取以保留说明；不存在、读取失败或类型不符时不写入。
func updateAppVariable(client *api.Client, appKey, key, value string) (string, error) {
	return updateAppConfig(client, appKey, key, value, "Make.Variable")
}

func updateAppConfig(client *api.Client, appKey, key, value, resourceType string) (string, error) {
	variable, err := getAppConfig(client, appKey, key, resourceType)
	if err != nil {
		return "", err
	}
	if variable.Type != resourceType {
		return "", fmt.Errorf("cannot update %q: expected %s, got %s", key, resourceType, variable.Type)
	}
	variable.Properties.Value = &value
	return "updated", client.UpdateVariable(*variable)
}

func getAppConfig(client *api.Client, appKey, key, resourceType string) (*api.Variable, error) {
	get := client.GetVariable
	if resourceType == "Make.Secret" {
		get = client.GetSecret
	}
	variable, err := get(key, appKey, appVariableScope)
	if err == nil {
		// 写入始终使用用户的入口 key + appRole，避免再次解析已返回的实际 key。
		variable.AppKey = appKey
	}
	return variable, err
}

func saveAppConfig(client *api.Client, appKey, key, value, resourceType string) (string, error) {
	variable, err := getAppConfig(client, appKey, key, resourceType)
	create := errors.Is(err, api.ErrNotFound)
	if err != nil && !create {
		return "", err
	}
	if create {
		variable = &api.Variable{Key: key, Type: resourceType, AppKey: appKey, Properties: api.VariableProperties{Scope: appVariableScope}}
	}
	if variable.Type != resourceType {
		return "", fmt.Errorf("%q already exists as %s in scope %q; cannot overwrite it with %s (secret and variable types are immutable)", key, variable.Type, appVariableScope, resourceType)
	}
	variable.Properties.Value = &value
	if create {
		return "created", client.CreateVariable(*variable)
	}
	return "updated", client.UpdateVariable(*variable)
}
