/**
 * [INPUT]: 依赖 apply.go、app_variable_test 的环境夹具、internal/api，encoding/json、net/http、net/http/httptest、slices、strings、testing
 * [OUTPUT]: 验证用户样例的 App 与变量分流、Get 类型声明、重复 apply、requiredSecrets 声明、非法输入零请求及变量失败中止
 * [POS]: cmd/apply 的 App 声明集成测试，真实 YAML/Cobra/HTTP 全链路、本地服务隔离
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/api"
)

const appConfigYAML = `key: MatrixProject
name: Make 项目管理
type: Make.App
meta:
  version: 1.0.0
properties:
  description: Make 项目管理演示项目
  vars:
    BASE_URL: https://api.example.com
    LOG_LEVEL: info
  requiredSecrets:
    - DATABASE_URL
    - OPENAI_API_KEY
`

func TestApplyAppRuntimeConfig(t *testing.T) {
	appExists := false
	variables := map[string]string{"REMOTE_ONLY": "preserved"}
	var appActions, variableActions []string
	var gotProperties map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		target := r.Header.Get("X-Make-Target")
		if r.URL.Path == "/api/make/meta/v1/app" {
			if r.URL.RawQuery != "" || body["key"] != "MatrixProject" {
				t.Errorf("unexpected App identity: %s %v", r.URL, body)
			}
			if target == "MakeService.GetResource" {
				if !appExists {
					_, _ = w.Write([]byte(`{"code":404,"msg":"missing"}`))
					return
				}
				_, _ = w.Write([]byte(`{"code":200,"data":{"key":"MatrixProject","name":"old"}}`))
				return
			}
			appActions = append(appActions, target)
			gotProperties, _ = body["properties"].(map[string]any)
			if _, exists := gotProperties["vars"]; exists {
				t.Error("Beta variable values must not be stored in shared App metadata")
			}
			appExists = true
			_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			return
		}
		if r.URL.Path != "/api/make/secret/v1/variable" || r.URL.Query().Get("appRole") != "beta" || body["appKey"] != "MatrixProject" {
			t.Errorf("unexpected variable identity: %s %v", r.URL, body)
		}
		props, _ := body["properties"].(map[string]any)
		if body["type"] != "Make.Variable" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":400,"msg":"invalid request: invalid type","data":{}}`))
			return
		}
		if props["scope"] != "runtime" {
			t.Errorf("unexpected variable scope: %v", props)
		}
		key, _ := body["key"].(string)
		if target == "MakeService.GetResource" {
			value, exists := variables[key]
			if !exists {
				_, _ = w.Write([]byte(`{"code":404,"msg":"missing"}`))
				return
			}
			variable := api.Variable{Key: key, Type: "Make.Variable", AppKey: "MatrixProject", Properties: api.VariableProperties{Scope: "runtime", Value: &value}}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": variable})
			return
		}
		variableActions = append(variableActions, target)
		if body["type"] != "Make.Variable" {
			t.Error("requiredSecrets must not be created as variables")
		}
		variables[key], _ = props["value"].(string)
		_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
	}))
	defer srv.Close()
	appVariableTestCmd(t, srv.URL)
	dir := t.TempDir()
	file := writeYAMLFileForApply(t, dir, "app.yaml", appConfigYAML)
	runApply := func() {
		t.Helper()
		cmd := newApplyCmd()
		cmd.SetArgs([]string{"-f", file})
		out := captureStdout(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, "Applied 1 resources successfully") || !strings.Contains(out, "beta, runtime") {
			t.Errorf("unexpected apply output: %s", out)
		}
	}
	runApply()
	if variables["BASE_URL"] != "https://api.example.com" || variables["LOG_LEVEL"] != "info" {
		t.Fatalf("vars were not saved: %v", variables)
	}
	writeYAMLFileForApply(t, dir, "app.yaml", strings.ReplaceAll(appConfigYAML, "LOG_LEVEL: info", "LOG_LEVEL: debug"))
	runApply()
	if !slices.Equal(appActions, []string{"MakeService.CreateResource", "MakeService.UpdateResource"}) {
		t.Errorf("unexpected App actions: %v", appActions)
	}
	if !slices.Equal(variableActions, []string{"MakeService.CreateResource", "MakeService.CreateResource", "MakeService.UpdateResource", "MakeService.UpdateResource"}) {
		t.Errorf("unexpected variable actions: %v", variableActions)
	}
	if variables["LOG_LEVEL"] != "debug" || variables["REMOTE_ONLY"] != "preserved" {
		t.Errorf("re-apply must update declared vars and retain remote-only vars: %v", variables)
	}
	keys, _ := gotProperties["requiredSecrets"].([]any)
	if len(keys) != 2 || keys[0] != "DATABASE_URL" || keys[1] != "OPENAI_API_KEY" || gotProperties["description"] != "Make 项目管理演示项目" {
		t.Errorf("requiredSecrets must be stored with App metadata: %v", gotProperties)
	}
	writeYAMLFileForApply(t, dir, "app.yaml", `key: MatrixProject
name: Make 项目管理
type: Make.App
properties:
  vars: {LOG_LEVEL: warn}
  requiredSecrets: []
`)
	runApply()
	if variables["BASE_URL"] != "https://api.example.com" || variables["LOG_LEVEL"] != "warn" {
		t.Errorf("removing a declared variable must retain its remote value: %v", variables)
	}
	keys, ok := gotProperties["requiredSecrets"].([]any)
	if !ok || len(keys) != 0 {
		t.Errorf("an explicit empty requiredSecrets list must clear the contract: %v", gotProperties)
	}
}

func TestApplyAppConfigInvalidInput(t *testing.T) {
	for _, properties := range []string{
		"vars: []", "vars: null", "vars: {BAD-NAME: value}", "vars: {MAKE_TOKEN: value}",
		"vars: {X: true}", "vars: {X: 123}", "requiredSecrets: token",
		"requiredSecrets: [123]", "requiredSecrets: [BAD-NAME]", "requiredSecrets: [MAKE_TOKEN]",
		"vars: {TOKEN: value}\n  requiredSecrets: [TOKEN]",
	} {
		t.Run(properties, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			appVariableTestCmd(t, srv.URL)
			file := writeYAMLFileForApply(t, t.TempDir(), "app.yaml", "key: demo\nname: Demo\ntype: Make.App\nproperties:\n  "+properties+"\n")
			if err := runAppApply(file, 2); err == nil {
				t.Fatal("expected invalid declaration error")
			}
			if calls != 0 {
				t.Fatalf("invalid declaration made %d requests", calls)
			}
		})
	}
}

func TestApplyAppConfigVariableFailure(t *testing.T) {
	for _, response := range []string{
		`{"code":403,"msg":"denied"}`,
		`{"code":200,"data":{"key":"BASE_URL","appKey":"MatrixProject","type":"Make.Secret","properties":{"scope":"runtime"}}}`,
	} {
		t.Run(response, func(t *testing.T) {
			variableCalls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/make/meta/v1/app" {
					_, _ = w.Write([]byte(`{"code":200,"data":{"key":"MatrixProject"}}`))
					return
				}
				variableCalls++
				if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
					t.Error("failed variable read must not write")
				}
				_, _ = w.Write([]byte(response))
			}))
			defer srv.Close()
			appVariableTestCmd(t, srv.URL)
			file := writeYAMLFileForApply(t, t.TempDir(), "app.yaml", appConfigYAML)
			out := captureStdout(t, func() {
				if err := runAppApply(file, 2); err == nil || !strings.Contains(err.Error(), "properties.vars.BASE_URL") {
					t.Errorf("expected variable-specific error, got %v", err)
				}
			})
			if variableCalls != 1 || strings.Contains(out, "successfully") {
				t.Fatalf("variableCalls=%d, stdout=%q", variableCalls, out)
			}
		})
	}
}
