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
	"strings"
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

// -----------------------------------------------------------------------
// ListReleases 测试
// -----------------------------------------------------------------------

func TestListReleases_Success(t *testing.T) {
	releases := []Release{
		{TagName: "v1.2.3", Name: "v1.2.3 - fix", PublishedAt: "2026-05-10T08:12:00Z", Prerelease: false, HTMLURL: "https://example.com/r/1.2.3"},
		{TagName: "v1.2.2", Name: "v1.2.2 - perf", PublishedAt: "2026-05-01T03:55:11Z", Prerelease: true, HTMLURL: "https://example.com/r/1.2.2"},
	}

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	got, err := ListReleases(20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d releases, want 2", len(got))
	}
	if got[0].TagName != "v1.2.3" || got[0].Name != "v1.2.3 - fix" {
		t.Errorf("first release mismatch: %+v", got[0])
	}
	if got[1].Prerelease != true {
		t.Errorf("expected second release prerelease=true, got %+v", got[1])
	}
	if gotQuery != "per_page=20" {
		t.Errorf("expected query per_page=20, got %q", gotQuery)
	}
}

func TestListReleases_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	_, err := ListReleases(20)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestListReleases_ParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	_, err := ListReleases(20)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// -----------------------------------------------------------------------
// NormalizeTag 测试
// -----------------------------------------------------------------------

func TestNormalizeTag(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"v0.2.0", "v0.2.0", false},
		{"0.2.0", "v0.2.0", false},
		{"v1.0.0-beta.1", "v1.0.0-beta.1", false},
		{"1.0.0-beta.1", "v1.0.0-beta.1", false},
		{"", "", true},
		{"v", "", true},
		{"abc", "", true},
		{"1.2", "", true},
		{"1.2.3.4", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeTag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeTag(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NormalizeTag(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// GetRelease 测试
// -----------------------------------------------------------------------

func TestGetRelease_Success(t *testing.T) {
	release := Release{
		TagName: "v0.2.0",
		Name:    "v0.2.0",
		Assets:  []Asset{{Name: "makecli_0.2.0_linux_amd64.tar.gz"}},
	}

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	got, err := GetRelease("v0.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TagName != "v0.2.0" {
		t.Errorf("got tag %q, want v0.2.0", got.TagName)
	}
	if gotPath != "/repos/qfeius/makecli/releases/tags/v0.2.0" {
		t.Errorf("unexpected path %q", gotPath)
	}
}

func TestGetRelease_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	_, err := GetRelease("v9.9.9")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestGetRelease_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()

	_, err := GetRelease("v0.2.0")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

// -----------------------------------------------------------------------
// metaClient 超时守卫
// -----------------------------------------------------------------------

func TestMetaClientHasTimeout(t *testing.T) {
	if metaClient.Timeout <= 0 {
		t.Error("metaClient must carry a positive timeout to bound background refresh")
	}
}

// -----------------------------------------------------------------------
// CompareVersions 测试
// -----------------------------------------------------------------------

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		target, current string
		want            int
	}{
		// 标准比较
		{"v1.0.0", "v0.9.0", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"v0.9.0", "v1.0.0", -1},
		// 不带 v 前缀的 current 也支持
		{"v1.0.0", "1.0.0", 0},
		// DEV current → 返回 1（永远旧）
		{"v1.0.0", "DEV", 1},
		{"v0.0.1", "DEV", 1},
		// 非法 current → 返回 1
		{"v1.0.0", "abc", 1},
		{"v1.0.0", "", 1},
		{"v1.0.0", "v0.2.16-7-gd65ec7e", 1}, // git-describe dirty 形式
		// pre-release
		{"v1.0.0-beta.2", "v1.0.0-beta.1", 1},
		{"v1.0.0", "v1.0.0-beta.1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.target+"_vs_"+tt.current, func(t *testing.T) {
			got := CompareVersions(tt.target, tt.current)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.target, tt.current, got, tt.want)
			}
		})
	}
}
