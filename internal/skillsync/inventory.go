/**
 * [INPUT]: 依赖 context、encoding/json、fmt、net/http、os、path/filepath、slices、strings、time、gopkg.in/yaml.v3
 * [OUTPUT]: 对外提供 List / Inventory / SkillInfo / Status* 常量（本地 lockfile × 远端 GitHub 状态合并清单）；包内 readLock / lockEntry / readDescription / extractFrontmatter 供 Remove（来源校验）复用
 * [POS]: internal/skillsync 的清单层，被 cmd/skills_list.go 调用；lockPathFunc / skillsDirFunc / inventoryAPIBaseURL 为测试接缝
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
