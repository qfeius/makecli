/**
 * [INPUT]: 依赖 cmd 包内 runPreflight / errPreflightFailed（白盒）、errors、os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 preflight 子命令工程骨架校验的单元测试
 * [POS]: cmd 模块 preflight.go 的配套测试，用 t.TempDir 构造真实目录树隔离文件系统
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

// mkValidLayout 在临时目录铺出完整合法骨架，返回工程根
func mkValidLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "apps", "dsl"), 0o755); err != nil {
		t.Fatalf("mkdir dsl: %v", err)
	}
	for _, sub := range []string{"service", "ui"} {
		dir := filepath.Join(root, "apps", sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s/package.json: %v", sub, err)
		}
	}
	return root
}

func TestRunPreflight(t *testing.T) {
	t.Run("passes on complete layout", func(t *testing.T) {
		root := mkValidLayout(t)
		out := captureStdout(t, func() {
			if err := runPreflight(root, "fullstack"); err != nil {
				t.Errorf("runPreflight: unexpected error %v", err)
			}
		})
		if !strings.Contains(out, "OK: project layout looks good") {
			t.Errorf("output missing OK line: %q", out)
		}
		if strings.Contains(out, "✗") {
			t.Errorf("clean layout should have no ✗ marks: %q", out)
		}
	})

	t.Run("fails when apps/dsl missing", func(t *testing.T) {
		root := mkValidLayout(t)
		if err := os.RemoveAll(filepath.Join(root, "apps", "dsl")); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if err := runPreflight(root, "fullstack"); !errors.Is(err, errPreflightFailed) {
				t.Errorf("expected errPreflightFailed, got %v", err)
			}
		})
		if !strings.Contains(out, "✗ apps/dsl") || !strings.Contains(out, "missing") {
			t.Errorf("output should flag missing dsl: %q", out)
		}
	})

	t.Run("fails when service package.json missing", func(t *testing.T) {
		root := mkValidLayout(t)
		if err := os.Remove(filepath.Join(root, "apps", "service", "package.json")); err != nil {
			t.Fatal(err)
		}
		if err := runPreflight(root, "fullstack"); !errors.Is(err, errPreflightFailed) {
			t.Errorf("expected errPreflightFailed, got %v", err)
		}
	})

	t.Run("fails when ui package.json missing", func(t *testing.T) {
		root := mkValidLayout(t)
		if err := os.Remove(filepath.Join(root, "apps", "ui", "package.json")); err != nil {
			t.Fatal(err)
		}
		if err := runPreflight(root, "fullstack"); !errors.Is(err, errPreflightFailed) {
			t.Errorf("expected errPreflightFailed, got %v", err)
		}
	})

	t.Run("fails when dsl is a file not a directory", func(t *testing.T) {
		root := mkValidLayout(t)
		dsl := filepath.Join(root, "apps", "dsl")
		if err := os.RemoveAll(dsl); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dsl, []byte("oops"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if err := runPreflight(root, "fullstack"); !errors.Is(err, errPreflightFailed) {
				t.Errorf("expected errPreflightFailed, got %v", err)
			}
		})
		if !strings.Contains(out, "expected directory") {
			t.Errorf("output should flag type mismatch: %q", out)
		}
	})

	t.Run("fails on empty directory", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := runPreflight(t.TempDir(), "fullstack"); !errors.Is(err, errPreflightFailed) {
				t.Errorf("expected errPreflightFailed, got %v", err)
			}
		})
		if !strings.Contains(out, "FAIL: 3/3 checks failed") {
			t.Errorf("output should report all checks failed: %q", out)
		}
	})

	// --app-type service: 只查 dsl + service，缺 ui 不影响通过
	t.Run("service type ignores missing ui", func(t *testing.T) {
		root := mkValidLayout(t)
		if err := os.RemoveAll(filepath.Join(root, "apps", "ui")); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if err := runPreflight(root, "service"); err != nil {
				t.Errorf("service preflight: unexpected error %v", err)
			}
		})
		if strings.Contains(out, "apps/ui") {
			t.Errorf("service type should not check apps/ui: %q", out)
		}
		if !strings.Contains(out, "Type:") || !strings.Contains(out, "service") {
			t.Errorf("output should echo the project type: %q", out)
		}
	})

	t.Run("service type still requires service package.json", func(t *testing.T) {
		root := mkValidLayout(t)
		if err := os.Remove(filepath.Join(root, "apps", "service", "package.json")); err != nil {
			t.Fatal(err)
		}
		if err := runPreflight(root, "service"); !errors.Is(err, errPreflightFailed) {
			t.Errorf("expected errPreflightFailed, got %v", err)
		}
	})

	// --app-type ui: 只查 dsl + ui，缺 service 不影响通过
	t.Run("ui type ignores missing service", func(t *testing.T) {
		root := mkValidLayout(t)
		if err := os.RemoveAll(filepath.Join(root, "apps", "service")); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if err := runPreflight(root, "ui"); err != nil {
				t.Errorf("ui preflight: unexpected error %v", err)
			}
		})
		if strings.Contains(out, "apps/service") {
			t.Errorf("ui type should not check apps/service: %q", out)
		}
	})

	t.Run("rejects unknown type", func(t *testing.T) {
		root := mkValidLayout(t)
		err := runPreflight(root, "bogus")
		if err == nil || errors.Is(err, errPreflightFailed) {
			t.Errorf("expected a plain invalid-type error, got %v", err)
		}
		if !strings.Contains(err.Error(), "invalid --app-type") {
			t.Errorf("error should name the offending flag: %v", err)
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
