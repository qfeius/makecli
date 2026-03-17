/**
 * [INPUT]: 依赖 testing、net/http/httptest、encoding/json
 * [OUTPUT]: 对外提供 update 包的单元测试
 * [POS]: internal/update 的测试文件，覆盖版本比较、asset 匹配、API 调用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// -----------------------------------------------------------------------
// isNewer 测试
// -----------------------------------------------------------------------

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current string
		remote  string
		want    bool
	}{
		{"1.0.0", "v1.0.1", true},
		{"1.0.0", "v1.0.0", false},
		{"1.0.1", "v1.0.0", false},
		{"v2.0.0", "v3.0.0", true},
		{"DEV", "v1.0.0", true},       // DEV 始终可更新
		{"invalid", "v1.0.0", true},   // 非法版本始终可更新
		{"1.0.0", "invalid", false},   // 远程版本非法则不更新
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tt.current, tt.remote), func(t *testing.T) {
			got := isNewer(tt.current, tt.remote)
			if got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.remote, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// assetName 测试
// -----------------------------------------------------------------------

func TestAssetName(t *testing.T) {
	name := assetName("1.2.3")
	expected := fmt.Sprintf("makecli_1.2.3_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	if name != expected {
		t.Errorf("assetName(1.2.3) = %q, want %q", name, expected)
	}
}

// -----------------------------------------------------------------------
// findAsset 测试
// -----------------------------------------------------------------------

func TestFindAsset(t *testing.T) {
	target := fmt.Sprintf("makecli_1.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	assets := []Asset{
		{Name: "makecli_1.0.0_fakeos_fakearch.tar.gz", BrowserDownloadURL: "https://example.com/other"},
		{Name: target, BrowserDownloadURL: "https://example.com/match"},
	}

	asset, err := findAsset(assets, "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if asset.BrowserDownloadURL != "https://example.com/match" {
		t.Errorf("got URL %q, want https://example.com/match", asset.BrowserDownloadURL)
	}
}

func TestFindAssetNotFound(t *testing.T) {
	assets := []Asset{
		{Name: "makecli_1.0.0_fakeos_fakearch.tar.gz"},
	}
	_, err := findAsset(assets, "1.0.0")
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
}

// -----------------------------------------------------------------------
// CheckLatest 测试（使用 httptest mock GitHub API）
// -----------------------------------------------------------------------

func TestCheckLatest_Newer(t *testing.T) {
	release := Release{
		TagName: "v2.0.0",
		Assets:  []Asset{{Name: "test.tar.gz"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	rel, newer, err := CheckLatest("1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer {
		t.Error("expected newer=true")
	}
	if rel.TagName != "v2.0.0" {
		t.Errorf("got tag %q, want v2.0.0", rel.TagName)
	}
}

func TestCheckLatest_UpToDate(t *testing.T) {
	release := Release{TagName: "v1.0.0"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	_, newer, err := CheckLatest("1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newer {
		t.Error("expected newer=false for same version")
	}
}

func TestCheckLatest_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	_, _, err := CheckLatest("1.0.0")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}
