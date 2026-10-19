/**
 * [INPUT]: Build byte logs, deployment event pages and a pinned task identity
 * [OUTPUT]: deploy/all snapshots and follow with independent cursors and a final drain after task termination
 * [POS]: Stage orchestration; shares the command context and process output cancellation
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/qfeius/makecli/internal/api"
)

// A combined cursor records both cursor domains and binds them to one task.
type stageLogCursor struct {
	TaskID int64  `json:"task"`
	Build  string `json:"build"`
	Deploy uint64 `json:"deploy"`
}

func validateStageCursor(stage, cursor string) error {
	if stage == "all" {
		_, err := decodeStageCursor(cursor)
		return err
	}
	if stage == "deploy" {
		if _, err := strconv.ParseUint(cursor, 10, 64); err != nil {
			return fmt.Errorf("--cursor must be a deployment event sequence")
		}
		return nil
	}
	n, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || n < 0 {
		return fmt.Errorf("--cursor must be a non-negative int64 decimal string")
	}
	return nil
}

func decodeStageCursor(cursor string) (stageLogCursor, error) {
	var state stageLogCursor
	if len(cursor) > 1024 || !strings.HasPrefix(cursor, "all:") {
		return state, fmt.Errorf("--cursor for --stage all must be a nextCursor returned by --stage all")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "all:"))
	if err == nil {
		err = json.Unmarshal(raw, &state)
	}
	if err != nil || state.TaskID <= 0 || validateStageCursor("build", state.Build) != nil {
		return state, fmt.Errorf("invalid combined log cursor")
	}
	return state, nil
}

func (state stageLogCursor) encode(stage string) string {
	if stage == "deploy" {
		return strconv.FormatUint(state.Deploy, 10)
	}
	// This struct contains only integers and a string; JSON marshaling cannot fail.
	raw, _ := json.Marshal(state)
	return "all:" + base64.RawURLEncoding.EncodeToString(raw)
}

func initialStageCursor(taskID int64, opts logsDeployOptions) (stageLogCursor, error) {
	state := stageLogCursor{TaskID: taskID, Build: "0"}
	if opts.cursor == "" {
		return state, nil
	}
	if opts.stage == "deploy" {
		cursor, err := strconv.ParseUint(opts.cursor, 10, 64)
		state.Deploy = cursor
		return state, err
	}
	state, err := decodeStageCursor(opts.cursor)
	if err != nil {
		return state, err
	}
	if state.TaskID != taskID {
		return state, fmt.Errorf("combined log cursor belongs to another build task")
	}
	return state, nil
}

type stageLogReader struct {
	client        *api.Client
	task          api.BuildTask
	opts          logsDeployOptions
	cursor        stageLogCursor
	buildDone     bool
	initialBuild  bool
	initialDeploy bool
}

// readDeploymentStages shares one deadline across build and deployment reads.
func readDeploymentStages(ctx context.Context, client *api.Client, task api.BuildTask, opts logsDeployOptions, stdout, stderr io.Writer) error {
	state, err := initialStageCursor(task.ID, opts)
	if err != nil {
		return err
	}
	reader := stageLogReader{client: client, task: task, opts: opts, cursor: state, buildDone: opts.stage == "deploy", initialBuild: opts.cursor == "", initialDeploy: opts.cursor == ""}
	for {
		content, completed, readErr := reader.read(ctx, stdout)
		if readErr != nil {
			_, _ = fmt.Fprintf(stderr, "%s log read failed (task #%d).\n", opts.stage, task.ID)
			return readErr
		}
		cursor := reader.cursor.encode(opts.stage)
		if opts.output == outputJSON {
			return writeJSONTo(stdout, buildLogView{AppKey: opts.appKey, CommitSHA: opts.commitSHA, Stage: opts.stage, BuildLogChunk: api.BuildLogChunk{Content: content, NextCursor: cursor, Completed: completed}})
		}
		if !opts.follow || completed {
			_, _ = fmt.Fprintf(stderr, "%s logs read (task #%d, nextCursor=%s, completed=%t).\n", opts.stage, task.ID, cursor, completed)
			return nil
		}
		if err := waitBuildLogPoll(ctx); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s log follow stopped (task #%d, nextCursor=%s).\n", opts.stage, task.ID, cursor)
			return err
		}
	}
}

func (reader *stageLogReader) read(ctx context.Context, stdout io.Writer) (string, bool, error) {
	buildContent := ""
	if !reader.buildDone {
		chunk, err := readStageBuild(ctx, reader.client, reader.task.ID, reader.cursor.Build, reader.opts.tail, reader.initialBuild)
		if err != nil {
			return "", false, fmt.Errorf("read build stage: %w", err)
		}
		if err := reader.write(stdout, chunk.Content); err != nil {
			return "", false, err
		}
		reader.cursor.Build, reader.buildDone, reader.initialBuild = chunk.NextCursor, chunk.Completed, false
		if reader.cursor.Build == "" {
			reader.cursor.Build = "0"
		}
		buildContent = chunk.Content
	}
	if !reader.buildDone && reader.opts.follow {
		return buildContent, false, nil
	}
	chunk, err := readStageDeployment(ctx, reader.client, reader.task, reader.cursor.Deploy, reader.opts.tail, reader.initialDeploy)
	if err != nil {
		return "", false, fmt.Errorf("read deploy stage: %w", err)
	}
	if err := reader.write(stdout, chunk.Content); err != nil {
		return "", false, err
	}
	reader.cursor.Deploy, reader.initialDeploy = chunk.NextCursor, false
	return buildContent + chunk.Content, reader.buildDone && chunk.Completed, nil
}

func (reader *stageLogReader) write(stdout io.Writer, content string) error {
	if reader.opts.output == outputJSON {
		return nil
	}
	if _, err := io.WriteString(stdout, content); err != nil {
		return fmt.Errorf("write deployment logs: %w", err)
	}
	return nil
}

func readStageBuild(ctx context.Context, client *api.Client, id int64, cursor string, tail int64, initial bool) (*api.BuildLogChunk, error) {
	options := api.BuildLogOptions{Cursor: cursor}
	if initial {
		options.Cursor = ""
		if tail >= 0 {
			options.TailLines = &tail
		}
	}
	chunk, err := client.GetBuildLogWithOptions(ctx, id, options)
	if err != nil {
		return nil, err
	}
	if err := checkBuildLogCursor(options.Cursor, *chunk); err != nil {
		return nil, err
	}
	return chunk, nil
}

// readStageDeployment drains existing pages before applying tail. Resume requests
// retain the event sequence even when tail zero suppresses all initial content.
// An observed terminal task requires a subsequent event read before completion.
func readStageDeployment(ctx context.Context, client *api.Client, task api.BuildTask, cursor uint64, tail int64, initial bool) (*api.DeploymentLogChunk, error) {
	if task.Finished() && task.Phase != "DEPLOY" {
		return &api.DeploymentLogChunk{NextCursor: cursor, Completed: true}, nil
	}
	history := ""
	terminalObserved := task.Finished()
	for {
		chunk, err := client.GetDeploymentLog(ctx, task, cursor)
		if err != nil {
			return nil, err
		}
		if chunk.NextCursor < cursor || chunk.Content != "" && chunk.NextCursor == cursor {
			return nil, fmt.Errorf("invalid deployment log response: event cursor did not advance or moved backwards")
		}
		history += chunk.Content
		if initial && tail >= 0 {
			history = tailLogLines(history, tail)
		}
		cursor = chunk.NextCursor
		if chunk.Completed || chunk.Content == "" {
			completed := chunk.Completed || terminalObserved
			if !completed {
				current, statusErr := client.GetBuildTaskByIDContext(ctx, task.ID)
				if statusErr != nil {
					return nil, statusErr
				}
				if current.Finished() {
					terminalObserved = true
					continue
				}
			}
			content := history
			if initial {
				content = tailLogLines(content, tail)
			}
			return &api.DeploymentLogChunk{Content: content, NextCursor: cursor, Completed: completed}, nil
		}
	}
}

func tailLogLines(content string, lines int64) string {
	if lines < 0 {
		return content
	}
	if lines == 0 {
		return ""
	}
	end := len(content)
	if end > 0 && content[end-1] == '\n' {
		end--
	}
	for index := end - 1; index >= 0; index-- {
		if content[index] == '\n' {
			lines--
			if lines == 0 {
				return content[index+1:]
			}
		}
	}
	return content
}
