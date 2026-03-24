# Relation Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `makecli relation` command group with create/update/delete/list subcommands for managing Relation resources.

**Architecture:** Mirror the existing entity command pattern exactly. Add Relation types + 5 CRUD methods to `internal/api/client.go`, create 5 new cmd files (group + 4 subcommands) with tests, register in `root.go`.

**Tech Stack:** Go + cobra + tablewriter + httptest

**Spec:** `docs/superpowers/specs/2026-03-24-relation-commands-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/api/client.go` | Add Relation/RelationEnd/RelationProperties types + CreateRelation/UpdateRelation/GetRelation/ListRelations/DeleteRelation methods |
| Create | `cmd/relation.go` | Command group with `--app` PersistentFlag |
| Create | `cmd/relation_create.go` | `relation create` subcommand |
| Create | `cmd/relation_create_test.go` | Tests for relation create |
| Create | `cmd/relation_update.go` | `relation update` subcommand |
| Create | `cmd/relation_update_test.go` | Tests for relation update |
| Create | `cmd/relation_delete.go` | `relation delete` subcommand |
| Create | `cmd/relation_delete_test.go` | Tests for relation delete |
| Create | `cmd/relation_list.go` | `relation list` subcommand (list + detail) |
| Create | `cmd/relation_list_test.go` | Tests for relation list |
| Modify | `cmd/root.go` | Add `rootCmd.AddCommand(newRelationCmd())` |

---

### Task 1: API Layer — Relation Types + CRUD Methods

**Files:**
- Modify: `internal/api/client.go`

- [ ] **Step 1: Add Relation types after Entity types block**

Append after the `EntityProperties` struct (after line 145) in `client.go`:

```go
// ---------------------------------- Relation 操作 ----------------------------------

// RelationEnd 描述关系的一端（实体 + 基数）
type RelationEnd struct {
	Entity      string `json:"entity"`
	Cardinality string `json:"cardinality"` // "one" | "many"
}

// RelationProperties 封装 Relation 的 from/to 两端
type RelationProperties struct {
	From RelationEnd `json:"from"`
	To   RelationEnd `json:"to"`
}

// Relation 代表 Meta Server 返回的单个 Relation 资源
type Relation struct {
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	App        string             `json:"app"`
	Meta       map[string]any     `json:"meta"`
	Properties RelationProperties `json:"properties"`
}
```

- [ ] **Step 2: Add CreateRelation method**

```go
// CreateRelation 调用 MakeService.CreateResource 在指定 App 下创建 Relation
func (c *Client) CreateRelation(name, app string, props RelationProperties) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Relation",
		"app":  app,
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"from": props.From,
			"to":   props.To,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/relation", body)
}
```

- [ ] **Step 3: Add UpdateRelation method**

```go
// UpdateRelation 调用 MakeService.UpdateResource 更新指定 Relation
func (c *Client) UpdateRelation(name, app string, props RelationProperties) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Relation",
		"app":  app,
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"from": props.From,
			"to":   props.To,
		},
	}
	return c.post("MakeService.UpdateResource", "/meta/v1/relation", body)
}
```

- [ ] **Step 4: Add ListRelations method**

```go
// ListRelations 调用 MakeService.ListResources 获取指定 App 下全部 Relation
// 返回 Relation 列表和服务端 total 数量
func (c *Client) ListRelations(app string, page, size int) ([]Relation, int, error) {
	reqBody := map[string]any{
		"app":        app,
		"sort":       []map[string]any{{"field": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
	}
	var result struct {
		Code    int        `json:"code"`
		Message string     `json:"msg"`
		Data    []Relation `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/meta/v1/relation", reqBody, &result); err != nil {
		return nil, 0, err
	}
	if result.Code != 200 {
		return nil, 0, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, result.Pagination.Total, nil
}
```

- [ ] **Step 5: Add GetRelation method**

```go
// GetRelation 调用 MakeService.GetResource 获取指定 Relation 的详细信息
func (c *Client) GetRelation(app, name string) (*Relation, error) {
	reqBody := map[string]any{"app": app, "name": name}
	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"msg"`
		Data    Relation `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/meta/v1/relation", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}
```

- [ ] **Step 6: Add DeleteRelation method**

```go
// DeleteRelation 调用 MakeService.DeleteResource 删除指定 Relation
func (c *Client) DeleteRelation(name, app string) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Relation",
		"app":  app,
	}
	return c.post("MakeService.DeleteResource", "/meta/v1/relation", body)
}
```

- [ ] **Step 7: Update L3 header of client.go**

Update the `[OUTPUT]` line to include Relation types and methods:

```
* [OUTPUT]: 对外提供 Client 类型、Option / WithDebug / WithHeaders 功能选项、New 构造函数、App / Field / Entity / EntityProperties / RelationEnd / RelationProperties / Relation 类型、CreateApp / CreateAppWithCode / ListApps / DeleteApp / GetApp / CreateEntity / ListEntities / GetEntity / UpdateEntity / DeleteEntity / CreateRelation / UpdateRelation / ListRelations / GetRelation / DeleteRelation 方法
```

- [ ] **Step 8: Run existing tests to verify no regression**

Run: `cd /Volumes/Coding/make/repos/makecli && go test ./internal/api/...`
Expected: All existing tests PASS

- [ ] **Step 9: Commit**

```bash
git add internal/api/client.go
git commit -m "feat(api): add Relation types and CRUD methods"
```

---

### Task 2: Command Group — `relation.go`

**Files:**
- Create: `cmd/relation.go`

- [ ] **Step 1: Create `cmd/relation.go`**

```go
/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationCmd 函数
 * [POS]: cmd 模块的 relation 命令组，挂载 create / update / delete / list 子命令，--app 参数为子命令继承
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newRelationCmd() *cobra.Command {
	var app string

	cmd := &cobra.Command{
		Use:   "relation",
		Short: "Manage relations",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if app == "" {
				return cmd.Usage()
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&app, "app", "", "app name (required)")
	_ = cmd.MarkPersistentFlagRequired("app")

	cmd.AddCommand(newRelationCreateCmd())
	cmd.AddCommand(newRelationUpdateCmd())
	cmd.AddCommand(newRelationDeleteCmd())
	cmd.AddCommand(newRelationListCmd())
	return cmd
}
```

- [ ] **Step 2: Register in `root.go`**

In `cmd/root.go`, add after the `newEntityCmd()` line (line 59):

```go
rootCmd.AddCommand(newRelationCmd())
```

- [ ] **Step 3: Update L3 header of `root.go`**

Update `[POS]` line to include `relation`:

```
* [POS]: cmd 模块的入口，挂载 version / configure / app / entity / relation / apply / diff / update 子命令
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Volumes/Coding/make/repos/makecli && go build ./...`
Expected: Build succeeds (note: this will fail until all 4 subcommand files exist — create stubs if needed, or complete Tasks 3–6 first, then build)

**Note:** Tasks 3, 4, 5, 6 can be done in parallel. After all four are done, run the build verification.

- [ ] **Step 5: Commit**

```bash
git add cmd/relation.go cmd/root.go
git commit -m "feat(cmd): add relation command group and register in root"
```

---

### Task 3: `relation create` Subcommand

**Files:**
- Create: `cmd/relation_create.go`
- Create: `cmd/relation_create_test.go`

- [ ] **Step 1: Create `cmd/relation_create.go`**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（RelationProperties/RelationEnd）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationCreateCmd 函数
 * [POS]: cmd/relation 的 create 子命令，从 JSON 文件加载 from/to 配置，调用 Meta Server API 创建 Relation
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

func newRelationCreateCmd() *cobra.Command {
	var profile string
	var jsonFile string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new relation on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			return runRelationCreate(args[0], app, jsonFile, profile)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing relation properties (required)")
	_ = cmd.MarkFlagRequired("json")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRelationCreate(name, app, jsonFile, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	props, err := loadRelationProperties(jsonFile)
	if err != nil {
		return err
	}

	if err := client.CreateRelation(name, app, props); err != nil {
		return err
	}

	fmt.Printf("Relation '%s' created successfully in app '%s'\n", name, app)
	return nil
}

// loadRelationProperties 读取 JSON 文件并解析为 RelationProperties
func loadRelationProperties(path string) (api.RelationProperties, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return api.RelationProperties{}, fmt.Errorf("读取 JSON 文件失败: %w", err)
	}
	var props api.RelationProperties
	if err := json.Unmarshal(data, &props); err != nil {
		return api.RelationProperties{}, fmt.Errorf("JSON 文件格式错误（需包含 from/to 对象）: %w", err)
	}
	return props, nil
}
```

- [ ] **Step 2: Create `cmd/relation_create_test.go`**

```go
/**
 * [INPUT]: 依赖 cmd 包内的 runRelationCreate / loadRelationProperties（包内白盒），internal/config、encoding/json、os、testing
 * [OUTPUT]: 覆盖 relation create 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 relation_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRelationCreate(t *testing.T) {
	t.Run("creates relation successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create relation success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "many"},
			"to":   map[string]any{"entity": "任务", "cardinality": "one"},
		})

		if err := runRelationCreate("project-has-tasks", "TODO", jsonFile, "default"); err != nil {
			t.Fatalf("runRelationCreate: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "many"},
			"to":   map[string]any{"entity": "任务", "cardinality": "one"},
		})

		if err := runRelationCreate("project-has-tasks", "TODO", jsonFile, "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid relation")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "many"},
			"to":   map[string]any{"entity": "任务", "cardinality": "one"},
		})

		if err := runRelationCreate("project-has-tasks", "TODO", jsonFile, "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "many"},
			"to":   map[string]any{"entity": "任务", "cardinality": "one"},
		})

		if err := runRelationCreate("project-has-tasks", "TODO", jsonFile, "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runRelationCreate("project-has-tasks", "TODO", bad, "default"); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("fails with nonexistent JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		if err := runRelationCreate("project-has-tasks", "TODO", "/nonexistent.json", "default"); err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

// writeRelationJSON 将 relation properties 写入临时 JSON 文件，返回路径
func writeRelationJSON(t *testing.T, props map[string]any) string {
	t.Helper()
	data, _ := json.Marshal(props)
	path := filepath.Join(t.TempDir(), "relation.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Volumes/Coding/make/repos/makecli && go test ./cmd/ -run TestRunRelationCreate -v`
Expected: All 6 tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/relation_create.go cmd/relation_create_test.go
git commit -m "feat(cmd): add relation create subcommand with tests"
```

---

### Task 4: `relation update` Subcommand

**Files:**
- Create: `cmd/relation_update.go`
- Create: `cmd/relation_update_test.go`

- [ ] **Step 1: Create `cmd/relation_update.go`**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/relation_create（loadRelationProperties）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationUpdateCmd 函数
 * [POS]: cmd/relation 的 update 子命令，从 JSON 文件加载 from/to 配置，调用 Meta Server API 更新 Relation
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRelationUpdateCmd() *cobra.Command {
	var profile string
	var jsonFile string

	cmd := &cobra.Command{
		Use:          "update <name>",
		Short:        "Update an existing relation on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			return runRelationUpdate(args[0], app, jsonFile, profile)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing relation properties (required)")
	_ = cmd.MarkFlagRequired("json")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRelationUpdate(name, app, jsonFile, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	props, err := loadRelationProperties(jsonFile)
	if err != nil {
		return err
	}

	if err := client.UpdateRelation(name, app, props); err != nil {
		return err
	}

	fmt.Printf("Relation '%s' updated successfully in app '%s'\n", name, app)
	return nil
}
```

- [ ] **Step 2: Create `cmd/relation_update_test.go`**

```go
/**
 * [INPUT]: 依赖 cmd 包内的 runRelationUpdate（包内白盒），internal/config、os、testing
 * [OUTPUT]: 覆盖 relation update 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 relation_update.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRelationUpdate(t *testing.T) {
	t.Run("updates relation successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "update relation success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "one"},
			"to":   map[string]any{"entity": "任务", "cardinality": "many"},
		})

		if err := runRelationUpdate("project-has-tasks", "TODO", jsonFile, "default"); err != nil {
			t.Fatalf("runRelationUpdate: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "one"},
			"to":   map[string]any{"entity": "任务", "cardinality": "many"},
		})

		if err := runRelationUpdate("project-has-tasks", "TODO", jsonFile, "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "relation not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "one"},
			"to":   map[string]any{"entity": "任务", "cardinality": "many"},
		})

		if err := runRelationUpdate("project-has-tasks", "TODO", jsonFile, "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entity": "项目", "cardinality": "one"},
			"to":   map[string]any{"entity": "任务", "cardinality": "many"},
		})

		if err := runRelationUpdate("project-has-tasks", "TODO", jsonFile, "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runRelationUpdate("project-has-tasks", "TODO", bad, "default"); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Volumes/Coding/make/repos/makecli && go test ./cmd/ -run TestRunRelationUpdate -v`
Expected: All 5 tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/relation_update.go cmd/relation_update_test.go
git commit -m "feat(cmd): add relation update subcommand with tests"
```

---

### Task 5: `relation delete` Subcommand

**Files:**
- Create: `cmd/relation_delete.go`
- Create: `cmd/relation_delete_test.go`

- [ ] **Step 1: Create `cmd/relation_delete.go`**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationDeleteCmd 函数
 * [POS]: cmd/relation 的 delete 子命令，调用 Meta Server API 删除指定 Relation
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRelationDeleteCmd() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:          "delete <name>",
		Short:        "Delete a relation on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			return runRelationDelete(args[0], app, profile)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRelationDelete(name, app, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	if err := client.DeleteRelation(name, app); err != nil {
		return err
	}

	fmt.Printf("Relation '%s' deleted successfully from app '%s'\n", name, app)
	return nil
}
```

- [ ] **Step 2: Create `cmd/relation_delete_test.go`**

```go
/**
 * [INPUT]: 依赖 cmd 包内的 runRelationDelete（包内白盒），internal/config、testing
 * [OUTPUT]: 覆盖 relation delete 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 relation_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"testing"
)

func TestRunRelationDelete(t *testing.T) {
	t.Run("deletes relation successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "delete relation success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationDelete("project-has-tasks", "TODO", "default"); err != nil {
			t.Fatalf("runRelationDelete: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"

		if err := runRelationDelete("project-has-tasks", "TODO", "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "relation not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationDelete("project-has-tasks", "TODO", "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		if err := runRelationDelete("project-has-tasks", "TODO", "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Volumes/Coding/make/repos/makecli && go test ./cmd/ -run TestRunRelationDelete -v`
Expected: All 4 tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/relation_delete.go cmd/relation_delete_test.go
git commit -m "feat(cmd): add relation delete subcommand with tests"
```

---

### Task 6: `relation list` Subcommand

**Files:**
- Create: `cmd/relation_list.go`
- Create: `cmd/relation_list_test.go`

- [ ] **Step 1: Create `cmd/relation_list.go`**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（Client）、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newRelationListCmd 函数
 * [POS]: cmd/relation 的 list 子命令，无 arg 时分页列出 app 下全部 relation，有 arg 时显示指定 relation 详情，支持 table/json 输出
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/api"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newRelationListCmd() *cobra.Command {
	var profile string
	var page int
	var size int
	var output string

	cmd := &cobra.Command{
		Use:          "list [relation-name]",
		Short:        "List relations in an app, or show a specific relation",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			relationName := ""
			if len(args) == 1 {
				relationName = args[0]
			}
			return runRelationList(app, relationName, profile, page, size, output)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of relations per page")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	return cmd
}

func runRelationList(app, relationName, profile string, page, size int, output string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}
	if page < 1 {
		return fmt.Errorf("page must be greater than or equal to 1")
	}
	if size < 1 {
		return fmt.Errorf("size must be greater than or equal to 1")
	}

	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	if relationName != "" {
		return showRelation(client, app, relationName, output)
	}
	return listRelations(client, app, page, size, output)
}

func listRelations(client *api.Client, app string, page, size int, output string) error {
	relations, total, err := client.ListRelations(app, page, size)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": relations,
			"pagination": map[string]int{
				"count": len(relations),
				"page":  page,
				"size":  size,
				"total": total,
			},
		})
	}

	if len(relations) == 0 {
		fmt.Printf("No relations found in app '%s'.\n", app)
		return nil
	}

	rows := make([][]string, len(relations))
	for i, r := range relations {
		version, _ := r.Meta["version"].(string)
		from := fmt.Sprintf("%s(%s)", r.Properties.From.Entity, r.Properties.From.Cardinality)
		to := fmt.Sprintf("%s(%s)", r.Properties.To.Entity, r.Properties.To.Cardinality)
		rows[i] = []string{r.Name, from, to, version}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "FROM", "TO", "VERSION")
	_ = table.Bulk(rows)
	_ = table.Render()

	fmt.Printf("\nShowing %d of %d relations\n", len(relations), total)
	return nil
}

func showRelation(client *api.Client, app, name, output string) error {
	relation, err := client.GetRelation(app, name)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": relation,
		})
	}

	version, _ := relation.Meta["version"].(string)
	fmt.Printf("Name:         %s\n", relation.Name)
	fmt.Printf("App:          %s\n", relation.App)
	fmt.Printf("Version:      %s\n", version)
	fmt.Printf("\nFrom:\n")
	fmt.Printf("  Entity:      %s\n", relation.Properties.From.Entity)
	fmt.Printf("  Cardinality: %s\n", relation.Properties.From.Cardinality)
	fmt.Printf("\nTo:\n")
	fmt.Printf("  Entity:      %s\n", relation.Properties.To.Entity)
	fmt.Printf("  Cardinality: %s\n", relation.Properties.To.Cardinality)
	return nil
}
```

- [ ] **Step 2: Create `cmd/relation_list_test.go`**

```go
/**
 * [INPUT]: 依赖 cmd 包内的 runRelationList（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 relation list 子命令核心逻辑的单元测试（列表/空列表/具体relation/JSON输出/无凭证/API错误/未知profile）
 * [POS]: cmd 模块 relation_list.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRelationList(t *testing.T) {
	t.Run("lists relations successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{
						"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
						"meta": map[string]any{"version": "1.0.0"},
						"properties": map[string]any{
							"from": map[string]any{"entity": "项目", "cardinality": "many"},
							"to":   map[string]any{"entity": "任务", "cardinality": "one"},
						},
					},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err != nil {
			t.Fatalf("runRelationList: %v", err)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err != nil {
			t.Fatalf("runRelationList empty: %v", err)
		}
	})

	t.Run("prints list as json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{
						"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
						"meta": map[string]any{"version": "1.0.0"},
						"properties": map[string]any{
							"from": map[string]any{"entity": "项目", "cardinality": "many"},
							"to":   map[string]any{"entity": "任务", "cardinality": "one"},
						},
					},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runRelationList("TODO", "", "default", 1, 20, outputJSON); err != nil {
				t.Fatalf("runRelationList json: %v", err)
			}
		})

		if !strings.Contains(output, "\"data\"") {
			t.Fatalf("expected JSON output, got %q", output)
		}
		if !strings.Contains(output, "\"count\": 1") {
			t.Fatalf("expected pagination count in JSON output, got %q", output)
		}
	})

	t.Run("shows specific relation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
					"meta": map[string]any{"version": "1.0.0"},
					"properties": map[string]any{
						"from": map[string]any{"entity": "项目", "cardinality": "many"},
						"to":   map[string]any{"entity": "任务", "cardinality": "one"},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRelationList("TODO", "project-has-tasks", "default", 1, 20, outputTable); err != nil {
				t.Fatalf("runRelationList detail: %v", err)
			}
		})

		if !strings.Contains(out, "project-has-tasks") {
			t.Fatalf("expected relation name in output, got %q", out)
		}
		if !strings.Contains(out, "项目") {
			t.Fatalf("expected from entity in output, got %q", out)
		}
		if !strings.Contains(out, "任务") {
			t.Fatalf("expected to entity in output, got %q", out)
		}
	})

	t.Run("prints specific relation as json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
					"meta": map[string]any{"version": "1.0.0"},
					"properties": map[string]any{
						"from": map[string]any{"entity": "项目", "cardinality": "many"},
						"to":   map[string]any{"entity": "任务", "cardinality": "one"},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runRelationList("TODO", "project-has-tasks", "default", 1, 20, outputJSON); err != nil {
				t.Fatalf("runRelationList json detail: %v", err)
			}
		})

		if !strings.Contains(output, "\"name\": \"project-has-tasks\"") {
			t.Fatalf("expected relation name in JSON output, got %q", output)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"
		if err := runRelationList("TODO", "", "nonexistent", 1, 20, outputTable); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on list API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails on get API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "relation not found"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationList("TODO", "不存在", "default", 1, 20, outputTable); err == nil {
			t.Fatal("expected error on get API failure")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runRelationList("TODO", "", "default", 0, 20, outputTable); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails when size is less than 1", func(t *testing.T) {
		if err := runRelationList("TODO", "", "default", 1, 0, outputTable); err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runRelationList("TODO", "", "default", 1, 20, "xml"); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Volumes/Coding/make/repos/makecli && go test ./cmd/ -run TestRunRelationList -v`
Expected: All 12 tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/relation_list.go cmd/relation_list_test.go
git commit -m "feat(cmd): add relation list subcommand with tests"
```

---

### Task 7: Full Test Suite + Documentation Update

**Files:**
- Modify: `internal/api/CLAUDE.md` (L2 doc)
- Modify: `cmd/CLAUDE.md` (L2 doc)
- Modify: `makecli/CLAUDE.md` (L1 doc, if directory listing changed)

- [ ] **Step 1: Run full test suite**

Run: `cd /Volumes/Coding/make/repos/makecli && go test ./... -v`
Expected: ALL tests pass (existing + new)

- [ ] **Step 2: Run vet**

Run: `cd /Volumes/Coding/make/repos/makecli && go vet ./...`
Expected: No issues

- [ ] **Step 3: Update `internal/api/CLAUDE.md`**

Update the `client.go` entry in the member list to include Relation types and methods.

- [ ] **Step 4: Update `cmd/CLAUDE.md`**

Add entries for the new relation files:
```
relation.go:                relation 命令组，挂载 create / update / delete / list 子命令
relation_create.go:         relation create 子命令，从 JSON 文件加载 from/to，调用 Meta Server API 创建 Relation；loadRelationProperties 从 JSON 文件加载关系属性；支持 --app（必选）/ --json（必选）/ --profile
relation_create_test.go:    覆盖 runRelationCreate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON/文件不存在），用 httptest 隔离网络
relation_update.go:         relation update 子命令，从 JSON 文件加载 from/to，调用 Meta Server API 更新 Relation；支持 --app（必选）/ --json（必选）/ --profile
relation_update_test.go:    覆盖 runRelationUpdate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
relation_delete.go:         relation delete 子命令，调用 Meta Server API 删除指定 Relation；支持 --app（必选）/ --profile
relation_delete_test.go:    覆盖 runRelationDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
relation_list.go:           relation list 子命令，无 arg 时分页列出 app 下全部 relation（NAME/FROM/TO/VERSION），有 arg 时显示指定 relation 详情；支持 --app（必选）/ --profile / --page / --size / --output
relation_list_test.go:      覆盖 runRelationList 的单元测试（列表/空列表/JSON列表/详情/JSON详情/无凭证/API错误/未知profile/非法页码/非法格式），用 httptest 隔离网络
```

- [ ] **Step 5: Commit documentation**

```bash
git add internal/api/CLAUDE.md cmd/CLAUDE.md
git commit -m "docs: update L2 documentation for relation commands"
```
