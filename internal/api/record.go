/**
 * [INPUT]: 依赖 fmt，依赖同包 Client.do / Client.post 方法、writeStatusErr / conflictData（409 唯一性冲突翻译，收口于 client.go）
 * [OUTPUT]: 对外提供 DeleteRecordResult / SortField / ListRecordOpts 类型、CreateRecord / GetRecord / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 方法（写方法违反唯一性约束时返回 UniqueConstraintError）
 * [POS]: internal/api 的 Data Service 层，封装 Record CRUD 操作，与 client.go 的 Meta Service 层平级
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import "fmt"

// ---------------------------------- 数据类型 ----------------------------------

// DeleteRecordResult 描述批量删除中单条记录的处理结果
type DeleteRecordResult struct {
	RecordID string `json:"recordID"`
	Code     int    `json:"code"`
	Message  string `json:"msg"`
}

// SortField 描述排序字段与方向（field key + 方向）
type SortField struct {
	FieldKey string `json:"fieldKey"`
	Order    string `json:"order"` // "asc" | "desc"
}

// ListRecordOpts 封装 ListRecords 的可选参数
// Filter 为可选的 CEL 表达式文本（原样透传，由服务端校验），空串时不发送
type ListRecordOpts struct {
	Fields []string
	Sort   []SortField
	Filter string
	Page   int
	Size   int
}

// ---------------------------------- Record 操作 ----------------------------------

// CreateRecord 调用 MakeService.CreateResource 创建一条记录
// 返回新创建记录的 recordID
func (c *Client) CreateRecord(appKey, entityKey string, data map[string]any) (string, error) {
	reqBody := map[string]any{
		"appKey":    appKey,
		"entityKey": entityKey,
		"data":      data,
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    struct {
			RecordID     string `json:"recordID"`
			conflictData        // 409 时携带 constraint/fields；成功响应无此键，零值忽略
		} `json:"data"`
	}
	if err := c.do("MakeService.CreateResource", "/data/v1/record", reqBody, &result); err != nil {
		return "", err
	}
	if result.Code != 200 {
		return "", writeStatusErr(result.Code, result.Message, result.Data.conflictData)
	}
	return result.Data.RecordID, nil
}

// GetRecord 调用 MakeService.GetResource 获取单条记录
// 返回记录的动态字段 map
func (c *Client) GetRecord(appKey, entityKey, recordID string) (map[string]any, error) {
	reqBody := map[string]any{
		"appKey":    appKey,
		"entityKey": entityKey,
		"recordID":  recordID,
	}
	var result struct {
		Code    int            `json:"code"`
		Message string         `json:"msg"`
		Data    map[string]any `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/data/v1/record", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, nil
}

// UpdateRecord 调用 MakeService.UpdateResource 更新单条记录
func (c *Client) UpdateRecord(appKey, entityKey, recordID string, data map[string]any) error {
	body := map[string]any{
		"appKey":    appKey,
		"entityKey": entityKey,
		"recordID":  recordID,
		"data":      data,
	}
	return c.post("MakeService.UpdateResource", "/data/v1/record", body)
}

// UpdateRecordsBatch 调用 MakeService.UpdateResource 批量更新多条记录
//
// 路由设计：CLI 的 `record update` 命令根据 recordID 数量透明路由——
// 单条走 UpdateRecord（/data/v1/record），多条走本方法（/data/v1/field）。
// 用户无需感知两个不同的 API 端点。
// data 的 key 应该是 fieldKey（英文标识符）。
func (c *Client) UpdateRecordsBatch(appKey, entityKey string, recordIDs []string, data map[string]any) error {
	body := map[string]any{
		"appKey":       appKey,
		"entityKey":    entityKey,
		"recordIDList": recordIDs,
		"data":         data,
	}
	return c.post("MakeService.UpdateResource", "/data/v1/field", body)
}

// DeleteRecords 调用 MakeService.DeleteResource 批量删除记录
// 返回每条记录的删除结果
func (c *Client) DeleteRecords(appKey, entityKey string, recordIDs []string) ([]DeleteRecordResult, error) {
	reqBody := map[string]any{
		"appKey":       appKey,
		"entityKey":    entityKey,
		"recordIDList": recordIDs,
	}
	var result struct {
		Code    int                  `json:"code"`
		Message string               `json:"msg"`
		Data    []DeleteRecordResult `json:"data"`
	}
	if err := c.do("MakeService.DeleteResource", "/data/v1/record", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, nil
}

// ListRecords 调用 MakeService.ListResources 分页查询记录列表
// 返回记录列表和服务端 total 数量
// fields/sort 字段名使用 fieldKey（英文标识符）
func (c *Client) ListRecords(appKey, entityKey string, opts ListRecordOpts) ([]map[string]any, int, error) {
	reqBody := map[string]any{
		"appKey":     appKey,
		"entityKey":  entityKey,
		"pagination": map[string]any{"page": opts.Page, "size": opts.Size},
	}
	if len(opts.Fields) > 0 {
		reqBody["fields"] = opts.Fields
	}
	if len(opts.Sort) > 0 {
		reqBody["sort"] = opts.Sort
	}
	if opts.Filter != "" {
		reqBody["filter"] = map[string]any{"expression": opts.Filter}
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
