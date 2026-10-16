/**
 * [INPUT]: App logs command, httptest and local go-git test fixtures
 * [OUTPUT]: Task selection, cursor streaming, output, cancellation and read-only behavior tests
 * [POS]: app logs command integration tests; no real deployments
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setupAppLogsTest(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	stubMetaServer(t, srv.URL)
	setProfile(t, "default")
	setContextFlag(t, "dev")
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv(EnvAccessToken, "test-token")
	chdir(t, t.TempDir())
	old := buildLogPollInterval
	buildLogPollInterval = time.Millisecond
	t.Cleanup(func() { buildLogPollInterval = old })
}

func executeAppLogsTest(ctx context.Context, args ...string) (string, string, error) {
	command := newAppLogsCmd()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func TestAppLogsRegistered(t *testing.T) {
	command, _, err := newAppCmd().Find([]string{"logs"})
	if err != nil || command.Name() != "logs" {
		t.Fatalf("app logs not registered: %v", err)
	}
}

func TestAppLogsExplicitIDSnapshot(t *testing.T) {
	calls := 0
	setupAppLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/make/build/v1/build-log" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["buildTaskId"] != "42" || body["cursor"] != "13" || body["scope"] != "BUILD" {
			t.Errorf("unexpected body: %#v", body)
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"content":"existing logs","nextCursor":"26","completed":false}}`))
	})
	stdout, stderr, err := executeAppLogsTest(context.Background(), "--id", "42", "--cursor", "13", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		BuildTaskID         string `json:"buildTaskId"`
		Content, NextCursor string
		Completed           bool
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v; %s", err, stdout)
	}
	if calls != 1 || result.BuildTaskID != "42" || result.Content != "existing logs" || result.NextCursor != "26" || result.Completed {
		t.Errorf("unexpected result: %s", stdout)
	}
	if strings.Contains(stderr, "test-token") {
		t.Error("token leaked")
	}
}

func TestAppLogsCommitSelection(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "HEAD with dirty worktree", true: "explicit SHA outside repository"}[explicit], func(t *testing.T) {
			sha := strings.Repeat("a", 40)
			queries, logCalls := 0, 0
			setupAppLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/make/build/v1/build" {
					queries++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["commitSha"] != sha || body["environment"] != "beta" {
						t.Errorf("unexpected selection: %#v", body)
					}
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"status":"FAILED"}}`))
					return
				}
				if r.URL.Path != "/api/make/build/v1/build-log" {
					t.Errorf("must not push or query other resources: %s", r.URL)
				}
				logCalls++
				_, _ = w.Write([]byte(`{"code":200,"data":{"content":"failure details","nextCursor":"15","completed":true}}`))
			})
			args := []string{"--output", "table"}
			if explicit {
				args = append(args, "--commit-sha", sha)
			} else {
				writeTestFile(t, "tracked.txt", []byte("original"))
				gitCommitAll(t)
				sha = headSha(t)
				writeTestFile(t, "tracked.txt", []byte("uncommitted changes"))
			}
			stdout, _, err := executeAppLogsTest(context.Background(), args...)
			if err != nil || ExitCode(err) != 0 || stdout != "failure details" || queries != 1 || logCalls != 1 {
				t.Fatalf("read logs: %q, queries=%d logs=%d err=%v", stdout, queries, logCalls, err)
			}
		})
	}
}

func TestAppLogsFollowPinsTaskAndForwardsCursor(t *testing.T) {
	queries, reads := 0, 0
	setupAppLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/build/v1/build" {
			queries++
			if queries == 1 {
				_, _ = w.Write([]byte(`{"code":404,"msg":"not created"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":42}}`))
			return
		}
		reads++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["buildTaskId"] != "42" {
			t.Errorf("task changed: %#v", body)
		}
		responses := []string{
			`{"code":200,"data":{"content":"","nextCursor":"","completed":false}}`,
			`{"code":200,"data":{"content":"编译","nextCursor":"6","completed":false}}`,
			`{"code":200,"data":{"content":"done","nextCursor":"10","completed":true}}`,
		}
		if reads == 3 && body["cursor"] != "6" {
			t.Errorf("cursor must be forwarded, got %#v", body)
		}
		if reads > len(responses) {
			t.Error("polled after completion")
			return
		}
		_, _ = w.Write([]byte(responses[reads-1]))
	})
	stdout, stderr, err := executeAppLogsTest(context.Background(), "--commit-sha", strings.Repeat("a", 40), "--follow", "--timeout", "1s")
	if err != nil || stdout != "编译done" || queries != 2 || reads != 3 {
		t.Fatalf("out=%q queries=%d reads=%d err=%v", stdout, queries, reads, err)
	}
	if !strings.Contains(stderr, "42") {
		t.Errorf("task identity missing from stderr: %s", stderr)
	}
}

func TestAppLogsValidationBeforeIO(t *testing.T) {
	cases := [][]string{
		{"--id", "-1"}, {"--id", "0"}, {"--id", "9223372036854775808"}, {"--id", ""},
		{"--commit-sha", "abc"}, {"--commit-sha", ""},
		{"--id", "42", "--commit-sha", strings.Repeat("a", 40)},
		{"--id", "42", "--cursor", "-1"}, {"--id", "42", "--cursor", "invalid"}, {"--id", "42", "--cursor", "9223372036854775808"},
		{"--id", "42", "--follow", "--output", "json"}, {"--id", "42", "--timeout", "1s"}, {"--id", "42", "--follow", "--timeout", "0s"},
		{"--id", "42", "--output", "yaml"}, {"unexpected"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setupAppLogsTest(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("validation must precede network access") })
			stdout, _, err := executeAppLogsTest(context.Background(), args...)
			if err == nil || stdout != "" {
				t.Fatalf("invalid args accepted: out=%q err=%v", stdout, err)
			}
		})
	}
}

func TestAppLogsErrorsAndTimeout(t *testing.T) {
	for _, scenario := range []string{"missing snapshot", "missing explicit ID", "auth", "storage", "missing task timeout", "empty log timeout", "in-flight timeout", "snapshot deadline", "cancel request"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "snapshot deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 15*time.Millisecond)
				defer stop()
			}
			setupAppLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				switch scenario {
				case "missing snapshot", "missing explicit ID", "missing task timeout":
					_, _ = w.Write([]byte(`{"code":404,"msg":"not found"}`))
				case "auth":
					_, _ = w.Write([]byte(`{"code":990300403,"msg":"token invalid"}`))
				case "storage":
					_, _ = w.Write([]byte(`{"code":500,"msg":"storage failed"}`))
				case "empty log timeout":
					_, _ = w.Write([]byte(`{"code":200,"data":{"content":"","nextCursor":"","completed":false}}`))
				case "cancel request":
					cancel()
					<-r.Context().Done()
				case "in-flight timeout", "snapshot deadline":
					<-r.Context().Done()
				}
			})
			args := []string{"--id", "42", "--output", "table"}
			wantCode := 1
			switch scenario {
			case "missing snapshot":
				args = []string{"--commit-sha", strings.Repeat("a", 40)}
			case "missing explicit ID":
				args = append(args, "--follow", "--timeout", "1s")
			case "missing task timeout":
				args = []string{"--commit-sha", strings.Repeat("a", 40), "--follow", "--timeout", "15ms"}
				wantCode = 124
			case "empty log timeout", "in-flight timeout":
				args = append(args, "--follow", "--timeout", "15ms")
				wantCode = 124
			case "cancel request":
				args = append(args, "--follow")
				wantCode = 130
			}
			stdout, _, err := executeAppLogsTest(ctx, args...)
			if err == nil || stdout != "" || ExitCode(err) != wantCode {
				t.Fatalf("out=%q err=%v code=%d want=%d", stdout, err, ExitCode(err), wantCode)
			}
			if (scenario == "missing snapshot" || scenario == "missing explicit ID" || scenario == "auth" || scenario == "storage") && calls != 1 {
				t.Errorf("must fail without retries, calls=%d", calls)
			}
		})
	}
}

func TestAppLogsFollowRejectsBrokenCursor(t *testing.T) {
	for _, next := range []string{"invalid", "-1", "5", "4"} {
		t.Run(next, func(t *testing.T) {
			calls := 0
			setupAppLogsTest(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": "duplicate", "nextCursor": next, "completed": false}})
			})
			stdout, _, err := executeAppLogsTest(context.Background(), "--id", "42", "--cursor", "5", "--follow", "--timeout", "15ms")
			if err == nil || ExitCode(err) != 1 || stdout != "" || calls != 1 {
				t.Fatalf("invalid progress must fail before repeating output: out=%q calls=%d err=%v", stdout, calls, err)
			}
		})
	}
}

func TestAppLogsAutoOutput(t *testing.T) {
	for _, follow := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot JSON", true: "follow plain text"}[follow], func(t *testing.T) {
			setupAppLogsTest(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"code":200,"data":{"content":"done","nextCursor":"4","completed":true}}`))
			})
			old := stdoutIsTerminal
			stdoutIsTerminal = func() bool { return false }
			t.Cleanup(func() { stdoutIsTerminal = old })
			args := []string{"--id", "42"}
			if follow {
				args = append(args, "--follow")
			}
			stdout, stderr, err := executeAppLogsTest(context.Background(), args...)
			if err != nil {
				t.Fatal(err)
			}
			if follow {
				if stdout != "done" {
					t.Errorf("follow output = %q", stdout)
				}
			} else if !json.Valid([]byte(stdout)) {
				t.Errorf("snapshot is not JSON: %s", stdout)
			}
			if strings.Contains(stderr, "test-token") {
				t.Error("credential leaked")
			}
		})
	}
}

type brokenLogWriter struct{}

func (brokenLogWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAppLogsOutputFailureStopsFollow(t *testing.T) {
	calls := 0
	setupAppLogsTest(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"code":200,"data":{"content":"logs","nextCursor":"4","completed":false}}`))
	})
	command := newAppLogsCmd()
	command.SetOut(brokenLogWriter{})
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--id", "42", "--follow"})
	err := command.Execute()
	if !errors.Is(err, io.ErrClosedPipe) || calls != 1 {
		t.Fatalf("writer failure not propagated: %v calls=%d", err, calls)
	}
}
