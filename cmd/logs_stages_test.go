/**
 * [INPUT]: logs deploy stages and build/deployment HTTP fixtures
 * [OUTPUT]: Stage selection, independent cursors, tail, follow and bounded completion tests
 * [POS]: Deployment process and combined stage command contracts
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
	"testing"
)

func stageArgs(stage string) []string {
	return []string{"--app", "test_app", "--commit-sha", strings.Repeat("a", 40), "--stage", stage}
}

func TestLogsDeployStagesTailAndSnapshot(t *testing.T) {
	for _, stage := range []string{"deploy", "all"} {
		for _, tail := range []string{"-1", "0", "1", "200"} {
			t.Run(stage+tail, func(t *testing.T) {
				setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/build"):
						_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"SUCCESS","phase":"DEPLOY"}}`)
					case strings.HasSuffix(r.URL.Path, "/build-log"):
						var body map[string]any
						_ = json.NewDecoder(r.Body).Decode(&body)
						content := "build\n"
						if tail == "0" {
							content = ""
						}
						reply := map[string]any{"content": content, "nextCursor": "20", "completed": true}
						if n, ok := body["tailLines"]; ok {
							reply["tailLines"] = n
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reply})
					case strings.HasSuffix(r.URL.Path, "/deployment/events"):
						_, _ = io.WriteString(w, `{"code":200,"data":{"content":"starting\nhealthy\n","nextCursor":400,"completed":true}}`)
					default:
						t.Errorf("unexpected route %s", r.URL)
					}
				})
				args := append(stageArgs(stage), "--tail", tail, "--output", "json")
				out, _, err := executeLogsContract(context.Background(), args...)
				if err != nil {
					t.Fatal(err)
				}
				var chunk struct {
					Content, NextCursor, Stage string
					Completed                  bool
				}
				if err := json.Unmarshal([]byte(out), &chunk); err != nil {
					t.Fatal(err)
				}
				want := "starting\nhealthy\n"
				if tail == "1" {
					want = "healthy\n"
				}
				if tail == "0" {
					want = ""
				}
				if stage == "all" && tail != "0" {
					want = "build\n" + want
				}
				if chunk.Content != want || chunk.Stage != stage || !chunk.Completed || chunk.NextCursor == "" {
					t.Fatalf("snapshot=%s want=%q", out, want)
				}
			})
		}
	}
}

func TestLogsDeployStageFollowAndResume(t *testing.T) {
	for _, stage := range []string{"deploy", "all"} {
		t.Run(stage, func(t *testing.T) {
			reads, buildReads := 0, 0
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				switch {
				case strings.HasSuffix(r.URL.Path, "/build"):
					_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"DEPLOY"}}`)
				case strings.HasSuffix(r.URL.Path, "/build-log"):
					buildReads++
					content := ""
					if buildReads == 1 {
						content = "build\n"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": content, "nextCursor": "20", "completed": true}})
				case strings.HasSuffix(r.URL.Path, "/deployment/events"):
					reads++
					switch reads {
					case 1:
						if body["cursor"] != float64(0) {
							t.Errorf("first cursor=%v", body)
						}
						_, _ = io.WriteString(w, `{"code":200,"data":{"content":"old\n","nextCursor":10,"completed":false}}`)
					case 2:
						if body["cursor"] != float64(10) {
							t.Errorf("drain cursor=%v", body)
						}
						_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":10,"completed":false}}`)
					default:
						if body["cursor"] != float64(10) {
							t.Errorf("resume cursor=%v", body)
						}
						_, _ = io.WriteString(w, `{"code":200,"data":{"content":"ready\n","nextCursor":400,"completed":true}}`)
					}
				}
			})
			args := append(stageArgs(stage), "--tail", "-1", "-f", "--timeout", "1s")
			out, _, err := executeLogsContract(context.Background(), args...)
			want := "old\nready\n"
			if stage == "all" {
				want = "build\n" + want
			}
			if err != nil || out != want || reads != 3 {
				t.Fatalf("out=%q reads=%d err=%v", out, reads, err)
			}
		})
	}
}

func TestLogsDeployStageNoDeploymentAfterBuildFailure(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/build") {
			t.Errorf("must not wait for a deployment after build failure: %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"FAILED","phase":"BUILD"}}`)
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "-f", "--timeout", "1s")...)
	if err != nil || out != "" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLogsDeployStageEmptyTimeout(t *testing.T) {
	for _, stage := range []string{"deploy", "all"} {
		t.Run(stage, func(t *testing.T) {
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/build"):
					_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"DEPLOY"}}`)
				case strings.HasSuffix(r.URL.Path, "/build-log"):
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"0","completed":true}}`)
				default:
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":0,"completed":false}}`)
				}
			})
			_, _, err := executeLogsContract(context.Background(), append(stageArgs(stage), "--tail", "-1", "-f", "--timeout", "30ms")...)
			if ExitCode(err) != 124 {
				t.Fatalf("code=%d err=%v", ExitCode(err), err)
			}
		})
	}
}

func TestLogsDeployStageCursorResume(t *testing.T) {
	for _, stage := range []string{"deploy", "all"} {
		t.Run(stage, func(t *testing.T) {
			resume := false
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				switch {
				case strings.HasSuffix(r.URL.Path, "/build"):
					_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"SUCCESS","phase":"DEPLOY"}}`)
				case strings.HasSuffix(r.URL.Path, "/build-log"):
					content := "build\n"
					if resume {
						if body["cursor"] != "20" || body["tailLines"] != nil {
							t.Errorf("build resume=%v", body)
						}
						content = ""
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": content, "nextCursor": "20", "completed": true}})
				default:
					if resume && body["cursor"] != float64(400) {
						t.Errorf("event resume=%v", body)
					}
					content := "ready\n"
					if resume {
						content = ""
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": content, "nextCursor": 400, "completed": true}})
				}
			})
			args := append(stageArgs(stage), "--tail", "-1", "--output", "json")
			out, _, err := executeLogsContract(context.Background(), args...)
			if err != nil {
				t.Fatal(err)
			}
			var first struct{ NextCursor string }
			if err := json.Unmarshal([]byte(out), &first); err != nil {
				t.Fatal(err)
			}
			resume = true
			out, _, err = executeLogsContract(context.Background(), append(stageArgs(stage), "--cursor", first.NextCursor, "--output", "json")...)
			var next struct {
				Content   string
				Completed bool
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(out), &next); err != nil || next.Content != "" || !next.Completed {
				t.Fatalf("resume=%s err=%v", out, err)
			}
		})
	}
}

func TestLogsDeployStageBrokenCursor(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/build") {
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","phase":"DEPLOY"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":200,"data":{"content":"repeat","nextCursor":10,"completed":false}}`)
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "--cursor", "10", "-f", "--timeout", "1s")...)
	if err == nil || out != "" || ExitCode(err) != 1 {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLogsDeployStageBuildOutputBeforeSlowDeployment(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"DEPLOY"}}`)
		case strings.HasSuffix(r.URL.Path, "/build-log"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"build complete\n","nextCursor":"20","completed":true}}`)
		default:
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		}
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "-1", "-f", "--timeout", "30ms")...)
	if ExitCode(err) != 124 || out != "build complete\n" {
		t.Fatalf("out=%q code=%d err=%v", out, ExitCode(err), err)
	}
}

func TestLogsDeployStageZeroTailOnlyOnce(t *testing.T) {
	calls := 0
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/build") {
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"DEPLOY"}}`)
			return
		}
		calls++
		switch calls {
		case 1:
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"history\n","nextCursor":10,"completed":false}}`)
		case 2:
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":10,"completed":false}}`)
		default:
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"new\n","nextCursor":400,"completed":true}}`)
		}
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "--tail", "0", "-f", "--timeout", "1s")...)
	if err != nil || out != "new\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLogsDeployStagePaginationTail(t *testing.T) {
	reads := 0
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/build") {
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"SUCCESS","phase":"DEPLOY"}}`)
			return
		}
		reads++
		if reads == 1 {
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"page one\n","nextCursor":200,"completed":false}}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["cursor"] != float64(200) {
			t.Errorf("page cursor=%v", body)
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"content":"最后一行\n","nextCursor":400,"completed":true}}`)
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "--tail", "1", "--output", "table")...)
	if err != nil || out != "最后一行\n" || reads != 2 {
		t.Fatalf("out=%q reads=%d err=%v", out, reads, err)
	}
}

func TestLogsDeployStageEmptyCombinedCursorRoundTrip(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"BUILD"}}`)
		case strings.HasSuffix(r.URL.Path, "/build-log"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"","completed":false}}`)
		default:
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":0,"completed":false}}`)
		}
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "-1", "--output", "json")...)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct{ NextCursor string }
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatal(err)
	}
	_, _, err = executeLogsContract(context.Background(), append(stageArgs("all"), "--cursor", snapshot.NextCursor, "--output", "json")...)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLogsDeployStageInvalidCursorBeforeIO(t *testing.T) {
	for _, args := range [][]string{
		{"--stage", "deploy", "--cursor", "-1"},
		{"--stage", "deploy", "--cursor", "18446744073709551616"},
		{"--stage", "deploy", "--cursor", "all:abc"},
		{"--stage", "all", "--cursor", "0"},
		{"--stage", "all", "--cursor", "all:invalid"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setupDeployLogsTest(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid cursor reached network") })
			_, _, err := executeLogsContract(context.Background(), append(stageArgs("build"), args...)...)
			if err == nil {
				t.Fatal("invalid cursor accepted")
			}
		})
	}
}

func TestLogsDeployStageCursorRejectsAnotherTask(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/build") {
			t.Error("foreign cursor reached logs")
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":43,"orgId":1001,"appKey":"test_app"}}`)
	})
	cursor := (stageLogCursor{TaskID: 42, Build: "100", Deploy: 400}).encode("all")
	_, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--cursor", cursor)...)
	if err == nil || !strings.Contains(err.Error(), "another build task") {
		t.Fatalf("err=%v", err)
	}
}

func TestLogsDeployStageTerminalWithoutEvents(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/build") {
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"FAILED","phase":"DEPLOY"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":0,"completed":false}}`)
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "-f", "--timeout", "1s")...)
	if err != nil || out != "" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLogsDeployStageErrorsAndCancel(t *testing.T) {
	for _, scenario := range []string{"auth", "storage", "deadline", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","phase":"DEPLOY"}}`)
					return
				}
				switch scenario {
				case "auth":
					_, _ = io.WriteString(w, `{"code":990300403,"msg":"invalid token"}`)
				case "storage":
					_, _ = io.WriteString(w, `{"code":500,"msg":"storage failure"}`)
				default:
					_, _ = io.Copy(io.Discard, r.Body)
					if scenario == "cancel" {
						cancel()
					}
					<-r.Context().Done()
				}
			})
			out, _, err := executeLogsContract(ctx, append(stageArgs("deploy"), "-f", "--timeout", "30ms")...)
			want := 1
			if scenario == "deadline" {
				want = 124
			}
			if scenario == "cancel" {
				want = 130
			}
			if err == nil || ExitCode(err) != want || out != "" {
				t.Fatalf("out=%q err=%v code=%d", out, err, ExitCode(err))
			}
		})
	}
}

func TestLogsDeployStageAllBuildThenDeploy(t *testing.T) {
	reads := 0
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","phase":"BUILD","status":"RUNNING"}}`)
		case strings.HasSuffix(r.URL.Path, "/build-log"):
			reads++
			if reads == 1 {
				_, _ = io.WriteString(w, `{"code":200,"data":{"content":"install\n","nextCursor":"8","completed":false}}`)
				return
			}
			if body["cursor"] != "8" {
				t.Errorf("build cursor=%v", body)
			}
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"image\n","nextCursor":"14","completed":true}}`)
		default:
			if reads != 2 || body["cursor"] != float64(0) {
				t.Errorf("deployment started before build finished: reads=%d body=%v", reads, body)
			}
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"healthy\n","nextCursor":400,"completed":true}}`)
		}
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "-1", "-f", "--timeout", "1s")...)
	if err != nil || out != "install\nimage\nhealthy\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
