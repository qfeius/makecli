/**
 * [INPUT]: 依赖 context、encoding/json、log/slog、time；协议与传输来自 protocol.go/client.go，执行契约来自 adapter 包
 * [OUTPUT]: 对外提供 executeRun——单 run 的完整生命周期：start → 读触发区间 → 执行 → 事件批量上报 → complete/fail
 * [POS]: internal/daemon 的执行编排——batch_seq 单调保证模糊重试不双写；中间文本映射为 status（最终答复才是 message，
 *        message 事件在状态面物化出站投递）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/qfeius/makecli/internal/daemon/adapter"
)

// eventFlushSize / eventFlushInterval 是事件批量上报的攒批阈值。
const (
	eventFlushSize     = 16
	eventFlushInterval = 2 * time.Second
)

// executeRun 执行一个已 claim 的 run。ctx 取消即取消执行（取消指令经
// 心跳 actions 到达后由 daemon 调 cancel）；cancelled 标记决定收尾 reason。
func (d *Daemon) executeRun(ctx context.Context, backend adapter.Backend, claim RunClaim, cancelled *bool) {
	logger := d.logger.With("run", claim.RunID, "session", claim.SessionID, "provider", backend.Provider())
	if err := d.client.UpdateRun(ctx, UpdateRunRequest{RunID: claim.RunID, Status: RunStatusRunning, LeaseToken: claim.LeaseToken}); err != nil {
		logger.Error("start run", "err", err)
		return // start 失败不 FailRun：lease 可能已被回收，留给 sweeper 处置
	}

	fail := func(status, reason, detail string) {
		logger.Warn("run failed", "status", status, "reason", reason, "detail", detail)
		if err := d.client.UpdateRun(context.WithoutCancel(ctx), UpdateRunRequest{
			RunID: claim.RunID, Status: status, LeaseToken: claim.LeaseToken, FailureReason: reason,
		}); err != nil {
			logger.Error("fail run receipt", "err", err)
		}
	}

	// 读触发区间（合并语义兑现：区间可能覆盖多条积压消息）。
	listed, err := d.client.ListEvents(ctx, ListEventsRequest{
		SessionID: claim.SessionID, From: claim.Trigger.FromSeq, To: claim.Trigger.ToSeq, Limit: 1000,
	})
	if err != nil {
		fail(RunStatusFailed, FailReasonCLICrash, "读取触发事件失败: "+err.Error())
		return
	}
	prompt := BuildPrompt(listed)
	if prompt == "" {
		prompt = "(空消息)"
	}

	workDir, resumable, err := PrepareWorkDir(d.workBaseDir, claim)
	if err != nil {
		fail(RunStatusFailed, FailReasonCLICrash, "准备工作目录失败: "+err.Error())
		return
	}
	resumeSessionID := claim.Resume.CLISessionID
	if !resumable {
		// resume 目录不可用（跨设备遗留路径）：连续性整体放弃，新会话起步。
		logger.Warn("resume 目录不可用,放弃连续性新会话执行", "staleWorkDir", claim.Resume.WorkDir)
		resumeSessionID = ""
	}

	session, err := backend.Execute(ctx, prompt, adapter.ExecOptions{
		WorkDir:         workDir,
		ResumeSessionID: resumeSessionID,
		MaxRunDuration:  d.maxRunDuration,
	})
	if err != nil {
		fail(RunStatusFailed, FailReasonCLICrash, "启动 CLI 失败: "+err.Error())
		return
	}

	reporter := &eventReporter{daemon: d, claim: claim, logger: logger}
	for message := range session.Messages {
		reporter.add(ctx, message)
	}
	result := <-session.Result
	reporter.finish(ctx, result)

	switch {
	case *cancelled:
		// 取消指令收尾：无论 CLI 以何种方式退出，终态都是 cancelled。
		fail(RunStatusCancelled, "", "")
	case result.IsError && ctx.Err() != nil:
		fail(RunStatusFailed, FailReasonTimeout, result.ErrorMessage)
	case result.IsError:
		fail(RunStatusFailed, FailReasonCLICrash, result.ErrorMessage)
	default:
		request := UpdateRunRequest{
			RunID: claim.RunID, Status: RunStatusCompleted, LeaseToken: claim.LeaseToken,
			CLISessionID: result.CLISessionID, WorkDir: workDir,
		}
		if result.Usage != nil {
			request.Usage = &Usage{
				InputTokens:         result.Usage.InputTokens,
				OutputTokens:        result.Usage.OutputTokens,
				CacheReadTokens:     result.Usage.CacheReadTokens,
				CacheCreationTokens: result.Usage.CacheCreationTokens,
			}
		}
		if err := d.client.UpdateRun(context.WithoutCancel(ctx), request); err != nil {
			logger.Error("complete run", "err", err)
			return
		}
		logger.Info("run completed")
	}
}

// eventReporter 攒批上报执行事件。batch_seq per-run 从 1 单调递增——
// 服务端以此幂等吸收模糊重试，绝不双写。
type eventReporter struct {
	daemon   *Daemon
	claim    RunClaim
	logger   *slog.Logger
	buffer   []NewEvent
	batchSeq int64
	lastSent time.Time
}

// add 归一并缓冲一条执行事件，满批或超时即冲刷。
// 中间助手文本映射为 status——最终答复（Result.Text）才是 message 事件，
// 出站投递只由 message 物化，群里不会收到每一步的碎片文本。
func (r *eventReporter) add(ctx context.Context, message adapter.Message) {
	event := NewEvent{Actor: Actor{Kind: "agent"}, RunID: r.claim.RunID}
	switch message.Type {
	case adapter.MessageThinking:
		event.Type = "thinking"
		event.Payload = mustJSON(map[string]string{"text": message.Text})
	case adapter.MessageText:
		event.Type = "status"
		event.Payload = mustJSON(map[string]string{"text": message.Text})
	case adapter.MessageStatus:
		event.Type = "status"
		event.Payload = mustJSON(map[string]string{"text": message.Text})
	case adapter.MessageToolUse:
		event.Type = "tool_use"
		event.Payload = mustJSON(map[string]any{"callID": message.CallID, "tool": message.Tool, "input": json.RawMessage(nonEmptyJSON(message.Input))})
	case adapter.MessageToolResult:
		event.Type = "tool_result"
		event.Payload = mustJSON(map[string]any{"callID": message.CallID, "output": truncate(message.Output, 16*1024), "isError": message.IsError})
	case adapter.MessageError:
		event.Type = "error"
		event.Payload = mustJSON(map[string]string{"code": "brain_error", "message": message.Text})
	default:
		return
	}
	r.buffer = append(r.buffer, event)
	if len(r.buffer) >= eventFlushSize || time.Since(r.lastSent) >= eventFlushInterval {
		r.flush(ctx)
	}
}

// finish 追加最终 message 事件（成功且有产出时）并冲刷余量。
func (r *eventReporter) finish(ctx context.Context, result adapter.Result) {
	if !result.IsError && result.Text != "" {
		r.buffer = append(r.buffer, NewEvent{
			Type: "message", Actor: Actor{Kind: "agent"}, RunID: r.claim.RunID,
			Payload: mustJSON(map[string]any{"blocks": []Block{{Kind: "text", Text: result.Text}}}),
		})
	}
	if result.IsError && result.ErrorMessage != "" {
		r.buffer = append(r.buffer, NewEvent{
			Type: "error", Actor: Actor{Kind: "agent"}, RunID: r.claim.RunID,
			Payload: mustJSON(map[string]string{"code": "brain_error", "message": result.ErrorMessage}),
		})
	}
	r.flush(ctx)
}

func (r *eventReporter) flush(ctx context.Context) {
	if len(r.buffer) == 0 {
		return
	}
	r.batchSeq++
	// 收尾冲刷必须在取消后仍可达——用不承继取消的 ctx。
	_, err := r.daemon.client.AppendEvents(context.WithoutCancel(ctx), CreateEventsRequest{
		SessionID:  r.claim.SessionID,
		LeaseToken: r.claim.LeaseToken,
		BatchSeq:   r.batchSeq,
		Events:     r.buffer,
	})
	if err != nil {
		// 上报失败不终止执行——事件流是尽力而为的可观测面，run 终态才是契约。
		r.logger.Warn("append events", "batch_seq", r.batchSeq, "err", err)
	}
	r.buffer = r.buffer[:0]
	r.lastSent = time.Now()
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func nonEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`null`)
	}
	return raw
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n…(截断)"
}
