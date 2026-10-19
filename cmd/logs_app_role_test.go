/**
 * [INPUT]: logs deploy appRole flag and build/deployment HTTP fixtures
 * [OUTPUT]: beta/prod role selection, app pairing, cursor isolation and validation tests
 * [POS]: App role contracts shared by build/deploy/all log stages
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

func TestLogsDeployAppRoleSelection(t *testing.T) {
	for _, env := range []string{"default", "beta", "prod"} {
		for _, stage := range []string{"build", "deploy", "all"} {
			for _, follow := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/follow=%t", env, stage, follow), func(t *testing.T) {
					publicEnv, serverEnv, taskID := "beta", "beta", "42"
					if env == "prod" {
						publicEnv, serverEnv, taskID = "prod", "production", "84"
					}
					setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if strings.HasSuffix(r.URL.Path, "/build") {
							if body["environment"] != serverEnv || body["commitSha"] != strings.Repeat("a", 40) {
								t.Errorf("task selection=%v", body)
							}
							if body["appRole"] != nil || body["env"] != nil {
								t.Errorf("task lookup must keep its environment protocol: %v", body)
							}
							_, _ = fmt.Fprintf(w, `{"code":200,"data":{"id":%s,"orgId":1001,"appKey":"test_app","environment":%q,"status":"SUCCESS","phase":"DEPLOY"}}`, taskID, publicEnv)
							return
						}
						if body["buildTaskId"] != taskID {
							t.Errorf("log task identity=%v want=%s", body, taskID)
						}
						if strings.HasSuffix(r.URL.Path, "/build-log") {
							_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": "build\n", "nextCursor": "6", "completed": true, "tailLines": body["tailLines"]}})
							return
						}
						if body["appKey"] != "test_app" || body["tenantID"] != "1001" {
							t.Errorf("deployment route=%v", body)
						}
						_, _ = io.WriteString(w, `{"code":200,"data":{"content":"deploy\n","nextCursor":400,"completed":true}}`)
					})
					args := stageArgs(stage)
					if env != "default" {
						args = append(args, "--appRole", env)
					}
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
						var snapshot struct {
							AppRole string          `json:"appRole"`
							Content string          `json:"content"`
							Env     json.RawMessage `json:"env"`
						}
						if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
							t.Fatal(err)
						}
						if snapshot.AppRole != publicEnv || snapshot.Env != nil {
							t.Errorf("snapshot appRole=%q want=%s obsolete env=%s", snapshot.AppRole, publicEnv, snapshot.Env)
						}
						content = snapshot.Content
					}
					want := map[string]string{"build": "build\n", "deploy": "deploy\n", "all": "build\ndeploy\n"}[stage]
					if content != want {
						t.Errorf("content=%q want=%q", content, want)
					}
				})
			}
		}
	}
}

func TestLogsDeployProdResolvesPairedApp(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"id":84,"orgId":1001,"appKey":"prod_app","environment":"production","status":"SUCCESS","phase":"DEPLOY"}}`)
		case strings.HasSuffix(r.URL.Path, "/app"):
			_, _ = io.WriteString(w, `{"code":200,"data":{"key":"test_app","meta":{"appRole":"beta","pairAppKey":"prod_app"}}}`)
		default:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["appKey"] != "prod_app" || body["buildTaskId"] != "84" {
				t.Errorf("production route=%v", body)
			}
			_, _ = io.WriteString(w, `{"code":200,"data":{"content":"prod ready\n","nextCursor":400,"completed":true}}`)
		}
	})
	out, _, err := executeLogsContract(context.Background(), append(stageArgs("deploy"), "--appRole", "prod", "--output", "table")...)
	if err != nil || out != "prod ready\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLogsDeployAppRoleValidation(t *testing.T) {
	for _, env := range []string{"", "production", "preview", "all", "PROD"} {
		t.Run(env, func(t *testing.T) {
			setupDeployLogsTest(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid environment reached HTTP") })
			_, _, err := executeLogsContract(context.Background(), append(stageArgs("build"), "--appRole", env)...)
			if err == nil || !strings.Contains(err.Error(), "--appRole") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestLogsDeployRejectsWrongTaskEnvironment(t *testing.T) {
	for _, test := range []struct{ selected, returned string }{{"prod", "beta"}, {"beta", "prod"}, {"prod", ""}} {
		t.Run(test.selected+"/"+test.returned, func(t *testing.T) {
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/build") {
					t.Error("wrong environment reached log API")
				}
				_, _ = fmt.Fprintf(w, `{"code":200,"data":{"id":42,"appKey":"test_app","environment":%q}}`, test.returned)
			})
			out, _, err := executeLogsContract(context.Background(), append(stageArgs("build"), "--appRole", test.selected)...)
			if err == nil || out != "" || !strings.Contains(err.Error(), "environment") {
				t.Fatalf("out=%q err=%v", out, err)
			}
		})
	}
}

func TestLogsDeployProdRejectsBetaCombinedCursor(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/build") {
			t.Error("beta cursor reached production log API")
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":84,"appKey":"test_app","environment":"production"}}`)
	})
	cursor := (stageLogCursor{TaskID: 42, Build: "20", Deploy: 400}).encode("all")
	_, _, err := executeLogsContract(context.Background(), append(stageArgs("all"), "--appRole", "prod", "--cursor", cursor)...)
	if err == nil || !strings.Contains(err.Error(), "another build task") {
		t.Fatalf("err=%v", err)
	}
}

func TestLogsDeployProdWaitTimeoutHint(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":404,"msg":"not found"}`)
	})
	_, diag, err := executeLogsContract(context.Background(), append(stageArgs("build"), "--appRole", "prod", "-f", "--timeout", "30ms")...)
	if ExitCode(err) != 124 || !strings.Contains(diag, "Waiting for prod") || !strings.Contains(err.Error(), "--appRole prod") {
		t.Fatalf("diag=%q code=%d err=%v", diag, ExitCode(err), err)
	}
}

func TestLogsDeployAppRoleFlagContract(t *testing.T) {
	command := newLogsDeployCmd()
	role, err := command.Flags().GetString("appRole")
	if err != nil || role != "beta" || command.Flags().Lookup("env") != nil {
		t.Fatalf("appRole=%q env=%v err=%v", role, command.Flags().Lookup("env"), err)
	}
	setupDeployLogsTest(t, func(http.ResponseWriter, *http.Request) { t.Error("removed --env reached HTTP") })
	_, _, err = executeLogsContract(context.Background(), append(stageArgs("build"), "--env", "prod")...)
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --env") {
		t.Fatalf("removed flag accepted: %v", err)
	}
}
