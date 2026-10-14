/**
 * [INPUT]: 依赖 cmd/client、cmd/app 的 validResourceKey、cmd/agent 的 validAgentAppKey、cmd/apply 的 defaultName、cmd/jsonflag、cmd/output，internal/api、fmt、os、strings、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAgentCreateCmd；包内 runAgentCreate / agentCreateRequest / agentCreateOptions
 * [POS]: cmd/agent 的远端配置创建子命令；--app 指定实际环境 appKey，固定 runtimeKey=make-runtime-service，不生成本地工程；--model 映射 API modelKey
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

// agentCreateOptions 只包含用户可配置的创建输入，运行服务由命令确定。
type agentCreateOptions struct {
	appKey, name, modelKey, description, prompt, promptFile, status, pluginsJSON, output string
}

func newAgentCreateCmd() *cobra.Command {
	var o agentCreateOptions
	cmd := &cobra.Command{
		Use:          "create <agentKey>",
		Short:        "Create an agent on Make app",
		Long:         "Create a remote agent belonging to the actual environment appKey specified by --app, with runtimeKey=make-runtime-service. No local files are created.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Example: `  makecli agent create expense_agent --app expense_app --model deepseek --name "报销助手"
  makecli agent create expense_agent --app expense_app --model deepseek --prompt-file prompt.txt --plugins-json @plugins.json
  makecli agent create expense_agent --app expense_app --model deepseek --status enabled --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentCreate(args[0], o)
		},
	}
	cmd.Flags().StringVar(&o.appKey, "app", "", "actual environment app key (required)")
	_ = cmd.MarkFlagRequired("app")
	cmd.Flags().StringVar(&o.name, "name", "", "agent display name (defaults to agentKey)")
	cmd.Flags().StringVar(&o.modelKey, "model", "", "model catalog key (required)")
	_ = cmd.MarkFlagRequired("model")
	cmd.Flags().StringVar(&o.description, "description", "", "agent description")
	cmd.Flags().StringVar(&o.prompt, "prompt", "", "agent prompt")
	cmd.Flags().StringVar(&o.promptFile, "prompt-file", "", "read the agent prompt from a UTF-8 file")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	cmd.Flags().StringVar(&o.status, "status", "disabled", "initial agent status (enabled|disabled)")
	cmd.Flags().StringVar(&o.pluginsJSON, "plugins-json", "", "plugin configurations as a JSON array ("+jsonFlagForms+")")
	addOutputFlag(cmd, &o.output)
	return cmd
}

func runAgentCreate(key string, o agentCreateOptions) error {
	output, err := resolveOutputFormat(o.output)
	if err != nil {
		return err
	}
	req, err := agentCreateRequest(key, o)
	if err != nil {
		return err
	}
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}
	saved, err := client.CreateAgent(req)
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(saved)
	}
	fmt.Printf("Agent '%s' created successfully\n", saved.Key)
	fmt.Printf("%-10s %s\n%-10s %s\n%-10s %s\n", "App", saved.AppKey, "Model", saved.ModelKey, "Status", saved.Status)
	return nil
}

// agentCreateRequest 在请求前读取文件并检查基本形态，插件 Schema 与目录权限交服务端校验。
func agentCreateRequest(key string, o agentCreateOptions) (api.CreateAgentRequest, error) {
	req := api.CreateAgentRequest{
		Key: key, Name: defaultName(o.name, key), ModelKey: o.modelKey,
		AppKey: o.appKey, RuntimeKey: "make-runtime-service",
		Description: o.description, Prompt: o.prompt, Status: o.status,
	}
	if err := validResourceKey(key); err != nil {
		return req, err
	}
	if err := validAgentAppKey(req.AppKey); err != nil {
		return req, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.ModelKey) == "" {
		return req, fmt.Errorf("--name and --model must not be blank")
	}
	if o.status != "enabled" && o.status != "disabled" {
		return req, fmt.Errorf("--status must be enabled or disabled")
	}
	if o.promptFile != "" {
		raw, err := os.ReadFile(o.promptFile)
		if err != nil {
			return req, fmt.Errorf("--prompt-file: %w", err)
		}
		req.Prompt = string(raw)
	}
	if o.pluginsJSON != "" {
		if err := decodeJSONFlag("plugins-json", o.pluginsJSON, &req.Plugins); err != nil {
			return req, err
		}
		if req.Plugins == nil {
			return req, fmt.Errorf("--plugins-json must be a JSON array")
		}
	}
	return req, nil
}
