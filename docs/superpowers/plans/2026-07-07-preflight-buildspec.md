# preflight 对齐 build spec 检查清单 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重写 `makecli preflight`，以 make-build-service `build_spec.md` 第 5 节检查清单为唯一实现依据（设计定稿：`docs/superpowers/specs/2026-07-07-preflight-buildspec-design.md`）。

**Architecture:** 一次性构建 `preflightContext`（模式 A/B 自动判定、lockfile→包管理器判定、各 package.json / pnpm-workspace.yaml 投影），然后表驱动求值 `[]preflightCheck`（与 spec 条目 1:1，含 ERROR/WARN/INFO 三级与每条 fix 指引），失败输出附 "How to fix" 块面向 AI agent 一步收敛。

**Tech Stack:** Go 1.25.8、cobra、encoding/json、gopkg.in/yaml.v3（均已有，零新依赖）。

## Global Constraints

- 检查 ID 与 build_spec.md 第 5 节完全一致（G1/G2/P1、A1–A9/A11/A15、B1–B3）+ makecli 自有 D1；BUILD-TIME 条目（A10/A12/A13/A14/B4）本版不做。
- G1 正则原文：`^[a-z0-9]+([._-][a-z0-9]+)*$`；lockfile 优先级原文：`pnpm-lock.yaml` > `yarn.lock` > `package-lock.json`，无 lockfile 时包管理器回退 `npm`。
- 用户可见输出一律英文；退出码：有 ERROR → `errPreflightFailed`（exit 1），仅 WARN/INFO → exit 0。
- **每个 Task 提交前必须** `make vet && make test` exit 0（Go 工具链命令在命令沙箱下因 module cache 不可写会假性失败，须禁用沙箱运行）。禁止同一批工具调用里 test + commit。
- Task 1–3 与旧实现共存于 `cmd/preflight.go`（新代码追加在文件尾部），Task 4 才删除旧代码与旧测试；每个 Task 结束时全包编译、测试全绿。
- 单测运行单个用例用 `go test ./cmd/ -run <Name> -v`（唯一手段），提交门禁一律走 `make vet && make test`。

---

### Task 1: 文件投影与判定原语

**Files:**
- Modify: `cmd/preflight.go`（文件尾部追加；不动旧代码）
- Test: `cmd/preflight_test.go`（文件尾部追加）

**Interfaces:**
- Consumes: 无（纯 stdlib + yaml.v3）
- Produces（Task 2/3 依赖）:
  - `type packageJSON struct { Name string; Scripts map[string]string; Workspaces workspacesField }`
  - `type workspacesField []string`（UnmarshalJSON 兼容数组与 `{"packages": []}` 两形态）
  - `type pkgFile struct { path string; exists bool; err error; pkg packageJSON }`
  - `func loadPackageJSON(root, rel string) *pkgFile`
  - `type pnpmWorkspaceFile struct { exists bool; err error; packages []string }`
  - `func loadPnpmWorkspace(root string) *pnpmWorkspaceFile`（固定读 `apps/pnpm-workspace.yaml`）
  - `func detectLockfiles(dir string) (files []string, pm string)`
  - `func workspaceCovers(patterns []string, component string) bool`
  - 测试侧 helper：`func pfWrite(t *testing.T, root, rel, content string)`

- [ ] **Step 1: 写失败测试**

在 `cmd/preflight_test.go` 尾部追加（import 需补 `"encoding/json"`、`"slices"`）：

```go
// ---------------------------------- build spec 检查：文件投影原语 ----------------------------------

// pfWrite 在 root 下写文件，自动建父目录
func pfWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacesField(t *testing.T) {
	cases := []struct {
		name, json string
		want       []string
	}{
		{"array form", `{"workspaces":["ui","service"]}`, []string{"ui", "service"}},
		{"object form", `{"workspaces":{"packages":["ui"]}}`, []string{"ui"}},
		{"absent", `{}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pkg packageJSON
			if err := json.Unmarshal([]byte(tc.json), &pkg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !slices.Equal([]string(pkg.Workspaces), tc.want) {
				t.Errorf("workspaces = %v, want %v", pkg.Workspaces, tc.want)
			}
		})
	}
}

func TestLoadPackageJSON(t *testing.T) {
	root := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		p := loadPackageJSON(root, "package.json")
		if p.exists || p.err != nil {
			t.Errorf("missing file should be exists=false err=nil, got exists=%v err=%v", p.exists, p.err)
		}
	})

	t.Run("valid file", func(t *testing.T) {
		pfWrite(t, root, "apps/ui/package.json", `{"name":"ui","scripts":{"build":"vite build"}}`)
		p := loadPackageJSON(root, "apps/ui/package.json")
		if !p.exists || p.err != nil {
			t.Fatalf("expected clean load, got exists=%v err=%v", p.exists, p.err)
		}
		if p.pkg.Name != "ui" || p.pkg.Scripts["build"] != "vite build" {
			t.Errorf("bad projection: %+v", p.pkg)
		}
	})

	t.Run("invalid JSON keeps exists=true with err", func(t *testing.T) {
		pfWrite(t, root, "bad/package.json", `{not json`)
		p := loadPackageJSON(root, "bad/package.json")
		if !p.exists || p.err == nil {
			t.Errorf("broken file should be exists=true err!=nil, got exists=%v err=%v", p.exists, p.err)
		}
	})
}

func TestLoadPnpmWorkspace(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		w := loadPnpmWorkspace(t.TempDir())
		if w.exists || w.err != nil {
			t.Errorf("missing should be exists=false err=nil, got %+v", w)
		}
	})
	t.Run("valid", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "apps/pnpm-workspace.yaml", "packages:\n  - ui\n  - service\n")
		w := loadPnpmWorkspace(root)
		if !w.exists || w.err != nil || !slices.Equal(w.packages, []string{"ui", "service"}) {
			t.Errorf("bad load: %+v", w)
		}
	})
	t.Run("invalid YAML", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "apps/pnpm-workspace.yaml", "packages: [\n")
		w := loadPnpmWorkspace(root)
		if !w.exists || w.err == nil {
			t.Errorf("broken yaml should be exists=true err!=nil, got %+v", w)
		}
	})
}

func TestDetectLockfiles(t *testing.T) {
	t.Run("priority pnpm over yarn over npm", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "package-lock.json", "{}")
		pfWrite(t, root, "yarn.lock", "")
		pfWrite(t, root, "pnpm-lock.yaml", "")
		files, pm := detectLockfiles(root)
		if pm != "pnpm" {
			t.Errorf("pm = %q, want pnpm", pm)
		}
		if !slices.Equal(files, []string{"pnpm-lock.yaml", "yarn.lock", "package-lock.json"}) {
			t.Errorf("files = %v", files)
		}
	})
	t.Run("yarn wins over npm", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "yarn.lock", "")
		pfWrite(t, root, "package-lock.json", "{}")
		_, pm := detectLockfiles(root)
		if pm != "yarn" {
			t.Errorf("pm = %q, want yarn", pm)
		}
	})
	t.Run("no lockfile falls back to npm", func(t *testing.T) {
		files, pm := detectLockfiles(t.TempDir())
		if pm != "npm" || len(files) != 0 {
			t.Errorf("got files=%v pm=%q, want none/npm", files, pm)
		}
	})
}

func TestWorkspaceCovers(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		comp     string
		want     bool
	}{
		{"exact", []string{"ui", "service"}, "ui", true},
		{"dot slash prefix", []string{"./ui"}, "ui", true},
		{"trailing slash", []string{"ui/"}, "ui", true},
		{"star glob", []string{"*"}, "service", true},
		{"miss", []string{"packages/*"}, "ui", false},
		{"exclusion ignored", []string{"!ui"}, "ui", false},
		{"empty", nil, "ui", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspaceCovers(tc.patterns, tc.comp); got != tc.want {
				t.Errorf("workspaceCovers(%v, %q) = %v, want %v", tc.patterns, tc.comp, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run 'TestWorkspacesField|TestLoadPackageJSON|TestLoadPnpmWorkspace|TestDetectLockfiles|TestWorkspaceCovers' -v`（禁用沙箱）
Expected: 编译失败 `undefined: packageJSON` 等

- [ ] **Step 3: 实现**

在 `cmd/preflight.go` 尾部追加，import 块补 `"encoding/json"`、`"path"`、`"strings"`、`"gopkg.in/yaml.v3"`：

```go
// ================================ build spec 检查 ================================
// 以 make-build-service build_spec.md 第 5 节检查清单为实现依据。

// ---------------------------------- 文件投影 ----------------------------------

// workspacesField 兼容 package.json workspaces 的两种形态：
// ["ui"] 数组、{"packages": ["ui"]} 对象。
type workspacesField []string

func (w *workspacesField) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*w = arr
		return nil
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*w = obj.Packages
	return nil
}

// packageJSON 是 package.json 面向检查的最小投影。
type packageJSON struct {
	Name       string            `json:"name"`
	Scripts    map[string]string `json:"scripts"`
	Workspaces workspacesField   `json:"workspaces"`
}

// pkgFile 是一次 package.json 读取的三态结果：不存在 / 存在但坏 / 存在且可用。
// 存在性与内容可用性分离——A1 只问存在，A2/A5/A6 才问内容，坏 JSON 归为内容检查的失败原因。
type pkgFile struct {
	path   string // 相对工程根，输出用
	exists bool
	err    error
	pkg    packageJSON
}

func loadPackageJSON(root, rel string) *pkgFile {
	p := &pkgFile{path: rel}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		p.exists = !errors.Is(err, os.ErrNotExist)
		if p.exists {
			p.err = err
		}
		return p
	}
	p.exists = true
	if err := json.Unmarshal(data, &p.pkg); err != nil {
		p.err = fmt.Errorf("invalid JSON: %v", err)
	}
	return p
}

// pnpmWorkspaceFile 是 apps/pnpm-workspace.yaml 的读取结果，三态同 pkgFile。
type pnpmWorkspaceFile struct {
	exists   bool
	err      error
	packages []string
}

func loadPnpmWorkspace(root string) *pnpmWorkspaceFile {
	w := &pnpmWorkspaceFile{}
	data, err := os.ReadFile(filepath.Join(root, "apps", "pnpm-workspace.yaml"))
	if err != nil {
		w.exists = !errors.Is(err, os.ErrNotExist)
		if w.exists {
			w.err = err
		}
		return w
	}
	w.exists = true
	var doc struct {
		Packages []string `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		w.err = fmt.Errorf("invalid YAML: %v", err)
		return w
	}
	w.packages = doc.Packages
	return w
}

// ---------------------------------- 判定原语 ----------------------------------

// lockfilePriority 按 spec 第 1 节优先级排列：命中即定包管理器，其余被忽略。
var lockfilePriority = []struct{ file, pm string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"yarn.lock", "yarn"},
	{"package-lock.json", "npm"},
}

// detectLockfiles 返回 dir 下按优先级排列的 lockfile 清单与判定出的包管理器。
// 无 lockfile 时包管理器回退 npm（spec 第 1 节：模式 A 走 npm install 兜底）。
func detectLockfiles(dir string) (files []string, pm string) {
	for _, lf := range lockfilePriority {
		if info, err := os.Stat(filepath.Join(dir, lf.file)); err == nil && !info.IsDir() {
			files = append(files, lf.file)
			if pm == "" {
				pm = lf.pm
			}
		}
	}
	if pm == "" {
		pm = "npm"
	}
	return files, pm
}

// workspaceCovers 判断 workspace 声明（pnpm packages / yarn+npm workspaces）是否覆盖
// 组件目录名。条目按 glob 语义匹配（path.Match），归一化 "./ui"、"ui/" 等写法；
// "!" 排除条目跳过——只判覆盖不判排除，排除误伤交构建期暴露。
func workspaceCovers(patterns []string, component string) bool {
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			continue
		}
		p = strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
		if ok, err := path.Match(p, component); err == nil && ok {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./cmd/ -run 'TestWorkspacesField|TestLoadPackageJSON|TestLoadPnpmWorkspace|TestDetectLockfiles|TestWorkspaceCovers' -v`（禁用沙箱）
Expected: PASS（旧 TestRunPreflight 不在 -run 范围，不受影响）

- [ ] **Step 5: 门禁 + 提交**

Run: `make vet && make test`（禁用沙箱），确认 exit 0。之后单独提交：

```bash
git add cmd/preflight.go cmd/preflight_test.go
git commit -m "feat(preflight): add build-spec file projections and package-manager detection"
```

---

### Task 2: preflightContext 构建

**Files:**
- Modify: `cmd/preflight.go`（尾部追加）
- Test: `cmd/preflight_test.go`（尾部追加）

**Interfaces:**
- Consumes: Task 1 全部；`loadAppManifestFromFile(path string) (ResourceManifest, error)`（cmd/app.go）、`appDSLPath`（cmd/app_create.go，值 `apps/dsl/app.yaml`）
- Produces（Task 3/4 依赖）:
  - `type preflightContext struct { root, repoName, repoNameSource string; modeA bool; lockfiles []string; pm, lockDir string; hasDSL bool; appsPkg, uiPkg, servicePkg, rootPkg *pkgFile; uiDirExists, serviceDirExists bool; pnpmWS *pnpmWorkspaceFile; hasDockerfile bool }`
  - `func buildPreflightContext(root string) *preflightContext`
  - `func resolveRepoName(root string) (name, source string)`
  - `func dirExists(p string) bool`

- [ ] **Step 1: 写失败测试**

`cmd/preflight_test.go` 尾部追加：

```go
func TestBuildPreflightContext(t *testing.T) {
	t.Run("mode A when any component package.json exists", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "apps/service/package.json", `{"name":"service"}`)
		ctx := buildPreflightContext(root)
		if !ctx.modeA {
			t.Error("expected mode A")
		}
		if ctx.lockDir != "apps" {
			t.Errorf("lockDir = %q, want apps", ctx.lockDir)
		}
	})

	t.Run("component dir without package.json stays mode B", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "apps", "ui"), 0o755); err != nil {
			t.Fatal(err)
		}
		ctx := buildPreflightContext(root)
		if ctx.modeA {
			t.Error("expected mode B: apps/ui/ dir alone must not trigger mode A")
		}
		if !ctx.uiDirExists {
			t.Error("uiDirExists should be true")
		}
		if ctx.lockDir != "." {
			t.Errorf("lockDir = %q, want .", ctx.lockDir)
		}
	})

	t.Run("mode A reads lockfiles from apps/ not root", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "apps/ui/package.json", `{"name":"ui"}`)
		pfWrite(t, root, "apps/yarn.lock", "")
		pfWrite(t, root, "pnpm-lock.yaml", "") // 根目录的不算
		ctx := buildPreflightContext(root)
		if ctx.pm != "yarn" || !slices.Equal(ctx.lockfiles, []string{"yarn.lock"}) {
			t.Errorf("pm=%q lockfiles=%v, want yarn from apps/ only", ctx.pm, ctx.lockfiles)
		}
	})

	t.Run("root Dockerfile and dsl dir detected", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "Dockerfile", "FROM scratch\n")
		if err := os.MkdirAll(filepath.Join(root, "apps", "dsl"), 0o755); err != nil {
			t.Fatal(err)
		}
		ctx := buildPreflightContext(root)
		if !ctx.hasDockerfile || !ctx.hasDSL {
			t.Errorf("hasDockerfile=%v hasDSL=%v, want both true", ctx.hasDockerfile, ctx.hasDSL)
		}
	})
}

func TestResolveRepoName(t *testing.T) {
	t.Run("app.yaml key wins and is lowered", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "apps/dsl/app.yaml", "key: MyShop\nname: shop\ntype: Make.App\n")
		name, source := resolveRepoName(root)
		if name != "myshop" {
			t.Errorf("name = %q, want myshop", name)
		}
		if source != appDSLPath+" key" {
			t.Errorf("source = %q", source)
		}
	})

	t.Run("falls back to directory basename", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "My-App")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		name, source := resolveRepoName(root)
		if name != "my-app" || source != "directory name" {
			t.Errorf("got %q from %q", name, source)
		}
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run 'TestBuildPreflightContext|TestResolveRepoName' -v`（禁用沙箱）
Expected: 编译失败 `undefined: buildPreflightContext`

- [ ] **Step 3: 实现**

`cmd/preflight.go` 尾部追加：

```go
// ---------------------------------- 上下文 ----------------------------------

// preflightContext 一次性收集全部检查所需事实，检查函数只读不做 IO。
type preflightContext struct {
	root           string
	repoName       string // 已 lower，G1 校验对象
	repoNameSource string // repoName 从哪来（输出用）

	modeA     bool
	lockfiles []string // 检测目录下的 lockfile，按 spec 优先级排列
	pm        string   // pnpm | yarn | npm（无 lockfile 回退 npm）
	lockDir   string   // lockfile 检测目录的相对名："apps" 或 "."（输出用）

	hasDSL           bool // apps/dsl/ 目录存在
	appsPkg          *pkgFile
	uiPkg            *pkgFile
	servicePkg       *pkgFile
	uiDirExists      bool
	serviceDirExists bool
	pnpmWS           *pnpmWorkspaceFile

	rootPkg       *pkgFile
	hasDockerfile bool // 根目录 Dockerfile 存在（文件）
}

func buildPreflightContext(root string) *preflightContext {
	ctx := &preflightContext{
		root:       root,
		appsPkg:    loadPackageJSON(root, "apps/package.json"),
		uiPkg:      loadPackageJSON(root, "apps/ui/package.json"),
		servicePkg: loadPackageJSON(root, "apps/service/package.json"),
		rootPkg:    loadPackageJSON(root, "package.json"),
		pnpmWS:     loadPnpmWorkspace(root),
	}
	// spec 第 2 节：任一组件 package.json 存在即模式 A，无回退
	ctx.modeA = ctx.uiPkg.exists || ctx.servicePkg.exists
	ctx.hasDSL = dirExists(filepath.Join(root, "apps", "dsl"))
	ctx.uiDirExists = dirExists(filepath.Join(root, "apps", "ui"))
	ctx.serviceDirExists = dirExists(filepath.Join(root, "apps", "service"))
	if info, err := os.Stat(filepath.Join(root, "Dockerfile")); err == nil && !info.IsDir() {
		ctx.hasDockerfile = true
	}

	// spec 第 1 节：lockfile 检测目录模式 A 为 apps/，模式 B 为仓库根
	ctx.lockDir = "."
	lockPath := root
	if ctx.modeA {
		ctx.lockDir = "apps"
		lockPath = filepath.Join(root, "apps")
	}
	ctx.lockfiles, ctx.pm = detectLockfiles(lockPath)

	ctx.repoName, ctx.repoNameSource = resolveRepoName(root)
	return ctx
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// resolveRepoName 取镜像仓库名候选：apps/dsl/app.yaml 的 app key 优先（deploy 建仓即按
// key 派生远端仓库名），缺失或不可读回退目录 basename；统一 lower 后交 G1 校验。
func resolveRepoName(root string) (name, source string) {
	if manifest, err := loadAppManifestFromFile(filepath.Join(root, appDSLPath)); err == nil && manifest.Key != "" {
		return strings.ToLower(manifest.Key), appDSLPath + " key"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return strings.ToLower(filepath.Base(abs)), "directory name"
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./cmd/ -run 'TestBuildPreflightContext|TestResolveRepoName' -v`（禁用沙箱）
Expected: PASS

- [ ] **Step 5: 门禁 + 提交**

Run: `make vet && make test`（禁用沙箱），exit 0 后单独提交：

```bash
git add cmd/preflight.go cmd/preflight_test.go
git commit -m "feat(preflight): build preflight context with mode/PM/repo-name resolution"
```

---

### Task 3: 检查表（spec 第 5 节全量确定性条目）

**Files:**
- Modify: `cmd/preflight.go`（尾部追加）
- Test: `cmd/preflight_test.go`（尾部追加）

**Interfaces:**
- Consumes: Task 1/2 全部
- Produces（Task 4 依赖）:
  - `type checkLevel int`；常量 `levelError` / `levelWarn` / `levelInfo`
  - `type checkResult struct { ok bool; msg string }`；`func passed() checkResult`、`func failf(format string, a ...any) checkResult`
  - `type preflightCheck struct { id string; level checkLevel; label string; applies func(*preflightContext) bool; run func(*preflightContext) checkResult; fix func(*preflightContext) string }`
  - `var preflightChecks []preflightCheck`（求值顺序 = 输出顺序：D1、A1–A15、B1–B3、G1、P1、G2）

- [ ] **Step 1: 写失败测试**

`cmd/preflight_test.go` 尾部追加：

```go
// ---------------------------------- 检查表 ----------------------------------

// evalChecks 对 root 跑全部检查，按等级收集未通过条目的 id（表级断言用）
func evalChecks(t *testing.T, root string) (errIDs, warnIDs, infoIDs []string) {
	t.Helper()
	ctx := buildPreflightContext(root)
	for _, c := range preflightChecks {
		if !c.applies(ctx) {
			continue
		}
		if res := c.run(ctx); !res.ok {
			switch c.level {
			case levelError:
				errIDs = append(errIDs, c.id)
			case levelWarn:
				warnIDs = append(warnIDs, c.id)
			default:
				infoIDs = append(infoIDs, c.id)
			}
		}
	}
	return errIDs, warnIDs, infoIDs
}

func countID(ids []string, id string) int {
	n := 0
	for _, x := range ids {
		if x == id {
			n++
		}
	}
	return n
}

// pfModeAPnpm 铺出一个全绿的模式 A pnpm 工程（各失败场景在其上做减法/篡改）
func pfModeAPnpm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pfWrite(t, root, "apps/dsl/app.yaml", "key: myapp\nname: myapp\ntype: Make.App\n")
	pfWrite(t, root, "apps/package.json", `{"name":"apps"}`)
	pfWrite(t, root, "apps/pnpm-lock.yaml", "lockfileVersion: 9\n")
	pfWrite(t, root, "apps/pnpm-workspace.yaml", "packages:\n  - ui\n  - service\n")
	pfWrite(t, root, "apps/ui/package.json", `{"name":"ui","scripts":{"build":"vite build"}}`)
	pfWrite(t, root, "apps/service/package.json", `{"name":"service","scripts":{"build":"tsc -b"}}`)
	return root
}

// pfModeAYarn 铺出一个 yarn 模式 A 工程（除 A15 TEMP 差距外全绿）
func pfModeAYarn(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pfWrite(t, root, "apps/dsl/app.yaml", "key: myapp\nname: myapp\ntype: Make.App\n")
	pfWrite(t, root, "apps/package.json", `{"name":"apps","workspaces":["ui","service"]}`)
	pfWrite(t, root, "apps/yarn.lock", "")
	pfWrite(t, root, "apps/ui/package.json", `{"name":"ui","scripts":{"build":"vite build"}}`)
	pfWrite(t, root, "apps/service/package.json", `{"name":"service","scripts":{"build":"tsc -b"}}`)
	return root
}

// pfModeB 铺出一个全绿的模式 B 前端工程
func pfModeB(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pfWrite(t, root, "Dockerfile", "FROM scratch\n")
	pfWrite(t, root, "package.json", `{"name":"site","scripts":{"build":"vite build"}}`)
	pfWrite(t, root, "package-lock.json", "{}")
	return root
}

func TestPreflightChecksModeA(t *testing.T) {
	t.Run("clean pnpm fullstack is all green", func(t *testing.T) {
		errIDs, warnIDs, _ := evalChecks(t, pfModeAPnpm(t))
		if len(errIDs) != 0 || len(warnIDs) != 0 {
			t.Errorf("clean project: errors=%v warnings=%v", errIDs, warnIDs)
		}
	})

	t.Run("D1 fires without apps/dsl", func(t *testing.T) {
		root := pfModeAPnpm(t)
		if err := os.RemoveAll(filepath.Join(root, "apps", "dsl")); err != nil {
			t.Fatal(err)
		}
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "D1") {
			t.Errorf("want D1, got %v", errIDs)
		}
	})

	t.Run("A1 fires without apps/package.json", func(t *testing.T) {
		root := pfModeAPnpm(t)
		if err := os.Remove(filepath.Join(root, "apps", "package.json")); err != nil {
			t.Fatal(err)
		}
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "A1") {
			t.Errorf("want A1, got %v", errIDs)
		}
	})

	// spec §7: apps 组件 package.json 缺 scripts.build → A2（ui、service 分别报告）
	t.Run("A2 fires per component on missing or blank build script", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/ui/package.json", `{"name":"ui","scripts":{"build":"  "}}`)
		pfWrite(t, root, "apps/service/package.json", `{"name":"service"}`)
		errIDs, _, _ := evalChecks(t, root)
		if countID(errIDs, "A2") != 2 {
			t.Errorf("want A2 twice (ui+service), got %v", errIDs)
		}
	})

	// spec §7: service 组件存在但 apps/ 无 lockfile → A3（并连带 A15：PM 回退 npm）
	t.Run("A3 and A15 fire when service exists without lockfile", func(t *testing.T) {
		root := pfModeAPnpm(t)
		if err := os.Remove(filepath.Join(root, "apps", "pnpm-lock.yaml")); err != nil {
			t.Fatal(err)
		}
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "A3") || !slices.Contains(errIDs, "A15") {
			t.Errorf("want A3+A15, got %v", errIDs)
		}
	})

	// spec §7: 组件未声明进 workspace → A4（pnpm）
	t.Run("A4 fires when pnpm-workspace misses a component", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/pnpm-workspace.yaml", "packages:\n  - ui\n")
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "A4") {
			t.Errorf("want A4, got %v", errIDs)
		}
	})

	t.Run("A4 fires when pnpm-workspace.yaml is absent", func(t *testing.T) {
		root := pfModeAPnpm(t)
		if err := os.Remove(filepath.Join(root, "apps", "pnpm-workspace.yaml")); err != nil {
			t.Fatal(err)
		}
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "A4") {
			t.Errorf("want A4, got %v", errIDs)
		}
	})

	// spec §7: 组件未声明进 workspace → A5（yarn/npm）
	t.Run("A5 fires when yarn workspaces misses a component", func(t *testing.T) {
		root := pfModeAYarn(t)
		pfWrite(t, root, "apps/package.json", `{"name":"apps","workspaces":["ui"]}`)
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "A5") {
			t.Errorf("want A5, got %v", errIDs)
		}
	})

	// spec §7: 组件 name 与目录名不一致 → A6 / A7
	t.Run("A6 and A7 fire on component name mismatch", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/ui/package.json", `{"name":"frontend","scripts":{"build":"vite build"}}`)
		pfWrite(t, root, "apps/service/package.json", `{"name":"backend","scripts":{"build":"tsc -b"}}`)
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "A6") || !slices.Contains(errIDs, "A7") {
			t.Errorf("want A6+A7, got %v", errIDs)
		}
	})

	t.Run("A8 warns on component dir without package.json in mode A", func(t *testing.T) {
		root := pfModeAPnpm(t)
		if err := os.Remove(filepath.Join(root, "apps", "ui", "package.json")); err != nil {
			t.Fatal(err)
		}
		_, warnIDs, _ := evalChecks(t, root)
		if !slices.Contains(warnIDs, "A8") {
			t.Errorf("want A8, got %v", warnIDs)
		}
	})

	// spec §9 A9: 仅 ui 且无 lockfile → npm install 兜底警告（不是 ERROR）
	t.Run("A9 warns for ui-only without lockfile", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "apps/dsl/app.yaml", "key: myapp\nname: myapp\ntype: Make.App\n")
		pfWrite(t, root, "apps/package.json", `{"name":"apps","workspaces":["ui"]}`)
		pfWrite(t, root, "apps/ui/package.json", `{"name":"ui","scripts":{"build":"vite build"}}`)
		errIDs, warnIDs, _ := evalChecks(t, root)
		if len(errIDs) != 0 {
			t.Errorf("ui-only without lockfile must not error, got %v", errIDs)
		}
		if !slices.Contains(warnIDs, "A9") {
			t.Errorf("want A9, got %v", warnIDs)
		}
	})

	t.Run("A11 informs about ignored root files in mode A", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "Dockerfile", "FROM scratch\n")
		pfWrite(t, root, "package.json", `{"name":"root"}`)
		_, _, infoIDs := evalChecks(t, root)
		if !slices.Contains(infoIDs, "A11") {
			t.Errorf("want A11, got %v", infoIDs)
		}
	})

	// spec §7: service 组件用 yarn/npm（当前版本）→ A15，且是唯一 ERROR
	t.Run("A15 is the only error for a clean yarn fullstack", func(t *testing.T) {
		errIDs, _, _ := evalChecks(t, pfModeAYarn(t))
		if len(errIDs) != 1 || errIDs[0] != "A15" {
			t.Errorf("want exactly [A15], got %v", errIDs)
		}
	})

	t.Run("A15 not applicable for ui-only yarn", func(t *testing.T) {
		root := pfModeAYarn(t)
		if err := os.RemoveAll(filepath.Join(root, "apps", "service")); err != nil {
			t.Fatal(err)
		}
		pfWrite(t, root, "apps/package.json", `{"name":"apps","workspaces":["ui"]}`)
		errIDs, _, _ := evalChecks(t, root)
		if len(errIDs) != 0 {
			t.Errorf("ui-only yarn should be green, got %v", errIDs)
		}
	})
}

func TestPreflightChecksModeB(t *testing.T) {
	t.Run("clean mode B is all green", func(t *testing.T) {
		errIDs, warnIDs, _ := evalChecks(t, pfModeB(t))
		if len(errIDs) != 0 || len(warnIDs) != 0 {
			t.Errorf("clean mode B: errors=%v warnings=%v", errIDs, warnIDs)
		}
	})

	// spec §7: 根前端 build 成功但无根 Dockerfile → B1
	t.Run("B1 fires without root Dockerfile", func(t *testing.T) {
		root := pfModeB(t)
		if err := os.Remove(filepath.Join(root, "Dockerfile")); err != nil {
			t.Fatal(err)
		}
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "B1") {
			t.Errorf("want B1, got %v", errIDs)
		}
	})

	// spec §7: 根 package.json 存在但无任何 lockfile → B2（npm ci 必败，无 npm install 兜底）
	t.Run("B2 fires without lockfile next to package.json", func(t *testing.T) {
		root := pfModeB(t)
		if err := os.Remove(filepath.Join(root, "package-lock.json")); err != nil {
			t.Fatal(err)
		}
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "B2") {
			t.Errorf("want B2, got %v", errIDs)
		}
	})

	t.Run("B3 fires on missing build script", func(t *testing.T) {
		root := pfModeB(t)
		pfWrite(t, root, "package.json", `{"name":"site"}`)
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "B3") {
			t.Errorf("want B3, got %v", errIDs)
		}
	})

	t.Run("pure Dockerfile repo (Go/Java) is green", func(t *testing.T) {
		root := t.TempDir()
		pfWrite(t, root, "Dockerfile", "FROM scratch\n")
		errIDs, warnIDs, _ := evalChecks(t, root)
		if len(errIDs) != 0 || len(warnIDs) != 0 {
			t.Errorf("Dockerfile-only repo: errors=%v warnings=%v", errIDs, warnIDs)
		}
	})

	// spec §7 首行: apps/ui/ 目录存在但无 package.json、根目录也无 Dockerfile → A8 + B1 同报
	t.Run("A8 and B1 fire together on orphan component dir falling back to mode B", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "apps", "ui"), 0o755); err != nil {
			t.Fatal(err)
		}
		errIDs, warnIDs, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "B1") || !slices.Contains(warnIDs, "A8") {
			t.Errorf("spec §7 row 1: want B1 error + A8 warning, got errors=%v warnings=%v", errIDs, warnIDs)
		}
	})
}

func TestPreflightChecksGeneric(t *testing.T) {
	t.Run("G1 fires on invalid app key from app.yaml", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/dsl/app.yaml", "key: my__app\nname: x\ntype: Make.App\n")
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "G1") {
			t.Errorf("want G1, got %v", errIDs)
		}
	})

	t.Run("G1 fires on invalid directory name fallback", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "My__App")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		pfWrite(t, root, "Dockerfile", "FROM scratch\n")
		errIDs, _, _ := evalChecks(t, root)
		if !slices.Contains(errIDs, "G1") {
			t.Errorf("want G1, got %v", errIDs)
		}
	})

	// spec P1: 多 lockfile 并存 → WARN，且不改变优先级判定
	t.Run("P1 warns on multiple lockfiles and pnpm still wins", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/yarn.lock", "")
		errIDs, warnIDs, _ := evalChecks(t, root)
		if len(errIDs) != 0 {
			t.Errorf("multiple lockfiles is warn-only, got errors %v", errIDs)
		}
		if !slices.Contains(warnIDs, "P1") {
			t.Errorf("want P1, got %v", warnIDs)
		}
	})

	t.Run("G2 always informs", func(t *testing.T) {
		_, _, infoIDs := evalChecks(t, pfModeB(t))
		if !slices.Contains(infoIDs, "G2") {
			t.Errorf("want G2, got %v", infoIDs)
		}
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run 'TestPreflightChecks' -v`（禁用沙箱）
Expected: 编译失败 `undefined: preflightChecks`

- [ ] **Step 3: 实现检查表**

`cmd/preflight.go` 尾部追加，import 块补 `"regexp"`：

```go
// ---------------------------------- 等级与结果 ----------------------------------

type checkLevel int

const (
	levelError checkLevel = iota
	levelWarn
	levelInfo
)

// checkResult 是单项检查的裁决：ok=true 通过；否则 msg 给出「实际 vs 期望」。
type checkResult struct {
	ok  bool
	msg string
}

func passed() checkResult { return checkResult{ok: true} }

func failf(format string, a ...any) checkResult {
	return checkResult{msg: fmt.Sprintf(format, a...)}
}

// ---------------------------------- 检查清单 ----------------------------------

// repoNameRE 是镜像仓库名合法性正则（spec G1，push 前置条件）。
var repoNameRE = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// preflightCheck 与 build spec 第 5 节条目 1:1 对应：applies 是「条件」列，run 是
// 「判定」列，fix 是失败时的修复动作（为什么 + 改哪个文件 + 改成什么），供
// How to fix 块引导人或 AI agent 一步收敛。fix 为 nil 的条目（INFO）不进该块。
type preflightCheck struct {
	id      string
	level   checkLevel
	label   string // ERROR 级通过时打印的短标签
	applies func(*preflightContext) bool
	run     func(*preflightContext) checkResult
	fix     func(*preflightContext) string
}

func always(*preflightContext) bool { return true }

// componentBuildScript 校验 package.json 的 scripts.build 非空（A2 / B3 同构复用）。
func componentBuildScript(p *pkgFile) checkResult {
	if p.err != nil {
		return failf("%s: %v", p.path, p.err)
	}
	if strings.TrimSpace(p.pkg.Scripts["build"]) == "" {
		return failf("%s: scripts.build is missing or empty", p.path)
	}
	return passed()
}

// componentName 校验组件包名与目录名一致（A6 / A7 同构复用）。
func componentName(p *pkgFile, want string) checkResult {
	if p.err != nil {
		return failf("%s: %v", p.path, p.err)
	}
	if p.pkg.Name != want {
		return failf("%s: name is %q, must be %q", p.path, p.pkg.Name, want)
	}
	return passed()
}

// uncoveredComponents 汇总未被 workspace 声明覆盖的既存组件名（A4 / A5 共用）。
func uncoveredComponents(ctx *preflightContext, patterns []string) []string {
	var missing []string
	if ctx.uiPkg.exists && !workspaceCovers(patterns, "ui") {
		missing = append(missing, "ui")
	}
	if ctx.servicePkg.exists && !workspaceCovers(patterns, "service") {
		missing = append(missing, "service")
	}
	return missing
}

// preflightChecks 的顺序即输出顺序：模式相关条目在前（最可能出错），通用尾随，INFO 收尾。
var preflightChecks = []preflightCheck{
	{
		id: "D1", level: levelError, label: "apps/dsl/ (Make app DSL)",
		applies: func(ctx *preflightContext) bool { return ctx.modeA },
		run: func(ctx *preflightContext) checkResult {
			if !ctx.hasDSL {
				return failf("apps/dsl/ not found — Make app identity (app.yaml) lives there")
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return "run `makecli app init` in the project root to scaffold apps/dsl/app.yaml; `makecli app deploy` reads the app key from it"
		},
	},
	{
		id: "A1", level: levelError, label: "apps/package.json (workspace root)",
		applies: func(ctx *preflightContext) bool { return ctx.modeA },
		run: func(ctx *preflightContext) checkResult {
			if !ctx.appsPkg.exists {
				return failf("apps/package.json not found")
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return "create apps/package.json as the workspace root — dependency install runs there once for all components"
		},
	},
	{
		id: "A2", level: levelError, label: "apps/ui/package.json scripts.build",
		applies: func(ctx *preflightContext) bool { return ctx.uiPkg.exists },
		run:     func(ctx *preflightContext) checkResult { return componentBuildScript(ctx.uiPkg) },
		fix: func(_ *preflightContext) string {
			return `add a non-empty "build" script to apps/ui/package.json; its build must emit apps/ui/dist/index.html`
		},
	},
	{
		id: "A2", level: levelError, label: "apps/service/package.json scripts.build",
		applies: func(ctx *preflightContext) bool { return ctx.servicePkg.exists },
		run:     func(ctx *preflightContext) checkResult { return componentBuildScript(ctx.servicePkg) },
		fix: func(_ *preflightContext) string {
			return `add a non-empty "build" script to apps/service/package.json; its build must emit apps/service/dist/server.js`
		},
	},
	{
		id: "A3", level: levelError, label: "lockfile in apps/ (service frozen install)",
		applies: func(ctx *preflightContext) bool { return ctx.modeA && ctx.servicePkg.exists },
		run: func(ctx *preflightContext) checkResult {
			if len(ctx.lockfiles) == 0 {
				return failf("no lockfile in apps/ (need one of pnpm-lock.yaml / yarn.lock / package-lock.json)")
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return "the service image reinstalls production deps with a frozen lockfile: run your package manager's install inside apps/ (e.g. `cd apps && pnpm install`) and commit the lockfile"
		},
	},
	{
		id: "A4", level: levelError, label: "apps/pnpm-workspace.yaml covers components",
		applies: func(ctx *preflightContext) bool { return ctx.modeA && ctx.pm == "pnpm" },
		run: func(ctx *preflightContext) checkResult {
			ws := ctx.pnpmWS
			if !ws.exists {
				return failf("apps/pnpm-workspace.yaml not found (required with pnpm)")
			}
			if ws.err != nil {
				return failf("apps/pnpm-workspace.yaml: %v", ws.err)
			}
			if missing := uncoveredComponents(ctx, ws.packages); len(missing) > 0 {
				return failf("apps/pnpm-workspace.yaml packages %v does not cover: %s", ws.packages, strings.Join(missing, ", "))
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return "declare every component in apps/pnpm-workspace.yaml (packages: [ui, service]) — dependency install does not reach undeclared components"
		},
	},
	{
		id: "A5", level: levelError, label: `apps/package.json "workspaces" covers components`,
		applies: func(ctx *preflightContext) bool {
			return ctx.modeA && ctx.pm != "pnpm" && ctx.appsPkg.exists
		},
		run: func(ctx *preflightContext) checkResult {
			if ctx.appsPkg.err != nil {
				return failf("%s: %v", ctx.appsPkg.path, ctx.appsPkg.err)
			}
			if missing := uncoveredComponents(ctx, ctx.appsPkg.pkg.Workspaces); len(missing) > 0 {
				return failf("apps/package.json workspaces %v does not cover: %s", ctx.appsPkg.pkg.Workspaces, strings.Join(missing, ", "))
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return `add "workspaces": ["ui", "service"] (the components that exist) to apps/package.json — yarn/npm install only reaches declared workspace members`
		},
	},
	{
		id: "A6", level: levelError, label: `apps/ui/package.json name == "ui"`,
		applies: func(ctx *preflightContext) bool { return ctx.uiPkg.exists },
		run:     func(ctx *preflightContext) checkResult { return componentName(ctx.uiPkg, "ui") },
		fix: func(_ *preflightContext) string {
			return `set "name": "ui" in apps/ui/package.json — the build system locates components by package name (--filter/--workspace), not by path`
		},
	},
	{
		id: "A7", level: levelError, label: `apps/service/package.json name == "service"`,
		applies: func(ctx *preflightContext) bool { return ctx.servicePkg.exists },
		run:     func(ctx *preflightContext) checkResult { return componentName(ctx.servicePkg, "service") },
		fix: func(_ *preflightContext) string {
			return `set "name": "service" in apps/service/package.json — the build system locates components by package name (--filter/--workspace), not by path`
		},
	},
	// A8 不限模式：spec 第 7 节要求「组件目录存在但无 package.json、回退模式 B」同报 A8+B1
	{
		id: "A8", level: levelWarn,
		applies: func(ctx *preflightContext) bool { return ctx.uiDirExists && !ctx.uiPkg.exists },
		run: func(_ *preflightContext) checkResult {
			return failf("apps/ui/ exists but has no package.json — the component is silently skipped, no ui image will be built")
		},
		fix: func(_ *preflightContext) string {
			return `add apps/ui/package.json (with "name": "ui" and a "build" script) or delete the apps/ui/ directory`
		},
	},
	{
		id: "A8", level: levelWarn,
		applies: func(ctx *preflightContext) bool { return ctx.serviceDirExists && !ctx.servicePkg.exists },
		run: func(_ *preflightContext) checkResult {
			return failf("apps/service/ exists but has no package.json — the component is silently skipped, no service image will be built")
		},
		fix: func(_ *preflightContext) string {
			return `add apps/service/package.json (with "name": "service" and a "build" script) or delete the apps/service/ directory`
		},
	},
	{
		id: "A9", level: levelWarn,
		applies: func(ctx *preflightContext) bool {
			return ctx.modeA && ctx.uiPkg.exists && !ctx.servicePkg.exists && len(ctx.lockfiles) == 0
		},
		run: func(_ *preflightContext) checkResult {
			return failf("no lockfile in apps/: build falls back to plain `npm install`, results are not reproducible")
		},
		fix: func(_ *preflightContext) string {
			return "run your package manager's install inside apps/ and commit the resulting lockfile"
		},
	},
	{
		id: "A11", level: levelInfo,
		applies: func(ctx *preflightContext) bool {
			return ctx.modeA && (ctx.rootPkg.exists || ctx.hasDockerfile)
		},
		run: func(ctx *preflightContext) checkResult {
			var ignored []string
			if ctx.rootPkg.exists {
				ignored = append(ignored, "package.json")
			}
			if ctx.hasDockerfile {
				ignored = append(ignored, "Dockerfile")
			}
			return failf("root %s ignored in mode A (apps components take over)", strings.Join(ignored, " and "))
		},
	},
	// A15 TEMP：build-job.sh(1b83199) 的 service 镜像模板仅支持 pnpm；差距关闭后整行删除
	{
		id: "A15", level: levelError, label: "service component uses pnpm (temporary gap)",
		applies: func(ctx *preflightContext) bool { return ctx.modeA && ctx.servicePkg.exists && ctx.pm != "pnpm" },
		run: func(ctx *preflightContext) checkResult {
			return failf("service images currently support pnpm only, detected %s (temporary build service gap)", ctx.pm)
		},
		fix: func(_ *preflightContext) string {
			return "switch apps/ to pnpm: delete other lockfiles, run `cd apps && pnpm import && pnpm install` (converts the existing lockfile) and commit pnpm-lock.yaml + pnpm-workspace.yaml"
		},
	},
	{
		id: "B1", level: levelError, label: "Dockerfile at repo root",
		applies: func(ctx *preflightContext) bool { return !ctx.modeA },
		run: func(ctx *preflightContext) checkResult {
			if !ctx.hasDockerfile {
				return failf("Dockerfile not found at repo root")
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return "add a Dockerfile at the repo root (build context is the repo root); or, if you meant the apps component mode, add apps/ui/package.json or apps/service/package.json"
		},
	},
	{
		id: "B2", level: levelError, label: "lockfile next to package.json",
		applies: func(ctx *preflightContext) bool { return !ctx.modeA && ctx.rootPkg.exists },
		run: func(ctx *preflightContext) checkResult {
			if len(ctx.lockfiles) == 0 {
				return failf("package.json exists but no lockfile (need one of pnpm-lock.yaml / yarn.lock / package-lock.json)")
			}
			return passed()
		},
		fix: func(_ *preflightContext) string {
			return "root-Dockerfile mode has no `npm install` fallback — without a lockfile the build runs `npm ci` and always fails; run your package manager's install and commit the lockfile"
		},
	},
	{
		id: "B3", level: levelError, label: "package.json scripts.build",
		applies: func(ctx *preflightContext) bool { return !ctx.modeA && ctx.rootPkg.exists },
		run:     func(ctx *preflightContext) checkResult { return componentBuildScript(ctx.rootPkg) },
		fix: func(_ *preflightContext) string {
			return `add a non-empty "build" script to package.json — with a root package.json the build service always runs install + build before the docker build`
		},
	},
	{
		id: "G1", level: levelError, label: "image repository name",
		applies: always,
		run: func(ctx *preflightContext) checkResult {
			if !repoNameRE.MatchString(ctx.repoName) {
				return failf("repo name %q (from %s) is not a valid image repository name (need %s)", ctx.repoName, ctx.repoNameSource, repoNameRE)
			}
			return passed()
		},
		fix: func(ctx *preflightContext) string {
			return fmt.Sprintf("rename so that %s lower-cases to letters/digits with single ./_/- separators — image push uses it as the repository name", ctx.repoNameSource)
		},
	},
	{
		id: "P1", level: levelWarn,
		applies: func(ctx *preflightContext) bool { return len(ctx.lockfiles) > 1 },
		run: func(ctx *preflightContext) checkResult {
			return failf("multiple lockfiles in %s: %s — %s wins (pnpm > yarn > npm), the rest are ignored",
				ctx.lockDir, strings.Join(ctx.lockfiles, ", "), ctx.lockfiles[0])
		},
		fix: func(ctx *preflightContext) string {
			return fmt.Sprintf("keep exactly one lockfile in %s: delete %s", ctx.lockDir, strings.Join(ctx.lockfiles[1:], ", "))
		},
	},
	{
		id: "G2", level: levelInfo, applies: always,
		run: func(_ *preflightContext) checkResult {
			return failf("build job time limit is 30 minutes by default")
		},
	},
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./cmd/ -run 'TestPreflightChecks' -v`（禁用沙箱）
Expected: PASS 全部子用例

- [ ] **Step 5: 门禁 + 提交**

Run: `make vet && make test`（禁用沙箱），exit 0 后单独提交：

```bash
git add cmd/preflight.go cmd/preflight_test.go
git commit -m "feat(preflight): implement build_spec section-5 deterministic check table"
```

---

### Task 4: 渲染与命令面（替换旧实现）

**Files:**
- Modify: `cmd/preflight.go`（删除旧骨架代码，重写 `newPreflightCmd` / `runPreflight`，更新文件头）
- Test: `cmd/preflight_test.go`（删除旧 `TestRunPreflight` + `mkValidLayout`，新增输出/退出码测试，更新文件头）

**Interfaces:**
- Consumes: Task 1–3 全部；既有 `errPreflightFailed` 哨兵（保留不动）；测试侧 `captureStdout`（stdout_test.go）
- Produces: `func runPreflight(root string) error`（签名从 `(root, projectType string)` 变为 `(root string)`）；`newPreflightCmd()` 不再有 `--app-type` flag

- [ ] **Step 1: 删除旧测试、写新输出测试**

在 `cmd/preflight_test.go`：删除 `mkValidLayout` 函数与整个旧 `TestRunPreflight`，在原位置写入：

```go
func TestRunPreflight(t *testing.T) {
	t.Run("clean mode A prints header, marks and OK", func(t *testing.T) {
		root := pfModeAPnpm(t)
		out := captureStdout(t, func() {
			if err := runPreflight(root); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
		for _, want := range []string{"Mode:", "A (apps components)", "Package manager:", "pnpm", "✓ D1", "✓ A1", "i G2", "OK: ready for the make build service"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "How to fix") {
			t.Errorf("clean run must not print a fix section:\n%s", out)
		}
	})

	t.Run("errors fail with sentinel and How to fix", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/ui/package.json", `{"name":"frontend","scripts":{"build":"vite build"}}`)
		out := captureStdout(t, func() {
			if err := runPreflight(root); !errors.Is(err, errPreflightFailed) {
				t.Errorf("expected errPreflightFailed, got %v", err)
			}
		})
		for _, want := range []string{"✗ A6", `name is "frontend"`, "How to fix:", `set "name": "ui"`, "FAIL: 1 error"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("warnings alone exit clean with fix hints", func(t *testing.T) {
		root := pfModeAPnpm(t)
		pfWrite(t, root, "apps/yarn.lock", "")
		out := captureStdout(t, func() {
			if err := runPreflight(root); err != nil {
				t.Errorf("warnings must not fail: %v", err)
			}
		})
		for _, want := range []string{"! P1", "How to fix:", "OK with 1 warning"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("mode B header", func(t *testing.T) {
		root := pfModeB(t)
		out := captureStdout(t, func() {
			if err := runPreflight(root); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
		if !strings.Contains(out, "B (root Dockerfile)") {
			t.Errorf("output missing mode B header:\n%s", out)
		}
	})

	t.Run("rejects non-directory root", func(t *testing.T) {
		err := runPreflight(filepath.Join(t.TempDir(), "nope"))
		if err == nil || errors.Is(err, errPreflightFailed) {
			t.Errorf("want plain error, got %v", err)
		}
	})

	t.Run("--app-type flag removed", func(t *testing.T) {
		if newPreflightCmd().Flags().Lookup("app-type") != nil {
			t.Error("--app-type must be removed")
		}
	})
}
```

同步把测试文件头部注释改为：

```go
/**
 * [INPUT]: 依赖 cmd 包内 preflight 检查表与 runPreflight / errPreflightFailed（白盒）、encoding/json、errors、os、path/filepath、slices、strings、testing
 * [OUTPUT]: 覆盖 preflight 子命令 build-spec 检查清单的单元测试
 * [POS]: cmd 模块 preflight.go 的配套测试，用 t.TempDir 构造真实目录树隔离文件系统，
 *        覆盖文件投影原语、上下文构建、spec 第 5 节检查表（含第 7 节常见失败结构）与输出渲染
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/ -run 'TestRunPreflight' -v`（禁用沙箱）
Expected: 编译失败（新测试用单参 `runPreflight(root)`，旧实现是双参）

- [ ] **Step 3: 替换实现**

在 `cmd/preflight.go`：

1. 删除旧「必需骨架」段落：`layoutEntry`、`layoutDSL`、`layoutService`、`layoutUI`、`layoutByType`、`checkLayoutEntry`，以及旧 `newPreflightCmd` / `runPreflight` 全部函数体（保留 `errPreflightFailed` 哨兵段落）。
2. 文件头部注释替换为：

```go
/**
 * [INPUT]: 依赖 encoding/json、errors、fmt、os、path、path/filepath、regexp、strings、
 *          gopkg.in/yaml.v3、github.com/spf13/cobra、cmd/app（loadAppManifestFromFile）、cmd/app_create（appDSLPath）
 * [OUTPUT]: 对外提供 newPreflightCmd 函数、errPreflightFailed 哨兵错误
 * [POS]: cmd 模块的顶层 preflight 命令，以 make-build-service build_spec.md 第 5 节检查
 *        清单为实现依据（设计定稿 docs/superpowers/specs/2026-07-07-preflight-buildspec-design.md）：
 *        构建模式 A/B 自动判定、包管理器按 lockfile 优先级判定（buildPreflightContext 一次
 *        收集事实），preflightChecks 表驱动逐项检查（ERROR/WARN/INFO 三级、条目与 spec 1:1、
 *        另有 makecli 自有 D1=apps/dsl），失败输出附 How to fix 指引（面向 AI agent 一步收敛）；
 *        存在 ERROR 返回 errPreflightFailed（main.go 转译退出码 1），作 CI / deploy 前置门禁
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
```

3. 写入新的命令定义与渲染（放在哨兵错误段落之后、`build spec 检查` 段落之前）：

```go
// ---------------------------------- 命令定义 ----------------------------------

func newPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight [dir]",
		Short: "Check the directory satisfies the make build service contract",
		Long: `Preflight validates the project against the make build service build spec
before pushing, so builds fail here instead of remotely.

The build mode is auto-detected:

  mode A  apps components — apps/ui/package.json or apps/service/package.json
          exists; the platform provides the Dockerfiles for the components
  mode B  root Dockerfile — everything else; the repo brings its own Dockerfile

The package manager follows the lockfile (pnpm-lock.yaml > yarn.lock >
package-lock.json). Findings are reported as ERROR / WARN / INFO with a
"How to fix" hint each; any ERROR fails the run (exit code 1) so it can gate
CI or deploy. The directory defaults to the current working directory.`,
		Example: `  makecli preflight
  makecli preflight ./my-app`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return runPreflight(root)
		},
	}
	return cmd
}

// finding 是一条未通过的检查，携带渲染 How to fix 所需的一切。
type finding struct {
	id, fix string
}

// runPreflight 构建上下文后按表求值：ERROR 通过打 ✓、失败打 ✗，WARN/INFO 仅在
// 命中时打 !/i（通过无话可说）；失败条目汇入 How to fix 块；存在 ERROR 返回
// errPreflightFailed（退出码 1），仅 WARN/INFO 不拦截。
func runPreflight(root string) error {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %s", root)
	}
	ctx := buildPreflightContext(root)

	display := root
	if abs, err := filepath.Abs(root); err == nil {
		display = abs
	}
	mode := "B (root Dockerfile)"
	if ctx.modeA {
		mode = "A (apps components)"
	}
	pm := ctx.pm
	if len(ctx.lockfiles) == 0 {
		pm += " (no lockfile)"
	}
	fmt.Printf("%-17s %s\n", "Project:", display)
	fmt.Printf("%-17s %s\n", "Mode:", mode)
	fmt.Printf("%-17s %s\n\n", "Package manager:", pm)

	var errs, warns int
	var findings []finding
	for _, c := range preflightChecks {
		if !c.applies(ctx) {
			continue
		}
		res := c.run(ctx)
		switch {
		case res.ok:
			if c.level == levelError { // WARN/INFO 通过无话可说，不打印
				fmt.Printf("✓ %-3s %s\n", c.id, c.label)
			}
		case c.level == levelInfo:
			fmt.Printf("i %-3s [INFO]  %s\n", c.id, res.msg)
		case c.level == levelWarn:
			fmt.Printf("! %-3s [WARN]  %s\n", c.id, res.msg)
			warns++
			findings = append(findings, finding{id: c.id, fix: c.fix(ctx)})
		default:
			fmt.Printf("✗ %-3s [ERROR] %s\n", c.id, res.msg)
			errs++
			findings = append(findings, finding{id: c.id, fix: c.fix(ctx)})
		}
	}

	if len(findings) > 0 {
		fmt.Printf("\nHow to fix:\n")
		for _, f := range findings {
			fmt.Printf("  %-3s %s\n", f.id, f.fix)
		}
	}

	switch {
	case errs > 0:
		fmt.Printf("\nFAIL: %s, %s — the build service would reject or fail this build\n", plural(errs, "error"), plural(warns, "warning"))
		return errPreflightFailed
	case warns > 0:
		fmt.Printf("\nOK with %s — the build should succeed, see warnings above\n", plural(warns, "warning"))
	default:
		fmt.Printf("\nOK: ready for the make build service\n")
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./cmd/ -v`（禁用沙箱）
Expected: 全包 PASS（含 Task 1–3 的所有测试）

- [ ] **Step 5: 门禁 + 提交**

Run: `make vet && make test && golangci-lint run ./...`（禁用沙箱），全部 exit 0 / 0 issues 后单独提交：

```bash
git add cmd/preflight.go cmd/preflight_test.go
git commit -m "feat(preflight): replace --app-type skeleton check with build-spec checklist"
```

---

### Task 5: 文档同步与终验

**Files:**
- Modify: `cmd/CLAUDE.md`（preflight.go / preflight_test.go 两条成员清单）

**Interfaces:**
- Consumes: Task 4 完成后的最终形态
- Produces: 无代码产出

- [ ] **Step 1: 更新 cmd/CLAUDE.md 成员清单**

把 `preflight.go:` 条目替换为：

```
preflight.go:        preflight 顶级子命令，以 make-build-service build_spec.md 第 5 节检查清单为实现依据（可选位置参数 [dir]，默认 cwd；--app-type 已移除）：构建模式 A/B 自动判定（apps/ui|service/package.json 任一存在→A，否则 B）、包管理器按 lockfile 优先级判定（pnpm>yarn>npm，检测目录 A=apps/ B=根；无 lockfile 回退 npm）；buildPreflightContext 一次收集事实（各 package.json/pnpm-workspace.yaml 三态投影 pkgFile、repoName 取 app.yaml key 回退目录名），preflightChecks 表驱动求值（条目与 spec 1:1：G1/G2/P1、A1-A9/A11/A15(TEMP)、B1-B3 + makecli 自有 D1=apps/dsl；BUILD-TIME 启发式 A10/A12/A13/A14/B4 本版不做），A8 刻意不限模式（spec §7 要求孤儿组件目录回退 B 时同报 A8+B1）；ERROR/WARN/INFO 三级输出（✓/✗/!/i + 检查 ID），失败条目附 How to fix 块（每条 fix 指引：为什么+改哪+改成什么，面向 AI agent 一步收敛）；存在 ERROR 返回 errPreflightFailed（退出码 1），仅 WARN/INFO 放行，作 CI/deploy 前置门禁
```

把 `preflight_test.go:` 条目替换为：

```
preflight_test.go:   覆盖 preflight 的单元测试（文件投影 workspacesField 双形态/pkgFile 三态/pnpm-workspace 解析、lockfile 优先级与 npm 回退、workspaceCovers glob 各形态、上下文构建含孤儿组件目录不触发模式 A、repoName 两来源、检查表全条目含 spec §7 常见失败结构 8 行与 A8+B1 同报、渲染与退出码含 How to fix 块/仅警告放行/--app-type 已移除），evalChecks 白盒跑表收集分级 id + pfModeAPnpm/pfModeAYarn/pfModeB 全绿夹具做减法 + t.TempDir 隔离文件系统
```

- [ ] **Step 2: 终验**

Run: `make vet && make test && golangci-lint run ./...`（禁用沙箱）
Expected: 全部 exit 0 / 0 issues

- [ ] **Step 3: 手工冒烟**

Run: `make build && ./bin/makecli preflight --help && ./bin/makecli preflight .`（禁用沙箱；makecli 仓库自身无 apps/ 组件、无 Dockerfile）
Expected: help 无 --app-type；`preflight .` 走模式 B、B1 报 ERROR、How to fix 给出 Dockerfile 指引、退出码 1

- [ ] **Step 4: 提交**

```bash
git add cmd/CLAUDE.md
git commit -m "docs(cmd): sync CLAUDE.md for preflight build-spec rewrite"
```

---

## Self-Review 记录

- **Spec 覆盖**：设计文档全部章节均有对应任务——定位/模式/PM 判定（Task 2）、确定性条目 G1/G2/P1/A1-A9/A11/A15/B1-B3/D1（Task 3）、A8 不限模式（Task 3 测试显式断言）、G1 双来源（Task 2/3）、fix 指引与 How to fix 块（Task 3 表 + Task 4 渲染）、输出英文/退出码（Task 4）、--app-type 移除（Task 4）、CLAUDE.md（Task 5）。spec §7 速查表 9 行中 8 行有测试；第 7 行（产物目录非 dist → A12/A13）属 BUILD-TIME，本版明确不做。
- **类型一致性**：`passed()/failf()` 命名避开 govet printf 检查陷阱（failf 后缀 f）；`fileExists` 已被 app_create.go 占用，故目录判定命名 `dirExists`；Task 3/4 引用的 `preflightContext` 字段与 Task 2 定义逐一核对一致。
- **无占位符**：所有步骤含完整代码与命令。
