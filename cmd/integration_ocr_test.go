/**
 * [INPUT]: 依赖 cmd 包内的 runIntegrationOCR / renderOCRTable（包内白盒），internal/api（OCROptions），encoding/json、net/http、net/http/httptest、os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 integration ocr 子命令核心逻辑的单元测试（成功 table / 成功 json / 非法扩展名 / 非法格式 / 文件不存在 / 无凭证 / 未知 profile / API 错误 / table 渲染回退）
 * [POS]: cmd 模块 integration_ocr.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/api"
)

// sampleOCRResponse 来自 spec sample（精简版本）
var sampleOCRResponse = map[string]any{
	"code":       200,
	"msg":        "success",
	"request_id": "req-1",
	"data": map[string]any{
		"task_id":                "billocr-1",
		"status":                 "COMPLETED",
		"file_name":              "demo.pdf",
		"file_type":              "pdf",
		"bill_count":             1,
		"processing_duration_ms": 779,
		"result": map[string]any{
			"pages": []any{
				map[string]any{
					"page_number": 0,
					"bills": []any{
						map[string]any{
							"type":             "vat_digital_invoice",
							"type_description": "数电票",
							"items": []any{
								map[string]any{"key": "vat_invoice_total_cover_tax_digits", "label": "价税合计小写", "value": "¥167.70"},
								map[string]any{"key": "vat_invoice_seller_name", "label": "销售方名称", "value": "河南滴滴出行科技有限公司"},
								map[string]any{"key": "vat_invoice_subtotal", "label": "金额小计", "value": ""}, // 空值应被过滤
							},
						},
					},
				},
			},
		},
	},
}

func TestRunIntegrationOCR(t *testing.T) {
	t.Run("renders table from spec sample", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				t.Errorf("expected multipart, got %q", r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(sampleOCRResponse)
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		dir := t.TempDir()
		file := filepath.Join(dir, "demo.pdf")
		if err := os.WriteFile(file, []byte("PDFBYTES"), 0o644); err != nil {
			t.Fatal(err)
		}

		out := captureStdout(t, func() {
			if err := runIntegrationOCR(file, "default", outputTable, api.OCROptions{}); err != nil {
				t.Fatalf("runIntegrationOCR: %v", err)
			}
		})
		for _, want := range []string{
			"File:",
			"demo.pdf",
			"数电票",
			"价税合计小写",
			"¥167.70",
			"河南滴滴出行科技有限公司",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("table output missing %q\nfull output:\n%s", want, out)
			}
		}
		if strings.Contains(out, "金额小计") {
			t.Errorf("empty-value row should be filtered, but found 金额小计 in:\n%s", out)
		}
	})

	t.Run("emits JSON when --output json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(sampleOCRResponse)
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		dir := t.TempDir()
		file := filepath.Join(dir, "demo.pdf")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		out := captureStdout(t, func() {
			if err := runIntegrationOCR(file, "default", outputJSON, api.OCROptions{}); err != nil {
				t.Fatalf("runIntegrationOCR json: %v", err)
			}
		})
		if !strings.Contains(out, "\"task_id\"") {
			t.Errorf("expected task_id in JSON output: %s", out)
		}
	})

	t.Run("rejects unsupported extension", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runIntegrationOCR("/tmp/foo.txt", "default", outputTable, api.OCROptions{}); err == nil {
			t.Fatal("expected error for unsupported extension")
		}
	})

	t.Run("rejects unsupported output format", func(t *testing.T) {
		if err := runIntegrationOCR("/tmp/foo.pdf", "default", "xml", api.OCROptions{}); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})

	t.Run("rejects missing file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runIntegrationOCR("/tmp/does-not-exist.pdf", "default", outputTable, api.OCROptions{}); err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		dir := t.TempDir()
		file := filepath.Join(dir, "demo.png")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runIntegrationOCR(file, "default", outputTable, api.OCROptions{}); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"
		dir := t.TempDir()
		file := filepath.Join(dir, "demo.jpg")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runIntegrationOCR(file, "nonexistent", outputTable, api.OCROptions{}); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "boom"})
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		dir := t.TempDir()
		file := filepath.Join(dir, "demo.jpeg")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runIntegrationOCR(file, "default", outputTable, api.OCROptions{}); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

func TestRenderOCRTable_FallbackOnUnexpectedShape(t *testing.T) {
	// data 缺 result/pages 时应回退到 JSON 输出而非崩溃
	out := captureStdout(t, func() {
		if err := renderOCRTable(map[string]any{"file_name": "x.pdf"}); err != nil {
			t.Fatalf("renderOCRTable: %v", err)
		}
	})
	if !strings.Contains(out, "x.pdf") {
		t.Errorf("expected file_name in fallback output: %s", out)
	}
}
