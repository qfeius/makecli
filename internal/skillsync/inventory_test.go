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
