/**
 * [INPUT]: 依赖 internal/api 包内的 Client.OCR / OCROptions（包内白盒），encoding/json、io、mime、mime/multipart、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 Client.OCR 的单元测试（multipart 字段断言 / query 参数断言 / verify_vat 三态 / API 错误 / 网络错误）
 * [POS]: internal/api 模块 integration.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// readMultipart 把请求体里所有字段读成 map[name]value（仅文本字段；file 字段单独返回）
func readMultipart(t *testing.T, r *http.Request) (map[string]string, string, string) {
	t.Helper()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	fields := map[string]string{}
	var fileName, fileContent string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		b, _ := io.ReadAll(part)
		if part.FileName() != "" {
			fileName = part.FileName()
			fileContent = string(b)
		} else {
			fields[part.FormName()] = string(b)
		}
	}
	return fields, fileName, fileContent
}

func TestClientOCR(t *testing.T) {
	t.Run("uploads file with default options", func(t *testing.T) {
		var (
			gotFields  map[string]string
			gotFile    string
			gotContent string
			gotQuery   string
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			gotFields, gotFile, gotContent = readMultipart(t, r)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{"file_name": "demo.pdf"},
			})
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		data, err := c.OCR("/tmp/demo.pdf", strings.NewReader("PDFBYTES"), OCROptions{})
		if err != nil {
			t.Fatalf("OCR: %v", err)
		}
		if data["file_name"] != "demo.pdf" {
			t.Errorf("data.file_name = %v", data["file_name"])
		}
		if gotFile != "demo.pdf" {
			t.Errorf("file name = %q, want demo.pdf", gotFile)
		}
		if gotContent != "PDFBYTES" {
			t.Errorf("file content = %q", gotContent)
		}
		if _, ok := gotFields["business_id"]; ok {
			t.Errorf("business_id should not be sent when 0, got %v", gotFields)
		}
		if _, ok := gotFields["verify_vat"]; ok {
			t.Errorf("verify_vat should not be sent when nil, got %v", gotFields)
		}
		if gotQuery != "" {
			t.Errorf("query string should be empty by default, got %q", gotQuery)
		}
	})

	t.Run("sends business_id and verify_vat when set", func(t *testing.T) {
		var gotFields map[string]string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotFields, _, _ = readMultipart(t, r)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok", "data": map[string]any{}})
		}))
		defer srv.Close()

		falseVal := false
		c := New(srv.URL, "tok")
		_, err := c.OCR("x.png", strings.NewReader("x"), OCROptions{
			BusinessID: 100234,
			VerifyVAT:  &falseVal,
		})
		if err != nil {
			t.Fatalf("OCR: %v", err)
		}
		if gotFields["business_id"] != "100234" {
			t.Errorf("business_id = %q", gotFields["business_id"])
		}
		if gotFields["verify_vat"] != "false" {
			t.Errorf("verify_vat = %q", gotFields["verify_vat"])
		}
	})

	t.Run("encodes query parameters", func(t *testing.T) {
		var gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query().Encode()
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok", "data": map[string]any{}})
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		_, err := c.OCR("x.png", strings.NewReader("x"), OCROptions{
			CoordRestoreOriginal:    true,
			SpecificPages:           "1,3,2",
			CropCompleteImage:       true,
			CropValueImage:          true,
			MergeDigitalElecInvoice: true,
			ReturnPPI:               true,
		})
		if err != nil {
			t.Fatalf("OCR: %v", err)
		}
		for _, want := range []string{
			"coord_restore=1",
			"specific_pages=1%2C3%2C2",
			"crop_complete_image=1",
			"crop_value_image=1",
			"merge_digital_elec_invoice=1",
			"return_ppi=1",
		} {
			if !strings.Contains(gotQuery, want) {
				t.Errorf("query missing %q, got %q", want, gotQuery)
			}
		}
	})

	t.Run("strips position fields recursively", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "ok",
				"data": map[string]any{
					"file_name": "x.pdf",
					"position":  []int{1, 2, 3},
					"result": map[string]any{
						"pages": []any{
							map[string]any{
								"page_number": 0,
								"bills": []any{
									map[string]any{
										"items": []any{
											map[string]any{
												"key":      "k",
												"value":    "v",
												"position": []int{0, 0},
											},
										},
									},
								},
							},
						},
					},
				},
			})
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		data, err := c.OCR("x.png", strings.NewReader("x"), OCROptions{})
		if err != nil {
			t.Fatalf("OCR: %v", err)
		}
		if _, ok := data["position"]; ok {
			t.Errorf("top-level position not stripped: %v", data)
		}
		// 钻到底层 item 验证
		pages := data["result"].(map[string]any)["pages"].([]any)
		bill := pages[0].(map[string]any)["bills"].([]any)[0].(map[string]any)
		item := bill["items"].([]any)[0].(map[string]any)
		if _, ok := item["position"]; ok {
			t.Errorf("nested item.position not stripped: %v", item)
		}
		if item["key"] != "k" || item["value"] != "v" {
			t.Errorf("non-position fields lost: %v", item)
		}
	})

	t.Run("returns API error on non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "boom"})
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		if _, err := c.OCR("x.png", strings.NewReader("x"), OCROptions{}); err == nil {
			t.Fatal("expected API error")
		}
	})

	t.Run("returns transport error on bad URL", func(t *testing.T) {
		c := New("http://127.0.0.1:1", "tok")
		if _, err := c.OCR("x.png", strings.NewReader("x"), OCROptions{}); err == nil {
			t.Fatal("expected transport error")
		}
	})
}
