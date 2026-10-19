/**
 * [INPUT]: Root CLI, logs deploy, subprocesses and unread stdout/stderr pipes
 * [OUTPUT]: Regression tests for stage deadlines, SIGINT and final error output under backpressure
 * [POS]: Process-lifetime cancellation tests; no real deployments
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A subprocess owns its blocked stdout writer; exiting the CLI must not wait for
// an unread pipe. Running this in-process would retain the blocked goroutine.
func TestLogsDeployPipeChild(t *testing.T) {
	if os.Getenv("MAKECLI_LOG_PIPE_CHILD") != "1" {
		return
	}
	stage := os.Getenv("MAKECLI_LOG_PIPE_STAGE")
	if stage == "" {
		stage = "build"
	}
	os.Args = []string{os.Args[0], "logs", "deploy", "--stage", stage, "--app", "test_app", "--commit-sha", strings.Repeat("a", 40), "--tail", "-1", "-f", "--timeout", os.Getenv("MAKECLI_LOG_PIPE_TIMEOUT")}
	if os.Getenv("MAKECLI_LOG_PIPE_DEBUG") == "1" {
		os.Args = append(os.Args, "--debug")
	}
	os.Exit(ExitCode(Execute("test", "")))
}

func TestLogsDeployBlockedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt delivery is not supported by Go on Windows")
	}
	for _, scenario := range []string{"timeout", "interrupt", "deploy-timeout", "deploy-interrupt", "all-timeout", "all-interrupt"} {
		t.Run(scenario, func(t *testing.T) {
			responseSent := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","phase":"DEPLOY"}}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": strings.Repeat("x", 1<<20), "nextCursor": logPipeCursor(r.URL.Path), "completed": true}})
				responseSent <- struct{}{}
			}))
			defer srv.Close()
			timeout := "400ms"
			want := 124
			if strings.HasSuffix(scenario, "interrupt") {
				timeout = "10s"
				want = 130
			}
			childCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestLogsDeployPipeChild$")
			stage := "build"
			if strings.HasPrefix(scenario, "deploy-") {
				stage = "deploy"
			}
			if strings.HasPrefix(scenario, "all-") {
				stage = "all"
			}
			child.Env = append(os.Environ(), "MAKECLI_LOG_PIPE_STAGE="+stage, "MAKECLI_LOG_PIPE_CHILD=1", "MAKE_CLI_UPDATE_NOTIFIER=0", "MAKECLI_LOG_PIPE_TIMEOUT="+timeout, EnvAccessToken+"=test-token", EnvMetaServerURL+"="+srv.URL, EnvContext+"=dev", "MAKE_CLI_CONFIG_DIR="+t.TempDir())
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdout.Close() }()
			child.Stderr = io.Discard
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-responseSent:
			case <-childCtx.Done():
				t.Fatal("no log response")
			}
			// Reading a single byte proves the child entered output without draining it.
			if _, err := io.ReadFull(stdout, make([]byte, 1)); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(scenario, "interrupt") {
				if err := child.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			}
			err = child.Wait()
			var exitErr *exec.ExitError
			if childCtx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != want {
				t.Fatalf("blocked %s: err=%v context=%v want=%d", scenario, err, childCtx.Err(), want)
			}
		})
	}
}

func logPipeCursor(path string) any {
	if strings.HasSuffix(path, "/deployment/events") {
		return 400
	}
	return "1048576"
}

func TestLogsDeployBlockedStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt delivery is not supported by Go on Windows")
	}
	for _, stage := range []string{"build", "deploy", "all"} {
		for _, mode := range []string{"debug-timeout", "debug-interrupt", "error-timeout"} {
			t.Run(stage+"/"+mode, func(t *testing.T) {
				responseSent := make(chan struct{}, 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/build") {
						_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","phase":"DEPLOY"}}`)
						return
					}
					if mode == "error-timeout" {
						_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": strings.Repeat("failure", 1<<18)})
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": strings.Repeat("x", 1<<20), "nextCursor": logPipeCursor(r.URL.Path), "completed": true}})
					}
					responseSent <- struct{}{}
				}))
				defer srv.Close()
				childCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestLogsDeployPipeChild$")
				timeout, want := "400ms", 124
				if mode == "debug-interrupt" {
					timeout, want = "10s", 130
				}
				debug := "0"
				if strings.HasPrefix(mode, "debug-") {
					debug = "1"
				}
				child.Env = append(os.Environ(), "MAKECLI_LOG_PIPE_CHILD=1", "MAKECLI_LOG_PIPE_STAGE="+stage, "MAKECLI_LOG_PIPE_TIMEOUT="+timeout, "MAKECLI_LOG_PIPE_DEBUG="+debug, "MAKE_CLI_UPDATE_NOTIFIER=0", EnvAccessToken+"=test-token", EnvMetaServerURL+"="+srv.URL, EnvContext+"=dev", "MAKE_CLI_CONFIG_DIR="+t.TempDir())
				child.Stdout = io.Discard
				stderr, err := child.StderrPipe()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = stderr.Close() }()
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				select {
				case <-responseSent:
				case <-childCtx.Done():
					t.Fatal("no log response")
				}
				// Prove the large diagnostic write started, then leave the remaining pipe unread.
				if _, err := io.ReadFull(stderr, make([]byte, 128<<10)); err != nil {
					t.Fatal(err)
				}
				if mode == "debug-interrupt" {
					if err := child.Process.Signal(os.Interrupt); err != nil {
						t.Fatal(err)
					}
				}
				err = child.Wait()
				var exitErr *exec.ExitError
				if childCtx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != want {
					t.Fatalf("stderr %s: err=%v context=%v want=%d", mode, err, childCtx.Err(), want)
				}
			})
		}
	}
}

func TestLogsDeployProcessErrorDiagnostics(t *testing.T) {
	for _, kind := range []string{"auth", "storage"} {
		t.Run(kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if kind == "auth" {
					_, _ = io.WriteString(w, `{"code":990300403,"msg":"invalid token"}`)
					return
				}
				_, _ = io.WriteString(w, `{"code":500,"msg":"storage unavailable"}`)
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLogsDeployPipeChild$")
			child.Env = append(os.Environ(), "MAKECLI_LOG_PIPE_CHILD=1", "MAKECLI_LOG_PIPE_TIMEOUT=1s", "MAKECLI_LOG_PIPE_DEBUG=0", "MAKE_CLI_UPDATE_NOTIFIER=0", EnvAccessToken+"=test-token", EnvMetaServerURL+"="+srv.URL, EnvContext+"=dev", "MAKE_CLI_CONFIG_DIR="+t.TempDir())
			output, err := child.CombinedOutput()
			var exitErr *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("err=%v output=%s", err, output)
			}
			text := string(output)
			if strings.Count(text, "error:") != 1 {
				t.Fatalf("expected exactly one error report: %s", text)
			}
			if kind == "auth" && !strings.Contains(text, "makecli login") {
				t.Fatalf("missing auth guidance: %s", text)
			}
			if kind == "storage" && !strings.Contains(text, "storage unavailable") {
				t.Fatalf("missing original error: %s", text)
			}
		})
	}
}
