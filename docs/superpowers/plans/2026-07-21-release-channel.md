# 发布通道（stable / beta）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** makecli 支持 beta 预发布通道：`configure set channel beta` 后，裸 `update` 与后台更新提示自动追踪 GitHub prerelease；稳定用户与所有已发行旧版本完全无感。

**Architecture:** GitHub release 的 `prerelease` 标志为通道归属单一真相源。stable 通道走 `/releases/latest`（服务端过滤，现状零改动）；beta 通道走 `/releases` 列表客户端取 semver 最高（候选天然含稳定版）。通道持久化在 `[settings] channel`，完全复刻 environment 的既有模式。notifier 用 `^beta\.\d+$` 白名单区分真 beta 与 git-describe 伪版本，缓存带 channel 字段跨通道失效。

**Tech Stack:** Go 1.25.8 · Masterminds/semver/v3（已有依赖）· GoReleaser `prerelease: auto`

**Spec:** `docs/superpowers/specs/2026-07-21-release-channel-design.md`

## Global Constraints

- 验证门控提交：每个 task 单独跑 `make vet && make test` **确认 exit 0** 后才 commit；禁止同一批工具调用里 test + commit
- Go 工具链命令（vet/test/build）沙箱下假性失败（module cache 不可写），直接禁用沙箱跑
- 通道常量只有两个值：`stable` / `beta`（`config.ChannelStable` / `config.ChannelBeta`），默认 `stable`
- beta 白名单正则：`^beta\.[0-9]+$`（拒 git-describe 伪版本如 `16-ga4765c1`）
- 提交信息用 conventional commit；每个 task 一个 commit
- 触及模块的 GEB L2 头部/成员清单在 Task 7 统一回环（任务中不必逐个改）

---

### Task 1: internal/config — 通道域常量与 Settings.Channel

**Files:**
- Create: `internal/config/channel.go`
- Create: `internal/config/channel_test.go`
- Modify: `internal/config/settings.go`（Settings 结构体 + LoadSettings）

**Interfaces:**
- Consumes: 既有 `parseINISections` / `LoadSettings` 机制
- Produces: `config.ChannelStable` / `config.ChannelBeta` / `config.DefaultChannel` 常量、`config.ChannelNames() []string`、`config.Settings.Channel string`（空串 = 未配置）

- [ ] **Step 1: 写失败测试** `internal/config/channel_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestChannelNames(t *testing.T) {
	if names := ChannelNames(); !slices.Equal(names, []string{"stable", "beta"}) {
		t.Fatalf("ChannelNames() = %v, want [stable beta]", names)
	}
	if DefaultChannel != ChannelStable {
		t.Fatalf("DefaultChannel = %q, want %q", DefaultChannel, ChannelStable)
	}
}

func TestLoadSettingsChannel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	content := "[settings]\nchannel = beta\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Channel != ChannelBeta {
		t.Fatalf("Channel = %q, want %q", s.Channel, ChannelBeta)
	}
}

func TestLoadSettingsChannelUnset(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	s, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Channel != "" {
		t.Fatalf("Channel = %q, want empty (未配置)", s.Channel)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**（`ChannelNames` undefined 编译错）

Run: `go test ./internal/config/`（禁用沙箱）
Expected: FAIL / build error: undefined: ChannelNames

- [ ] **Step 3: 实现** — 新建 `internal/config/channel.go`：

```go
/**
 * [INPUT]: 无外部依赖
 * [OUTPUT]: 对外提供 ChannelStable/ChannelBeta/DefaultChannel 常量与 ChannelNames 函数
 * [POS]: internal/config 的发布通道域常量（域取值单一真相源，与 environment.go 同责），被 cmd 层与 internal/notifier 消费
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

// 发布通道：stable 只跟踪正式版（GitHub /releases/latest 服务端语义），
// beta 额外跟踪 prerelease（/releases 列表取 semver 最高，候选天然含稳定版）。
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"

	// DefaultChannel 是未配置时的回退通道
	DefaultChannel = ChannelStable
)

// ChannelNames 返回全部合法通道名（固定顺序，供校验与错误提示）
func ChannelNames() []string {
	return []string{ChannelStable, ChannelBeta}
}
```

修改 `internal/config/settings.go`：Settings 加字段（放在 Environment 之后）：

```go
	// Channel 是发布通道名（stable/beta）；空串 = 未配置（调用方回退 DefaultChannel）
	Channel string
```

`LoadSettings` 的 settings 段读取处，在 `s.Environment = kv["environment"]` 之后加：

```go
		s.Channel = kv["channel"]
```

同步更新 settings.go 的 L3 头部 `[OUTPUT]` 行（Settings 字段描述补 Channel）。

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/config/channel.go internal/config/channel_test.go internal/config/settings.go
git commit -m "feat(config): release channel constants and [settings] channel key"
```

---

### Task 2: internal/update — CheckLatest 双通道签名 + IsPrerelease

**Files:**
- Modify: `internal/update/update.go`（CheckLatest 改签名 + latestRelease/maxSemverRelease/IsPrerelease）
- Modify: `internal/update/update_test.go`（既有 3 处 CheckLatest 调用补 `false` + 新测试）
- Modify: `cmd/update.go:65,100`（机械补 `false`，Task 4 再接真通道）
- Modify: `internal/notifier/notifier.go:52`（机械补 `false`，Task 3 再接真通道）

**Interfaces:**
- Consumes: 既有 `ListReleases` / `isNewer` / `fetchJSON` / `SetAPIBaseURLForTest(url string) string`
- Produces: `update.CheckLatest(currentVersion string, includePrerelease bool) (*Release, bool, error)`、`update.IsPrerelease(version string) bool`

- [ ] **Step 1: 写失败测试**（追加到 `internal/update/update_test.go`）

```go
func TestCheckLatest_IncludePrerelease_PicksHighestSemver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/qfeius/makecli/releases" {
			t.Errorf("path = %s, want /repos/qfeius/makecli/releases", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.6.0-beta.2","prerelease":true},
			{"tag_name":"v0.5.5","prerelease":false},
			{"tag_name":"not-a-version","prerelease":false},
			{"tag_name":"v0.6.0-beta.10","prerelease":true}
		]`))
	}))
	defer srv.Close()
	old := SetAPIBaseURLForTest(srv.URL)
	defer SetAPIBaseURLForTest(old)

	rel, newer, err := CheckLatest("0.5.5", true)
	if err != nil {
		t.Fatal(err)
	}
	// semver 数值段排序：beta.10 > beta.2；非法 tag 跳过
	if rel.TagName != "v0.6.0-beta.10" {
		t.Fatalf("TagName = %s, want v0.6.0-beta.10", rel.TagName)
	}
	if !newer {
		t.Fatal("expected newer=true")
	}
}

func TestCheckLatest_IncludePrerelease_StableWins(t *testing.T) {
	// 稳定版反超 beta：候选集含稳定版，semver 最大自然指向稳定版
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.7.0-beta.3","prerelease":true},
			{"tag_name":"v0.7.0","prerelease":false}
		]`))
	}))
	defer srv.Close()
	old := SetAPIBaseURLForTest(srv.URL)
	defer SetAPIBaseURLForTest(old)

	rel, _, err := CheckLatest("0.7.0-beta.3", true)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v0.7.0" {
		t.Fatalf("TagName = %s, want v0.7.0", rel.TagName)
	}
}

func TestCheckLatest_IncludePrerelease_NoValidReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"garbage","prerelease":false}]`))
	}))
	defer srv.Close()
	old := SetAPIBaseURLForTest(srv.URL)
	defer SetAPIBaseURLForTest(old)

	if _, _, err := CheckLatest("1.0.0", true); err == nil {
		t.Fatal("expected error for no valid releases")
	}
}

func TestIsPrerelease(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"v0.6.0-beta.1", true},
		{"0.6.0-beta.1", true},
		{"v0.3.0-16-ga4765c1", true}, // git-describe 伪版本也是预发布段（本函数不做白名单）
		{"v0.6.0", false},
		{"DEV", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsPrerelease(c.version); got != c.want {
			t.Errorf("IsPrerelease(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**（CheckLatest 参数不匹配 / IsPrerelease undefined）

Run: `go test ./internal/update/`（禁用沙箱）
Expected: FAIL / build error

- [ ] **Step 3: 实现** — `internal/update/update.go` 用以下内容**替换**现有 `CheckLatest`：

```go
// CheckLatest 查询 GitHub 最新 release，返回 release 信息和是否有更新。
//
//	includePrerelease=false → GET /releases/latest：GitHub 服务端契约只返回
//	  最新的非 prerelease、非 draft release（stable 通道）
//	includePrerelease=true  → GET /releases 列表取 semver 最高者（beta 通道；
//	  候选天然含稳定版，稳定版反超 beta 时自动收敛回 stable）
func CheckLatest(currentVersion string, includePrerelease bool) (*Release, bool, error) {
	release, err := latestRelease(includePrerelease)
	if err != nil {
		return nil, false, err
	}
	return release, isNewer(currentVersion, release.TagName), nil
}

// latestRelease 按通道语义取最新 release
func latestRelease(includePrerelease bool) (*Release, error) {
	if !includePrerelease {
		url := apiBaseURL + "/repos/qfeius/makecli/releases/latest"
		var release Release
		status, err := fetchJSON(url, &release)
		if err != nil {
			return nil, fmt.Errorf("failed to check for updates: %w", err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("failed to check for updates: HTTP %d", status)
		}
		return &release, nil
	}

	releases, err := ListReleases(100)
	if err != nil {
		return nil, err
	}
	if r := maxSemverRelease(releases); r != nil {
		return r, nil
	}
	return nil, fmt.Errorf("failed to check for updates: no valid releases")
}

// maxSemverRelease 返回 tag 为合法 semver 的最高版本 release（含预发布段；非法
// tag 跳过）。列表按 created_at 倒序，但乱序补发旧版 tag 时时间序不可靠，故显式
// 取 semver 最大而非首元素。
func maxSemverRelease(releases []Release) *Release {
	var best *Release
	var bestV *semver.Version
	for i := range releases {
		v, err := semver.NewVersion(strings.TrimPrefix(releases[i].TagName, "v"))
		if err != nil {
			continue
		}
		if bestV == nil || v.GreaterThan(bestV) {
			best, bestV = &releases[i], v
		}
	}
	return best
}

// IsPrerelease 判定版本是否带 semver 预发布段（v0.6.0-beta.1 → true）。
// DEV / 非法 semver → false。
func IsPrerelease(version string) bool {
	v, err := semver.NewVersion(strings.TrimPrefix(version, "v"))
	if err != nil {
		return false
	}
	return v.Prerelease() != ""
}
```

机械更新全部既有调用点（保持现状行为，传 `false`）：

- `internal/update/update_test.go`：`CheckLatest("1.0.0")` → `CheckLatest("1.0.0", false)`（3 处：Newer/UpToDate/HTTPError）
- `cmd/update.go` 两处：`update.CheckLatest(currentVersion)` → `update.CheckLatest(currentVersion, false)`
- `internal/notifier/notifier.go` 一处：`update.CheckLatest(build.Version)` → `update.CheckLatest(build.Version, false)`

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/update/update.go internal/update/update_test.go cmd/update.go internal/notifier/notifier.go
git commit -m "feat(update): CheckLatest gains includePrerelease channel path + IsPrerelease"
```

---

### Task 3: internal/notifier — 通道感知（缓存 Channel + versionInChannel + Start/Finish 接线）

**Files:**
- Modify: `internal/notifier/cache.go`（cacheData 加 Channel）
- Modify: `internal/notifier/decision.go`（isReleaseVersion → versionInChannel；shouldNotify 加 channel 参数）
- Modify: `internal/notifier/notifier.go`（channelOf helper；Start/Finish 接通道）
- Modify: `internal/notifier/decision_test.go`（既有 shouldNotify 用例适配 + 新矩阵）
- Modify: `internal/notifier/cache_test.go` / `notifier_test.go`（按需适配 + 新用例）

**Interfaces:**
- Consumes: `config.ChannelStable` / `config.ChannelBeta` / `config.LoadSettings`（Task 1）、`update.CheckLatest(v, includePrerelease)`（Task 2）
- Produces: 包内 `versionInChannel(current, channel string) bool`、`shouldNotify(current, cmdName string, isTTY bool, ci string, cache cacheData, channel string) bool`、`channelOf(s config.Settings) string`、`cacheData.Channel string`

- [ ] **Step 1: 写失败测试**（追加到 `internal/notifier/decision_test.go`；notifier_test.go 加 Start 用例）

```go
func TestVersionInChannel(t *testing.T) {
	cases := []struct {
		version string
		channel string
		want    bool
	}{
		{"v0.5.5", config.ChannelStable, true},           // 正式版 ∈ stable
		{"v0.5.5", config.ChannelBeta, true},             // 正式版 ∈ beta（超集）
		{"v0.6.0-beta.1", config.ChannelBeta, true},      // 真 beta ∈ beta
		{"v0.6.0-beta.1", config.ChannelStable, false},   // 真 beta ∉ stable（现状语义）
		{"v0.3.0-16-ga4765c1", config.ChannelBeta, false}, // git-describe 伪版本被白名单拒绝
		{"v0.6.0-rc.1", config.ChannelBeta, false},       // 非 beta.N 预发布段不进 beta 通道
		{"DEV", config.ChannelBeta, false},
		{"DEV", config.ChannelStable, false},
	}
	for _, c := range cases {
		if got := versionInChannel(c.version, c.channel); got != c.want {
			t.Errorf("versionInChannel(%q, %q) = %v, want %v", c.version, c.channel, got, c.want)
		}
	}
}

func TestShouldNotifyChannelMismatchCache(t *testing.T) {
	// 跨通道缓存不可用：beta 通道拿着 stable 缓存不提示
	cache := cacheData{LatestVersion: "v9.9.9", Channel: config.ChannelStable}
	if shouldNotify("v0.5.5", "apply", true, "", cache, config.ChannelBeta) {
		t.Fatal("cross-channel cache must not notify")
	}
}

func TestShouldNotifyBetaChannel(t *testing.T) {
	// beta 通道 + 真 beta current + beta 缓存 → 正常提示
	cache := cacheData{LatestVersion: "v0.6.0-beta.2", Channel: config.ChannelBeta}
	if !shouldNotify("v0.6.0-beta.1", "apply", true, "", cache, config.ChannelBeta) {
		t.Fatal("expected notify for newer beta on beta channel")
	}
}
```

`internal/notifier/notifier_test.go` 追加（import 需补 `path/filepath`、`github.com/qfeius/makecli/internal/config`）：

```go
func TestStartRefreshesBetaChannel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[settings]\nchannel = beta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[{"tag_name":"v9.9.9-beta.1","prerelease":true,"html_url":"https://example.com/r"}]`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	n := Start()
	<-n.done

	if gotPath != "/repos/qfeius/makecli/releases" {
		t.Fatalf("path = %s, want /repos/qfeius/makecli/releases (beta 走列表端点)", gotPath)
	}
	cache, err := readCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache.Channel != config.ChannelBeta {
		t.Fatalf("cache.Channel = %q, want beta", cache.Channel)
	}
	if cache.LatestVersion != "v9.9.9-beta.1" {
		t.Fatalf("cache.LatestVersion = %q", cache.LatestVersion)
	}
}

func TestStartRefreshesOnChannelSwitch(t *testing.T) {
	// 新鲜但跨通道的缓存必须触发刷新
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[settings]\nchannel = beta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v0.5.5", Channel: config.ChannelStable}); err != nil {
		t.Fatal(err)
	}
	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		_, _ = w.Write([]byte(`[{"tag_name":"v0.6.0-beta.1","prerelease":true}]`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	n := Start()
	<-n.done

	if !requested {
		t.Fatal("fresh-but-cross-channel cache must trigger refresh")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/notifier/`（禁用沙箱）
Expected: FAIL / build error: undefined versionInChannel / cacheData 无 Channel 字段

- [ ] **Step 3: 实现**

`cache.go` — cacheData 加字段：

```go
type cacheData struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
	HTMLURL       string    `json:"html_url"`
	Channel       string    `json:"channel"`
}
```

（旧二进制写的缓存无 channel 字段 → 反序列化为空串 → 与任何通道不匹配 → 触发一次刷新后自愈，无需迁移逻辑。）

`decision.go` — **删除** `isReleaseVersion`，替换为（import 补 `regexp`、`github.com/qfeius/makecli/internal/config`）：

```go
// betaSegRe 匹配合法 beta 预发布段（beta.N 白名单）。git-describe 伪版本
// （如 16-ga4765c1）与 go install 模块伪版本天然被拒——开发态构建即使切了
// beta 通道也保持静默。
var betaSegRe = regexp.MustCompile(`^beta\.[0-9]+$`)

// versionInChannel 判定 current 是否为 channel 内的正式发布版本。
// stable：无预发布段；beta：无预发布段或 beta.N。DEV / 非法 semver 恒 false。
// 调用必须先于 CompareVersions（DEV/非法版本在其中恒返回 +1，不加此守卫会让
// 开发构建永远显示更新提示）。
func versionInChannel(current, channel string) bool {
	v, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return false
	}
	pre := v.Prerelease()
	if pre == "" {
		return true
	}
	return channel == config.ChannelBeta && betaSegRe.MatchString(pre)
}
```

`shouldNotify` 改签名并接通道（首守卫替换 + 新增跨通道缓存短路）：

```go
func shouldNotify(current, cmdName string, isTTY bool, ci string, cache cacheData, channel string) bool {
	if !versionInChannel(current, channel) {
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
	if cache.Channel != channel {
		return false
	}
	if cache.LatestVersion == "" {
		return false
	}
	return update.CompareVersions(cache.LatestVersion, current) > 0
}
```

`notifier.go` — 加 helper 并接线 Start/Finish：

```go
// channelOf 从 Settings 提取通道，未知值回退 stable——notifier 侧 fail-safe
// 不报错（报错属于 update 命令的职责边界）。
func channelOf(s config.Settings) string {
	if s.Channel == config.ChannelBeta {
		return config.ChannelBeta
	}
	return config.ChannelStable
}
```

`Start`：

```go
func Start() *Notifier {
	n := &Notifier{done: make(chan struct{})}

	settings, _ := config.LoadSettings()
	channel := channelOf(settings)

	cache, _ := readCache()
	if !cache.expired(checkInterval, time.Now()) && cache.Channel == channel {
		close(n.done)
		return n
	}

	go func() {
		defer close(n.done)
		defer func() { _ = recover() }() // 兜底 panic，绝不影响主流程

		cleanStaleTemps(time.Now()) // 清扫此前 writeCache 夭折的孤儿临时文件

		release, _, err := update.CheckLatest(build.Version, channel == config.ChannelBeta)
		if err != nil || release == nil {
			// 刷新失败也落盘退避标记（含通道，避免下次误判跨通道再刷）
			_ = writeCache(cacheData{CheckedAt: time.Now(), Channel: channel})
			return
		}
		_ = writeCache(cacheData{
			CheckedAt:     time.Now(),
			LatestVersion: release.TagName,
			HTMLURL:       release.HTMLURL,
			Channel:       channel,
		})
	}()
	return n
}
```

`Finish`：`shouldNotify` 调用处传入通道（复用已加载的 settings）：

```go
	if !shouldNotify(build.Version, cmdName, isStderrTTY(), os.Getenv("CI"), cache, channelOf(settings)) {
		return
	}
```

既有测试适配（同一 Step 内完成）：

- `decision_test.go` 中所有 `shouldNotify(...)` 调用补第 6 参数 `config.ChannelStable`，其 cache 字面量补 `Channel: config.ChannelStable`（否则跨通道短路导致误 false）；直接测试 `isReleaseVersion` 的用例改为测 `versionInChannel(v, config.ChannelStable)`（语义完全等价）
- `notifier_test.go` / `backoff_test.go` 中断言 `writeCache`/`readCache` 结果的用例，期望值补 `Channel: config.ChannelStable`（Start 现在总是落盘通道）；若既有 Start 用例的临时 config 目录无 config 文件，行为不变（LoadSettings 空 → stable）

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/
git commit -m "feat(notifier): channel-aware update notices with per-channel cache"
```

---

### Task 4: cmd/update — 通道解析、beta 回显、降级提示

**Files:**
- Modify: `cmd/client.go`（resolveChannel，与 resolveEnvironment 并列）
- Modify: `cmd/update.go`（runUpdateCheck / runUpdateLatest 接通道 + channelSuffix / hintBetaAboveStable + Example 补一行）
- Modify: `cmd/client_test.go`（resolveChannel 用例）
- Modify: `cmd/update_test.go`（beta 通道走列表端点 + 降级提示 + 通道回显用例）

**Interfaces:**
- Consumes: `config.ChannelNames/DefaultChannel/ChannelBeta/ChannelStable`（Task 1）、`update.CheckLatest(v, bool)` / `update.IsPrerelease` / `update.CompareVersions`（Task 2）
- Produces: 包内 `resolveChannel() (string, error)`、`channelSuffix(channel string) string`、`hintBetaAboveStable(w io.Writer, current, latestStable, channel string)`

- [ ] **Step 1: 写失败测试**

`cmd/client_test.go` 追加（import 补 `os`、`path/filepath`、`github.com/qfeius/makecli/internal/config`）：

```go
func TestResolveChannel(t *testing.T) {
	writeSettings := func(t *testing.T, content string) {
		t.Helper()
		dir := t.TempDir()
		t.Setenv(config.EnvConfigDir, dir)
		if content != "" {
			if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("unset falls back to stable", func(t *testing.T) {
		writeSettings(t, "")
		ch, err := resolveChannel()
		if err != nil {
			t.Fatal(err)
		}
		if ch != config.ChannelStable {
			t.Fatalf("channel = %q, want stable", ch)
		}
	})

	t.Run("beta from settings", func(t *testing.T) {
		writeSettings(t, "[settings]\nchannel = beta\n")
		ch, err := resolveChannel()
		if err != nil {
			t.Fatal(err)
		}
		if ch != config.ChannelBeta {
			t.Fatalf("channel = %q, want beta", ch)
		}
	})

	t.Run("unknown value rejected", func(t *testing.T) {
		writeSettings(t, "[settings]\nchannel = nightly\n")
		if _, err := resolveChannel(); err == nil {
			t.Fatal("expected error for unknown channel")
		}
	})
}
```

`cmd/update_test.go` 追加（打开该文件抄现有 `SetAPIBaseURLForTest` + cobra SetOut 捕获输出的用法形态；import 按需补齐）：

```go
func TestRunUpdateCheckBetaChannel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[settings]\nchannel = beta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/qfeius/makecli/releases" {
			t.Errorf("path = %s, want /repos/qfeius/makecli/releases", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"tag_name":"v9.9.9-beta.1","prerelease":true,"html_url":"https://example.com/r"}]`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	var buf bytes.Buffer
	cmd := newUpdateCmd()
	cmd.SetOut(&buf)

	if err := runUpdateCheck(cmd, "0.5.5"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "v9.9.9-beta.1") {
		t.Fatalf("output missing beta tag: %s", out)
	}
	if !strings.Contains(out, "[beta channel]") {
		t.Fatalf("output missing channel echo: %s", out)
	}
}

func TestRunUpdateCheckStableHintsWhenCurrentBetaHigher(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir()) // channel 未配置 → stable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.5","html_url":"https://example.com/r"}`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	var buf bytes.Buffer
	cmd := newUpdateCmd()
	cmd.SetOut(&buf)

	if err := runUpdateCheck(cmd, "0.6.0-beta.1"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "pre-release above the stable channel") {
		t.Fatalf("output missing beta-above-stable hint: %s", out)
	}
	if !strings.Contains(out, "--force") {
		t.Fatalf("hint must mention --force downgrade path: %s", out)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/`（禁用沙箱）
Expected: FAIL / build error: undefined resolveChannel

- [ ] **Step 3: 实现**

`cmd/client.go` 追加（与 resolveEnvironment 相邻；import 已有 slices/strings 则复用）：

```go
// resolveChannel 收口发布通道解析：[settings] channel > 默认 stable。
// 未知通道名报错（对齐 resolveEnvironment 的未知名报错先例；notifier 侧
// 的静默回退是另一职责，见 internal/notifier channelOf）。
func resolveChannel() (string, error) {
	settings, err := config.LoadSettings()
	if err != nil {
		return "", err
	}
	if settings.Channel == "" {
		return config.DefaultChannel, nil
	}
	if !slices.Contains(config.ChannelNames(), settings.Channel) {
		return "", fmt.Errorf("unknown channel '%s' in config, valid: %s",
			settings.Channel, strings.Join(config.ChannelNames(), ", "))
	}
	return settings.Channel, nil
}
```

`cmd/update.go` — `runUpdateCheck` 与 `runUpdateLatest` 头部改为解析通道并传给 CheckLatest（替换 Task 2 的机械 `false`）；"Already up to date" / "Update available" / "Checking for updates" 行追加 `channelSuffix(channel)`；`!newer` 分支在打印后调用 `hintBetaAboveStable`。完整改后的两个函数：

```go
func runUpdateCheck(cmd *cobra.Command, currentVersion string) error {
	channel, err := resolveChannel()
	if err != nil {
		return err
	}
	release, newer, err := update.CheckLatest(currentVersion, channel == config.ChannelBeta)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if !newer {
		_, _ = fmt.Fprintf(out, "Already up to date (%s)%s\n", release.TagName, channelSuffix(channel))
		hintBetaAboveStable(out, currentVersion, release.TagName, channel)
		return nil
	}

	_, _ = fmt.Fprintf(out, "Update available: %s → %s%s\n",
		formatCurrentVersion(currentVersion), release.TagName, channelSuffix(channel))
	_, _ = fmt.Fprintf(out, "  Release:   %s\n", release.HTMLURL)
	_, _ = fmt.Fprintf(out, "  Changelog: %s\n", changelogFileURL())
	_, _ = fmt.Fprintf(out, "\nRun `makecli update` to install.\n")
	return nil
}

func runUpdateLatest(cmd *cobra.Command, currentVersion string, skipSkills bool) error {
	channel, err := resolveChannel()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Checking for updates...%s\n", channelSuffix(channel))

	release, newer, err := update.CheckLatest(currentVersion, channel == config.ChannelBeta)
	if err != nil {
		return err
	}

	if !newer {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (%s)%s\n", release.TagName, channelSuffix(channel))
		hintBetaAboveStable(cmd.OutOrStdout(), currentVersion, release.TagName, channel)
		return runSkillSync(cmd, release.TagName, skipSkills)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updating makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), release.TagName)

	if err := applyFunc(release); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), release.TagName)
	return runSkillSync(cmd, release.TagName, skipSkills)
}
```

新增两个 helper（放在 formatCurrentVersion 附近；import 补 `github.com/qfeius/makecli/internal/config`）：

```go
// channelSuffix 仅在非默认通道时回显，稳定用户输出零变化
func channelSuffix(channel string) string {
	if channel == config.ChannelBeta {
		return " [beta channel]"
	}
	return ""
}

// hintBetaAboveStable 消解「Already up to date 却拿着 -beta.N」的字面矛盾：
// stable 通道下当前预发布版本高于最新稳定版时，补两行去向说明。
func hintBetaAboveStable(w io.Writer, current, latestStable, channel string) {
	if channel != config.ChannelStable || !update.IsPrerelease(current) {
		return
	}
	if update.CompareVersions(latestStable, current) >= 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "Note: current %s is a pre-release above the stable channel.\n", formatCurrentVersion(current))
	_, _ = fmt.Fprintf(w, "Run `makecli update %s --force` to return to stable, or wait for a newer stable release.\n", latestStable)
}
```

`newUpdateCmd` 的 Example 追加一行：

```go
	Example: `  makecli update
  makecli update --check
  makecli update v0.2.0
  makecli update --force v0.0.1
  makecli update --skip-skills
  makecli configure set channel beta   # make bare `update` track pre-releases`,
```

（注意 Example 是反引号原始串，内嵌反引号不可用——改用单引号包 update：`# make bare 'update' track pre-releases`。）

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0（既有 update 用例走 stable 路径行为不变；若有用例因 channelSuffix 输出断言失败，按新文案修正期望）

- [ ] **Step 5: Commit**

```bash
git add cmd/client.go cmd/client_test.go cmd/update.go cmd/update_test.go
git commit -m "feat(cmd): update command follows the configured release channel"
```

---

### Task 5: cmd/configure — channel 特殊键 set/get + sample 模板

**Files:**
- Modify: `cmd/configure.go`（channelKey / setChannel、set/get 路由分支、Long/Example 文案、sampleConfig [settings] 块）
- Modify: `cmd/configure_test.go`（set/get channel 用例；sampleConfig 完整性测试若断言 [settings] 键集则补 channel）

**Interfaces:**
- Consumes: `config.ChannelNames/DefaultChannel/SetSetting/LoadSettings`（Task 1）、既有 `firstNonEmpty`
- Produces: `configure set channel <stable|beta>` / `configure get channel` 用户命令

- [ ] **Step 1: 写失败测试**（追加到 `cmd/configure_test.go`，抄该文件现有 environment set/get 用例的隔离形态——temp config dir + captureStdout）

```go
func TestConfigureSetGetChannel(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())

	if err := runConfigureSet("channel", "beta"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runConfigureGet("channel"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.TrimSpace(out) != "beta" {
		t.Fatalf("get channel = %q, want beta", strings.TrimSpace(out))
	}
}

func TestConfigureGetChannelDefaultsToStable(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	out := captureStdout(t, func() {
		if err := runConfigureGet("channel"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.TrimSpace(out) != "stable" {
		t.Fatalf("get channel = %q, want stable (缺省回退)", strings.TrimSpace(out))
	}
}

func TestConfigureSetChannelRejectsUnknown(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	err := runConfigureSet("channel", "nightly")
	if err == nil || !strings.Contains(err.Error(), "stable, beta") {
		t.Fatalf("expected unknown-channel error listing valid names, got %v", err)
	}
}
```

（`captureStdout` 签名以 `cmd/stdout_test.go` 现有实现为准，按实际形态调整调用。）

- [ ] **Step 2: 跑测试确认失败**（set 走 validateConfigKey 报 unknown key）

Run: `go test ./cmd/ -run TestConfigureSetGetChannel`（禁用沙箱）
Expected: FAIL

- [ ] **Step 3: 实现** — `cmd/configure.go`：

environmentKey 常量旁追加：

```go
// channelKey 是 configure set/get 里路由到全局 [settings] 的发布通道特殊键名。
const channelKey = "channel"

// setChannel 校验通道名后写入全局 [settings] channel（不受 --profile 影响）。
func setChannel(value string) error {
	if !slices.Contains(config.ChannelNames(), value) {
		return fmt.Errorf("unknown channel '%s', valid: %s", value, strings.Join(config.ChannelNames(), ", "))
	}
	return config.SetSetting(channelKey, value)
}
```

`runConfigureSet` 的 environment 分支后追加：

```go
	if key == channelKey {
		return setChannel(value)
	}
```

`runConfigureGet` 的 environment 分支后追加：

```go
	if key == channelKey {
		settings, err := config.LoadSettings()
		if err != nil {
			return err
		}
		fmt.Println(firstNonEmpty(settings.Channel, config.DefaultChannel))
		return nil
	}
```

set 子命令 Long 第二段改为（get 子命令对称改写）：

```
The special keys "environment" and "channel" instead write to the global
[settings] section (shared by every profile). environment accepts: dev, test,
production. channel accepts: stable, beta.
```

set 的 Example 追加：

```
  # track pre-releases with bare `makecli update`
  makecli configure set channel beta
```

`sampleConfig` 的 `[settings]` 块 check-for-updates 行后追加：

```
# Release channel for updates and the update notifier. One of: stable, beta
channel = stable
```

若 `configure_test.go` 的 sampleConfig 完整性测试枚举 [settings] 键，将 `channel` 加入期望集合。

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add cmd/configure.go cmd/configure_test.go
git commit -m "feat(configure): channel as a [settings] special key (set/get/sample)"
```

---

### Task 6: cmd/version_list — TYPE 列标注 Pre-release

**Files:**
- Modify: `cmd/version_list.go`（表头 + 行 + releaseTypeLabel）
- Modify: `cmd/version_list_test.go`（表格断言适配 + prerelease 行用例）

**Interfaces:**
- Consumes: 既有 `update.Release.Prerelease`（已解析，无需数据层改动）
- Produces: 表格列序 CURRENT / VERSION / TYPE / PUBLISHED / URL；包内 `releaseTypeLabel(prerelease bool) string`

- [ ] **Step 1: 写失败测试**（追加到 `cmd/version_list_test.go`，抄该文件现有表格断言形态）

```go
func TestRunVersionListMarksPrerelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.6.0-beta.1","prerelease":true,"published_at":"2026-07-20","html_url":"https://example.com/b"},
			{"tag_name":"v0.5.5","prerelease":false,"published_at":"2026-07-18","html_url":"https://example.com/s"}
		]`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	out := captureStdout(t, func() {
		if err := runVersionList(20, "table"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "TYPE") {
		t.Fatalf("table missing TYPE header: %s", out)
	}
	if !strings.Contains(out, "Pre-release") {
		t.Fatalf("table missing Pre-release label: %s", out)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run TestRunVersionListMarksPrerelease`（禁用沙箱）
Expected: FAIL（无 TYPE 表头）

- [ ] **Step 3: 实现** — `cmd/version_list.go`：

`renderReleaseTable` 行构造与表头：

```go
		rows[i] = []string{marker, r.TagName, releaseTypeLabel(r.Prerelease), r.PublishedAt, r.HTMLURL}
```

```go
	table.Header("CURRENT", "VERSION", "TYPE", "PUBLISHED", "URL")
```

文件尾追加：

```go
// releaseTypeLabel 对齐 gh release list 的 TYPE 列：预发布标注，正式版留空
func releaseTypeLabel(prerelease bool) string {
	if prerelease {
		return "Pre-release"
	}
	return ""
}
```

既有 version_list 表格用例若断言旧表头/列数，按新列序修正期望。

- [ ] **Step 4: 跑测试确认通过**

Run: `make vet && make test`（禁用沙箱）
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add cmd/version_list.go cmd/version_list_test.go
git commit -m "feat(version): mark pre-releases with a TYPE column in version list"
```

---

### Task 7: 发布侧配置 + GEB 文档回环 + 终验

**Files:**
- Modify: `.goreleaser.yml`（release.prerelease + brews skip_upload）
- Modify: `CLAUDE.md`（L1：internal/config / internal/update / internal/notifier / cmd 条目补通道语义）
- Modify: `internal/config/CLAUDE.md`、`internal/update/CLAUDE.md`、`internal/notifier/CLAUDE.md`、`cmd/CLAUDE.md`（L2 成员清单同步）
- Modify: 触及文件的 L3 头部若有过时（settings.go / update.go / decision.go / notifier.go / cache.go）

**Interfaces:**
- Consumes: Task 1-6 的全部产出（纯配置与文档，无代码接口）
- Produces: beta tag 推送即自动标 prerelease、不推 Homebrew；文档与代码同构

- [ ] **Step 1: .goreleaser.yml** — checksum 块之后、brews 之前插入：

```yaml
# -------------------------------------------------------------------------
# Release：tag 带 semver 预发布段（v0.6.0-beta.1）自动标为 GitHub prerelease，
# /releases/latest 与稳定通道用户（含所有旧版本二进制）天然不受影响
# -------------------------------------------------------------------------
release:
  prerelease: auto
```

brews 条目内（`name: makecli` 之后）追加：

```yaml
    # 预发布版本不推 Homebrew formula：brew 用户永远只见稳定版
    skip_upload: auto
```

- [ ] **Step 2: GEB 回环** — 按各 task 实际落地的签名逐条更新：

- L1 `CLAUDE.md`：`internal/config/` 条目补「发布通道常量 stable/beta（channel.go），[settings] channel 选通道」；`internal/update/` 条目补「CheckLatest 双通道：stable 走 /releases/latest 服务端过滤、beta 走列表取 semver 最高」；`internal/notifier/` 条目补「按通道判定与提示，缓存带 channel 跨通道失效，beta.N 白名单拒 git-describe 伪版本」；`cmd/` 条目在 configure 描述中补 channel 特殊键
- 4 个 L2 成员清单：channel.go 新条目、settings.go / update.go / decision.go / notifier.go / cache.go / client.go / update.go(cmd) / configure.go / version_list.go 及对应 _test 条目按实际改动重写描述
- 检查上述文件的 L3 头部 INPUT/OUTPUT 是否过时，过时则修

- [ ] **Step 3: 终验**（golangci-lint 门禁与 CI 对齐）

Run: `make vet && make test && golangci-lint run ./...`（禁用沙箱）
Expected: 全绿 0 issues

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yml CLAUDE.md internal/config/CLAUDE.md internal/update/CLAUDE.md internal/notifier/CLAUDE.md cmd/CLAUDE.md
git commit -m "feat(release): auto-prerelease beta tags, skip Homebrew for pre-releases + GEB sync"
```

---

## 发布 Runbook（实现合并后，人工执行）

1. 先发一个稳定版（存量用户拿到带通道能力的二进制）：`/ship`
2. 首个 beta：`git tag v<next>-beta.1 && git push --tags`
3. 验证清单：GitHub release 带 Pre-release 徽标；homebrew-makecli 无新 commit；旧版本 `update --check` 仍报稳定版；`configure set channel beta` 后裸 `update` 装上 beta；`version list` 显示 TYPE=Pre-release
