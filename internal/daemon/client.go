/**
 * [INPUT]: 依赖 bytes、context、encoding/json、fmt、io、net/http、time；协议类型来自 protocol.go
 * [OUTPUT]: 对外提供 Client（gateway 设备面 /v1/daemon/* 的类型化调用）与 APIError（信封错误还原）
 * [POS]: internal/daemon 的传输层——Bearer token 鉴权，POST + X-Make-Target + 信封解包；正确性建立在拉取式 claim 上，连接断开只影响延迟
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIError 还原 gateway/context 的错误信封。
type APIError struct {
	HTTPStatus int
	Reason     string
	Msg        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gateway %d %s: %s", e.HTTPStatus, e.Reason, e.Msg)
}

// Client 是 gateway 设备面的 HTTP client。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient 构造 Client；baseURL 形如 https://gateway.example.com。
func NewClient(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// call 执行统一调用风格请求并解包信封。
func (c *Client) call(ctx context.Context, resource, target string, requestBody, responseData any) error {
	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+PathPrefix+"/"+resource, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set(TargetHeader, target)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("gateway unreachable: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var envelope Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return &APIError{HTTPStatus: response.StatusCode, Reason: "invalid_envelope", Msg: string(raw)}
	}
	if response.StatusCode != http.StatusOK {
		var errorData ErrorData
		_ = json.Unmarshal(envelope.Data, &errorData)
		return &APIError{HTTPStatus: response.StatusCode, Reason: errorData.Reason, Msg: envelope.Msg}
	}
	if responseData != nil {
		if err := json.Unmarshal(envelope.Data, responseData); err != nil {
			return fmt.Errorf("unmarshal data: %w", err)
		}
	}
	return nil
}

// RegisterDevice 注册（幂等，身份来自 token）。
func (c *Client) RegisterDevice(ctx context.Context, request CreateDeviceRequest) (CreateDeviceResponse, error) {
	var response CreateDeviceResponse
	err := c.call(ctx, ResourceDevice, TargetCreateResource, request, &response)
	return response, err
}

// Heartbeat 心跳（15s）；响应 actions 携带取消指令。
func (c *Client) Heartbeat(ctx context.Context, request CreateDeviceHeartbeatRequest) (CreateDeviceHeartbeatResponse, error) {
	var response CreateDeviceHeartbeatResponse
	err := c.call(ctx, ResourceDeviceHeartbeat, TargetCreateResource, request, &response)
	return response, err
}

// ClaimRuns 领取待执行 run（run-claim 资源的 CreateResource：claim 即创建租约）。
func (c *Client) ClaimRuns(ctx context.Context, request CreateRunClaimRequest) ([]RunClaim, error) {
	var claims []RunClaim
	err := c.call(ctx, ResourceRunClaim, TargetCreateResource, request, &claims)
	return claims, err
}

// UpdateRun 状态迁移统一入口（语义由 status 目标值表达）。
func (c *Client) UpdateRun(ctx context.Context, request UpdateRunRequest) error {
	return c.call(ctx, ResourceRun, TargetUpdateResource, request, nil)
}

// AppendEvents 租约 append（batchSeq 幂等，模糊重试安全）。
func (c *Client) AppendEvents(ctx context.Context, request CreateEventsRequest) (CreateEventsResponse, error) {
	var response CreateEventsResponse
	err := c.call(ctx, ResourceEvent, TargetCreateResource, request, &response)
	return response, err
}

// ListEvents 区间读取（触发区间与恢复现场共用）。
func (c *Client) ListEvents(ctx context.Context, request ListEventsRequest) ([]Event, error) {
	var events []Event
	err := c.call(ctx, ResourceEvent, TargetListResources, request, &events)
	return events, err
}
