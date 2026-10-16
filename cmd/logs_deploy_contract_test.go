/**
 * [INPUT]: logs deploy, app manifests, Git and HTTP fixtures
 * [OUTPUT]: Command defaults, app/SHA selection and initial tail/cursor contracts
 * [POS]: Deployment-log command contract tests
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func executeLogsContract(ctx context.Context, args ...string) (string, string, error) {
	command := newLogsCmd()
	var out, diag bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&diag)
	command.SetArgs(append([]string{"deploy"}, args...))
	err := command.ExecuteContext(ctx)
	return out.String(), diag.String(), err
}

func TestLogsDeployContractDefaults(t *testing.T) {
	command, _, err := newLogsCmd().Find([]string{"deploy"})
	if err != nil || command.CommandPath() != "logs deploy" {
		t.Fatalf("registration: %v", err)
	}
	timeout, _ := command.Flags().GetDuration("timeout")
	tail, _ := command.Flags().GetInt64("tail")
	if timeout != 3*time.Minute || tail != 200 || command.Flags().Lookup("id") != nil {
		t.Fatalf("defaults timeout=%s tail=%d", timeout, tail)
	}
	for _, child := range newAppCmd().Commands() {
		if child.Name() == "logs" {
			t.Fatal("legacy app logs still registered")
		}
	}
}

func TestLogsDeployContractTailOnceAndSHA(t *testing.T) {
	sha := strings.Repeat("a", 40)
	queries, reads := 0, 0
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "/build") {
			queries++
			if body["commitSha"] != sha || body["environment"] != "beta" {
				t.Errorf("lookup: %#v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"appKey":"test_app"}}`))
			return
		}
		reads++
		if reads == 1 {
			if body["tailLines"] != float64(2) || body["cursor"] != nil {
				t.Errorf("first read: %#v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"content":"last\n","nextCursor":"100","completed":false,"tailLines":2}}`))
		} else {
			if body["tailLines"] != nil || body["cursor"] != "100" {
				t.Errorf("resume: %#v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"content":"new\n","nextCursor":"104","completed":true}}`))
		}
	})
	out, _, err := executeLogsContract(context.Background(), "--app", "test_app", "--commit-sha", sha, "--tail", "2", "-f", "--timeout", "1s")
	if err != nil || out != "last\nnew\n" || queries != 1 || reads != 2 {
		t.Fatalf("out=%q queries=%d reads=%d err=%v", out, queries, reads, err)
	}
}

func TestLogsDeployContractRejectsWrongApp(t *testing.T) {
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/build") {
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"appKey":"other_app"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/app") {
			_, _ = w.Write([]byte(`{"code":200,"data":{"key":"test_app","meta":{"appRole":"beta"}}}`))
			return
		}
		t.Error("must not read logs for another app")
	})
	out, _, err := executeLogsContract(context.Background(), "--app", "test_app", "--commit-sha", strings.Repeat("a", 40))
	if err == nil || out != "" {
		t.Fatalf("wrong app accepted: %q %v", out, err)
	}
}

func TestLogsDeployContractValidation(t *testing.T) {
	cases := [][]string{{"--tail", "-2"}, {"--cursor", ""}, {"--tail", "x"}, {"--tail", "1", "--cursor", "0"}, {"--follow", "--timeout", "0s"}, {"--follow", "--timeout", "120"}, {"--timeout", "2m"}, {"--id", "42"}, {"--stage", "deploy"}, {"--stage", "all"}}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setupDeployLogsTest(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid args reached network") })
			out, _, err := executeLogsContract(context.Background(), append([]string{"--app", "test_app", "--commit-sha", strings.Repeat("a", 40)}, args...)...)
			if err == nil || out != "" {
				t.Fatalf("invalid args accepted: %q %v", out, err)
			}
		})
	}
}

func TestLogsDeployContractProjectDefaults(t *testing.T) {
	sha := ""
	setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/build") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["commitSha"] != sha {
				t.Errorf("HEAD=%v want=%s", body["commitSha"], sha)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"appKey":"project_beta"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/app") {
			_, _ = w.Write([]byte(`{"code":200,"data":{"key":"project","meta":{"appRole":"prod","pairAppKey":"project_beta"}}}`))
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["tailLines"] != float64(200) {
			t.Errorf("default tail: %#v", body)
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"content":"last","nextCursor":"100","completed":true,"tailLines":200}}`))
	})
	if err := os.MkdirAll(filepath.Dir(appDSLPath), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, appDSLPath, []byte("key: project\nname: Project\ntype: Make.App\n"))
	gitCommitAll(t)
	sha = headSha(t)
	writeTestFile(t, "dirty.txt", []byte("uncommitted"))
	out, _, err := executeLogsContract(context.Background(), "--output", "table")
	if err != nil || out != "last" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLogsDeployContractTailZeroAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			calls := 0
			setupDeployLogsTest(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/build") {
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":42,"appKey":"test_app"}}`))
					return
				}
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if calls == 1 && !resume {
					if body["tailLines"] != float64(0) {
						t.Errorf("zero tail missing: %#v", body)
					}
					_, _ = w.Write([]byte(`{"code":200,"data":{"content":"","nextCursor":"100","tailLines":0}}`))
					return
				}
				if body["tailLines"] != nil || body["cursor"] != "100" {
					t.Errorf("resume=%#v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":{"content":"new","nextCursor":"103","completed":true}}`))
			})
			args := []string{"--app", "test_app", "--commit-sha", strings.Repeat("a", 40), "-f", "--timeout", "1s"}
			if resume {
				args = append(args, "--cursor", "100")
			} else {
				args = append(args, "--tail", "0")
			}
			out, _, err := executeLogsContract(context.Background(), args...)
			wantCalls := 2
			if resume {
				wantCalls = 1
			}
			if err != nil || out != "new" || calls != wantCalls {
				t.Fatalf("out=%q calls=%d err=%v", out, calls, err)
			}
		})
	}
}

func TestLogsDeployContractMissingDefaults(t *testing.T) {
	for _, app := range []bool{false, true} {
		t.Run(fmt.Sprint(app), func(t *testing.T) {
			setupDeployLogsTest(t, func(http.ResponseWriter, *http.Request) { t.Error("missing identity reached network") })
			args := []string{}
			hint := "--app"
			if app {
				args = append(args, "--app", "test_app")
				hint = "--commit-sha"
			}
			_, _, err := executeLogsContract(context.Background(), args...)
			if err == nil || !strings.Contains(err.Error(), hint) {
				t.Fatalf("err=%v want hint=%s", err, hint)
			}
		})
	}
}
