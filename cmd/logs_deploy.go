/**
 * [INPUT]: Build log API with server-side tail, app manifest, Git HEAD and CLI output helpers
 * [OUTPUT]: logs deploy with app/SHA selection, initial tail and bounded follow
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

type logsDeployOptions struct {
	appKey    string
	stage     string
	tail      int64
	commitSHA string
	cursor    string
	follow    bool
	timeout   time.Duration
	output    string
}

func newLogsCmd() *cobra.Command {
	command := &cobra.Command{Use: "logs", Short: "Read Make logs"}
	command.AddCommand(newLogsDeployCmd())
	return command
}

func newLogsDeployCmd() *cobra.Command {
	opts := logsDeployOptions{}
	command := &cobra.Command{
		Use:   "deploy",
		Short: "Read build and deployment logs",
		Long: `Read beta deployment logs for an app and commit. The current release
supports --stage build. App and commit default to apps/dsl/app.yaml and Git HEAD.

--tail limits the initial history on the server. --follow then reads new content
until the stage completes or --timeout expires.`,
		Example: `  makecli logs deploy
  makecli logs deploy -f --timeout 3m --tail 100
  makecli logs deploy --app <appKey> --commit-sha <fullSHA>
  makecli logs deploy --commit-sha <fullSHA> --cursor <cursor>`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := opts.validate(command); err != nil {
				return err
			}
			if opts.appKey == "" {
				key, err := appKeyFromManifest(appDSLPath)
				if err != nil {
					return fmt.Errorf("read app identity: %w; use --app outside an app project", err)
				}
				opts.appKey = key
			}
			if opts.commitSHA == "" {
				repo, err := openRepo()
				if err != nil {
					return fmt.Errorf("read current Git HEAD: %w; use --commit-sha outside a repository", err)
				}
				head, err := repo.Head()
				if err != nil {
					return fmt.Errorf("read current Git HEAD: %w; commit first or use --commit-sha", err)
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
			err = readLogsCommand(ctx, client, opts, command.OutOrStdout(), command.ErrOrStderr())
			switch {
			case opts.follow && errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded):
				return fmt.Errorf("%w: build logs (%s); retry with --commit-sha %s", errWaitTimeout, opts.timeout, opts.commitSHA)
			case errors.Is(err, context.Canceled):
				return errLogsInterrupted
			default:
				return err
			}
		},
	}
	command.Flags().StringVar(&opts.appKey, "app", "", "app key (default: apps/dsl/app.yaml)")
	command.Flags().StringVar(&opts.commitSHA, "commit-sha", "", "full commit SHA to query in beta (default: current HEAD)")
	command.Flags().StringVar(&opts.stage, "stage", "build", "log stage (currently: build)")
	command.Flags().Int64Var(&opts.tail, "tail", 200, "initial history lines; 0 skips history, -1 reads all")
	command.Flags().StringVar(&opts.cursor, "cursor", "", "resume cursor returned by a previous log response")
	command.Flags().BoolVarP(&opts.follow, "follow", "f", false, "wait for the task and follow new log content")
	command.Flags().DurationVar(&opts.timeout, "timeout", 3*time.Minute, "total follow limit, e.g. 3m or 180s (requires --follow)")
	addOutputFlag(command, &opts.output)
	return command
}

// A CLI process owns its stdout writer. One worker keeps a blocked pipe from
// preventing cancellation; main exits with 124/130 without waiting on that pipe.
// Injected writers run synchronously so callers retain ownership and no worker
// can outlive an in-process command or continue writing into a reused buffer.
func readLogsCommand(ctx context.Context, client *api.Client, opts logsDeployOptions, stdout, stderr io.Writer) error {
	file, processOutput := stdout.(*os.File)
	if !processOutput || file != os.Stdout {
		return readDeployLogs(ctx, client, opts, stdout, stderr)
	}
	result := make(chan error, 1)
	go func() { result <- readDeployLogs(ctx, client, opts, stdout, stderr) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-result:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
}

// validate is pure with respect to the filesystem/network; invalid flags never
// resolve credentials, inspect Git, or send a request.
func (opts *logsDeployOptions) validate(command *cobra.Command) error {
	if command.Flags().Changed("app") {
		if err := validResourceKey(opts.appKey); err != nil {
			return fmt.Errorf("--app: %w", err)
		}
	}
	if command.Flags().Changed("commit-sha") && !fullCommitSHA.MatchString(opts.commitSHA) {
		return fmt.Errorf("--commit-sha must be a full 40-character hexadecimal commit SHA")
	}
	if opts.stage != "build" {
		return fmt.Errorf("--stage %q is not supported yet; use build", opts.stage)
	}
	if opts.tail < -1 {
		return fmt.Errorf("--tail must be -1 or a non-negative line count")
	}
	if command.Flags().Changed("tail") && command.Flags().Changed("cursor") {
		return fmt.Errorf("--tail and --cursor are mutually exclusive")
	}
	if command.Flags().Changed("cursor") {
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
	AppKey    string `json:"appKey"`
	CommitSHA string `json:"commitSha"`
	Stage     string `json:"stage"`
	api.BuildLogChunk
}

// readDeployLogs is the I/O boundary. It logs selection/wait/completion to stderr;
// errors propagate to the standard CLI error reporter without logging secrets.
func readDeployLogs(ctx context.Context, client *api.Client, opts logsDeployOptions, stdout, stderr io.Writer) error {
	_, _ = fmt.Fprintln(stderr, "Reading build logs...")
	id, err := resolveBuildLogTask(ctx, client, opts, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Build log task lookup failed.")
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Build: #%d\n", id)
	cursor := opts.cursor
	var tail *int64
	if opts.cursor == "" && opts.tail >= 0 {
		tail = &opts.tail
	}
	for {
		chunk, err := client.GetBuildLogWithOptions(ctx, id, api.BuildLogOptions{Cursor: cursor, TailLines: tail})
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
			return writeJSONTo(stdout, buildLogView{AppKey: opts.appKey, CommitSHA: opts.commitSHA, Stage: opts.stage, BuildLogChunk: *chunk})
		}
		if _, err := io.WriteString(stdout, chunk.Content); err != nil {
			return fmt.Errorf("write build logs: %w", err)
		}
		if !opts.follow || chunk.Completed {
			_, _ = fmt.Fprintf(stderr, "Build logs read (task #%d, nextCursor=%s, completed=%t).\n", id, chunk.NextCursor, chunk.Completed)
			return nil
		}
		cursor = chunk.NextCursor
		tail = nil
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

func resolveBuildLogTask(ctx context.Context, client *api.Client, opts logsDeployOptions, stderr io.Writer) (int64, error) {
	waiting := false
	for {
		task, err := client.GetBuildTaskContext(ctx, opts.commitSHA, api.EnvBeta)
		if err == nil {
			if task.ID <= 0 {
				return 0, fmt.Errorf("invalid build task response: missing task ID")
			}
			if task.AppKey != opts.appKey {
				app, appErr := client.GetAppContext(ctx, opts.appKey)
				if appErr != nil {
					return 0, fmt.Errorf("resolve beta app: %w", appErr)
				}
				betaKey, appErr := app.KeyForEnv(api.EnvBeta)
				if appErr != nil {
					return 0, appErr
				}
				if task.AppKey != betaKey {
					return 0, fmt.Errorf("commit %s has no build task for app %s", opts.commitSHA, opts.appKey)
				}
			}
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
