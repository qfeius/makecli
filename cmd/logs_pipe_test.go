/**
 * [INPUT]: logs deploy, subprocesses and an unread stdout pipe
 * [OUTPUT]: Regression tests for timeout and SIGINT while output is blocked
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
	command := newLogsCmd()
	command.SetOut(os.Stdout)
	command.SetErr(os.Stderr)
	command.SetArgs([]string{"deploy", "--app", "test_app", "--commit-sha", strings.Repeat("a", 40), "--tail", "-1", "-f", "--timeout", os.Getenv("MAKECLI_LOG_PIPE_TIMEOUT")})
	os.Exit(ExitCode(command.Execute()))
}

func TestLogsDeployBlockedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt delivery is not supported by Go on Windows")
	}
	for _, scenario := range []string{"timeout", "interrupt"} {
		t.Run(scenario, func(t *testing.T) {
			responseSent := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"appKey":"test_app"}}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": strings.Repeat("x", 1<<20), "nextCursor": "1048576", "completed": true}})
				responseSent <- struct{}{}
			}))
			defer srv.Close()
			timeout := "400ms"
			want := 124
			if scenario == "interrupt" {
				timeout = "10s"
				want = 130
			}
			childCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestLogsDeployPipeChild$")
			child.Env = append(os.Environ(), "MAKECLI_LOG_PIPE_CHILD=1", "MAKECLI_LOG_PIPE_TIMEOUT="+timeout, EnvAccessToken+"=test-token", EnvMetaServerURL+"="+srv.URL, EnvContext+"=dev", "MAKE_CLI_CONFIG_DIR="+t.TempDir())
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
			if scenario == "interrupt" {
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
