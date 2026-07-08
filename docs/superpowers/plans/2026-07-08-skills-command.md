# makecli skills 命令组实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 增加 `makecli skills` 命令组（默认 = list），独立查看、升级、删除 Make platform skills，list 含 GitHub 远端 outdated 比对。

**Architecture:** `internal/skillsync` 新增 inventory.go（lockfile + SKILL.md frontmatter + GitHub Contents API 三源合并）和 remove.go（来源校验 + npx 透传）；`cmd` 新增 skills.go 命令组 + 三个子命令文件，update 子命令零新逻辑复用现有 `runSkillSync`。

**Tech Stack:** Go 1.25.8, cobra, olekukonko/tablewriter v1.1.3, gopkg.in/yaml.v3（均已在 go.mod）。

**Spec:** `docs/superpowers/specs/2026-07-08-skills-command-design.md`

## Global Constraints

- 表格统一 `github.com/olekukonko/tablewriter`（`NewTable(os.Stdout)` + `Header` + `Bulk` + `Render`），禁止 stdlib `text/tabwriter`。
- 每个 Go 文件头部带 `[INPUT]/[OUTPUT]/[POS]/[PROTOCOL]` 注释块（照抄邻居文件格式）。
- **验证门控提交**：每个 Task 先单独跑 `make vet && make test` 确认 exit 0，再单独执行 git commit。禁止同一批工具调用里 test + commit。Go 命令在沙箱下会因 module cache 不可写假性失败——直接禁用沙箱跑。
- Make skills 来源常量复用已有的 `skillsync.SkillsSource`（值 `"qfeius/make-platform-skills"`），不重复定义。
- 远端比对失败不是错误：STATUS 降级 `unknown`、stderr 警告、退出码 0。
- 不做：`remove --all`、非 Make 来源展示、项目级 lockfile、semver。

---

### Task 1: internal/skillsync/inventory.go — lockfile 读取 + SKILL.md frontmatter

**Files:**
- Create: `internal/skillsync/inventory.go`
- Test: `internal/skillsync/inventory_test.go`

**Interfaces:**
- Consumes: 已有的 `SkillsSource` 常量（sync.go）。
- Produces: `readLock() (map[string]lockEntry, string)`、`lockEntry{Source, SkillFolderHash, InstalledAt, UpdatedAt string}`、`readDescription(dir, name string) string`、`extractFrontmatter(data []byte) []byte`、测试接缝 `lockPathFunc`/`skillsDirFunc`（`var … = defaultXxx` 形式，Task 2/3 复用）。

- [ ] **Step 1: 写失败测试**

创建 `internal/skillsync/inventory_test.go`：

```go
/**
 * [INPUT]: 依赖 os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 readLock（缺失/过滤/损坏/版本不匹配）与 extractFrontmatter / readDescription
 * [POS]: internal/skillsync 清单层的本地数据源测试，t.TempDir 隔离文件系统
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubLockFile 把 lockPathFunc 指向 t.TempDir 下的临时 lockfile；content 为空则不创建文件。
func stubLockFile(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".skill-lock.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write lock: %v", err)
		}
	}
	orig := lockPathFunc
	lockPathFunc = func() string { return path }
	t.Cleanup(func() { lockPathFunc = orig })
}

// stubSkillsDir 把 skillsDirFunc 指向临时目录并返回该目录。
func stubSkillsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := skillsDirFunc
	skillsDirFunc = func() string { return dir }
	t.Cleanup(func() { skillsDirFunc = orig })
	return dir
}

const sampleLock = `{
  "version": 3,
  "skills": {
    "makedsl": {"source": "qfeius/make-platform-skills", "sourceType": "github", "skillFolderHash": "hash-dsl", "installedAt": "2026-07-01T00:00:00.000Z", "updatedAt": "2026-07-02T00:00:00.000Z"},
    "makeui": {"source": "qfeius/make-platform-skills", "sourceType": "github", "skillFolderHash": "hash-ui", "installedAt": "2026-07-01T00:00:00.000Z", "updatedAt": "2026-07-01T00:00:00.000Z"},
    "swiftui-pro": {"source": "twostraws/swiftui-agent-skill", "sourceType": "github", "skillFolderHash": "hash-x", "installedAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z"}
  }
}`

func TestReadLockFiltersMakeSkills(t *testing.T) {
	stubLockFile(t, sampleLock)

	entries, warning := readLock()

	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 make skills, got %d", len(entries))
	}
	if _, ok := entries["swiftui-pro"]; ok {
		t.Fatal("third-party skill must be filtered out")
	}
	if entries["makedsl"].SkillFolderHash != "hash-dsl" {
		t.Fatalf("unexpected hash: %s", entries["makedsl"].SkillFolderHash)
	}
	if entries["makedsl"].UpdatedAt != "2026-07-02T00:00:00.000Z" {
		t.Fatalf("unexpected updatedAt: %s", entries["makedsl"].UpdatedAt)
	}
}

func TestReadLockMissingFile(t *testing.T) {
	stubLockFile(t, "")

	entries, warning := readLock()

	if warning != "" {
		t.Fatalf("missing lockfile is empty state, got warning: %s", warning)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d", len(entries))
	}
}

func TestReadLockCorruptJSON(t *testing.T) {
	stubLockFile(t, "{not json")

	entries, warning := readLock()

	if warning == "" {
		t.Fatal("corrupt lockfile must produce a warning")
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty on corrupt file, got %d", len(entries))
	}
}

func TestReadLockVersionMismatch(t *testing.T) {
	stubLockFile(t, strings.Replace(sampleLock, `"version": 3`, `"version": 2`, 1))

	entries, warning := readLock()

	if !strings.Contains(warning, "2") || !strings.Contains(warning, "3") {
		t.Fatalf("warning must mention actual and expected version, got: %s", warning)
	}
	if len(entries) != 2 {
		t.Fatalf("best-effort parse expected 2 entries, got %d", len(entries))
	}
}

func TestExtractFrontmatter(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"normal", "---\nname: x\ndescription: y\n---\nbody", "name: x\ndescription: y"},
		{"no frontmatter", "# just markdown", ""},
		{"unclosed", "---\nname: x\n", ""},
		{"empty file", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(extractFrontmatter([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadDescriptionFoldedYAML(t *testing.T) {
	dir := stubSkillsDir(t)
	skillDir := filepath.Join(dir, "makedsl")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: makedsl\ndescription: >-\n  DSL 设计与生成，\n  覆盖 App/Entity/Relation 建模。\n---\n# makedsl\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readDescription(dir, "makedsl")

	if strings.Contains(got, "\n") {
		t.Fatalf("description must be single line, got %q", got)
	}
	if !strings.Contains(got, "DSL 设计与生成") || !strings.Contains(got, "建模") {
		t.Fatalf("folded content lost: %q", got)
	}
}

func TestReadDescriptionMissingFile(t *testing.T) {
	dir := stubSkillsDir(t)

	if got := readDescription(dir, "nope"); got != "" {
		t.Fatalf("expected empty for missing SKILL.md, got %q", got)
	}
}

func TestReadDescriptionBadYAML(t *testing.T) {
	dir := stubSkillsDir(t)
	skillDir := filepath.Join(dir, "bad")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\n\t: bad\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := readDescription(dir, "bad"); got != "" {
		t.Fatalf("expected empty for bad YAML, got %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/skillsync/ -run 'TestReadLock|TestExtractFrontmatter|TestReadDescription' -v`（禁用沙箱）
Expected: 编译失败，`undefined: lockPathFunc` / `undefined: readLock` 等。

- [ ] **Step 3: 写最小实现**

创建 `internal/skillsync/inventory.go`：

```go
/**
 * [INPUT]: 依赖 encoding/json、fmt、os、path/filepath、strings、gopkg.in/yaml.v3
 * [OUTPUT]: 包内提供 readLock / lockEntry / readDescription / extractFrontmatter，本地数据源（lockfile + SKILL.md frontmatter）
 * [POS]: internal/skillsync 的清单层本地半边，被 List（远端合并）与 Remove（来源校验）复用；lockPathFunc / skillsDirFunc 为测试接缝
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// lockSchemaVersion 是 vercel-labs/skills lockfile 的当前 schema 版本。
const lockSchemaVersion = 3

// lockEntry 是 lockfile 中单个 skill 的安装记录（只取需要的字段）。
type lockEntry struct {
	Source          string `json:"source"`
	SkillFolderHash string `json:"skillFolderHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type lockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

// lockPathFunc / skillsDirFunc 是路径解析接缝，测试注入 t.TempDir。
var lockPathFunc = defaultLockPath
var skillsDirFunc = defaultSkillsDir

// defaultLockPath 复刻 vercel-labs/skills 的解析链：
// $XDG_STATE_HOME/skills/.skill-lock.json，回退 ~/.agents/.skill-lock.json。
func defaultLockPath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "skills", ".skill-lock.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", ".skill-lock.json")
}

func defaultSkillsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "skills")
}

// readLock 读 lockfile 并过滤出 Make platform skills（source == SkillsSource）。
// 文件缺失 = 空态非错误；JSON 损坏或 schema 版本不匹配降级为 warning 尽力解析，永不失败。
func readLock() (map[string]lockEntry, string) {
	path := lockPathFunc()
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]lockEntry{}, ""
	}

	var lf lockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return map[string]lockEntry{}, fmt.Sprintf("cannot parse %s: %v", path, err)
	}

	warning := ""
	if lf.Version != lockSchemaVersion {
		warning = fmt.Sprintf("%s schema version is %d (expected %d), results may be incomplete",
			path, lf.Version, lockSchemaVersion)
	}

	entries := make(map[string]lockEntry, len(lf.Skills))
	for name, e := range lf.Skills {
		if e.Source == SkillsSource {
			entries[name] = e
		}
	}
	return entries, warning
}

// readDescription 从 <dir>/<name>/SKILL.md 的 YAML frontmatter 取 description 并折叠为单行；
// 任何失败返回空串不阻断（description 是展示增强，不是数据依赖）。
func readDescription(dir, name string) string {
	data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
	if err != nil {
		return ""
	}
	fm := extractFrontmatter(data)
	if fm == nil {
		return ""
	}
	var meta struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(meta.Description), " ")
}

// extractFrontmatter 取首个 "---" 行与下一个 "---" 行之间的内容；无 frontmatter 返回 nil。
func extractFrontmatter(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return []byte(strings.Join(lines[1:i], "\n"))
		}
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0，新增测试全绿。

- [ ] **Step 5: 提交**

```bash
git add internal/skillsync/inventory.go internal/skillsync/inventory_test.go
git commit -m "feat(skillsync): read skill lockfile and SKILL.md descriptions"
```

---

### Task 2: inventory.go — GitHub 远端比对 + List 合并

**Files:**
- Modify: `internal/skillsync/inventory.go`（追加远端半边 + List）
- Test: `internal/skillsync/inventory_test.go`（追加）

**Interfaces:**
- Consumes: Task 1 的 `readLock` / `readDescription` / `skillsDirFunc`；已有 `SkillsSource`。
- Produces: `List(ctx context.Context) Inventory`、`Inventory{Skills []SkillInfo; LockWarning string; RemoteErr error}`、`SkillInfo{Name, Status, Description, InstalledAt, UpdatedAt, LocalHash, RemoteHash string}`（json tags：name/status/description,omitempty/installedAt,omitempty/updatedAt,omitempty/localHash,omitempty/remoteHash,omitempty）、状态常量 `StatusUpToDate="up-to-date"` / `StatusOutdated="outdated"` / `StatusNotInstalled="not installed"` / `StatusRemovedUpstream="removed upstream"` / `StatusUnknown="unknown"`、测试接缝 `inventoryAPIBaseURL`。

- [ ] **Step 1: 写失败测试**

追加到 `internal/skillsync/inventory_test.go`（import 增加 `context`、`net/http`、`net/http/httptest`）：

```go
// stubRemoteAPI 起 httptest server 替换 inventoryAPIBaseURL。
func stubRemoteAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	orig := inventoryAPIBaseURL
	inventoryAPIBaseURL = server.URL
	t.Cleanup(func() {
		inventoryAPIBaseURL = orig
		server.Close()
	})
}

const sampleRemote = `[
  {"name": "makedsl", "sha": "hash-dsl-new", "type": "dir"},
  {"name": "makeui", "sha": "hash-ui", "type": "dir"},
  {"name": "make-app-auth", "sha": "hash-auth", "type": "dir"},
  {"name": "setup-make-poc.md", "sha": "hash-file", "type": "file"}
]`

func TestFetchRemoteSkills(t *testing.T) {
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/repos/qfeius/make-platform-skills/contents/skills"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path %s, want %s", r.URL.Path, wantPath)
		}
		_, _ = w.Write([]byte(sampleRemote))
	})

	remote, err := fetchRemoteSkills(context.Background())

	if err != nil {
		t.Fatalf("fetchRemoteSkills: %v", err)
	}
	if len(remote) != 3 {
		t.Fatalf("expected 3 dirs (file filtered), got %d", len(remote))
	}
	if remote["makedsl"] != "hash-dsl-new" {
		t.Fatalf("unexpected sha: %s", remote["makedsl"])
	}
	if _, ok := remote["setup-make-poc.md"]; ok {
		t.Fatal("non-dir entries must be filtered out")
	}
}

func TestFetchRemoteSkillsHTTPError(t *testing.T) {
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := fetchRemoteSkills(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestListMergesStatuses(t *testing.T) {
	stubLockFile(t, sampleLock) // makedsl hash-dsl(旧) + makeui hash-ui + swiftui-pro(第三方,被过滤)
	stubSkillsDir(t)
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleRemote)) // makedsl hash-dsl-new + makeui hash-ui + make-app-auth
	})

	inv := List(context.Background())

	if inv.RemoteErr != nil {
		t.Fatalf("unexpected remote error: %v", inv.RemoteErr)
	}
	want := map[string]string{
		"make-app-auth": StatusNotInstalled,
		"makedsl":       StatusOutdated,
		"makeui":        StatusUpToDate,
	}
	if len(inv.Skills) != len(want) {
		t.Fatalf("expected %d skills, got %d: %+v", len(want), len(inv.Skills), inv.Skills)
	}
	for _, s := range inv.Skills {
		if want[s.Name] != s.Status {
			t.Errorf("%s: got status %q, want %q", s.Name, s.Status, want[s.Name])
		}
	}
	// 按名字排序
	for i := 1; i < len(inv.Skills); i++ {
		if inv.Skills[i-1].Name > inv.Skills[i].Name {
			t.Fatalf("skills not sorted: %s > %s", inv.Skills[i-1].Name, inv.Skills[i].Name)
		}
	}
}

func TestListLocalOnlySkillIsRemovedUpstream(t *testing.T) {
	stubLockFile(t, sampleLock)
	stubSkillsDir(t)
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name": "makeui", "sha": "hash-ui", "type": "dir"}]`))
	})

	inv := List(context.Background())

	statuses := map[string]string{}
	for _, s := range inv.Skills {
		statuses[s.Name] = s.Status
	}
	if statuses["makedsl"] != StatusRemovedUpstream {
		t.Fatalf("makedsl: got %q, want %q", statuses["makedsl"], StatusRemovedUpstream)
	}
}

func TestListRemoteUnreachable(t *testing.T) {
	stubLockFile(t, sampleLock)
	stubSkillsDir(t)
	// 指向已关闭的 server 制造网络失败
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	orig := inventoryAPIBaseURL
	inventoryAPIBaseURL = server.URL
	t.Cleanup(func() { inventoryAPIBaseURL = orig })

	inv := List(context.Background())

	if inv.RemoteErr == nil {
		t.Fatal("expected RemoteErr on unreachable remote")
	}
	if len(inv.Skills) != 2 {
		t.Fatalf("expected 2 local skills only, got %d", len(inv.Skills))
	}
	for _, s := range inv.Skills {
		if s.Status != StatusUnknown {
			t.Errorf("%s: got %q, want %q", s.Name, s.Status, StatusUnknown)
		}
	}
}

func TestListFillsDescriptionForInstalled(t *testing.T) {
	stubLockFile(t, sampleLock)
	dir := stubSkillsDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "makeui"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: makeui\ndescription: 页面布局与 UI 组件组织\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "makeui", "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleRemote))
	})

	inv := List(context.Background())

	for _, s := range inv.Skills {
		switch s.Name {
		case "makeui":
			if s.Description != "页面布局与 UI 组件组织" {
				t.Errorf("makeui description: %q", s.Description)
			}
			if s.InstalledAt == "" || s.LocalHash == "" {
				t.Error("installed skill must carry installedAt and localHash")
			}
		case "make-app-auth":
			if s.Description != "" {
				t.Errorf("not-installed skill must have empty description, got %q", s.Description)
			}
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/skillsync/ -run 'TestFetchRemote|TestList' -v`（禁用沙箱）
Expected: 编译失败，`undefined: fetchRemoteSkills` / `undefined: List` 等。

- [ ] **Step 3: 写实现**

`internal/skillsync/inventory.go` import 增加 `context`、`net/http`、`slices`、`time`，并追加：

```go
// 状态常量：本地 lockfile × 远端仓库的比对结果。
const (
	StatusUpToDate        = "up-to-date"
	StatusOutdated        = "outdated"
	StatusNotInstalled    = "not installed"
	StatusRemovedUpstream = "removed upstream"
	StatusUnknown         = "unknown"
)

// SkillInfo 是单个 skill 的合并视图（本地安装记录 + 远端状态）。
type SkillInfo struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	InstalledAt string `json:"installedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	LocalHash   string `json:"localHash,omitempty"`
	RemoteHash  string `json:"remoteHash,omitempty"`
}

// Inventory 是 List 的完整结果；LockWarning / RemoteErr 由调用方渲染为 stderr 警告。
type Inventory struct {
	Skills      []SkillInfo
	LockWarning string
	RemoteErr   error
}

// inventoryAPIBaseURL 可在测试中替换（internal/update apiBaseURL 同款接缝）。
var inventoryAPIBaseURL = "https://api.github.com"

// inventoryClient 带 5 秒超时：远端比对是展示增强，不值得让 list 卡更久。
var inventoryClient = &http.Client{Timeout: 5 * time.Second}

// fetchRemoteSkills 匿名调 GitHub Contents API，一次拿到全部远端 skill 目录名 → tree SHA。
// lockfile 的 skillFolderHash 与该 SHA 同语义（GitHub tree SHA），可直接等值比对。
func fetchRemoteSkills(ctx context.Context) (map[string]string, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/skills", inventoryAPIBaseURL, SkillsSource)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := inventoryClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var entries []struct {
		Name string `json:"name"`
		SHA  string `json:"sha"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}

	remote := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Type == "dir" {
			remote[e.Name] = e.SHA
		}
	}
	return remote, nil
}

// List 合并本地 lockfile 与 GitHub 远端状态，产出按名字排序的 Make platform skills 清单。
// 远端失败不是错误：全部已装条目降级 StatusUnknown，错误进 Inventory.RemoteErr。
func List(ctx context.Context) Inventory {
	if ctx == nil {
		ctx = context.Background()
	}

	local, warning := readLock()
	remote, remoteErr := fetchRemoteSkills(ctx)

	names := make(map[string]bool, len(local)+len(remote))
	for name := range local {
		names[name] = true
	}
	for name := range remote {
		names[name] = true
	}

	skills := make([]SkillInfo, 0, len(names))
	for name := range names {
		entry, installed := local[name]
		info := SkillInfo{Name: name, RemoteHash: remote[name]}
		if installed {
			info.Description = readDescription(skillsDirFunc(), name)
			info.InstalledAt = entry.InstalledAt
			info.UpdatedAt = entry.UpdatedAt
			info.LocalHash = entry.SkillFolderHash
		}
		switch {
		case remoteErr != nil:
			info.Status = StatusUnknown
		case !installed:
			info.Status = StatusNotInstalled
		case remote[name] == "":
			info.Status = StatusRemovedUpstream
		case remote[name] == entry.SkillFolderHash:
			info.Status = StatusUpToDate
		default:
			info.Status = StatusOutdated
		}
		skills = append(skills, info)
	}
	slices.SortFunc(skills, func(a, b SkillInfo) int { return strings.Compare(a.Name, b.Name) })

	return Inventory{Skills: skills, LockWarning: warning, RemoteErr: remoteErr}
}
```

同时更新文件头部注释的 [OUTPUT]/[POS]（对外提供 List / Inventory / SkillInfo / Status* 常量）。

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0。

- [ ] **Step 5: 提交**

```bash
git add internal/skillsync/inventory.go internal/skillsync/inventory_test.go
git commit -m "feat(skillsync): merge local and remote skill status into inventory"
```

---

### Task 3: internal/skillsync/remove.go — 来源校验 + npx 删除

**Files:**
- Create: `internal/skillsync/remove.go`
- Test: `internal/skillsync/remove_test.go`

**Interfaces:**
- Consumes: Task 1 的 `readLock`；sync.go 已有的 `runSkillsCommand` seam、`syncTimeout`、`trimOutput`。
- Produces: `Remove(ctx context.Context, names []string) error`、`RemoveCommand(names []string) []string`。

- [ ] **Step 1: 写失败测试**

创建 `internal/skillsync/remove_test.go`：

```go
/**
 * [INPUT]: 依赖 context、errors、slices、strings、testing
 * [OUTPUT]: 覆盖 RemoveCommand 构造与 Remove 的来源校验/执行/失败路径
 * [POS]: internal/skillsync 删除层测试，白盒替换 runSkillsCommand 避免真实执行 npx
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// stubRunSkillsCommand 替换 runSkillsCommand，记录调用并返回给定结果。
func stubRunSkillsCommand(t *testing.T, output string, err error) *[][]string {
	t.Helper()
	var calls [][]string
	orig := runSkillsCommand
	runSkillsCommand = func(ctx context.Context, command []string) (string, error) {
		calls = append(calls, command)
		return output, err
	}
	t.Cleanup(func() { runSkillsCommand = orig })
	return &calls
}

func TestRemoveCommand(t *testing.T) {
	got := RemoveCommand([]string{"makedsl", "makeui"})
	want := []string{"npx", "-y", "skills", "remove", "makedsl", "makeui", "-y"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRemoveExecutesCommand(t *testing.T) {
	stubLockFile(t, sampleLock)
	calls := stubRunSkillsCommand(t, "removed", nil)

	if err := Remove(context.Background(), []string{"makedsl"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 command execution, got %d", len(*calls))
	}
	if !slices.Equal((*calls)[0], RemoveCommand([]string{"makedsl"})) {
		t.Fatalf("unexpected command: %v", (*calls)[0])
	}
}

func TestRemoveRejectsThirdPartySkill(t *testing.T) {
	stubLockFile(t, sampleLock) // swiftui-pro 是第三方来源
	calls := stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"swiftui-pro"})

	if err == nil {
		t.Fatal("expected error for third-party skill")
	}
	if !strings.Contains(err.Error(), "swiftui-pro") {
		t.Fatalf("error must name the invalid skill: %v", err)
	}
	if !strings.Contains(err.Error(), "makedsl") || !strings.Contains(err.Error(), "makeui") {
		t.Fatalf("error must list installed candidates: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("must not execute command when validation fails")
	}
}

func TestRemoveNotInstalledName(t *testing.T) {
	stubLockFile(t, sampleLock)
	calls := stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"no-such-skill"})

	if err == nil || !strings.Contains(err.Error(), "no-such-skill") {
		t.Fatalf("expected error naming unknown skill, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("must not execute command when validation fails")
	}
}

func TestRemoveEmptyLockfile(t *testing.T) {
	stubLockFile(t, "")
	stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"makedsl"})

	if err == nil || !strings.Contains(err.Error(), "none installed") {
		t.Fatalf("expected '(none installed)' hint, got %v", err)
	}
}

func TestRemoveCommandFailure(t *testing.T) {
	stubLockFile(t, sampleLock)
	stubRunSkillsCommand(t, "boom output", errors.New("exit 1"))

	err := Remove(context.Background(), []string{"makedsl"})

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "manual fix") || !strings.Contains(err.Error(), "boom output") {
		t.Fatalf("error must carry manual fix and output: %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/skillsync/ -run TestRemove -v`（禁用沙箱）
Expected: 编译失败，`undefined: RemoveCommand` / `undefined: Remove`。

- [ ] **Step 3: 写实现**

创建 `internal/skillsync/remove.go`：

```go
/**
 * [INPUT]: 依赖 context、fmt、maps、slices、strings
 * [OUTPUT]: 对外提供 Remove / RemoveCommand，删除已安装的 Make platform skills
 * [POS]: internal/skillsync 的删除层，被 cmd/skills_remove.go 消费；来源校验挡住误删第三方 skills；复用 sync.go 的 runSkillsCommand seam 与 syncTimeout
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// RemoveCommand 返回删除指定 skills 的非交互命令。
func RemoveCommand(names []string) []string {
	command := []string{"npx", "-y", "skills", "remove"}
	command = append(command, names...)
	return append(command, "-y")
}

// Remove 删除指定的 Make platform skills。
// 名字必须都是 lockfile 中 source == SkillsSource 的已安装 skill——
// 用户机器上可能有几十个第三方 skills，makecli 不越界删除。
func Remove(ctx context.Context, names []string) error {
	installed, _ := readLock()

	var invalid []string
	for _, name := range names {
		if _, ok := installed[name]; !ok {
			invalid = append(invalid, name)
		}
	}
	if len(invalid) > 0 {
		hint := "(none installed)"
		if candidates := slices.Sorted(maps.Keys(installed)); len(candidates) > 0 {
			hint = strings.Join(candidates, ", ")
		}
		return fmt.Errorf("not installed Make platform skills: %s\ninstalled Make platform skills: %s",
			strings.Join(invalid, ", "), hint)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	command := RemoveCommand(names)
	output, err := runSkillsCommand(runCtx, command)
	if err != nil {
		return fmt.Errorf("failed to remove skills: %w\nmanual fix: %s\n%s",
			err, strings.Join(command, " "), trimOutput(strings.TrimSpace(output)))
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0。

- [ ] **Step 5: 提交**

```bash
git add internal/skillsync/remove.go internal/skillsync/remove_test.go
git commit -m "feat(skillsync): remove Make platform skills with source validation"
```

---

### Task 4: cmd/skills.go + cmd/skills_list.go — 命令组与 list 渲染

**Files:**
- Create: `cmd/skills.go`
- Create: `cmd/skills_list.go`
- Modify: `cmd/root.go`（`rootCmd.AddCommand(newUpdateCmd())` 行之后插入挂载）
- Test: `cmd/skills_list_test.go`

**Interfaces:**
- Consumes: `skillsync.List` / `skillsync.Inventory` / `skillsync.SkillInfo` / `skillsync.Status*`（Task 2）；cmd 包已有 `validateOutputFormat` / `writeJSON` / `outputTable` / `outputJSON`（output.go）、`captureStdout` / `captureStderr`（stdout_test.go）。
- Produces: `newSkillsCmd() *cobra.Command`、`runSkillsList(ctx context.Context, output string) error`、测试接缝 `listSkillsFunc`。Task 5 会在 skills.go 追加两行 AddCommand。

- [ ] **Step 1: 写失败测试**

创建 `cmd/skills_list_test.go`：

```go
/**
 * [INPUT]: 依赖 context、encoding/json、strings、testing、internal/skillsync
 * [OUTPUT]: 覆盖 runSkillsList（table/json/空态/警告/非法格式）与 skills 默认行为 = list
 * [POS]: cmd/skills list 子命令测试，stubListSkills 打桩隔离 lockfile 与网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/skillsync"
)

// stubListSkills 打桩 listSkillsFunc 返回固定 Inventory。
func stubListSkills(t *testing.T, inv skillsync.Inventory) {
	t.Helper()
	orig := listSkillsFunc
	listSkillsFunc = func(ctx context.Context) skillsync.Inventory { return inv }
	t.Cleanup(func() { listSkillsFunc = orig })
}

func sampleInventory() skillsync.Inventory {
	return skillsync.Inventory{Skills: []skillsync.SkillInfo{
		{Name: "make-app-auth", Status: skillsync.StatusNotInstalled, RemoteHash: "d"},
		{Name: "makedsl", Status: skillsync.StatusOutdated, Description: "DSL 设计与生成", UpdatedAt: "2026-07-02T00:00:00.000Z", LocalHash: "a", RemoteHash: "b"},
		{Name: "makeui", Status: skillsync.StatusUpToDate, Description: "页面布局", UpdatedAt: "2026-07-01T00:00:00.000Z", LocalHash: "c", RemoteHash: "c"},
	}}
}

func TestRunSkillsListTable(t *testing.T) {
	stubListSkills(t, sampleInventory())

	out := captureStdout(t, func() {
		if err := runSkillsList(context.Background(), outputTable); err != nil {
			t.Errorf("runSkillsList: %v", err)
		}
	})

	for _, want := range []string{"NAME", "STATUS", "makedsl", "outdated", "makeui", "up-to-date", "make-app-auth", "not installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "2 installed, 1 outdated, 1 available") {
		t.Errorf("missing summary line:\n%s", out)
	}
	if !strings.Contains(out, "makecli skills update") {
		t.Errorf("missing update hint:\n%s", out)
	}
	// UPDATED AT 裁到日期
	if !strings.Contains(out, "2026-07-02") || strings.Contains(out, "00:00:00") {
		t.Errorf("updated at must be date-only:\n%s", out)
	}
}

func TestRunSkillsListJSON(t *testing.T) {
	stubListSkills(t, sampleInventory())

	out := captureStdout(t, func() {
		if err := runSkillsList(context.Background(), outputJSON); err != nil {
			t.Errorf("runSkillsList: %v", err)
		}
	})

	var payload struct {
		Data []skillsync.SkillInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(payload.Data) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(payload.Data))
	}
	if payload.Data[1].Description != "DSL 设计与生成" {
		t.Fatalf("JSON must keep full description: %+v", payload.Data[1])
	}
}

func TestRunSkillsListEmpty(t *testing.T) {
	stubListSkills(t, skillsync.Inventory{})

	out := captureStdout(t, func() {
		if err := runSkillsList(context.Background(), outputTable); err != nil {
			t.Errorf("runSkillsList: %v", err)
		}
	})

	if !strings.Contains(out, "No Make platform skills installed") {
		t.Errorf("missing empty state:\n%s", out)
	}
	if !strings.Contains(out, "makecli skills update") {
		t.Errorf("empty state must guide installation:\n%s", out)
	}
}

func TestRunSkillsListWarnings(t *testing.T) {
	inv := sampleInventory()
	inv.LockWarning = "schema version is 2"
	inv.RemoteErr = context.DeadlineExceeded
	stubListSkills(t, inv)

	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if err := runSkillsList(context.Background(), outputTable); err != nil {
				t.Errorf("warnings must not fail the command: %v", err)
			}
		})
	})

	if !strings.Contains(stderr, "schema version is 2") {
		t.Errorf("stderr missing lock warning:\n%s", stderr)
	}
	if !strings.Contains(stderr, "remote check failed") {
		t.Errorf("stderr missing remote warning:\n%s", stderr)
	}
	if !strings.Contains(stdout, "makedsl") {
		t.Errorf("table must still render on warnings:\n%s", stdout)
	}
}

func TestRunSkillsListInvalidOutput(t *testing.T) {
	if err := runSkillsList(context.Background(), "xml"); err == nil {
		t.Fatal("expected error for invalid output format")
	}
}

func TestSkillsDefaultIsList(t *testing.T) {
	stubListSkills(t, sampleInventory())

	cmd := newSkillsCmd()
	cmd.SetArgs([]string{})
	out := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Errorf("execute: %v", err)
		}
	})

	if !strings.Contains(out, "makedsl") {
		t.Errorf("bare 'makecli skills' must render list:\n%s", out)
	}
}

func TestTruncateLine(t *testing.T) {
	if got := truncateLine("短描述", 60); got != "短描述" {
		t.Fatalf("short string must pass through, got %q", got)
	}
	long := strings.Repeat("很", 70)
	got := truncateLine(long, 60)
	if len([]rune(got)) != 61 || !strings.HasSuffix(got, "…") {
		t.Fatalf("expected 60 runes + ellipsis, got %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run 'TestRunSkillsList|TestSkillsDefault|TestTruncateLine' -v`（禁用沙箱）
Expected: 编译失败，`undefined: listSkillsFunc` / `undefined: runSkillsList` 等。

- [ ] **Step 3: 写实现**

创建 `cmd/skills.go`：

```go
/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newSkillsCmd 函数
 * [POS]: cmd 模块的 skills 命令组，挂载 list 子命令；默认 RunE = list（参考 version.go 的 gh 模式）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newSkillsCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:          "skills",
		Short:        "Manage Make platform skills",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsList(cmd.Context(), output)
		},
	}
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	cmd.AddCommand(newSkillsListCmd())
	return cmd
}
```

创建 `cmd/skills_list.go`：

```go
/**
 * [INPUT]: 依赖 context、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra、internal/skillsync、cmd/output 辅助
 * [OUTPUT]: 对外提供 newSkillsListCmd 函数；包内 runSkillsList 被 skills 命令组默认行为复用
 * [POS]: cmd/skills 的 list 子命令，合并本地 lockfile 与 GitHub 远端状态，输出列 NAME/STATUS/DESCRIPTION/UPDATED AT；支持 table|json；远端失败降级 unknown + stderr 警告，退出码恒 0
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/spf13/cobra"
)

// listSkillsFunc 包装 skillsync.List，便于测试打桩避免读真实 lockfile / 触网。
var listSkillsFunc = skillsync.List

func newSkillsListCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List Make platform skills and their remote status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsList(cmd.Context(), output)
		},
	}
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	return cmd
}

func runSkillsList(ctx context.Context, output string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}

	inv := listSkillsFunc(ctx)

	if inv.LockWarning != "" {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %s\n", inv.LockWarning)
	}
	if inv.RemoteErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: remote check failed: %v\n", inv.RemoteErr)
	}

	if output == outputJSON {
		return writeJSON(map[string]any{"data": inv.Skills})
	}

	if len(inv.Skills) == 0 {
		fmt.Println("No Make platform skills installed.")
		fmt.Println("Run 'makecli skills update' to install.")
		return nil
	}

	rows := make([][]string, len(inv.Skills))
	for i, s := range inv.Skills {
		rows[i] = []string{s.Name, s.Status, truncateLine(s.Description, 60), shortDate(s.UpdatedAt)}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "STATUS", "DESCRIPTION", "UPDATED AT")
	_ = table.Bulk(rows)
	_ = table.Render()

	installed, outdated, available := summarizeSkills(inv.Skills)
	fmt.Printf("\n%d installed, %d outdated, %d available\n", installed, outdated, available)
	if outdated+available > 0 {
		fmt.Println("Run 'makecli skills update' to install/upgrade.")
	}
	return nil
}

// summarizeSkills 统计已安装 / 落后 / 远端可装数量。
func summarizeSkills(skills []skillsync.SkillInfo) (installed, outdated, available int) {
	for _, s := range skills {
		switch s.Status {
		case skillsync.StatusNotInstalled:
			available++
		case skillsync.StatusOutdated:
			installed++
			outdated++
		default: // up-to-date / removed upstream / unknown 都属已安装
			installed++
		}
	}
	return installed, outdated, available
}

// truncateLine 把描述截到 max 个 rune 加省略号——表格列宽护栏，JSON 输出保留全文。
func truncateLine(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// shortDate 把 ISO 时间戳裁到日期部分——表格展示用，JSON 输出保留全值。
func shortDate(iso string) string {
	if len(iso) > 10 {
		return iso[:10]
	}
	return iso
}
```

修改 `cmd/root.go`：在 `rootCmd.AddCommand(newUpdateCmd())` 行之后插入：

```go
	rootCmd.AddCommand(newSkillsCmd())
```

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0。

手动验证一把真实链路（远端真网络，本地无 Make skills 的空态/远端可装展示）：

Run: `make build && ./bin/makecli skills`
Expected: 表格列出远端 9 个 skill，STATUS 全为 `not installed`，汇总行 `0 installed, 0 outdated, 9 available` + update 提示。

- [ ] **Step 5: 提交**

```bash
git add cmd/skills.go cmd/skills_list.go cmd/skills_list_test.go cmd/root.go
git commit -m "feat(cmd): add skills command group with list subcommand"
```

---

### Task 5: cmd/skills_update.go + cmd/skills_remove.go

**Files:**
- Create: `cmd/skills_update.go`
- Create: `cmd/skills_remove.go`
- Modify: `cmd/skills.go`（追加两行 AddCommand）
- Test: `cmd/skills_update_test.go`、`cmd/skills_remove_test.go`

**Interfaces:**
- Consumes: `runSkillSync(cmd *cobra.Command, version string, skipSkills bool) error` 与 `syncSkillsFunc` seam（update.go 已有）、`build.Version`、`skillsync.Remove`（Task 3）、`newSkillsCmd`（Task 4）。
- Produces: `newSkillsUpdateCmd()` / `newSkillsRemoveCmd()`、测试接缝 `removeSkillsFunc`。

- [ ] **Step 1: 写失败测试**

创建 `cmd/skills_update_test.go`：

```go
/**
 * [INPUT]: 依赖 bytes、context、strings、testing、internal/skillsync
 * [OUTPUT]: 覆盖 skills update 子命令走 runSkillSync 且不跳过
 * [POS]: cmd/skills update 子命令测试，syncSkillsFunc 打桩避免真实执行 npx
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/skillsync"
)

func TestSkillsUpdateRunsSync(t *testing.T) {
	var gotOpts skillsync.Options
	orig := syncSkillsFunc
	syncSkillsFunc = func(ctx context.Context, opts skillsync.Options) (skillsync.Result, error) {
		gotOpts = opts
		return skillsync.Result{
			Action:  skillsync.ActionSynced,
			Source:  skillsync.SkillsSource,
			Version: opts.Version,
			Command: skillsync.SkillsCommand(),
		}, nil
	}
	t.Cleanup(func() { syncSkillsFunc = orig })

	cmd := newSkillsUpdateCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if gotOpts.Skip {
		t.Fatal("skills update must never skip sync")
	}
	if !strings.Contains(buf.String(), "Syncing Make platform skills") {
		t.Fatalf("missing sync start output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "Skills updated") {
		t.Fatalf("missing sync result output:\n%s", buf.String())
	}
}
```

创建 `cmd/skills_remove_test.go`：

```go
/**
 * [INPUT]: 依赖 bytes、context、errors、slices、strings、testing
 * [OUTPUT]: 覆盖 skills remove 子命令的透传/报错/必填参数
 * [POS]: cmd/skills remove 子命令测试，removeSkillsFunc 打桩避免真实执行 npx
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// stubRemoveSkills 打桩 removeSkillsFunc，记录传入名字并返回给定错误。
func stubRemoveSkills(t *testing.T, err error) *[]string {
	t.Helper()
	var got []string
	orig := removeSkillsFunc
	removeSkillsFunc = func(ctx context.Context, names []string) error {
		got = names
		return err
	}
	t.Cleanup(func() { removeSkillsFunc = orig })
	return &got
}

func TestSkillsRemoveSuccess(t *testing.T) {
	gotNames := stubRemoveSkills(t, nil)

	cmd := newSkillsRemoveCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"makedsl", "makeui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !slices.Equal(*gotNames, []string{"makedsl", "makeui"}) {
		t.Fatalf("unexpected names: %v", *gotNames)
	}
	if !strings.Contains(buf.String(), "Removed: makedsl, makeui") {
		t.Fatalf("missing confirmation output:\n%s", buf.String())
	}
}

func TestSkillsRemoveError(t *testing.T) {
	stubRemoveSkills(t, errors.New("not installed Make platform skills: x"))

	cmd := newSkillsRemoveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"x"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestSkillsRemoveRequiresArgs(t *testing.T) {
	stubRemoveSkills(t, nil)

	cmd := newSkillsRemoveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no names given")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run 'TestSkillsUpdate|TestSkillsRemove' -v`（禁用沙箱）
Expected: 编译失败，`undefined: newSkillsUpdateCmd` / `undefined: newSkillsRemoveCmd`。

- [ ] **Step 3: 写实现**

创建 `cmd/skills_update.go`：

```go
/**
 * [INPUT]: 依赖 github.com/spf13/cobra、internal/build
 * [OUTPUT]: 对外提供 newSkillsUpdateCmd 函数
 * [POS]: cmd/skills 的 update 子命令，复用 update.go 的 runSkillSync（skillsync.Sync 幂等：装缺的 + 升级已有的），与 makecli update 后置同步同一代码路径
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/qfeius/makecli/internal/build"
	"github.com/spf13/cobra"
)

func newSkillsUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "update",
		Short:        "Install missing and upgrade installed Make platform skills",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillSync(cmd, build.Version, false)
		},
	}
}
```

创建 `cmd/skills_remove.go`：

```go
/**
 * [INPUT]: 依赖 fmt、strings、github.com/spf13/cobra、internal/skillsync
 * [OUTPUT]: 对外提供 newSkillsRemoveCmd 函数
 * [POS]: cmd/skills 的 remove 子命令，透传 skillsync.Remove（来源校验挡住误删第三方 skills），名字必填
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/spf13/cobra"
)

// removeSkillsFunc 包装 skillsync.Remove，便于测试打桩避免真实执行 npx。
var removeSkillsFunc = skillsync.Remove

func newSkillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>...",
		Short:        "Remove installed Make platform skills",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := removeSkillsFunc(cmd.Context(), args); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed: %s\n", strings.Join(args, ", "))
			return nil
		},
	}
}
```

修改 `cmd/skills.go`：在 `cmd.AddCommand(newSkillsListCmd())` 之后追加：

```go
	cmd.AddCommand(newSkillsUpdateCmd())
	cmd.AddCommand(newSkillsRemoveCmd())
```

并把 skills.go 头部 [POS] 更新为「挂载 list / update / remove 子命令」。

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0。

- [ ] **Step 5: 提交**

```bash
git add cmd/skills.go cmd/skills_update.go cmd/skills_update_test.go cmd/skills_remove.go cmd/skills_remove_test.go
git commit -m "feat(cmd): add skills update and remove subcommands"
```

---

### Task 6: 文档同步 + 全量验证

**Files:**
- Modify: `cmd/CLAUDE.md`（成员清单 + root.go 描述行）
- Modify: `internal/skillsync/CLAUDE.md`（成员清单）
- Modify: `CLAUDE.md`（根目录 `<directory>` 中 cmd 与 internal/skillsync 两行）

**Interfaces:**
- Consumes: Task 1–5 全部落地后的最终文件形态。
- Produces: 无代码；GEB 最低要求的架构级文档同步。

- [ ] **Step 1: 更新 cmd/CLAUDE.md**

成员清单按字母序插入（描述与实际实现一致，格式对齐邻居条目）：

```
skills.go:              skills 命令组，挂载 list / update / remove 子命令；默认 RunE = list（version.go 同款 gh 模式），组级 --output 供裸 `makecli skills` 使用
skills_list.go:         skills list 子命令，调 skillsync.List 合并本地 lockfile 与 GitHub 远端状态，tablewriter 输出 NAME/STATUS/DESCRIPTION(60 rune 截断)/UPDATED AT(裁到日期)；汇总行 N installed, M outdated, K available + update 提示；LockWarning/RemoteErr 渲染为 stderr 警告不阻断，退出码恒 0；支持 --output table|json（JSON 保留全文 description 与完整时间戳）；listSkillsFunc 包级可打桩变量
skills_list_test.go:    覆盖 runSkillsList 的单元测试（table 渲染+汇总行/JSON 全文/空态引导/警告进 stderr 不阻断/非法格式/裸 skills 默认= list/truncateLine），stubListSkills 打桩隔离 lockfile 与网络
skills_update.go:       skills update 子命令，复用 update.go 的 runSkillSync（skillsync.Sync 幂等：装缺的 + 升级已有的），与 makecli update 后置同步同一代码路径
skills_update_test.go:  覆盖 skills update 走 runSkillSync 且 Skip 恒 false，syncSkillsFunc 打桩避免真实执行 npx
skills_remove.go:       skills remove 子命令，名字必填（MinimumNArgs(1)），透传 skillsync.Remove（来源校验挡住误删第三方 skills）；removeSkillsFunc 包级可打桩变量
skills_remove_test.go:  覆盖 skills remove 的透传/错误上抛/必填参数，stubRemoveSkills 打桩隔离 npx
```

root.go 描述行的顶级子命令列表中补 `skills`。

- [ ] **Step 2: 更新 internal/skillsync/CLAUDE.md**

成员清单追加：

```
inventory.go:      Make platform skills 清单层，List 合并三数据源——lockfile（$XDG_STATE_HOME/skills/.skill-lock.json 回退 ~/.agents/.skill-lock.json，按 source==SkillsSource 过滤，缺失=空态、损坏/版本不匹配降级 warning）+ SKILL.md frontmatter description（折叠单行，失败留空）+ GitHub Contents API（匿名 5s 超时，目录 tree SHA 与 skillFolderHash 同语义等值比对）；状态 up-to-date/outdated/not installed/removed upstream/unknown（远端失败全量降级）；lockPathFunc/skillsDirFunc/inventoryAPIBaseURL 为测试接缝
inventory_test.go: 覆盖 readLock（过滤/缺失/损坏/版本不匹配）、frontmatter 解析（folded YAML 折叠单行/缺失/坏 YAML）、fetchRemoteSkills（过滤非 dir/HTTP 错误）、List 合并（五状态/排序/远端不可达降级/description 填充），httptest + t.TempDir 隔离
remove.go:         Make platform skills 删除层，Remove 先经 readLock 校验名字都是 source==SkillsSource 的已装 skill（挡住误删第三方，失败列出合法候选），再执行 npx -y skills remove <names> -y（复用 runSkillsCommand seam + syncTimeout，失败附 manual fix）
remove_test.go:    覆盖 RemoveCommand 构造、来源校验拒绝第三方/未安装（校验失败不触发命令）、空 lockfile 提示、执行与失败路径，白盒替换 runSkillsCommand
```

同时把文件头一行的包定位从「同步编排层」扩为「同步/清单/删除」。

- [ ] **Step 3: 更新根 CLAUDE.md**

`<directory>` 中：

- `cmd/` 行的子命令列表在 `update` 后补 `、skills[list/update/remove]`。
- `internal/skillsync/` 行改为：

```
internal/skillsync/ - Make platform skills 同步/清单/删除（Sync 默认每次 npx 安装/升级 qfeius/make-platform-skills --all，--skip-skills 跳过；List 合并 lockfile + SKILL.md + GitHub Contents API 做 outdated 比对；Remove 来源校验后透传 npx skills remove），被 cmd/update 与 cmd/skills 消费
```

- [ ] **Step 4: 全量验证**

Run: `make vet && make test`（禁用沙箱），然后单独跑 `golangci-lint run ./...`（禁用沙箱）
Expected: 全部 exit 0、0 issues。

- [ ] **Step 5: 提交**

```bash
git add cmd/CLAUDE.md internal/skillsync/CLAUDE.md CLAUDE.md
git commit -m "docs: sync CLAUDE.md for skills command group"
```
