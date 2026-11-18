/**
 * [INPUT]: Trace ID and Console runtime log API
 * [OUTPUT]: logs runtime command with ordered paginated log output
 * [POS]: Trace-only runtime log reader; no project or app context required
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

var runtimeTraceID = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func newLogsRuntimeCmd() *cobra.Command {
	var traceID, output string
	command := &cobra.Command{
		Use: "runtime", Short: "Read runtime logs for a trace", Args: cobra.NoArgs, SilenceUsage: true,
		Example: "  makecli logs runtime --trace-id 9615209586ba957b3423f1023fd2d601",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !runtimeTraceID.MatchString(traceID) || strings.Trim(traceID, "0") == "" {
				return fmt.Errorf("--trace-id must be a non-zero 32-character hexadecimal trace ID")
			}
			format, err := resolveOutputFormat(output)
			if err != nil {
				return err
			}
			resolvedOutput = format
			client, err := newClientFromProfile()
			if err != nil {
				return err
			}
			return readRuntimeLogs(cmd.Context(), client, strings.ToLower(traceID), format, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	command.Flags().StringVar(&traceID, "trace-id", "", "trace ID (required)")
	addOutputFlag(command, &output)
	return command
}

func readRuntimeLogs(ctx context.Context, client *api.Client, traceID, format string, stdout, stderr io.Writer) error {
	const size, maxOffset = 100, 10000
	rows := make([]map[string]any, 0)
	total := 0
	truncated := false
	for page := 1; ; page++ {
		if (page-1)*size > maxOffset {
			truncated = true
			break
		}
		result, err := client.ListRuntimeLogs(ctx, traceID, page, size)
		if err != nil {
			return err
		}
		total = result.Pagination.Total
		rows = append(rows, result.Data...)
		if len(rows) >= total {
			break
		}
		if len(result.Data) < size {
			truncated = true
			break
		}
	}
	if format == outputJSON {
		return writeJSONTo(stdout, map[string]any{"data": rows, "total": total, "truncated": truncated})
	}
	if len(rows) == 0 {
		if _, err := fmt.Fprintln(stdout, "No runtime logs returned."); err != nil {
			return err
		}
		if truncated {
			_, err := fmt.Fprintln(stderr, "Log results are incomplete; the result changed during paging.")
			return err
		}
		return nil
	}
	table := tablewriter.NewTable(stdout)
	table.Header("TIME", "SERVICE", "LEVEL", "MESSAGE")
	for _, row := range rows {
		content, _ := row["content"].(map[string]any)
		if err := table.Append([]string{runtimeLogText(row["occurredAt"]), runtimeLogText(row["serviceName"]), runtimeLogText(row["level"]), runtimeLogText(content["message"])}); err != nil {
			return err
		}
	}
	if err := table.Render(); err != nil {
		return err
	}
	if truncated {
		_, err := fmt.Fprintln(stderr, "Log results are incomplete; server pagination limit reached or the result changed during paging.")
		return err
	}
	return nil
}

func runtimeLogText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
