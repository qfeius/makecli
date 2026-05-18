/**
 * [INPUT]: 依赖 cmd 包内的 runVersionList 与 internal/update 的 apiBaseURL
 * [OUTPUT]: 覆盖 version list 子命令的单元测试
 * [POS]: cmd 模块 version_list.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/update"
)

func mockReleasesServer(t *testing.T, status int, body any) func() {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
	old := update.SetAPIBaseURLForTest(srv.URL)
	return func() {
		update.SetAPIBaseURLForTest(old)
		srv.Close()
	}
}

func TestRunVersionList_Table(t *testing.T) {
	cleanup := mockReleasesServer(t, 0, []update.Release{
		{TagName: "v1.2.3", Name: "v1.2.3 - fix", PublishedAt: "2026-05-10T08:12:00Z", HTMLURL: "https://example.com/r/1.2.3"},
		{TagName: "v1.2.2", Name: "v1.2.2 - perf", PublishedAt: "2026-05-01T03:55:11Z", HTMLURL: "https://example.com/r/1.2.2"},
	})
	defer cleanup()

	out := captureStdout(t, func() {
		if err := runVersionList(20, "table"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	for _, want := range []string{"VERSION", "PUBLISHED", "URL", "v1.2.3", "v1.2.2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "NAME") {
		t.Errorf("NAME column should be removed, got:\n%s", out)
	}
}

func TestRunVersionList_TableMarksCurrent(t *testing.T) {
	oldVersion := build.Version
	build.Version = "1.2.3"
	defer func() { build.Version = oldVersion }()

	cleanup := mockReleasesServer(t, 0, []update.Release{
		{TagName: "v1.2.3", Name: "current", PublishedAt: "2026-05-10T08:12:00Z", HTMLURL: "https://example.com/r/1.2.3"},
		{TagName: "v1.2.2", Name: "older", PublishedAt: "2026-05-01T03:55:11Z", HTMLURL: "https://example.com/r/1.2.2"},
	})
	defer cleanup()

	out := captureStdout(t, func() {
		if err := runVersionList(20, "table"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	lines := strings.Split(out, "\n")
	var currentLine, olderLine string
	for _, ln := range lines {
		if strings.Contains(ln, "v1.2.3") {
			currentLine = ln
		}
		if strings.Contains(ln, "v1.2.2") {
			olderLine = ln
		}
	}
	if !strings.Contains(currentLine, "*") {
		t.Errorf("expected v1.2.3 row to contain *, got %q", currentLine)
	}
	if strings.Contains(olderLine, "*") {
		t.Errorf("expected v1.2.2 row to not contain *, got %q", olderLine)
	}
}

func TestRunVersionList_JSON(t *testing.T) {
	cleanup := mockReleasesServer(t, 0, []update.Release{
		{TagName: "v1.2.3", Name: "fix", PublishedAt: "2026-05-10T08:12:00Z", Prerelease: false, HTMLURL: "https://example.com/r/1.2.3"},
	})
	defer cleanup()

	out := captureStdout(t, func() {
		if err := runVersionList(20, "json"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["tag_name"] != "v1.2.3" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
	if _, ok := got[0]["assets"]; ok {
		t.Errorf("assets should not appear in JSON output, got %+v", got[0])
	}
}

func TestRunVersionList_Empty(t *testing.T) {
	cleanup := mockReleasesServer(t, 0, []update.Release{})
	defer cleanup()

	out := captureStdout(t, func() {
		if err := runVersionList(20, "table"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "No releases found.") {
		t.Errorf("expected 'No releases found.' in output, got %q", out)
	}
}

func TestRunVersionList_InvalidLimit(t *testing.T) {
	for _, lim := range []int{0, -1, 101, 1000} {
		if err := runVersionList(lim, "table"); err == nil {
			t.Errorf("expected error for limit=%d", lim)
		}
	}
}

func TestRunVersionList_InvalidOutput(t *testing.T) {
	if err := runVersionList(20, "xml"); err == nil {
		t.Fatal("expected error for output=xml")
	}
}

func TestRunVersionList_APIError(t *testing.T) {
	cleanup := mockReleasesServer(t, http.StatusInternalServerError, nil)
	defer cleanup()

	if err := runVersionList(20, "table"); err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestRunVersionList_TableDevVersionNoMarker(t *testing.T) {
	oldVersion := build.Version
	build.Version = "DEV"
	defer func() { build.Version = oldVersion }()

	cleanup := mockReleasesServer(t, 0, []update.Release{
		{TagName: "v1.2.3", Name: "fix", PublishedAt: "2026-05-10T08:12:00Z", HTMLURL: "https://example.com/r/1.2.3"},
		{TagName: "v1.2.2", Name: "perf", PublishedAt: "2026-05-01T03:55:11Z", HTMLURL: "https://example.com/r/1.2.2"},
	})
	defer cleanup()

	out := captureStdout(t, func() {
		if err := runVersionList(20, "table"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// DEV 版本不应在任何行打 * 标记
	if strings.Contains(out, "*") {
		t.Errorf("DEV build.Version should not produce any * marker, got:\n%s", out)
	}
}
