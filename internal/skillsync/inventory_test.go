/**
 * [INPUT]: 依赖 context、net/http、net/http/httptest、os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 readLock（缺失/过滤/损坏/版本不匹配）、extractFrontmatter / readDescription、fetchRemoteSkills（过滤非 dir/HTTP 错误）、List 合并（五状态/排序/远端不可达降级/description 填充）
 * [POS]: internal/skillsync 清单层测试，本地数据源用 t.TempDir 隔离文件系统，远端数据源用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"net/http"
	"net/http/httptest"
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
