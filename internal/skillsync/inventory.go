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
