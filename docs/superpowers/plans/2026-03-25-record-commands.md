# Record Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `makecli record` command group with CRUD-L operations against the Data Service API (`/data/v1/record`).

**Architecture:** Follows the existing relation command pattern — command group with persistent `--app` + `--entity` flags, subcommands for create/get/update/delete/list. Record data is dynamic `map[string]any` (not typed structs). The `update` subcommand transparently routes to different API endpoints based on argument count: 1 ID → single-record API (`/data/v1/record`), N IDs → batch-field API (`/data/v1/field`).

**Tech Stack:** Go + cobra + tablewriter + httptest

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/api/record.go` | Record data types + 6 API methods (Create/Get/Update/UpdateBatch/Delete/List) |
| Create | `internal/api/record_test.go` | API layer tests for all Record methods |
| Create | `cmd/record.go` | Command group with `--app` + `--entity` persistent flags |
| Create | `cmd/record_create.go` | create subcommand |
| Create | `cmd/record_create_test.go` | create tests |
| Create | `cmd/record_get.go` | get subcommand |
| Create | `cmd/record_get_test.go` | get tests |
| Create | `cmd/record_update.go` | update subcommand (transparent single/batch routing) |
| Create | `cmd/record_update_test.go` | update tests |
| Create | `cmd/record_delete.go` | delete subcommand (batch) |
| Create | `cmd/record_delete_test.go` | delete tests |
| Create | `cmd/record_list.go` | list subcommand with fields/sort/pagination |
| Create | `cmd/record_list_test.go` | list tests |
| Modify | `cmd/root.go:60` | Register `newRecordCmd()` |
| Modify | `cmd/CLAUDE.md` | Add record command entries to L2 |
| Modify | `internal/api/CLAUDE.md` | Add record.go entry to L2 |
| Modify | `CLAUDE.md` | Update L1 directory listing |

---

### Task 1: API Client — Record Types and Methods

**Files:**
- Create: `internal/api/record.go`
- Create: `internal/api/record_test.go`

- [ ] **Step 1: Create record.go with types and all 6 methods**

```go
/**
 * [INPUT]: 依赖 internal/api/client（Client.do / Client.post）、fmt
 * [OUTPUT]: 对外提供 DeleteRecordResult / SortField / ListRecordOpts 类型、CreateRecord / GetRecord / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 方法
 * [POS]: internal/api 的 Data Service 操作层，封装 /data/v1/record 和 /data/v1/field 的 HTTP 调用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import "fmt"

// ---------------------------------- Record 数据类型 ----------------------------------

// DeleteRecordResult 描述批量删除中每条记录的结果
type DeleteRecordResult struct {
	RecordID string `json:"recordID"`
	Code     int    `json:"code"`
	Message  string `json:"msg"`
}

// SortField 描述排序字段和方向
type SortField struct {
	Field string `json:"field"`
	Order string `json:"order"` // "asc" | "desc"
}

// ListRecordOpts 封装 ListRecords 的查询参数
type ListRecordOpts struct {
	Fields []string
	Sort   []SortField
	Page   int
	Size   int
}

// ---------------------------------- Record 操作 ----------------------------------

// CreateRecord 调用 MakeService.CreateResource 在指定 Entity 下创建 Record
// 返回服务端分配的 recordID
func (c *Client) CreateRecord(app, entity string, data map[string]any) (string, error) {
	body := map[string]any{
		"app":    app,
		"entity": entity,
		"data":   data,
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    struct {
			RecordID string `json:"recordID"`
		} `json:"data"`
	}
	if err := c.do("MakeService.CreateResource", "/data/v1/record", body, &result); err != nil {
		return "", err
	}
	if result.Code != 200 {
		return "", fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data.RecordID, nil
}

// GetRecord 调用 MakeService.GetResource 获取单条 Record
// 返回动态字段 map（含 recordID 和业务字段）
func (c *Client) GetRecord(app, entity, recordID string) (map[string]any, error) {
	body := map[string]any{
		"app":      app,
		"entity":   entity,
		"recordID": recordID,
	}
	var result struct {
		Code    int            `json:"code"`
		Message string         `json:"msg"`
		Data    map[string]any `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/data/v1/record", body, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, nil
}

// UpdateRecord 调用 MakeService.UpdateResource 更新单条 Record
func (c *Client) UpdateRecord(app, entity, recordID string, data map[string]any) error {
	body := map[string]any{
		"app":      app,
		"entity":   entity,
		"recordID": recordID,
		"data":     data,
	}
	return c.post("MakeService.UpdateResource", "/data/v1/record", body)
}

// UpdateRecordsBatch 调用 MakeService.UpdateResource 批量更新多条 Record 的字段值
// 走 /data/v1/field 端点
//
// 设计备注: 当 CLI `record update` 接收到多个 recordID 时，透明路由到此方法。
// 用户无需感知两个 API 端点的差异——单条走 /data/v1/record，多条走 /data/v1/field。
func (c *Client) UpdateRecordsBatch(app, entity string, recordIDs []string, data map[string]any) error {
	body := map[string]any{
		"app":          app,
		"entity":       entity,
		"recordIDList": recordIDs,
		"data":         data,
	}
	return c.post("MakeService.UpdateResource", "/data/v1/field", body)
}

// DeleteRecords 调用 MakeService.DeleteResource 批量删除 Record
// 返回每条记录的删除结果（含各自的 code/msg）
func (c *Client) DeleteRecords(app, entity string, recordIDs []string) ([]DeleteRecordResult, error) {
	body := map[string]any{
		"app":          app,
		"entity":       entity,
		"recordIDList": recordIDs,
	}
	var result struct {
		Code    int                  `json:"code"`
		Message string               `json:"msg"`
		Data    []DeleteRecordResult `json:"data"`
	}
	if err := c.do("MakeService.DeleteResource", "/data/v1/record", body, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, nil
}

// ListRecords 调用 MakeService.ListResources 分页查询 Record
// 返回记录列表和服务端 total 数量
func (c *Client) ListRecords(app, entity string, opts ListRecordOpts) ([]map[string]any, int, error) {
	reqBody := map[string]any{
		"app":        app,
		"entity":     entity,
		"pagination": map[string]any{"page": opts.Page, "size": opts.Size},
	}
	if len(opts.Fields) > 0 {
		reqBody["fields"] = opts.Fields
	}
	if len(opts.Sort) > 0 {
		reqBody["sort"] = opts.Sort
	}
	var result struct {
		Code    int              `json:"code"`
		Message string           `json:"msg"`
		Data    []map[string]any `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/data/v1/record", reqBody, &result); err != nil {
		return nil, 0, err
	}
	if result.Code != 200 {
		return nil, 0, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, result.Pagination.Total, nil
}
```

- [ ] **Step 2: Write record_test.go**

Test all 6 methods: CreateRecord, GetRecord, UpdateRecord, UpdateRecordsBatch, DeleteRecords, ListRecords.
Pattern: httptest server validates X-Make-Target header and request body path, returns appropriate response.
Cover: success + API error for each method.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/ -v -run TestRecord`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/record.go internal/api/record_test.go
git commit -m "feat(api): add Record CRUD methods for Data Service API"
```

---

### Task 2: Command Group — `record.go`

**Files:**
- Create: `cmd/record.go`
- Modify: `cmd/root.go:60`

- [ ] **Step 1: Create cmd/record.go**

```go
/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordCmd 函数
 * [POS]: cmd 模块的 record 命令组，挂载 create / get / update / delete / list 子命令，--app 和 --entity 参数为子命令继承
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newRecordCmd() *cobra.Command {
	var app string
	var entity string

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Manage records in an entity",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if app == "" || entity == "" {
				return cmd.Usage()
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&app, "app", "", "app name (required)")
	_ = cmd.MarkPersistentFlagRequired("app")
	cmd.PersistentFlags().StringVar(&entity, "entity", "", "entity name (required)")
	_ = cmd.MarkPersistentFlagRequired("entity")

	cmd.AddCommand(newRecordCreateCmd())
	cmd.AddCommand(newRecordGetCmd())
	cmd.AddCommand(newRecordUpdateCmd())
	cmd.AddCommand(newRecordDeleteCmd())
	cmd.AddCommand(newRecordListCmd())
	return cmd
}
```

- [ ] **Step 2: Register in root.go**

Add `rootCmd.AddCommand(newRecordCmd())` after the relation line in `cmd/root.go`.

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: compile error (subcommand functions not yet defined) — this is expected. We'll fix in subsequent tasks. If using stubs, verify clean build.

Note: You may need to create minimal stub files for the 5 subcommand constructors to compile. Create them with just `func newRecordXxxCmd() *cobra.Command { return &cobra.Command{Use: "xxx"} }` in each file, then replace with real implementations in later tasks.

- [ ] **Step 4: Commit**

```bash
git add cmd/record.go cmd/root.go
git commit -m "feat(cmd): add record command group with --app and --entity flags"
```

---

### Task 3: `record create` Subcommand

**Files:**
- Create: `cmd/record_create.go`
- Create: `cmd/record_create_test.go`

- [ ] **Step 1: Write test file**

Test cases:
- creates record successfully (mock returns recordID, verify stdout contains it)
- fails without credentials
- fails on API error response
- fails with unknown profile
- fails with invalid JSON file
- fails with nonexistent JSON file

Pattern: follow `relation_create_test.go` exactly. Helper: `writeRecordJSON(t, data map[string]any) string`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -v -run TestRunRecordCreate`
Expected: FAIL (function not defined)

- [ ] **Step 3: Write implementation**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordCreateCmd 函数
 * [POS]: cmd/record 的 create 子命令，从 JSON 文件加载数据，调用 Data Service API 创建 Record
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRecordCreateCmd() *cobra.Command {
	var profile string
	var jsonFile string

	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new record in an entity",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordCreate(app, entity, jsonFile, profile)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing record data (required)")
	_ = cmd.MarkFlagRequired("json")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRecordCreate(app, entity, jsonFile, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	data, err := loadRecordData(jsonFile)
	if err != nil {
		return err
	}

	recordID, err := client.CreateRecord(app, entity, data)
	if err != nil {
		return err
	}

	fmt.Printf("Record created successfully (recordID: %s)\n", recordID)
	return nil
}

// loadRecordData 读取 JSON 文件并解析为动态 KV map
func loadRecordData(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 JSON 文件失败: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("JSON 文件格式错误: %w", err)
	}
	return data, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v -run TestRunRecordCreate`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/record_create.go cmd/record_create_test.go
git commit -m "feat(cmd): add record create subcommand"
```

---

### Task 4: `record get` Subcommand

**Files:**
- Create: `cmd/record_get.go`
- Create: `cmd/record_get_test.go`

- [ ] **Step 1: Write test file**

Test cases:
- gets record successfully (table output, verify key-value lines in stdout)
- gets record as json
- fails without credentials
- fails on API error
- fails with unknown profile

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -v -run TestRunRecordGet`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、fmt、sort、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newRecordGetCmd 函数
 * [POS]: cmd/record 的 get 子命令，获取单条 Record 并以 table 或 json 格式展示
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newRecordGetCmd() *cobra.Command {
	var profile string
	var output string

	cmd := &cobra.Command{
		Use:          "get <record-id>",
		Short:        "Get a record by ID",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordGet(app, entity, args[0], profile, output)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	return cmd
}

func runRecordGet(app, entity, recordID, profile, output string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}

	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	data, err := client.GetRecord(app, entity, recordID)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{"data": data})
	}

	// table 模式: 按 key 排序，逐行输出 key-value
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-20s %v\n", k, data[k])
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v -run TestRunRecordGet`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/record_get.go cmd/record_get_test.go
git commit -m "feat(cmd): add record get subcommand"
```

---

### Task 5: `record update` Subcommand (Transparent Single/Batch Routing)

**Files:**
- Create: `cmd/record_update.go`
- Create: `cmd/record_update_test.go`

This is the key command with transparent API routing logic.

- [ ] **Step 1: Write test file**

Test cases:
- updates single record successfully (1 arg → verify X-Make-Target and path is /data/v1/record)
- updates multiple records in batch (N args → verify path is /data/v1/field)
- fails without credentials
- fails on API error
- fails with unknown profile
- fails with invalid JSON
- fails with no record IDs (0 args)

For the routing tests, the mock server MUST verify the request path to confirm correct routing:
- 1 arg → request to `/data/v1/record`
- N args → request to `/data/v1/field`

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -v -run TestRunRecordUpdate`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/record_create（loadRecordData）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordUpdateCmd 函数
 * [POS]: cmd/record 的 update 子命令，支持单条和批量更新
 *
 * 路由逻辑备注:
 *   1 个 recordID  → 调用 UpdateRecord（POST /data/v1/record）— 单条更新，可改多个字段
 *   N 个 recordID → 调用 UpdateRecordsBatch（POST /data/v1/field）— 批量更新，同一组 KV 应用到所有记录
 *   CLI 根据参数数量自动选择 API 端点，用户无需感知底层差异。
 *
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecordUpdateCmd() *cobra.Command {
	var profile string
	var jsonFile string

	cmd := &cobra.Command{
		Use:   "update <record-id> [record-id...]",
		Short: "Update one or more records",
		Long: `Update records with data from a JSON file.

When a single record ID is provided, updates that record via the record API.
When multiple record IDs are provided, applies the same data to all records
via the batch field API. The routing is transparent to the user.`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordUpdate(app, entity, args, jsonFile, profile)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing update data (required)")
	_ = cmd.MarkFlagRequired("json")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRecordUpdate(app, entity string, recordIDs []string, jsonFile, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	data, err := loadRecordData(jsonFile)
	if err != nil {
		return err
	}

	// 路由逻辑: 1 个 ID 走单条 API，多个 ID 走批量 API
	if len(recordIDs) == 1 {
		if err := client.UpdateRecord(app, entity, recordIDs[0], data); err != nil {
			return err
		}
		fmt.Printf("Record '%s' updated successfully\n", recordIDs[0])
	} else {
		if err := client.UpdateRecordsBatch(app, entity, recordIDs, data); err != nil {
			return err
		}
		fmt.Printf("%d records updated successfully\n", len(recordIDs))
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v -run TestRunRecordUpdate`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/record_update.go cmd/record_update_test.go
git commit -m "feat(cmd): add record update subcommand with transparent single/batch routing"
```

---

### Task 6: `record delete` Subcommand

**Files:**
- Create: `cmd/record_delete.go`
- Create: `cmd/record_delete_test.go`

- [ ] **Step 1: Write test file**

Test cases:
- deletes single record successfully
- deletes multiple records (batch)
- reports partial failures (some records fail in batch)
- fails without credentials
- fails on API error
- fails with unknown profile

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -v -run TestRunRecordDelete`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordDeleteCmd 函数
 * [POS]: cmd/record 的 delete 子命令，调用 Data Service API 批量删除 Record
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecordDeleteCmd() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:          "delete <record-id> [record-id...]",
		Short:        "Delete one or more records",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordDelete(app, entity, args, profile)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRecordDelete(app, entity string, recordIDs []string, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	results, err := client.DeleteRecords(app, entity, recordIDs)
	if err != nil {
		return err
	}

	// 汇报每条记录的删除结果
	var failed int
	for _, r := range results {
		if r.Code != 200 {
			fmt.Printf("  FAIL  %s: %s\n", r.RecordID, r.Message)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d records failed to delete", failed, len(results))
	}

	fmt.Printf("%d record(s) deleted successfully\n", len(recordIDs))
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v -run TestRunRecordDelete`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/record_delete.go cmd/record_delete_test.go
git commit -m "feat(cmd): add record delete subcommand with batch support"
```

---

### Task 7: `record list` Subcommand

**Files:**
- Create: `cmd/record_list.go`
- Create: `cmd/record_list_test.go`

- [ ] **Step 1: Write test file**

Test cases:
- lists records in table format (auto-detect columns from data keys)
- lists records as json with pagination
- empty list prints message
- uses --fields flag to select columns
- uses --sort flag to specify sort order
- fails without credentials
- fails on API error
- fails with unknown profile
- fails on invalid page/size
- fails on unsupported output format

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -v -run TestRunRecordList`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（ListRecordOpts/SortField）、fmt、os、sort、strings、github.com/olekukonko/tablewriter、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newRecordListCmd 函数
 * [POS]: cmd/record 的 list 子命令，分页查询 Record，支持 fields 选择、sort 排序、table/json 输出
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/qfeius/makecli/internal/api"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newRecordListCmd() *cobra.Command {
	var profile string
	var page int
	var size int
	var output string
	var fields string
	var sortSpec string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List records in an entity",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordList(app, entity, profile, page, size, output, fields, sortSpec)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().IntVar(&page, "page", 1, "page number (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "records per page")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	cmd.Flags().StringVar(&fields, "fields", "", "comma-separated field names to display")
	cmd.Flags().StringVar(&sortSpec, "sort", "", "sort specification, e.g. createdAt:desc,id:asc")
	return cmd
}

func runRecordList(app, entity, profile string, page, size int, output, fields, sortSpec string) error {
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

	opts := api.ListRecordOpts{Page: page, Size: size}
	if fields != "" {
		opts.Fields = strings.Split(fields, ",")
	}
	if sortSpec != "" {
		parsed, err := parseSortSpec(sortSpec)
		if err != nil {
			return err
		}
		opts.Sort = parsed
	}

	records, total, err := client.ListRecords(app, entity, opts)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": records,
			"pagination": map[string]int{
				"count": len(records),
				"page":  page,
				"size":  size,
				"total": total,
			},
		})
	}

	if len(records) == 0 {
		fmt.Printf("No records found in entity '%s'.\n", entity)
		return nil
	}

	// 自动从首条记录提取列名（或使用 --fields 指定的列）
	var headers []string
	if len(opts.Fields) > 0 {
		headers = opts.Fields
	} else {
		headers = extractKeys(records[0])
	}

	rows := make([][]string, len(records))
	for i, rec := range records {
		row := make([]string, len(headers))
		for j, h := range headers {
			row[j] = fmt.Sprintf("%v", rec[h])
		}
		rows[i] = row
	}

	upperHeaders := make([]string, len(headers))
	for i, h := range headers {
		upperHeaders[i] = strings.ToUpper(h)
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header(upperHeaders...)
	_ = table.Bulk(rows)
	_ = table.Render()

	fmt.Printf("\nShowing %d of %d records\n", len(records), total)
	return nil
}

// parseSortSpec 解析 "field:order,field:order" 格式的排序说明
func parseSortSpec(spec string) ([]api.SortField, error) {
	parts := strings.Split(spec, ",")
	result := make([]api.SortField, 0, len(parts))
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid sort spec %q, expected field:order", p)
		}
		order := strings.ToLower(kv[1])
		if order != "asc" && order != "desc" {
			return nil, fmt.Errorf("invalid sort order %q, expected asc or desc", kv[1])
		}
		result = append(result, api.SortField{Field: kv[0], Order: order})
	}
	return result, nil
}

// extractKeys 从 map 中提取排序后的 key 列表
func extractKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v -run TestRunRecordList`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/record_list.go cmd/record_list_test.go
git commit -m "feat(cmd): add record list subcommand with fields/sort/pagination"
```

---

### Task 8: Documentation — L1/L2 CLAUDE.md Updates

**Files:**
- Modify: `CLAUDE.md` (L1 — add record to cmd directory description)
- Modify: `cmd/CLAUDE.md` (L2 — add all record_*.go entries)
- Modify: `internal/api/CLAUDE.md` (L2 — add record.go entry)

- [ ] **Step 1: Update L1 CLAUDE.md**

Add `record` to the cmd/ directory listing.

- [ ] **Step 2: Update cmd/CLAUDE.md**

Add entries for: record.go, record_create.go, record_create_test.go, record_get.go, record_get_test.go, record_update.go, record_update_test.go, record_delete.go, record_delete_test.go, record_list.go, record_list_test.go.

- [ ] **Step 3: Update internal/api/CLAUDE.md**

Add record.go and record_test.go entries.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md cmd/CLAUDE.md internal/api/CLAUDE.md
git commit -m "docs: update L1/L2 documentation for record commands"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: ALL PASS

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: clean

- [ ] **Step 3: Manual smoke test (optional)**

```bash
go build -o makecli .
./makecli record --help
./makecli record --app TODO --entity 任务 list --help
./makecli record --app TODO --entity 任务 update --help
```

Verify help text shows correct flags and usage.
