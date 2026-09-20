package pluginsdk

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "changeme/pluginsdk/proto"
)

// HostClient 插件进程内调用宿主能力的客户端。
//
// 用法 (Start 时拿到端点与令牌后):
//
//	host, err := pluginsdk.DialHost(ctx, hc.HostEndpoint, hc.Token)
//	id, err := host.OpenTab(ctx, &pluginsdk.TabSpec{...})
type HostClient struct {
	raw pb.HostServiceClient
}

// DialHost 连接宿主 HostService (令牌经 metadata 下发; 连接为惰性建立)。
func DialHost(_ context.Context, endpoint, token string) (*HostClient, error) {
	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(metadata.AppendToOutgoingContext(ctx, "x-aceshell-token", token), method, req, reply, cc, opts...)
		}),
	)
	if err != nil {
		return nil, err
	}
	return &HostClient{raw: pb.NewHostServiceClient(conn)}, nil
}

// TabSpec 打开标签页的参数。
type TabSpec struct {
	TabKey      string         `json:"tabKey"`      // 插件内唯一, 幂等去重依据
	Title       string         `json:"title"`
	ComponentID string         `json:"componentId"` // dist/entry.js 导出 components 的键
	Props       map[string]any `json:"props,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	Color       string         `json:"color,omitempty"`
}

const defaultRPCTimeout = 15 * time.Second

func (h *HostClient) OpenTab(ctx context.Context, spec *TabSpec) (tabID string, err error) {
	ctx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	propsJSON := "{}"
	if spec.Props != nil {
		if b, merr := json.Marshal(spec.Props); merr == nil {
			propsJSON = string(b)
		}
	}
	resp, err := h.raw.OpenTab(ctx, &pb.OpenTabRequest{Spec: &pb.TabSpec{
		TabKey: spec.TabKey, Title: spec.Title, ComponentId: spec.ComponentID,
		PropsJson: propsJSON, Icon: spec.Icon, Color: spec.Color,
	}})
	if err != nil {
		return "", err
	}
	return resp.GetTabId(), nil
}

func (h *HostClient) CloseTab(ctx context.Context, tabKey string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := h.raw.CloseTab(ctx, &pb.CloseTabRequest{TabKey: tabKey})
	if err != nil {
		return false, err
	}
	return resp.GetClosed(), nil
}

func (h *HostClient) SetTabTitle(ctx context.Context, tabKey, title string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	_, err := h.raw.SetTabTitle(ctx, &pb.SetTabTitleRequest{TabKey: tabKey, Title: title})
	return err
}

func (h *HostClient) ShowToast(ctx context.Context, message, level string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	_, err := h.raw.ShowToast(ctx, &pb.ShowToastRequest{Message: message, Level: level})
	return err
}

// ListSessions 返回只读会话摘要 (JSON 数组字符串)。
func (h *HostClient) ListSessions(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := h.raw.ListSessions(ctx, &pb.ListSessionsRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetSessionsJson(), nil
}

// EmitUIEvent 推送 UI 事件到宿主前端 (按插件分发; payload 为任意 JSON)。
// 适合流式结果: 如 ping 逐包延迟、日志尾随、进度等。
func (h *HostClient) EmitUIEvent(ctx context.Context, payloadJSON string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	_, err := h.raw.EmitUIEvent(ctx, &pb.EmitUIEventRequest{PayloadJson: payloadJSON})
	return err
}
