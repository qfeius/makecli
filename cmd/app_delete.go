/**
 * [INPUT]: 依赖 cmd/client、cmd/app、internal/api、delete_confirm.go、errors、fmt、strings、time、context、cobra
 * [OUTPUT]: 提供 app delete --appRole beta|prod|all；内部查询异步删除状态，确认终态后返回
 * [POS]: cmd/app 的 delete 子命令；all 先等 Beta 成功，再发起 Prod 删除
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

const (
	appRoleAll              = "all"
	defaultAppDeleteTimeout = 30 * time.Minute
)

// Tests shorten this interval; production polls only while the status is not terminal.
var appDeletePollInterval = 3 * time.Second

var errAppDeleteFailed = errors.New("删除 App 失败")

// confirmDeleteFunc lets tests isolate the interactive confirmation.
var confirmDeleteFunc = confirmDeleteByTypingKey

func newAppDeleteCmd() *cobra.Command {
	var file string
	var appRole string
	var yes bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "delete [key] --appRole beta|prod|all",
		Short: "Delete an app on Make",
		Long: `Delete one half of a Make app pair, or both in order.
--appRole beta deletes the paired Beta app, --appRole prod deletes the Prod app,
and --appRole all waits for Beta to finish before deleting Prod.
<key> is always the Prod app key. The command waits for the final deletion result.`,
		Example: `  makecli app delete myapp --appRole beta
  makecli app delete myapp --appRole prod --yes
  makecli app delete myapp --appRole all
  makecli app delete -f app.yaml --appRole beta`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppDeleteFromFile(file, appRole, yes, timeout)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires app key or -f flag")
			}
			return runAppDelete(args[0], appRole, yes, timeout)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "path to YAML file containing Make.App resource")
	cmd.Flags().StringVar(&appRole, "appRole", "", "App role to delete: beta | prod | all (required)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the deletion confirmation prompt")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultAppDeleteTimeout, "maximum time to wait for deletion to finish")
	_ = cmd.MarkFlagRequired("appRole")
	return cmd
}

func runAppDeleteFromFile(path, appRole string, skipConfirm bool, timeout time.Duration) error {
	manifest, err := loadAppManifestFromFile(path)
	if err != nil {
		return err
	}
	return runAppDelete(manifest.Key, appRole, skipConfirm, timeout)
}

// resolveDeleteRoles returns the order in which the server allows the pair to be deleted.
func resolveDeleteRoles(appRole string) ([]string, error) {
	switch strings.ToLower(appRole) {
	case api.RoleProd:
		return []string{api.RoleProd}, nil
	case api.RoleBeta:
		return []string{api.RoleBeta}, nil
	case appRoleAll:
		return []string{api.RoleBeta, api.RoleProd}, nil
	default:
		return nil, fmt.Errorf("invalid --appRole %q: must be beta, prod or all", appRole)
	}
}

func runAppDelete(key, appRole string, skipConfirm bool, timeout time.Duration) error {
	roles, err := resolveDeleteRoles(appRole)
	if err != nil {
		return err
	}
	if timeout <= 0 {
		return fmt.Errorf("--timeout must be greater than zero")
	}
	if !skipConfirm {
		if err := confirmDeleteFunc(key, strings.ToLower(appRole)); err != nil {
			return err
		}
	}

	deadline := time.Now().Add(timeout)
	for _, role := range roles {
		if time.Until(deadline) <= 0 {
			return fmt.Errorf("%w：删除任务可能仍在执行，尚不能确认成功", errWaitTimeout)
		}
		client, err := newClientFromProfile(api.WithAppRole(role))
		if err != nil {
			return err
		}
		if err := client.DeleteApp(key); err != nil {
			return err
		}
		fmt.Printf("App '%s' %s deletion started\n", key, role)
		if err := waitForAppDeletion(client, key, role, time.Until(deadline)); err != nil {
			return err
		}
		fmt.Printf("App '%s' %s deleted successfully\n", key, role)
	}
	return nil
}

func waitForAppDeletion(client *api.Client, key, role string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(appDeletePollInterval)
	defer ticker.Stop()
	lastProgress := ""
	for {
		status, err := client.GetAppDeletionStatusContext(ctx, key)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%w：%s 删除任务可能仍在执行，尚不能确认成功", errWaitTimeout, role)
			}
			return fmt.Errorf("查询 %s 删除状态失败: %w", role, err)
		}
		progress := status.StepName
		if progress == "" {
			progress = status.State
		}
		if progress != lastProgress {
			fmt.Printf("  %s: %s\n", role, progress)
			lastProgress = progress
		}
		switch status.State {
		case "SUCCEEDED":
			return nil
		case "FAILED":
			details := []string{fmt.Sprintf("%s（%s）", key, role)}
			step := status.StepName
			if status.Step != "" {
				if step == "" {
					step = status.Step
				} else {
					step += "（" + status.Step + "）"
				}
			}
			if step != "" {
				details = append(details, "失败步骤："+step)
			}
			if status.Message != "" {
				details = append(details, "原因："+status.Message)
			}
			return fmt.Errorf("%w：%s", errAppDeleteFailed, strings.Join(details, "；"))
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w：%s 删除任务可能仍在执行，尚不能确认成功", errWaitTimeout, role)
		case <-ticker.C:
		}
	}
}

func confirmDeleteByTypingKey(key, role string) error {
	return confirmDeletionByTypingKey(key, fmt.Sprintf("%s app %q", role, key))
}
