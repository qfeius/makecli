/**
 * [INPUT]: Deployment events and pinned task status HTTP fixtures
 * [OUTPUT]: Regression tests for terminal-observation drain, tail, errors and deadlines
 * [POS]: logs deploy terminal-event delivery contracts for deploy/all
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLogsDeployTerminalDrainIncludesFinalPages(t *testing.T) {
	for _, stage := range []string{"deploy", "all"} {
		for _, status := range []string{"SUCCESS", "FAILED", "CANCELED"} {
			for _, follow := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/follow=%t", stage, status, follow), func(t *testing.T) {
					reads, statusChecks := 0, 0
					setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						switch {
						case strings.HasSuffix(r.URL.Path, "/build"):
							taskStatus := "RUNNING"
							if body["id"] != nil {
								if body["id"] != "42" || body["commitSha"] != nil {
									t.Errorf("task was not pinned: %v", body)
								}
								statusChecks++
								taskStatus = status
							}
							_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"id": 42, "orgId": 1001, "appKey": "test_app", "status": taskStatus, "phase": "DEPLOY"}})
						case strings.HasSuffix(r.URL.Path, "/build-log"):
							_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"20","completed":true}}`)
						default:
							reads++
							expected := float64(100)
							if reads == 3 {
								expected = 320
							}
							if body["cursor"] != expected {
								t.Errorf("cursor=%v want=%v", body["cursor"], expected)
							}
							switch reads {
							case 1:
								_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":100,"completed":false}}`)
							case 2:
								if statusChecks != 1 {
									t.Errorf("final drain must follow terminal observation: checks=%d", statusChecks)
								}
								_, _ = io.WriteString(w, `{"code":200,"data":{"content":"last work\n","nextCursor":320,"completed":false}}`)
							default:
								_, _ = io.WriteString(w, `{"code":200,"data":{"content":"deployment terminal\n","nextCursor":400,"completed":true}}`)
							}
						}
					})
					cursor := "100"
					if stage == "all" {
						cursor = (stageLogCursor{TaskID: 42, Build: "20", Deploy: 100}).encode(stage)
					}
					args := append(stageArgs(stage), "--cursor", cursor)
					if follow {
						args = append(args, "-f", "--timeout", "1s")
					} else {
						args = append(args, "--output", "json")
					}
					out, _, err := executeLogsContract(context.Background(), args...)
					if err != nil {
						t.Fatal(err)
					}
					content := out
					if !follow {
						var chunk struct {
							Content, NextCursor string
							Completed           bool
						}
						if err := json.Unmarshal([]byte(out), &chunk); err != nil {
							t.Fatal(err)
						}
						if !chunk.Completed {
							t.Errorf("final pages not completed: %s", out)
						}
						if stage == "deploy" && chunk.NextCursor != "400" {
							t.Errorf("final cursor=%s", chunk.NextCursor)
						}
						content = chunk.Content
					}
					if content != "last work\ndeployment terminal\n" || reads != 3 || statusChecks != 1 {
						t.Fatalf("content=%q eventReads=%d statusChecks=%d", content, reads, statusChecks)
					}
				})
			}
		}
	}
}

func TestLogsDeployTerminalDrainDoesNotReapplyTail(t *testing.T) {
	for _, tail := range []string{"0", "1"} {
		t.Run(tail, func(t *testing.T) {
			reads, statusChecks := 0, 0
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					status := "RUNNING"
					if body["id"] != nil {
						statusChecks++
						if statusChecks == 2 {
							status = "SUCCESS"
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"id": 42, "orgId": 1001, "appKey": "test_app", "status": status, "phase": "DEPLOY"}})
					return
				}
				reads++
				switch reads {
				case 1:
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"discard\nkeep\n","nextCursor":100,"completed":false}}`)
				case 2, 3:
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":100,"completed":false}}`)
				case 4:
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"late work\n","nextCursor":320,"completed":false}}`)
				default:
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"ready\n","nextCursor":400,"completed":true}}`)
				}
			})
			out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "--tail", tail, "-f", "--timeout", "1s")...)
			want := "late work\nready\n"
			if tail == "1" {
				want = "keep\n" + want
			}
			if err != nil || out != want || reads != 5 {
				t.Fatalf("out=%q reads=%d err=%v", out, reads, err)
			}
		})
	}
}

func TestLogsDeployTerminalDrainEmptyErrorAndDeadline(t *testing.T) {
	for _, scenario := range []string{"empty", "storage", "cursor", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			var reads, statusChecks atomic.Int32
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					status := "RUNNING"
					if body["id"] != nil {
						statusChecks.Add(1)
						status = "FAILED"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"id": 42, "orgId": 1001, "appKey": "test_app", "status": status, "phase": "DEPLOY"}})
					return
				}
				if reads.Add(1) == 1 {
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":100,"completed":false}}`)
					return
				}
				switch scenario {
				case "empty":
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":100,"completed":false}}`)
				case "storage":
					_, _ = io.WriteString(w, `{"code":500,"msg":"storage failed"}`)
				case "cursor":
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"duplicate","nextCursor":100,"completed":true}}`)
				default:
					_, _ = io.Copy(io.Discard, r.Body)
					<-r.Context().Done()
				}
			})
			out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "--cursor", "100", "-f", "--timeout", "50ms")...)
			want := 1
			if scenario == "empty" {
				want = 0
			}
			if scenario == "timeout" {
				want = 124
			}
			if ExitCode(err) != want || out != "" || reads.Load() != 2 || statusChecks.Load() != 1 {
				t.Fatalf("out=%q reads=%d statusChecks=%d code=%d err=%v", out, reads.Load(), statusChecks.Load(), ExitCode(err), err)
			}
		})
	}
}
