/**
 * [INPUT]: Build log API, go-git HEAD, shared profile/output/error helpers
 * [OUTPUT]: app logs with HEAD/commit SHA/task ID selection and bounded follow
 * [POS]: Read-only build log command; stdout is content/JSON, diagnostics use stderr
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

var (
	buildLogPollInterval = 3 * time.Second
	fullCommitSHA        = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
	errLogsInterrupted   = errors.New("build log reading interrupted")
)

type appLogsOptions struct {
	commitSHA string
	id        string
	cursor    string
	follow    bool
	timeout   time.Duration
	output    string
}

func newAppLogsCmd() *cobra.Command {
	opts := appLogsOptions{}
	command := &cobra.Command{
		Use:   "logs",
		Short: "Read build logs for a commit or build task",
		Long: `Read the BUILD log for the current Git HEAD in beta, an explicit full
commit SHA, or a build task ID. This command never pushes or starts a build.

--follow waits for a commit's task to appear, then reads new log content until
the build log stage ends. Log completion does not imply deployment success.
An explicit --id or --commit-sha also works outside a Git repository.`,
		Example: `  makecli app logs
  makecli app logs --follow --timeout 5m
  makecli app logs --commit-sha <fullSHA> --output json
  makecli app logs --id <buildTaskId> --cursor <cursor>`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := opts.validate(command); err != nil {
				return err
			}
			if opts.id == "" && opts.commitSHA == "" {
				repo, err := openRepo()
				if err != nil {
					return fmt.Errorf("read current Git HEAD: %w; use --commit-sha or --id outside a repository", err)
				}
				head, err := repo.Head()
				if err != nil {
					return fmt.Errorf("read current Git HEAD: %w; commit first or use --commit-sha / --id", err)
				}
				opts.commitSHA = head.Hash().String()
			}
			resolvedOutput = opts.output
			client, err := newClientFromProfile()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt)
			defer stop()
			if opts.follow {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, opts.timeout)
				defer cancel()
			}
			err = readAppLogs(ctx, client, opts, command.OutOrStdout(), command.ErrOrStderr())
			switch {
			case opts.follow && errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded):
				return fmt.Errorf("%w: build logs (%s); use --id or --cursor to resume", errWaitTimeout, opts.timeout)
			case errors.Is(err, context.Canceled):
				return errLogsInterrupted
			default:
				return err
			}
		},
	}
	command.Flags().StringVar(&opts.commitSHA, "commit-sha", "", "full commit SHA to query in beta (default: current HEAD)")
	command.Flags().StringVar(&opts.id, "id", "", "exact build task ID; bypasses commit lookup")
	command.Flags().StringVar(&opts.cursor, "cursor", "", "resume cursor returned by a previous log response")
	command.Flags().BoolVar(&opts.follow, "follow", false, "wait for the task and follow new build log content")
	command.Flags().DurationVar(&opts.timeout, "timeout", defaultWaitTimeout, "total task/log wait limit (requires --follow)")
	addOutputFlag(command, &opts.output)
	command.MarkFlagsMutuallyExclusive("id", "commit-sha")
	return command
}

// validate is pure with respect to the filesystem/network; invalid flags never
// resolve credentials, inspect Git, or send a request.
func (opts *appLogsOptions) validate(command *cobra.Command) error {
	if command.Flags().Changed("id") {
		id, err := strconv.ParseInt(opts.id, 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("--id must be a positive int64 build task ID")
		}
	}
	if command.Flags().Changed("commit-sha") && !fullCommitSHA.MatchString(opts.commitSHA) {
		return fmt.Errorf("--commit-sha must be a full 40-character hexadecimal commit SHA")
	}
	if opts.id != "" && opts.commitSHA != "" {
		return fmt.Errorf("--id and --commit-sha are mutually exclusive")
	}
	if opts.cursor != "" {
		cursor, err := strconv.ParseInt(opts.cursor, 10, 64)
		if err != nil || cursor < 0 {
			return fmt.Errorf("--cursor must be a non-negative int64 decimal string")
		}
	}
	if command.Flags().Changed("timeout") && !opts.follow {
		return fmt.Errorf("--timeout requires --follow")
	}
	if opts.follow && opts.timeout <= 0 {
		return fmt.Errorf("--timeout must be greater than zero")
	}
	if opts.follow && opts.output == outputJSON {
		return fmt.Errorf("--follow does not support --output json; omit --follow for a JSON snapshot")
	}
	if opts.follow && opts.output == outputAuto {
		opts.output = outputTable
	}
	format, err := resolveOutputFormat(opts.output)
	if err != nil {
		return err
	}
	opts.output = format
	opts.commitSHA = strings.ToLower(opts.commitSHA)
	return nil
}

type buildLogView struct {
	BuildTaskID string `json:"buildTaskId"`
	api.BuildLogChunk
}

// readAppLogs is the I/O boundary. It logs selection/wait/completion to stderr;
// errors propagate to the standard CLI error reporter without logging secrets.
func readAppLogs(ctx context.Context, client *api.Client, opts appLogsOptions, stdout, stderr io.Writer) error {
	_, _ = fmt.Fprintln(stderr, "Reading build logs...")
	id, err := resolveBuildLogTask(ctx, client, opts, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Build log task lookup failed.")
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Build: #%d\n", id)
	cursor := opts.cursor
	for {
		chunk, err := client.GetBuildLog(ctx, id, cursor)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Build log read failed (task #%d).\n", id)
			return err
		}
		if opts.follow {
			if err := checkBuildLogCursor(cursor, *chunk); err != nil {
				_, _ = fmt.Fprintf(stderr, "Build log cursor validation failed (task #%d).\n", id)
				return err
			}
		}
		if opts.output == outputJSON {
			return writeJSONTo(stdout, buildLogView{BuildTaskID: strconv.FormatInt(id, 10), BuildLogChunk: *chunk})
		}
		if _, err := io.WriteString(stdout, chunk.Content); err != nil {
			return fmt.Errorf("write build logs: %w", err)
		}
		if !opts.follow || chunk.Completed {
			_, _ = fmt.Fprintf(stderr, "Build logs read (task #%d, nextCursor=%s, completed=%t).\n", id, chunk.NextCursor, chunk.Completed)
			return nil
		}
		cursor = chunk.NextCursor
		if err := waitBuildLogPoll(ctx); err != nil {
			_, _ = fmt.Fprintf(stderr, "Build log follow stopped (task #%d, nextCursor=%s).\n", id, cursor)
			return err
		}
	}
}

// checkBuildLogCursor prevents a broken backend cursor from replaying output.
// The cursor is validated numerically but is always forwarded unchanged.
func checkBuildLogCursor(previous string, chunk api.BuildLogChunk) error {
	parse := func(value string) (int64, error) {
		if value == "" {
			return 0, nil
		}
		return strconv.ParseInt(value, 10, 64)
	}
	oldOffset, err := parse(previous)
	if err != nil {
		return fmt.Errorf("invalid previous build log cursor: %w", err)
	}
	nextOffset, err := parse(chunk.NextCursor)
	if err != nil || nextOffset < oldOffset || chunk.Content != "" && nextOffset == oldOffset {
		return fmt.Errorf("invalid build log response: cursor did not advance with content or moved backwards")
	}
	return nil
}

func resolveBuildLogTask(ctx context.Context, client *api.Client, opts appLogsOptions, stderr io.Writer) (int64, error) {
	if opts.id != "" {
		return strconv.ParseInt(opts.id, 10, 64)
	}
	waiting := false
	for {
		task, err := client.GetBuildTaskContext(ctx, opts.commitSHA, api.EnvBeta)
		if err == nil {
			return task.ID, nil
		}
		if !errors.Is(err, api.ErrNotFound) {
			return 0, err
		}
		if !opts.follow {
			return 0, fmt.Errorf("commit %s has no beta build task; deploy it first or use --follow to wait: %w", opts.commitSHA, err)
		}
		if !waiting {
			_, _ = fmt.Fprintf(stderr, "Waiting for beta build task of %s...\n", opts.commitSHA)
			waiting = true
		}
		if err := waitBuildLogPoll(ctx); err != nil {
			return 0, err
		}
	}
}

func waitBuildLogPoll(ctx context.Context) error {
	timer := time.NewTimer(buildLogPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
