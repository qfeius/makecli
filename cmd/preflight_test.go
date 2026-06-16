/**
 * [INPUT]: 依赖 cmd 包内 runPreflight / errPreflightFailed（白盒）、errors、os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 preflight 子命令工程骨架校验的单元测试
 * [POS]: cmd 模块 preflight.go 的配套测试，用 t.TempDir 构造真实目录树隔离文件系统
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"os"
	"path/filepath"
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

	// --type service: 只查 dsl + service，缺 ui 不影响通过
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

	// --type ui: 只查 dsl + ui，缺 service 不影响通过
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
		if !strings.Contains(err.Error(), "invalid --type") {
			t.Errorf("error should name the offending flag: %v", err)
		}
	})
}
