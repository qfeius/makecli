# Update Notifier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `makecli` 任意命令执行时，零延迟读本地缓存判定是否有新版本；有则在命令结束后向 stderr 打印一行升级提示。

**Architecture:** 新增 `internal/notifier` 包，单一职责（判定 + 缓存）。`cmd.Execute()` 头部 `notifier.Start()` 在缓存过期时起后台 goroutine 并行刷新；尾部 `(*Notifier).Finish()` 用 250ms 极短 deadline 收尾、读缓存、按判定链决定是否提示。启用与否走三态裁决：env `MAKE_CLI_UPDATE_NOTIFIER` > config `[settings] check-for-updates` > 默认开。

**Tech Stack:** Go 1.22 · cobra · Masterminds/semver/v3（已有）· mattn/go-isatty（已有间接依赖，提升为直接）· 复用 `internal/update.CheckLatest`、`internal/config.Dir`、`internal/build.Version`。

**约定：** 验证门（全量测试 / 构建）一律走 `make vet`/`make test`/`make build`（项目偏好）；TDD 红/绿微步用 `go test ./<pkg>/ -run <Name> -v` 精确定位单测。所有新建文件带 GEB L3 头部。

---

## File Structure

| 文件 | 职责 | 动作 |
|------|------|------|
| `internal/config/config.go` | 抽出通用 `parseINISections`，`parseConfigINI` 委托并跳过 `[settings]` | Modify |
| `internal/config/settings.go` | `Settings` 类型 + `LoadSettings`（读 `[settings]` 全局段） | Create |
| `internal/config/settings_test.go` | LoadSettings / settings 段隔离测试 | Create |
| `internal/update/update.go` | `metaClient` 带超时，用于 JSON 元数据请求（不含二进制下载） | Modify |
| `internal/notifier/cache.go` | `cacheData` + 原子读写 + 过期判定 | Create |
| `internal/notifier/cache_test.go` | 缓存往返 / 缺失 / 过期测试 | Create |
| `internal/notifier/decision.go` | `notifierEnabled` / `shouldNotify` / `renderNotice` 纯函数 | Create |
| `internal/notifier/decision_test.go` | 判定链穷举 + 渲染测试 | Create |
| `internal/notifier/notifier.go` | `Notifier` / `Start` / `Finish` 编排 + `isStderrTTY` | Create |
| `internal/notifier/notifier_test.go` | Start 刷新/跳过 + Finish 冒烟测试 | Create |
| `internal/notifier/CLAUDE.md` | L2 模块地图 | Create |
| `cmd/root.go` | `Execute` 钩入 Start/Finish；`commandName` 解析顶级命令 | Modify |
| `cmd/root_test.go` | `commandName` 单测 | Create |
| 各 `CLAUDE.md`（root/cmd/config/update） | GEB 文档同步 | Modify |

无新增第三方下载：`mattn/go-isatty` 已在 go.sum，仅 `go mod tidy` 将其从 indirect 提升为 direct。

---

### Task 1: config `[settings]` 全局段支持

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/settings.go`
- Test: `internal/config/settings_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/config/settings_test.go`：

```go
/**
 * [INPUT]: 依赖 config 包内 LoadSettings / LoadConfig / ConfigPath / settingsSection（白盒）
 * [OUTPUT]: 覆盖 [settings] 全局段读取与 profile 解析隔离的单元测试
 * [POS]: internal/config 模块 settings.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfigFile 在 ConfigPath 处写入 config 文件内容
func writeConfigFile(t *testing.T, content string) {
	t.Helper()
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestLoadSettings_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates != nil {
		t.Errorf("expected nil CheckForUpdates, got %v", *s.CheckForUpdates)
	}
}

func TestLoadSettings_Disabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = false\n\n[default]\nX-Tenant-ID = t1\n")

	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates == nil {
		t.Fatal("expected CheckForUpdates set, got nil")
	}
	if *s.CheckForUpdates != false {
		t.Errorf("CheckForUpdates = %v, want false", *s.CheckForUpdates)
	}
}

func TestLoadSettings_Enabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = true\n")
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates == nil || *s.CheckForUpdates != true {
		t.Errorf("expected true, got %v", s.CheckForUpdates)
	}
}

func TestLoadConfig_IgnoresSettingsSection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = false\n\n[default]\nX-Tenant-ID = t1\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, ok := cfg[settingsSection]; ok {
		t.Error("settings section should not appear as a profile")
	}
	if cfg["default"].XTenantID != "t1" {
		t.Errorf("default profile lost: %+v", cfg["default"])
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ -run 'TestLoadSettings|TestLoadConfig_IgnoresSettingsSection' -v`
Expected: 编译失败（`undefined: LoadSettings`、`undefined: settingsSection`）

- [ ] **Step 3: 抽出通用 INI 解析并让 parseConfigINI 委托**

编辑 `internal/config/config.go`：在 import 块加入 `"io"`，并在 `parseConfigINI` 上方插入通用解析器，同时替换 `parseConfigINI` 实现。

更新 import（现有为 bufio/fmt/os/path/filepath/strings，新增 io）：

```go
import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)
```

将原有 `parseConfigINI`（从 `// parseConfigINI 解析 INI 格式内容...` 到其闭合 `}`）整体替换为：

```go
// parseINISections 通用 INI 解析：section → (key → value)。
// 忽略空行与 # / ; 注释；无 section 头的键被丢弃。
func parseINISections(r io.Reader) (map[string]map[string]string, error) {
	sections := map[string]map[string]string{}
	current := ""

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := sections[current]; !ok {
				sections[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		sections[current][strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return sections, scanner.Err()
}

// parseConfigINI 解析 config 文件为 Config（profile 集合），跳过保留的 [settings] 全局段
func parseConfigINI(f *os.File) (Config, error) {
	sections, err := parseINISections(f)
	if err != nil {
		return nil, err
	}
	cfg := Config{}
	for name, kv := range sections {
		if name == settingsSection {
			continue
		}
		cfg[name] = ConfigProfile{
			ServerURL:  kv["server-url"],
			XTenantID:  kv["X-Tenant-ID"],
			OperatorID: kv["X-Operator-ID"],
		}
	}
	return cfg, nil
}
```

- [ ] **Step 4: 创建 settings.go**

创建 `internal/config/settings.go`：

```go
/**
 * [INPUT]: 依赖 fmt、os、strconv；依赖 config.go 的 parseINISections、ConfigPath
 * [OUTPUT]: 对外提供 Settings 类型、LoadSettings 函数；包内 settingsSection 常量
 * [POS]: internal/config 的全局设置读取，承载非 profile 相关的 [settings] 段（当前仅 check-for-updates）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"fmt"
	"os"
	"strconv"
)

// settingsSection 是 config 文件中承载全局（非 profile）配置的保留段名
const settingsSection = "settings"

// Settings 持有全局配置项。指针字段表达三态：nil = 文件未设置该项。
type Settings struct {
	// CheckForUpdates 控制自动更新提示是否启用；nil 表示未配置（由调用方决定默认）
	CheckForUpdates *bool
}

// LoadSettings 读取 config 文件的 [settings] 全局段。
// best-effort：文件不存在返回空 Settings 且无错误；解析失败返回错误。
func LoadSettings() (Settings, error) {
	path, err := ConfigPath()
	if err != nil {
		return Settings{}, err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("读取 config 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	sections, err := parseINISections(f)
	if err != nil {
		return Settings{}, err
	}

	var s Settings
	if kv, ok := sections[settingsSection]; ok {
		if raw, ok := kv["check-for-updates"]; ok {
			if b, err := strconv.ParseBool(raw); err == nil {
				s.CheckForUpdates = &b
			}
		}
	}
	return s, nil
}
```

- [ ] **Step 5: 运行测试确认通过（含既有 config 测试不回归）**

Run: `go test ./internal/config/ -v`
Expected: PASS（含既有 `TestParseConfigINI` / `TestSaveConfigAndLoad` 等全绿）

- [ ] **Step 6: 提交**

```bash
git add internal/config/config.go internal/config/settings.go internal/config/settings_test.go
git commit -m "feat(config): support [settings] global section with LoadSettings"
```

---

### Task 2: update 元数据请求加超时

**Files:**
- Modify: `internal/update/update.go`
- Test: `internal/update/update_test.go`（追加一条守护测试）

- [ ] **Step 1: 写失败测试**

在 `internal/update/update_test.go` 末尾追加（白盒，package update）：

```go
func TestMetaClientHasTimeout(t *testing.T) {
	if metaClient.Timeout <= 0 {
		t.Error("metaClient must carry a positive timeout to bound background refresh")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/update/ -run TestMetaClientHasTimeout -v`
Expected: 编译失败（`undefined: metaClient`）

- [ ] **Step 3: 实现 metaClient 并用于 fetchJSON**

编辑 `internal/update/update.go`：

(a) import 块加入 `"time"`（现有含 archive/tar、compress/gzip、encoding/json、fmt、io、net/http、os、path/filepath、runtime、strings、semver）。

(b) 在 `var apiBaseURL = "https://api.github.com"` 下方追加：

```go
// metaClient 用于元数据 JSON 请求（latest / list / tag），带超时以约束后台刷新。
// 注意：二进制下载（download）不复用此 client，避免大文件被超时打断。
var metaClient = &http.Client{Timeout: 10 * time.Second}
```

(c) 在 `fetchJSON` 内把 `resp, err := http.Get(url)` 改为 `resp, err := metaClient.Get(url)`（仅此一处；`download` 内的 `http.Get` 保持不变）。

- [ ] **Step 4: 运行测试确认通过 + 既有不回归**

Run: `go test ./internal/update/ -v`
Expected: PASS（既有 CheckLatest/ListReleases/GetRelease 等 httptest 即时响应，10s 超时不触发）

- [ ] **Step 5: 提交**

```bash
git add internal/update/update.go internal/update/update_test.go
git commit -m "feat(update): bound metadata HTTP requests with a client timeout"
```

---

### Task 3: notifier 缓存层

**Files:**
- Create: `internal/notifier/cache.go`
- Test: `internal/notifier/cache_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/notifier/cache_test.go`：

```go
/**
 * [INPUT]: 依赖 notifier 包内 cacheData / readCache / writeCache / expired（白盒）
 * [OUTPUT]: 覆盖缓存原子读写与过期判定的单元测试
 * [POS]: internal/notifier 模块 cache.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())

	in := cacheData{
		CheckedAt:     time.Now().UTC().Truncate(time.Second),
		LatestVersion: "v1.2.3",
		HTMLURL:       "https://example.com/x",
	}
	if err := writeCache(in); err != nil {
		t.Fatalf("writeCache: %v", err)
	}
	out, err := readCache()
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if out.LatestVersion != in.LatestVersion || out.HTMLURL != in.HTMLURL {
		t.Errorf("roundtrip mismatch: got %+v want %+v", out, in)
	}
	if !out.CheckedAt.Equal(in.CheckedAt) {
		t.Errorf("CheckedAt mismatch: got %v want %v", out.CheckedAt, in.CheckedAt)
	}
}

func TestReadCache_Missing(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	c, err := readCache()
	if err != nil {
		t.Fatalf("expected no error for missing cache, got %v", err)
	}
	if c.LatestVersion != "" {
		t.Errorf("expected zero cache, got %+v", c)
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	fresh := cacheData{CheckedAt: now.Add(-1 * time.Hour)}
	stale := cacheData{CheckedAt: now.Add(-25 * time.Hour)}
	zero := cacheData{}

	if fresh.expired(24*time.Hour, now) {
		t.Error("1h-old cache should be fresh under 24h interval")
	}
	if !stale.expired(24*time.Hour, now) {
		t.Error("25h-old cache should be expired under 24h interval")
	}
	if !zero.expired(24*time.Hour, now) {
		t.Error("zero cache should be expired")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/notifier/ -run 'TestCache|TestReadCache|TestExpired' -v`
Expected: 编译失败（包 `internal/notifier` 不存在 / `undefined: cacheData`）

- [ ] **Step 3: 实现 cache.go**

创建 `internal/notifier/cache.go`：

```go
/**
 * [INPUT]: 依赖 encoding/json、os、path/filepath、time；依赖 internal/config 的 Dir
 * [OUTPUT]: 对外提供（包内）cacheData 类型与 readCache/writeCache/cachePath，及 expired 方法
 * [POS]: internal/notifier 的本地缓存层，持久化最近一次 GitHub 检测结果，供 Start/Finish 消费
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/qfeius/makecli/internal/config"
)

// cacheData 是 update-check.json 的结构
type cacheData struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
	HTMLURL       string    `json:"html_url"`
}

// cacheFileName 缓存文件名
const cacheFileName = "update-check.json"

// cachePath 返回缓存文件绝对路径（<config.Dir>/update-check.json）
func cachePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheFileName), nil
}

// readCache 读取缓存。文件不存在返回零值且无错误；损坏返回零值 + 错误。
func readCache() (cacheData, error) {
	path, err := cachePath()
	if err != nil {
		return cacheData{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cacheData{}, nil
	}
	if err != nil {
		return cacheData{}, err
	}
	var c cacheData
	if err := json.Unmarshal(b, &c); err != nil {
		return cacheData{}, err
	}
	return c, nil
}

// writeCache 原子写入缓存：写临时文件后 rename，避免与并发读发生撕裂。
func writeCache(c cacheData) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// expired 判定缓存是否已超过 interval（now 显式传入便于测试）
func (c cacheData) expired(interval time.Duration, now time.Time) bool {
	return now.Sub(c.CheckedAt) >= interval
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/notifier/ -run 'TestCache|TestReadCache|TestExpired' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/notifier/cache.go internal/notifier/cache_test.go
git commit -m "feat(notifier): add atomic update-check cache layer"
```

---

### Task 4: notifier 判定与渲染

**Files:**
- Create: `internal/notifier/decision.go`
- Test: `internal/notifier/decision_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/notifier/decision_test.go`：

```go
/**
 * [INPUT]: 依赖 notifier 包内 notifierEnabled / shouldNotify / renderNotice（白盒）
 * [OUTPUT]: 覆盖三态启用裁决、判定链穷举、提示渲染的单元测试
 * [POS]: internal/notifier 模块 decision.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"bytes"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestNotifierEnabled(t *testing.T) {
	cases := []struct {
		name string
		env  string
		cfg  *bool
		want bool
	}{
		{"default on", "", nil, true},
		{"config off", "", boolPtr(false), false},
		{"config on", "", boolPtr(true), true},
		{"env off overrides config on", "false", boolPtr(true), false},
		{"env on overrides config off", "true", boolPtr(false), true},
		{"env invalid sinks to config", "garbage", boolPtr(false), false},
		{"env invalid sinks to default", "garbage", nil, true},
		{"env 0", "0", nil, false},
		{"env 1", "1", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := notifierEnabled(c.env, c.cfg); got != c.want {
				t.Errorf("notifierEnabled(%q,%v) = %v, want %v", c.env, c.cfg, got, c.want)
			}
		})
	}
}

func TestShouldNotify(t *testing.T) {
	newer := cacheData{LatestVersion: "v2.0.0"}
	same := cacheData{LatestVersion: "v1.0.0"}
	empty := cacheData{}

	cases := []struct {
		name    string
		current string
		cmd     string
		tty     bool
		ci      string
		cache   cacheData
		want    bool
	}{
		{"happy path", "1.0.0", "app", true, "", newer, true},
		{"dev version", "DEV", "app", true, "", newer, false},
		{"ci set", "1.0.0", "app", true, "true", newer, false},
		{"not tty", "1.0.0", "app", false, "", newer, false},
		{"skip version cmd", "1.0.0", "version", true, "", newer, false},
		{"skip update cmd", "1.0.0", "update", true, "", newer, false},
		{"empty cmd", "1.0.0", "", true, "", newer, false},
		{"no cache", "1.0.0", "app", true, "", empty, false},
		{"same version", "1.0.0", "app", true, "", same, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldNotify(c.current, c.cmd, c.tty, c.ci, c.cache); got != c.want {
				t.Errorf("shouldNotify(%q,%q,tty=%v,ci=%q) = %v, want %v",
					c.current, c.cmd, c.tty, c.ci, got, c.want)
			}
		})
	}
}

func TestRenderNotice(t *testing.T) {
	var buf bytes.Buffer
	renderNotice(&buf, "1.0.0", cacheData{LatestVersion: "v2.0.0", HTMLURL: "https://example.com/r"})
	out := buf.String()
	for _, want := range []string{"1.0.0 → 2.0.0", "makecli update", "https://example.com/r"} {
		if !strings.Contains(out, want) {
			t.Errorf("notice missing %q; got:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/notifier/ -run 'TestNotifierEnabled|TestShouldNotify|TestRenderNotice' -v`
Expected: 编译失败（`undefined: notifierEnabled` 等）

- [ ] **Step 3: 实现 decision.go**

创建 `internal/notifier/decision.go`：

```go
/**
 * [INPUT]: 依赖 fmt、io、strconv、strings；依赖 github.com/Masterminds/semver/v3、internal/update 的 CompareVersions
 * [OUTPUT]: 对外提供（包内）notifierEnabled / shouldNotify / renderNotice 与 skipCommands 表
 * [POS]: internal/notifier 的判定与渲染层，被 notifier.go 的 Finish 编排
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/qfeius/makecli/internal/update"
)

// skipCommands 列出不应触发更新提示的顶级命令
var skipCommands = map[string]bool{
	"version":    true,
	"update":     true,
	"help":       true,
	"completion": true,
}

// notifierEnabled 三态裁决是否启用更新提示：env > config > 默认(true)。
//
//	envVal: MAKE_CLI_UPDATE_NOTIFIER 原始值（"" = 未设置；非法值忽略并下沉）
//	cfgVal: config [settings] check-for-updates（nil = 未设置）
func notifierEnabled(envVal string, cfgVal *bool) bool {
	if envVal != "" {
		if b, err := strconv.ParseBool(envVal); err == nil {
			return b
		}
	}
	if cfgVal != nil {
		return *cfgVal
	}
	return true
}

// isReleaseVersion 判定 current 是否为合法发布版本（DEV / 非法 semver → false）
func isReleaseVersion(current string) bool {
	_, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	return err == nil
}

// shouldNotify 在「已启用」前提下，判定是否真的要打印提示。任一条件不满足即 false。
func shouldNotify(current, cmdName string, isTTY bool, ci string, cache cacheData) bool {
	if !isReleaseVersion(current) {
		return false
	}
	if ci != "" {
		return false
	}
	if !isTTY {
		return false
	}
	if cmdName == "" || skipCommands[cmdName] {
		return false
	}
	if cache.LatestVersion == "" {
		return false
	}
	return update.CompareVersions(cache.LatestVersion, current) > 0
}

// renderNotice 将升级提示写入 w（调用方传 os.Stderr）
func renderNotice(w io.Writer, current string, cache cacheData) {
	cur := strings.TrimPrefix(current, "v")
	latest := strings.TrimPrefix(cache.LatestVersion, "v")
	const line = "─────────────────────────────────────────────"
	_, _ = fmt.Fprintf(w, "\n%s\n", line)
	_, _ = fmt.Fprintf(w, " A new release of makecli is available: %s → %s\n", cur, latest)
	_, _ = fmt.Fprintf(w, " To upgrade, run: makecli update\n")
	if cache.HTMLURL != "" {
		_, _ = fmt.Fprintf(w, " %s\n", cache.HTMLURL)
	}
	_, _ = fmt.Fprintf(w, "%s\n", line)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/notifier/ -run 'TestNotifierEnabled|TestShouldNotify|TestRenderNotice' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/notifier/decision.go internal/notifier/decision_test.go
git commit -m "feat(notifier): add enable/should-notify decision and notice rendering"
```

---

### Task 5: notifier 编排（Start/Finish）

**Files:**
- Create: `internal/notifier/notifier.go`
- Test: `internal/notifier/notifier_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/notifier/notifier_test.go`：

```go
/**
 * [INPUT]: 依赖 notifier 包内 Start / Finish / isStderrTTY（白盒）；internal/build、internal/update 的测试钩子
 * [OUTPUT]: 覆盖后台刷新落盘、新鲜缓存跳过、Finish 收尾不阻塞的单元测试
 * [POS]: internal/notifier 模块 notifier.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/update"
)

func setBuildVersion(t *testing.T, v string) {
	t.Helper()
	old := build.Version
	build.Version = v
	t.Cleanup(func() { build.Version = old })
}

func setTTY(t *testing.T, v bool) {
	t.Helper()
	old := isStderrTTY
	isStderrTTY = func() bool { return v }
	t.Cleanup(func() { isStderrTTY = old })
}

// mockLatest 启动 httptest 返回指定 latest release 并替换 update 的 API URL
func mockLatest(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://example.com/` + tag + `"}`))
	}))
	old := update.SetAPIBaseURLForTest(srv.URL)
	t.Cleanup(func() {
		update.SetAPIBaseURLForTest(old)
		srv.Close()
	})
}

func TestStartRefreshesCache(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setBuildVersion(t, "1.0.0")
	mockLatest(t, "v2.0.0")

	n := Start()
	<-n.done // 等后台刷新完成（测试内确定性）

	c, err := readCache()
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if c.LatestVersion != "v2.0.0" {
		t.Errorf("cache latest = %q, want v2.0.0", c.LatestVersion)
	}
}

func TestStartSkipsWhenFresh(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setBuildVersion(t, "1.0.0")

	if err := writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v1.5.0"}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	mockLatest(t, "v9.9.9") // 若被请求会污染缓存

	n := Start()
	<-n.done

	c, _ := readCache()
	if c.LatestVersion != "v1.5.0" {
		t.Errorf("fresh cache should be untouched, got %q", c.LatestVersion)
	}
}

func TestFinishDisabledDoesNotBlock(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("MAKE_CLI_UPDATE_NOTIFIER", "false")
	setBuildVersion(t, "1.0.0")
	setTTY(t, true)
	_ = writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v2.0.0"})

	n := &Notifier{done: make(chan struct{})}
	close(n.done)

	done := make(chan struct{})
	go func() { n.Finish("app"); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Finish blocked too long")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/notifier/ -run 'TestStart|TestFinish' -v`
Expected: 编译失败（`undefined: Start` / `isStderrTTY` / `Notifier`）

- [ ] **Step 3: 实现 notifier.go**

创建 `internal/notifier/notifier.go`：

```go
/**
 * [INPUT]: 依赖 os、time；依赖 github.com/mattn/go-isatty、internal/build 的 Version、internal/config 的 LoadSettings、internal/update 的 CheckLatest
 * [OUTPUT]: 对外提供 Notifier 类型、Start、(*Notifier).Finish；包内 isStderrTTY 钩子
 * [POS]: internal/notifier 的编排入口，被 cmd.Execute 在命令头尾钩入：并行刷新缓存 + 收尾打印提示
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/update"
)

const (
	checkInterval  = 24 * time.Hour
	finishDeadline = 250 * time.Millisecond
	envEnable      = "MAKE_CLI_UPDATE_NOTIFIER"
)

// isStderrTTY 检测 stderr 是否为终端；包级变量便于测试替换
var isStderrTTY = func() bool {
	return isatty.IsTerminal(os.Stderr.Fd())
}

// Notifier 协调后台刷新与收尾提示
type Notifier struct {
	done chan struct{}
}

// Start 读缓存；缓存过期才起后台 goroutine 刷新。立即返回，不阻塞主命令。
func Start() *Notifier {
	n := &Notifier{done: make(chan struct{})}

	cache, _ := readCache()
	if !cache.expired(checkInterval, time.Now()) {
		close(n.done)
		return n
	}

	go func() {
		defer close(n.done)
		defer func() { _ = recover() }() // 兜底 panic，绝不影响主流程

		release, _, err := update.CheckLatest(build.Version)
		if err != nil || release == nil {
			return
		}
		_ = writeCache(cacheData{
			CheckedAt:     time.Now(),
			LatestVersion: release.TagName,
			HTMLURL:       release.HTMLURL,
		})
	}()
	return n
}

// Finish 给后台刷新一个极短的收尾窗口，然后按判定链决定是否打印提示。
// cmdName 为本次调用的顶级命令名（由 cmd 层解析传入）。
func (n *Notifier) Finish(cmdName string) {
	select {
	case <-n.done:
	case <-time.After(finishDeadline):
	}

	settings, _ := config.LoadSettings()
	if !notifierEnabled(os.Getenv(envEnable), settings.CheckForUpdates) {
		return
	}

	cache, err := readCache()
	if err != nil {
		return
	}
	if !shouldNotify(build.Version, cmdName, isStderrTTY(), os.Getenv("CI"), cache) {
		return
	}
	renderNotice(os.Stderr, build.Version, cache)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/notifier/ -v`
Expected: PASS（本包全部测试）

- [ ] **Step 5: 提交**

```bash
git add internal/notifier/notifier.go internal/notifier/notifier_test.go
git commit -m "feat(notifier): orchestrate background refresh and finish notice"
```

---

### Task 6: 接入 cmd.Execute

**Files:**
- Modify: `cmd/root.go`
- Create: `cmd/root_test.go`

- [ ] **Step 1: 写失败测试**

创建 `cmd/root_test.go`：

```go
/**
 * [INPUT]: 依赖 cmd 包内 commandName（白盒）、github.com/spf13/cobra
 * [OUTPUT]: 覆盖 commandName 顶级命令解析的单元测试
 * [POS]: cmd 模块 root.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandName(t *testing.T) {
	root := &cobra.Command{Use: "makecli"}
	version := &cobra.Command{Use: "version"}
	version.AddCommand(&cobra.Command{Use: "list"})
	app := &cobra.Command{Use: "app"}
	app.AddCommand(&cobra.Command{Use: "create"})
	root.AddCommand(version, app, &cobra.Command{Use: "update"})

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"version"}, "version"},
		{[]string{"version", "list"}, "version"},
		{[]string{"update"}, "update"},
		{[]string{"app", "create", "foo"}, "app"},
		{[]string{}, ""},
		{[]string{"nonsense"}, ""},
	}
	for _, c := range cases {
		if got := commandName(root, c.args); got != c.want {
			t.Errorf("commandName(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/ -run TestCommandName -v`
Expected: 编译失败（`undefined: commandName`）

- [ ] **Step 3: 实现 commandName 并钩入 Execute**

编辑 `cmd/root.go`：

(a) import 块加入 `"os"` 和 notifier（现有 import 为 cobra、pflag）：

```go
import (
	"os"

	"github.com/qfeius/makecli/internal/notifier"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)
```

(b) 把 `Execute` 末尾的 `return rootCmd.Execute()` 替换为：

```go
	n := notifier.Start()
	err := rootCmd.Execute()
	n.Finish(commandName(rootCmd, os.Args[1:]))
	return err
}

// commandName 解析本次实际调用的顶级子命令名（version/update/app...）。
// 无子命令或解析失败时返回 ""（由判定链视为跳过）。
func commandName(root *cobra.Command, args []string) string {
	cmd, _, err := root.Find(args)
	if err != nil || cmd == nil || cmd == root {
		return ""
	}
	for cmd.Parent() != nil && cmd.Parent() != root {
		cmd = cmd.Parent()
	}
	return cmd.Name()
}
```

(c) 更新 `cmd/root.go` 顶部 L3 头部 `[INPUT]` 与 `[OUTPUT]`：

```go
 * [INPUT]: 依赖 github.com/spf13/cobra、github.com/spf13/pflag、os、internal/notifier
 * [OUTPUT]: 对外提供 Execute 函数、rootCmd 根命令、全局变量 Profile / ServerURL / DebugMode；包内 commandName 解析器
```

- [ ] **Step 4: 运行测试确认通过 + go mod tidy 提升 isatty 为直接依赖**

```bash
go mod tidy
go test ./cmd/ -run TestCommandName -v
```
Expected: TestCommandName PASS；`go.mod` 中 `github.com/mattn/go-isatty` 行不再带 `// indirect`

- [ ] **Step 5: 提交**

```bash
git add cmd/root.go cmd/root_test.go go.mod go.sum
git commit -m "feat(cmd): wire update notifier into Execute lifecycle"
```

---

### Task 7: 文档同步（GEB）+ 全量验证

**Files:**
- Create: `internal/notifier/CLAUDE.md`
- Modify: `CLAUDE.md`（根）、`cmd/CLAUDE.md`、`internal/config/CLAUDE.md`、`internal/update/CLAUDE.md`

- [ ] **Step 1: 创建 notifier 的 L2 CLAUDE.md**

创建 `internal/notifier/CLAUDE.md`：

```markdown
# internal/notifier/
> L2 | 父级: /CLAUDE.md

## 成员清单
cache.go:        本地缓存层，cacheData(checked_at/latest_version/html_url) 原子读写（temp+rename）+ expired 过期判定；路径 <config.Dir>/update-check.json；读缺失返回零值，损坏返回错误
cache_test.go:   覆盖缓存往返 / 缺失零值 / 过期判定，用 MAKE_CLI_CONFIG_DIR 隔离文件系统
decision.go:     纯判定层，notifierEnabled（三态 env>config>默认开）/ shouldNotify（DEV·CI·非TTY·skipCommands·无更新逐条短路）/ renderNotice（提示写 stderr）/ isReleaseVersion
decision_test.go: 穷举 notifierEnabled / shouldNotify 组合 + renderNotice 内容断言
notifier.go:     编排入口，Start（缓存过期才起 goroutine 调 update.CheckLatest 刷新，recover 兜底）/ Finish（finishDeadline 收尾→LoadSettings→判定→renderNotice）；isStderrTTY 钩子便于测试
notifier_test.go: Start 刷新落盘 / 新鲜跳过 / Finish 禁用不阻塞，用 httptest + SetAPIBaseURLForTest 隔离网络

## 关键常量
checkInterval=24h · finishDeadline=250ms · envEnable=MAKE_CLI_UPDATE_NOTIFIER

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
```

- [ ] **Step 2: 更新根 CLAUDE.md 的 directory 段**

编辑 `CLAUDE.md`，在 `<directory>` 块 `internal/update/` 行下方追加：

```
internal/notifier/ - 自动更新提示（读本地缓存零延迟判定，过期后台 goroutine 刷新，stderr+仅TTY 提示；三态开关 env>config>默认开）
```

- [ ] **Step 3: 更新 cmd/CLAUDE.md**

编辑 `cmd/CLAUDE.md` 成员清单：

(a) 将 `root.go:` 行改为：

```
root.go:             根命令入口，挂载所有子命令（含 schema），对外暴露 Execute(version, date)；Execute 头尾钩入 notifier.Start/Finish 做更新提示；commandName 解析顶级命令名；定义全局 PersistentFlag --profile / --server-url / --debug
```

(b) 在 `stdout_test.go:` 行下方追加：

```
root_test.go:        覆盖 commandName 顶级命令解析（version/version list/update/app create/空/未知）的单元测试
```

- [ ] **Step 4: 更新 internal/config/CLAUDE.md**

编辑 `internal/config/CLAUDE.md` 成员清单：

(a) 将 `config.go:` 行改为：

```
config.go:           读写 config 文件（默认 ~/.make/config，INI 格式），提供 LoadConfig/SaveConfig/ConfigPath，Config/ConfigProfile 类型；通用 parseINISections（section→kv）下沉，parseConfigINI 委托并跳过保留的 [settings] 全局段
```

(b) 在 `config_test.go:` 行下方追加：

```
settings.go:         全局设置读取，Settings 类型（CheckForUpdates *bool 三态）+ LoadSettings 读 [settings] 段的 check-for-updates；best-effort（文件缺失返回空）
settings_test.go:    覆盖 LoadSettings 未设/开/关 + settings 段不污染 profile 解析，用 t.Setenv(HOME) 隔离
```

- [ ] **Step 5: 更新 internal/update/CLAUDE.md**

编辑 `internal/update/CLAUDE.md`，将 `update.go:` 行尾部补充：

```
；metaClient（http.Client+10s 超时）用于 JSON 元数据请求（CheckLatest/ListReleases/GetRelease），二进制下载不复用以免大文件被打断
```

- [ ] **Step 6: 全量验证**

```bash
make vet
make test
make build
```
Expected: vet 无输出；test 全绿（含 internal/notifier、internal/config、internal/update、cmd 各包）；build 产出 `bin/makecli`

- [ ] **Step 7: 手动冒烟（可选但推荐）**

```bash
# 预置一条"有新版本"的缓存，强制 TTY 路径在真实终端验证提示
MAKE_CLI_CONFIG_DIR=/tmp/mk-notify-smoke ./bin/makecli app list 2>&1 | tail -5 || true
```
说明：DEV 构建会被 `isReleaseVersion` 挡掉（不显示提示），这是预期；如需肉眼验证提示文案，临时用 `go run -ldflags "-X github.com/qfeius/makecli/internal/build.Version=1.0.0"` 并预置 `latest_version=v2.0.0` 的缓存。提示走 stderr，仅终端可见。

- [ ] **Step 8: 提交**

```bash
git add CLAUDE.md cmd/CLAUDE.md internal/config/CLAUDE.md internal/update/CLAUDE.md internal/notifier/CLAUDE.md
git commit -m "docs(geb): sync L1/L2 maps for update notifier"
```

---

## 自检（Spec 覆盖核对）

- §2 架构（Start/Execute/Finish） → Task 5 + 6 ✓
- §3 缓存文件路径/结构/best-effort → Task 3 ✓
- §4 goroutine+deadline + recover + HTTP 超时 → Task 5（goroutine/recover/deadline）+ Task 2（超时）✓
- §5 三态启用（env>config>默认）+ `[settings]` 段 + LoadSettings + 通用 parser → Task 1 + Task 4（notifierEnabled）✓
- §5b 判定链（DEV/CI/TTY/skip/无更新） → Task 4 shouldNotify ✓
- §6 提示文案→stderr → Task 4 renderNotice ✓
- §7 测试策略（t.Setenv 隔离 fs、httptest 隔离网络、TTY 可注入） → 各 Task ✓
- §8 常量默认值（24h / 250ms / 文件名 / env 名 / config 项） → Task 3 + 5 ✓
- §9 文档同步 → Task 7 ✓
- §10 非目标（无子进程、无自动更新） → 已遵守 ✓

类型/签名一致性核对：`cacheData`、`Settings.CheckForUpdates *bool`、`notifierEnabled(string,*bool)`、`shouldNotify(string,string,bool,string,cacheData)`、`renderNotice(io.Writer,string,cacheData)`、`commandName(*cobra.Command,[]string)`、`Start() *Notifier`、`(*Notifier).Finish(string)` —— 跨任务一致。无导入环（notifier→config/update/build；cmd→notifier）。
