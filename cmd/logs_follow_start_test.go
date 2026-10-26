/**
 * [INPUT]: Combined zero-tail followers and build/deployment HTTP fixtures
 * [OUTPUT]: Start cursors, build events during initialization, stage transitions and errors
 * [POS]: TC-013 regression contracts; all new deployment events survive stage switching
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

func TestLogsAllZeroTailFollowKeepsBuildDuringDeploymentInitialization(t *testing.T) {
	for _, historyPages := range []int{0, 2} {
		t.Run(fmt.Sprintf("historyPages=%d", historyPages), func(t *testing.T) {
			setupBuildInitializationRace(t, historyPages)
			out, diagnostic, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "0", "--follow", "--timeout", "1s")...)
			if err != nil || out != "BUILD_FAILED_AFTER_START\n" {
				t.Fatalf("stdout=%q stderr=%q err=%v", out, diagnostic, err)
			}
			cursor := (stageLogCursor{TaskID: 42, Build: "20", Deploy: uint64(historyPages * 10)}).encode("all")
			if !strings.Contains(diagnostic, "nextCursor="+cursor+", completed=true") {
				t.Fatalf("final cursor/completion missing: %s", diagnostic)
			}
		})
	}
}

func setupBuildInitializationRace(t *testing.T, historyPages int) {
	t.Helper()
	deploymentReads := 0
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/build"):
			status := "RUNNING"
			if body["id"] != nil {
				status = "FAILED"
			}
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":%q,"phase":"BUILD"}}`, status)
		case strings.HasSuffix(r.URL.Path, "/build-log"):
			writeBuildInitializationRace(w, body, deploymentReads > 0)
		default:
			deploymentReads++
			content := ""
			if deploymentReads <= historyPages {
				content = "D_OLD\n"
			}
			cursor := min(deploymentReads, historyPages) * 10
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": content, "nextCursor": cursor, "completed": false}})
		}
	})
}

func writeBuildInitializationRace(w http.ResponseWriter, body map[string]any, failed bool) {
	content, cursor := "", "10"
	if failed {
		cursor = "20"
		if body["cursor"] == "10" {
			content = "BUILD_FAILED_AFTER_START\n"
		}
	}
	reply := map[string]any{"content": content, "nextCursor": cursor, "completed": failed}
	if tail, ok := body["tailLines"]; ok {
		reply["tailLines"] = tail
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reply})
}

func TestLogsAllZeroTailFollowKeepsDeploymentAfterStart(t *testing.T) {
	for _, historyPages := range []int{0, 2} {
		t.Run(fmt.Sprintf("historyPages=%d", historyPages), func(t *testing.T) {
			buildReads, deploymentReads := 0, 0
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				switch {
				case strings.HasSuffix(r.URL.Path, "/build"):
					_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"BUILD"}}`)
				case strings.HasSuffix(r.URL.Path, "/build-log"):
					buildReads++
					writeFollowStartBuild(t, w, body, buildReads)
				default:
					deploymentReads++
					writeFollowStartDeployment(t, w, body, buildReads, deploymentReads, historyPages)
				}
			})
			out, diagnostic, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "0", "--follow", "--timeout", "1s")...)
			if err != nil || out != "B_NEW\nD_NEW\nDEPLOY_FAIL_END instance failed\n" {
				t.Fatalf("stdout=%q stderr=%q err=%v", out, diagnostic, err)
			}
			cursor := (stageLogCursor{TaskID: 42, Build: "16", Deploy: 102}).encode("all")
			if !strings.Contains(diagnostic, "nextCursor="+cursor+", completed=true") {
				t.Fatalf("final cursor/completion missing: %s", diagnostic)
			}
		})
	}
}

func writeFollowStartBuild(t *testing.T, w http.ResponseWriter, body map[string]any, reads int) {
	t.Helper()
	if reads == 1 {
		if body["tailLines"] != float64(0) || body["cursor"] != nil {
			t.Errorf("initial build request=%v", body)
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"10","completed":false,"tailLines":0}}`)
		return
	}
	if body["cursor"] != "10" || body["tailLines"] != nil {
		t.Errorf("build resume request=%v", body)
	}
	_, _ = io.WriteString(w, `{"code":200,"data":{"content":"B_NEW\n","nextCursor":"16","completed":true}}`)
}

func writeFollowStartDeployment(t *testing.T, w http.ResponseWriter, body map[string]any, buildReads, reads, historyPages int) {
	t.Helper()
	if buildReads < 2 {
		if reads <= historyPages {
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"content":"D_OLD_%d\n","nextCursor":%d,"completed":false}}`, reads, reads*10)
			return
		}
		_, _ = fmt.Fprintf(w, `{"code":200,"data":{"content":"","nextCursor":%d,"completed":false}}`, historyPages*10)
		return
	}
	if body["cursor"] != float64(historyPages*10) {
		t.Errorf("deployment resume cursor=%v want=%d", body["cursor"], historyPages*10)
	}
	_, _ = io.WriteString(w, `{"code":200,"data":{"content":"D_NEW\nDEPLOY_FAIL_END instance failed\n","nextCursor":102,"completed":true}}`)
}

func TestLogsAllZeroTailFollowSkipsCompletedHistory(t *testing.T) {
	for _, phase := range []string{"BUILD", "DEPLOY"} {
		t.Run(phase, func(t *testing.T) {
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/build"):
					_, _ = fmt.Fprintf(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"FAILED","phase":%q}}`, phase)
				case strings.HasSuffix(r.URL.Path, "/build-log"):
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"16","completed":true,"tailLines":0}}`)
				default:
					if phase == "BUILD" {
						t.Error("terminal build failure queried deployment events")
					}
					var body struct{ Cursor uint64 }
					_ = json.NewDecoder(r.Body).Decode(&body)
					content := ""
					if body.Cursor == 0 {
						content = "D_OLD_FAILURE\n"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": content, "nextCursor": 102, "completed": true}})
				}
			})
			out, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "0", "--follow", "--timeout", "1s")...)
			if err != nil || out != "" {
				t.Fatalf("stdout=%q err=%v", out, err)
			}
		})
	}
}

func TestLogsAllZeroTailFollowKeepsTerminalEventAfterInitialEmptyPage(t *testing.T) {
	deploymentReads := 0
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/build"):
			status, phase := "RUNNING", "BUILD"
			if body["id"] != nil {
				status, phase = "FAILED", "DEPLOY"
			}
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":%q,"phase":%q}}`, status, phase)
		case strings.HasSuffix(r.URL.Path, "/build-log"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"16","completed":true,"tailLines":0}}`)
		default:
			deploymentReads++
			if deploymentReads == 1 {
				_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":0,"completed":false}}`)
				return
			}
			content := ""
			if body["cursor"] == float64(0) {
				content = "DEPLOY_FAIL_AFTER_INITIAL_EMPTY\n"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": content, "nextCursor": 390, "completed": true}})
		}
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--tail", "0", "--follow", "--timeout", "1s")...)
	if err != nil || out != "DEPLOY_FAIL_AFTER_INITIAL_EMPTY\n" {
		t.Fatalf("stdout=%q err=%v", out, err)
	}
}

func TestLogsAllZeroTailFollowInitializationErrors(t *testing.T) {
	for _, scenario := range []string{"storage", "cursor", "timeout", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_app","status":"RUNNING","phase":"BUILD"}}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/build-log") {
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"","nextCursor":"10","completed":false,"tailLines":0}}`)
					return
				}
				switch scenario {
				case "storage":
					_, _ = io.WriteString(w, `{"code":500,"msg":"deployment baseline unavailable"}`)
				case "cursor":
					_, _ = io.WriteString(w, `{"code":200,"data":{"content":"history","nextCursor":0,"completed":false}}`)
				default:
					_, _ = io.Copy(io.Discard, r.Body)
					if scenario == "cancel" {
						cancel()
					}
					<-r.Context().Done()
				}
			})
			out, _, err := executeLogsContract(ctx, append(stageArgs("all"), "--tail", "0", "--follow", "--timeout", "30ms")...)
			want := map[string]int{"storage": 1, "cursor": 1, "timeout": 124, "cancel": 130}[scenario]
			if err == nil || ExitCode(err) != want || out != "" {
				t.Fatalf("stdout=%q exit=%d err=%v", out, ExitCode(err), err)
			}
		})
	}
}
