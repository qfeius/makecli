# makecli update [version] Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `makecli update` to accept an optional `[version]` positional arg (e.g. `v0.2.0` or `0.2.0`), defaulting to latest. Downgrades require `--force`. DEV builds skip comparison.

**Architecture:** Add 3 exported helpers to `internal/update` (`NormalizeTag` / `GetRelease` / `CompareVersions`). Refactor `cmd/update.go` into latest vs specific branches with an `applyFunc` test hook so unit tests don't actually replace the binary. New `cmd/update_test.go` covers all decision branches.

**Tech Stack:** Go, `github.com/Masterminds/semver/v3` (already a dep), `github.com/spf13/cobra`, `net/http/httptest`.

**Reference spec:** `docs/superpowers/specs/2026-05-18-update-versioned-design.md`

---

## File Structure

**Modify:**
- `internal/update/update.go` — add `NormalizeTag`, `GetRelease`, `CompareVersions`
- `internal/update/update_test.go` — tests for the three new functions
- `cmd/update.go` — `[version]` arg + `--force` flag, branches, `applyFunc` hook
- `cmd/CLAUDE.md` — L2 update
- `internal/update/CLAUDE.md` — L2 update

**Create:**
- `cmd/update_test.go` — first test file for the update command

---

## Task 1: Add `NormalizeTag` to `internal/update`

**Files:**
- Modify: `internal/update/update_test.go` (append test)
- Modify: `internal/update/update.go` (add function + update OUTPUT header)

- [ ] **Step 1: Write failing test**

Append to `internal/update/update_test.go`:

```go
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
```

- [ ] **Step 2: Verify test fails**

Run: `go test ./internal/update/ -run TestNormalizeTag -v`
Expected: FAIL with "undefined: NormalizeTag"

- [ ] **Step 3: Implement `NormalizeTag`**

In `internal/update/update.go`, add under the `// 公开 API` section after `ListReleases`:

```go
// NormalizeTag 将输入归一化为带 v 前缀的合法 semver tag。
//   "v0.2.0"          → "v0.2.0"
//   "0.2.0"           → "v0.2.0"
//   "1.0.0-beta.1"    → "v1.0.0-beta.1"
//   非法 semver 返回 error。
func NormalizeTag(input string) (string, error) {
	stripped := strings.TrimPrefix(input, "v")
	if stripped == "" {
		return "", fmt.Errorf("invalid version %q: empty", input)
	}
	if _, err := semver.NewVersion(stripped); err != nil {
		return "", fmt.Errorf("invalid version %q: %w", input, err)
	}
	return "v" + stripped, nil
}
```

- [ ] **Step 4: Update L3 header**

In `internal/update/update.go`, update the `[OUTPUT]` line to:

```
 * [OUTPUT]: 对外提供 CheckLatest / ListReleases / NormalizeTag / Apply 函数、Release / Asset 结构体
```

- [ ] **Step 5: Verify test passes**

Run: `go test ./internal/update/ -run TestNormalizeTag -v`
Expected: PASS, all 9 sub-tests.

- [ ] **Step 6: Commit**

```bash
git add internal/update/update.go internal/update/update_test.go
git commit -m "feat(update): add NormalizeTag for version string handling"
```

---

## Task 2: Add `GetRelease` to `internal/update`

**Files:**
- Modify: `internal/update/update_test.go`
- Modify: `internal/update/update.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/update/update_test.go`:

```go
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
```

- [ ] **Step 2: Verify test fails**

Run: `go test ./internal/update/ -run TestGetRelease -v`
Expected: FAIL with "undefined: GetRelease"

- [ ] **Step 3: Implement `GetRelease`**

In `internal/update/update.go`, add under the `// 公开 API` section after `NormalizeTag`:

```go
// GetRelease 按 tag 拉取指定 release。tag 必须是规范化形式（带 v 前缀）。
//   404 → "release {tag} not found"
//   其他非 200 → "failed to fetch release {tag}: HTTP {code}"
func GetRelease(tag string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/qfeius/makecli/releases/tags/%s", apiBaseURL, tag)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release %s: %w", tag, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release %s not found", tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch release %s: HTTP %d", tag, resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release %s: %w", tag, err)
	}
	return &release, nil
}
```

- [ ] **Step 4: Update L3 header**

Update `[OUTPUT]` line to:

```
 * [OUTPUT]: 对外提供 CheckLatest / ListReleases / NormalizeTag / GetRelease / Apply 函数、Release / Asset 结构体
```

- [ ] **Step 5: Verify test passes**

Run: `go test ./internal/update/ -run TestGetRelease -v`
Expected: PASS, all 3 sub-tests.

- [ ] **Step 6: Commit**

```bash
git add internal/update/update.go internal/update/update_test.go
git commit -m "feat(update): add GetRelease for specific tag lookup"
```

---

## Task 3: Add `CompareVersions` to `internal/update`

**Files:**
- Modify: `internal/update/update_test.go`
- Modify: `internal/update/update.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/update/update_test.go`:

```go
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
```

- [ ] **Step 2: Verify test fails**

Run: `go test ./internal/update/ -run TestCompareVersions -v`
Expected: FAIL with "undefined: CompareVersions"

- [ ] **Step 3: Implement `CompareVersions`**

In `internal/update/update.go`, add under the `// 公开 API` section after `GetRelease`:

```go
// CompareVersions 比较 target 与 current 的 semver 大小：
//   target > current  →  1
//   target == current →  0
//   target < current  → -1
//
// 若 current 解析失败（DEV / dirty / 非法），返回 1 — 视为「current 永远旧」，
// 这样调用方的「降级保护」对 DEV 构建自然失效。
func CompareVersions(target, current string) int {
	tgt, err := semver.NewVersion(strings.TrimPrefix(target, "v"))
	if err != nil {
		// target 应已被 NormalizeTag 校验过；保险返回 0
		return 0
	}
	cur, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return 1
	}
	return tgt.Compare(cur)
}
```

- [ ] **Step 4: Update L3 header**

Update `[OUTPUT]` line to:

```
 * [OUTPUT]: 对外提供 CheckLatest / ListReleases / NormalizeTag / GetRelease / CompareVersions / Apply 函数、Release / Asset 结构体
```

- [ ] **Step 5: Verify all tests pass**

Run: `make test`
Expected: PASS — all packages including the 11 new sub-tests across `TestCompareVersions`.

- [ ] **Step 6: Commit**

```bash
git add internal/update/update.go internal/update/update_test.go
git commit -m "feat(update): add CompareVersions with DEV-safe semantics"
```

---

## Task 4: Refactor `cmd/update.go` with arg + flag + applyFunc hook

**Files:**
- Modify: `cmd/update.go`

**Why standalone first:** This task introduces the new shape but keeps the latest-only behavior compiling and passing tests. Task 5 adds the test file that exercises both branches.

- [ ] **Step 1: Replace `cmd/update.go` content**

Replace the entire file with:

```go
/**
 * [INPUT]: 依赖 github.com/spf13/cobra、internal/update、internal/build
 * [OUTPUT]: 对外提供 newUpdateCmd 函数
 * [POS]: cmd 模块的 update 子命令，从 GitHub Releases 自更新二进制；
 *        无 arg 走 latest 流程，有 arg 走指定版本流程；降级需 --force；
 *        DEV 版本跳过比较
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/update"
	"github.com/spf13/cobra"
)

// applyFunc 包装 update.Apply，便于测试打桩避免真实替换二进制。
var applyFunc = update.Apply

func newUpdateCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:          "update [version]",
		Short:        "Update makecli to the latest or a specific version",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runUpdate(cmd, target, force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "allow downgrade to an older version")
	return cmd
}

func runUpdate(cmd *cobra.Command, target string, force bool) error {
	currentVersion := build.Version
	if target == "" {
		return runUpdateLatest(cmd, currentVersion)
	}
	return runUpdateSpecific(cmd, currentVersion, target, force)
}

func runUpdateLatest(cmd *cobra.Command, currentVersion string) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Checking for updates...\n")

	release, newer, err := update.CheckLatest(currentVersion)
	if err != nil {
		return err
	}

	if !newer {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (%s)\n", release.TagName)
		return nil
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updating makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), release.TagName)

	if err := applyFunc(release); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), release.TagName)
	return nil
}

func runUpdateSpecific(cmd *cobra.Command, currentVersion, target string, force bool) error {
	tag, err := update.NormalizeTag(target)
	if err != nil {
		return err
	}

	release, err := update.GetRelease(tag)
	if err != nil {
		return err
	}

	cmp := update.CompareVersions(tag, currentVersion)
	switch {
	case cmp == 0:
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already at %s, skipping.\n", tag)
		return nil
	case cmp < 0 && !force:
		return fmt.Errorf("%s is older than current %s. Use --force to downgrade",
			tag, formatCurrentVersion(currentVersion))
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updating makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), tag)

	if err := applyFunc(release); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), tag)
	return nil
}

// formatCurrentVersion 格式化当前版本号用于显示
func formatCurrentVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if v == "DEV" {
		return v
	}
	return "v" + v
}
```

- [ ] **Step 2: Verify build and existing behavior**

Run: `make vet && make build`
Expected: both succeed; no test changes yet so existing tests still pass via:
```
make test
```

- [ ] **Step 3: Smoke check help**

Run: `./bin/makecli update --help 2>&1 | head -15`
Expected: shows `update [version]` usage and `--force, -f` flag.

- [ ] **Step 4: Commit**

```bash
git add cmd/update.go
git commit -m "feat(update): accept [version] arg and --force flag

Refactor into runUpdateLatest / runUpdateSpecific branches, with an
applyFunc package var to enable test stubbing without replacing the
real binary."
```

---

## Task 5: Create `cmd/update_test.go` with TDD coverage

**Files:**
- Create: `cmd/update_test.go`

- [ ] **Step 1: Create the test file**

```go
/**
 * [INPUT]: 依赖 cmd 包内的 runUpdate / applyFunc（白盒），internal/update 的 Release 类型 + SetAPIBaseURLForTest，internal/build 的 Version
 * [OUTPUT]: 覆盖 update 子命令决策逻辑的单元测试
 * [POS]: cmd 模块 update.go 的配套测试，applyFunc 钩子打桩避免真实替换二进制
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
	"github.com/spf13/cobra"
)

// setApplyFunc 在测试期间打桩 applyFunc 并在结束时恢复
func setApplyFunc(t *testing.T, f func(*update.Release) error) *bool {
	t.Helper()
	called := false
	old := applyFunc
	applyFunc = func(r *update.Release) error {
		called = true
		return f(r)
	}
	t.Cleanup(func() { applyFunc = old })
	return &called
}

// setBuildVersion 在测试期间覆盖 build.Version
func setBuildVersion(t *testing.T, v string) {
	t.Helper()
	old := build.Version
	build.Version = v
	t.Cleanup(func() { build.Version = old })
}

// mockReleaseServer 启动 httptest 服务器并替换 apiBaseURL
//   path == "" 时所有请求都返回 body；否则只匹配该 path 返回 body，其他返回 404
func mockReleaseServer(t *testing.T, status int, body any) func() {
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

// noopApply 是 applyFunc 的成功桩
func noopApply(_ *update.Release) error { return nil }

// dummyCmd 提供一个有 OutOrStdout 的 cobra.Command 实例供 runUpdate 使用
func dummyCmd() *cobra.Command {
	return &cobra.Command{}
}

// ----------------------------------------------------------------------
// 无 arg：latest 流程
// ----------------------------------------------------------------------

func TestRunUpdate_NoArg_AlreadyLatest(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v1.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	if err := runUpdate(dummyCmd(), "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *called {
		t.Error("applyFunc should not be called when already up to date")
	}
}

func TestRunUpdate_NoArg_Upgrade(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v2.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	if err := runUpdate(dummyCmd(), "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*called {
		t.Error("applyFunc should be called when newer release is available")
	}
}

// ----------------------------------------------------------------------
// 指定版本
// ----------------------------------------------------------------------

func TestRunUpdate_SpecificVersion_Upgrade(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v2.0.0"})
	defer cleanup()

	var appliedTag string
	called := setApplyFunc(t, func(r *update.Release) error {
		appliedTag = r.TagName
		return nil
	})

	if err := runUpdate(dummyCmd(), "v2.0.0", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*called {
		t.Fatal("applyFunc should be called for upgrade")
	}
	if appliedTag != "v2.0.0" {
		t.Errorf("applied tag = %q, want v2.0.0", appliedTag)
	}
}

func TestRunUpdate_SpecificVersion_NormalizeWithoutV(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v2.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	// 输入不带 v 前缀
	if err := runUpdate(dummyCmd(), "2.0.0", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*called {
		t.Error("applyFunc should be called when target normalizes to a newer version")
	}
}

func TestRunUpdate_SpecificVersion_SameVersion(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v1.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	if err := runUpdate(dummyCmd(), "v1.0.0", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *called {
		t.Error("applyFunc should NOT be called when target == current")
	}
}

func TestRunUpdate_SpecificVersion_DowngradeRefused(t *testing.T) {
	setBuildVersion(t, "2.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v1.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	err := runUpdate(dummyCmd(), "v1.0.0", false)
	if err == nil {
		t.Fatal("expected downgrade refusal error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should hint at --force, got: %v", err)
	}
	if *called {
		t.Error("applyFunc should NOT be called on refused downgrade")
	}
}

func TestRunUpdate_SpecificVersion_DowngradeWithForce(t *testing.T) {
	setBuildVersion(t, "2.0.0")
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v1.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	if err := runUpdate(dummyCmd(), "v1.0.0", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*called {
		t.Error("applyFunc should be called with --force on downgrade")
	}
}

func TestRunUpdate_InvalidSemver(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	// mock server should not be hit
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v1.0.0"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	err := runUpdate(dummyCmd(), "abc", false)
	if err == nil {
		t.Fatal("expected error for invalid semver")
	}
	if *called {
		t.Error("applyFunc should NOT be called for invalid input")
	}
}

func TestRunUpdate_TagNotFound(t *testing.T) {
	setBuildVersion(t, "1.0.0")
	cleanup := mockReleaseServer(t, http.StatusNotFound, nil)
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	err := runUpdate(dummyCmd(), "v9.9.9", false)
	if err == nil {
		t.Fatal("expected error for missing tag")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say 'not found', got: %v", err)
	}
	if *called {
		t.Error("applyFunc should NOT be called on tag not found")
	}
}

func TestRunUpdate_DEVSkipsComparison(t *testing.T) {
	setBuildVersion(t, "DEV")
	// 选一个明显"旧"的版本作为 target —— DEV 应该允许而不需要 --force
	cleanup := mockReleaseServer(t, 0, update.Release{TagName: "v0.0.1"})
	defer cleanup()

	called := setApplyFunc(t, noopApply)

	if err := runUpdate(dummyCmd(), "v0.0.1", false); err != nil {
		t.Fatalf("DEV should allow apply without --force, got: %v", err)
	}
	if !*called {
		t.Error("applyFunc should be called when current is DEV")
	}
}
```

- [ ] **Step 2: Run tests to verify all pass**

Run: `go test ./cmd/ -run TestRunUpdate -v`
Expected: PASS — all 10 tests.

- [ ] **Step 3: Run full suite**

Run: `make test && make vet`
Expected: both clean.

- [ ] **Step 4: Run lint**

Run: `make lint`
Expected: 0 issues.

- [ ] **Step 5: Commit**

```bash
git add cmd/update_test.go
git commit -m "test(update): cover latest/specific/force/DEV decision branches"
```

---

## Task 6: Update L2 docs

**Files:**
- Modify: `cmd/CLAUDE.md`
- Modify: `internal/update/CLAUDE.md`

- [ ] **Step 1: Update `cmd/CLAUDE.md`**

In `cmd/CLAUDE.md`, locate the existing `update.go` line under `## 成员清单`:

```
update.go:           update 子命令，从 GitHub Releases 自更新二进制；直接 import internal/build 读取版本，委托 internal/update 执行检查和替换
```

Replace it with:

```
update.go:           update 子命令，支持 [version] 位置参数（v0.2.0 或 0.2.0）和 --force 标志；无 arg 走 CheckLatest 流程，指定版本走 GetRelease；CompareVersions 决定 upgrade/same/downgrade 分支，降级需 --force；DEV 版本跳过比较；applyFunc 包级变量便于测试打桩
update_test.go:      覆盖 runUpdate 的单元测试（latest 已到位/有更新、specific 升级/同版本/降级拒绝/--force 降级/规范化无 v 前缀/非法 semver/tag 不存在/DEV 跳过比较），applyFunc 打桩避免真实替换二进制
```

- [ ] **Step 2: Update `internal/update/CLAUDE.md`**

In `internal/update/CLAUDE.md`, replace the `update.go:` line with:

```
update.go:      自更新引擎，CheckLatest 查询 GitHub latest release、ListReleases 拉取最近 N 条 release、GetRelease 按 tag 精确查询、NormalizeTag 规范化版本号、CompareVersions 比较版本（DEV current 视为永远旧）、Apply 下载→解压→原子替换；内部实现 isNewer（semver 比较，DEV 视为始终可更新）、download/extractBinary/replaceBinary 完整流水线；导出 SetAPIBaseURLForTest 供 cmd 层测试替换 API URL
```

And replace the `update_test.go:` line with:

```
update_test.go: 覆盖 isNewer / assetName / findAsset / CheckLatest / ListReleases / NormalizeTag / GetRelease / CompareVersions 的单元测试，用 httptest 隔离网络
```

- [ ] **Step 3: Verify diffs**

Run: `git diff cmd/CLAUDE.md internal/update/CLAUDE.md`
Expected: only the two member-list lines changed in each file; `[PROTOCOL]` lines preserved.

- [ ] **Step 4: Commit**

```bash
git add cmd/CLAUDE.md internal/update/CLAUDE.md
git commit -m "docs: sync L2 maps for update [version] feature"
```

---

## Task 7: Final verification

- [ ] **Step 1: Full quality gates**

Run in sequence (or as one chained command):

```
make test
make vet
make lint
make build
```

All four must succeed (lint should report `0 issues.`).

- [ ] **Step 2: Smoke test help**

Run: `./bin/makecli update --help`
Expected output includes:
- `update [version]` in usage
- `--force, -f` flag with description "allow downgrade to an older version"

- [ ] **Step 3: Smoke test invalid input (no network needed)**

Run: `./bin/makecli update abc 2>&1; echo "exit=$?"`
Expected: prints error containing `invalid version`, `exit=1`.

- [ ] **Step 4: Smoke test tag not found (network required)**

Run: `./bin/makecli update v99.99.99 2>&1; echo "exit=$?"`
Expected: prints error containing `not found`, `exit=1`.

- [ ] **Step 5: Smoke test no-arg path (network required)**

Run: `./bin/makecli update 2>&1 | head -5`
Expected: prints "Checking for updates..." and either "Already up to date" or "Updating makecli: ..." (do NOT proceed past this — just verify the output line, then Ctrl+C if it stalls on download).

Note: if the binary is freshly built from a non-tagged commit (`build.Version` is a git-describe form like `v0.2.16-9-gXXX`), the CompareVersions treats it as "always older" — so `update` will attempt the upgrade. Add a `--help` check elsewhere if you don't want the real download to happen during smoke.

- [ ] **Step 6: No-op commit if verification reveals nothing**

If issues surface, fix them as follow-up tasks. Otherwise, the previous commits already capture the work — nothing more to commit here.
