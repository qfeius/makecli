/**
 * [INPUT]: 依赖 cmd 包内 preflight 检查表与 runPreflight / errPreflightFailed（白盒）、encoding/json、errors、os、path/filepath、slices、strings、testing
 * [OUTPUT]: 覆盖 preflight 子命令 build-spec 检查清单的单元测试
 * [POS]: cmd 模块 preflight.go 的配套测试，用 t.TempDir 构造真实目录树隔离文件系统，
 *        覆盖文件投影原语、上下文构建、spec 第 5 节检查表（含第 7 节常见失败结构）与输出渲染
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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
