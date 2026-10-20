/**
 * [INPUT]: 依赖 client.go 的 Client.UpdateApp，encoding/json、net/http、net/http/httptest、testing
 * [OUTPUT]: 验证 App 更新的请求路由、必需密钥声明及空列表保留、错误透传
 * [POS]: internal/api 的 App UpdateResource 协议测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateApp(t *testing.T) {
	for _, required := range [][]string{{"DATABASE_URL", "OPENAI_API_KEY"}, {}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/meta/v1/app" || r.Header.Get("X-Make-Target") != "MakeService.UpdateResource" {
				t.Errorf("unexpected update request: %s %s", r.Method, r.URL)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			props, _ := body["properties"].(map[string]any)
			keys, ok := props["requiredSecrets"].([]any)
			if body["key"] != "MatrixProject" || body["name"] != "Make 项目管理" || body["type"] != "Make.App" || !ok || len(keys) != len(required) {
				t.Errorf("unexpected update body: %v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
		}))
		err := New(srv.URL, "token").UpdateApp("MatrixProject", "Make 项目管理", map[string]any{"requiredSecrets": required})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, response := range []string{`{"code":403,"msg":"denied"}`, `{"code":409,"msg":"conflict"}`, `{`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
		err := New(srv.URL, "token").UpdateApp("MatrixProject", "Make 项目管理", nil)
		srv.Close()
		if err == nil {
			t.Fatal("expected update error")
		}
	}
}
