# makecli version list Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `makecli version list` subcommand that lists historical GitHub releases as a tablewriter table, with `--limit` and `--output` flags.

**Architecture:** Extend `internal/update` with `ListReleases(limit int)` that calls GitHub Releases API. Refactor `cmd/version.go` to host a `list` subcommand while preserving its default `Run`. Add `cmd/version_list.go` for the new command. Output style mirrors `app list`.

**Tech Stack:** Go, `github.com/spf13/cobra`, `github.com/olekukonko/tablewriter`, `net/http`, `encoding/json`, `net/http/httptest` for tests.

**Reference spec:** `docs/superpowers/specs/2026-05-18-version-list-design.md`

---

## File Structure

**Modify:**
- `internal/update/update.go` — extend `Release` struct, add `ListReleases(limit int)`
- `internal/update/update_test.go` — tests for `ListReleases`
- `internal/update/CLAUDE.md` — L2 update
- `cmd/version.go` — refactor `newVersionCmd` to mount `list` subcommand
- `cmd/CLAUDE.md` — L2 update

**Create:**
- `cmd/version_list.go` — new subcommand implementation
- `cmd/version_list_test.go` — tests for the new subcommand

---

## Task 1: Extend `Release` struct in `internal/update`

**Files:**
- Modify: `internal/update/update.go` (struct definition near top of file)

**Why:** `ListReleases` callers need `Name`, `PublishedAt`, `Prerelease`, `HTMLURL`. These are GitHub API fields not currently captured. Tests in Task 2 will assert them.

- [ ] **Step 1: Extend Release struct**

In `internal/update/update.go`, replace the existing `Release` struct with:

```go
// Release 表示 GitHub Releases API 返回的版本
type Release struct {
	TagName     string  `json:"tag_name"`
	Name        string  `json:"name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets"`
}
```

- [ ] **Step 2: Verify existing tests still pass**

Run: `make test`
Expected: PASS — `CheckLatest` and `Apply` paths only use `TagName` and `Assets`, new fields default to zero values.

- [ ] **Step 3: Commit**

```bash
git add internal/update/update.go
git commit -m "refactor(update): extend Release with name/published_at/prerelease/html_url"
```

---

## Task 2: Add `ListReleases` with TDD

**Files:**
- Modify: `internal/update/update_test.go`
- Modify: `internal/update/update.go`

- [ ] **Step 1: Write failing tests for `ListReleases`**

Append to `internal/update/update_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/update/ -run TestListReleases -v`
Expected: FAIL with "undefined: ListReleases"

- [ ] **Step 3: Implement `ListReleases`**

In `internal/update/update.go`, add this function under the `// 公开 API` section, after `CheckLatest`:

```go
// ListReleases 拉取最近 limit 条 release（按 created_at 倒序）
func ListReleases(limit int) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/qfeius/makecli/releases?per_page=%d", apiBaseURL, limit)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list releases: HTTP %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases: %w", err)
	}

	return releases, nil
}
```

- [ ] **Step 4: Update file header L3 contract**

In `internal/update/update.go`, update the `[OUTPUT]` line of the file header comment to include `ListReleases`:

```go
 * [OUTPUT]: 对外提供 CheckLatest / ListReleases / Apply 函数、Release / Asset 结构体
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test`
Expected: PASS — all `internal/update` tests including the 3 new ones.

- [ ] **Step 6: Commit**

```bash
git add internal/update/update.go internal/update/update_test.go
git commit -m "feat(update): add ListReleases for GitHub releases API"
```

---

## Task 3: Refactor `cmd/version.go` to host `list` subcommand

**Files:**
- Modify: `cmd/version.go`

**Why:** Cobra commands can have both `Run` and child commands. We preserve `makecli version` behavior while enabling `makecli version list`.

- [ ] **Step 1: Modify `newVersionCmd` to mount subcommand**

Replace `newVersionCmd` in `cmd/version.go` with:

```go
func newVersionCmd(version, buildDate string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(formatVersion(version, buildDate))
		},
	}
	cmd.AddCommand(newVersionListCmd())
	return cmd
}
```

`formatVersion` and `changelogURL` stay unchanged.

- [ ] **Step 2: Update file header L3 contract**

In `cmd/version.go`, update the `[POS]` line:

```go
 * [POS]: cmd 模块的 version 子命令，挂载 list 子命令；默认 Run 打印当前版本（参考 GitHub CLI 模式）
```

- [ ] **Step 3: Verify compile (will fail until Task 4 adds `newVersionListCmd`)**

Run: `go build ./...`
Expected: FAIL with "undefined: newVersionListCmd" — that's expected, we'll fix in Task 4. Do NOT commit yet; combine with Task 4 implementation.

---

## Task 4: Implement `cmd/version_list.go` with TDD

**Files:**
- Create: `cmd/version_list_test.go`
- Create: `cmd/version_list.go`

- [ ] **Step 1: Write failing tests**

Create `cmd/version_list_test.go`:

```go
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
		if err := runVersionList(nil, 20, "table"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	for _, want := range []string{"VERSION", "PUBLISHED", "NAME", "URL", "v1.2.3", "v1.2.2", "v1.2.3 - fix"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
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
		if err := runVersionList(nil, 20, "table"); err != nil {
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
		if err := runVersionList(nil, 20, "json"); err != nil {
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
		if err := runVersionList(nil, 20, "table"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "No releases found.") {
		t.Errorf("expected 'No releases found.' in output, got %q", out)
	}
}

func TestRunVersionList_InvalidLimit(t *testing.T) {
	for _, lim := range []int{0, -1, 101, 1000} {
		if err := runVersionList(nil, lim, "table"); err == nil {
			t.Errorf("expected error for limit=%d", lim)
		}
	}
}

func TestRunVersionList_InvalidOutput(t *testing.T) {
	if err := runVersionList(nil, 20, "xml"); err == nil {
		t.Fatal("expected error for output=xml")
	}
}

func TestRunVersionList_APIError(t *testing.T) {
	cleanup := mockReleasesServer(t, http.StatusInternalServerError, nil)
	defer cleanup()

	if err := runVersionList(nil, 20, "table"); err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}
```

The tests reference `update.SetAPIBaseURLForTest`, which doesn't exist yet — we add it in Step 2 below.

- [ ] **Step 2: Add test helper to `internal/update`**

Append to `internal/update/update.go` (at end of file):

```go
// SetAPIBaseURLForTest 替换 GitHub API 基础 URL，返回原值用于恢复。仅供测试使用。
func SetAPIBaseURLForTest(url string) string {
	old := apiBaseURL
	apiBaseURL = url
	return old
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/ -run TestRunVersionList -v`
Expected: FAIL with "undefined: runVersionList" and "undefined: newVersionListCmd".

- [ ] **Step 4: Create `cmd/version_list.go`**

Create `cmd/version_list.go`:

```go
/**
 * [INPUT]: 依赖 cmd/output（writeJSON / validateOutputFormat / outputTable / outputJSON）、internal/update（ListReleases）、internal/build（Version）、fmt、os、strings、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newVersionListCmd 函数（包内）
 * [POS]: cmd 模块 version 子命令下的 list 子命令，列出 GitHub 上的历史 release
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/update"
	"github.com/spf13/cobra"
)

func newVersionListCmd() *cobra.Command {
	var limit int
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List historical releases from GitHub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersionList(cmd, limit, output)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "number of releases to fetch (1-100)")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format: table|json")
	return cmd
}

func runVersionList(_ *cobra.Command, limit int, output string) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	if err := validateOutputFormat(output); err != nil {
		return err
	}

	releases, err := update.ListReleases(limit)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(toReleaseJSONView(releases))
	}
	return renderReleaseTable(releases, build.Version)
}

type releaseJSONView struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	HTMLURL     string `json:"html_url"`
}

func toReleaseJSONView(releases []update.Release) []releaseJSONView {
	out := make([]releaseJSONView, len(releases))
	for i, r := range releases {
		out[i] = releaseJSONView{
			TagName:     r.TagName,
			Name:        r.Name,
			PublishedAt: r.PublishedAt,
			Prerelease:  r.Prerelease,
			HTMLURL:     r.HTMLURL,
		}
	}
	return out
}

func renderReleaseTable(releases []update.Release, currentVersion string) error {
	if len(releases) == 0 {
		fmt.Println("No releases found.")
		return nil
	}

	currentTag := strings.TrimPrefix(currentVersion, "v")
	rows := make([][]string, len(releases))
	for i, r := range releases {
		marker := ""
		if strings.TrimPrefix(r.TagName, "v") == currentTag && currentTag != "" && currentTag != "DEV" {
			marker = "*"
		}
		name := r.Name
		if name == "" {
			name = r.TagName
		}
		rows[i] = []string{marker, r.TagName, r.PublishedAt, name, r.HTMLURL}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("CURRENT", "VERSION", "PUBLISHED", "NAME", "URL")
	_ = table.Bulk(rows)
	_ = table.Render()
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test`
Expected: PASS — all tests across the repo including the 7 new `TestRunVersionList_*` and 3 new `TestListReleases_*`.

- [ ] **Step 6: Run vet and build**

Run: `make vet && make build`
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add cmd/version.go cmd/version_list.go cmd/version_list_test.go internal/update/update.go
git commit -m "feat(version): add 'version list' subcommand to list GitHub releases"
```

---

## Task 5: Update documentation (L2 maps)

**Files:**
- Modify: `cmd/CLAUDE.md`
- Modify: `internal/update/CLAUDE.md`

- [ ] **Step 1: Update `cmd/CLAUDE.md` member list**

In `cmd/CLAUDE.md`, locate the `version.go:` line under `## 成员清单`. Replace it with:

```
version.go:          version 子命令组，默认 Run 打印当前版本（参考 GitHub CLI 模式），挂载 list 子命令
version_test.go:     覆盖 formatVersion / changelogURL 的纯函数测试
version_list.go:     version list 子命令，调 internal/update.ListReleases 拉取 GitHub 最近 N 条 release，tablewriter 输出 CURRENT/VERSION/PUBLISHED/NAME/URL；CURRENT 列对比 build.Version 标记当前安装版本；支持 --limit（默认20，1-100）/ --output（table|json）
version_list_test.go: 覆盖 runVersionList 的单元测试（table 渲染 / CURRENT 标记 / JSON 输出去除 assets / 空列表 / 非法 limit / 非法 output / API 错误），用 httptest 隔离网络
```

- [ ] **Step 2: Update `internal/update/CLAUDE.md` member list**

Replace the content under `## 成员清单` in `internal/update/CLAUDE.md` with:

```
update.go:      自更新引擎，CheckLatest 查询 GitHub latest release、ListReleases 拉取最近 N 条 release、Apply 下载→解压→原子替换；内部实现 isNewer（semver 比较，DEV 视为始终可更新）、download/extractBinary/replaceBinary 完整流水线；导出 SetAPIBaseURLForTest 供 cmd 层测试替换 API URL
update_test.go: 覆盖 isNewer / assetName / findAsset / CheckLatest / ListReleases 的单元测试，用 httptest 隔离网络
```

- [ ] **Step 3: Commit**

```bash
git add cmd/CLAUDE.md internal/update/CLAUDE.md
git commit -m "docs: sync L2 maps for version list subcommand"
```

---

## Task 6: Final verification

- [ ] **Step 1: Run full test suite**

Run: `make test`
Expected: PASS — entire repo.

- [ ] **Step 2: Static check**

Run: `make vet`
Expected: no warnings.

- [ ] **Step 3: Build binary**

Run: `make build`
Expected: produces `bin/makecli`.

- [ ] **Step 4: Smoke test the new command**

Run: `./bin/makecli version`
Expected: prints `makecli version DEV` + a changelog URL (unchanged behavior).

Run: `./bin/makecli version list --limit 5`
Expected: prints a tablewriter table with up to 5 historical releases. `CURRENT` column empty for DEV build. No errors.

Run: `./bin/makecli version list --limit 3 --output json`
Expected: JSON array of 3 releases with `tag_name`, `name`, `published_at`, `prerelease`, `html_url`. No `assets` field.

Run: `./bin/makecli version list --limit 0`
Expected: exits non-zero with `Error: limit must be between 1 and 100`.

Run: `./bin/makecli version list --output xml`
Expected: exits non-zero with `Error: unsupported output format "xml"...`.

- [ ] **Step 5: No-op commit if verification reveals nothing**

If the manual run uncovers issues, fix them as a new task. Otherwise, the previous commits already capture the work — no extra commit needed.
