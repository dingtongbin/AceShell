// Package pluginsdk 是 AceShell 插件的 Go SDK。
//
// 插件 = 独立进程, 经 hashicorp/go-plugin (gRPC + AutoMTLS) 与宿主握手通信:
//
//	func main() { pluginsdk.Serve(&MyPlugin{}) }
//
// UI 契约: 插件目录下 dist/entry.js 导出 { components: { <componentID>: Component } },
// 宿主前端动态 import 后直挂侧栏面板与标签页 (主题令牌自动联动)。
//
// 非 Go 语言: 按,proto/aceshell.proto 自行实现 AcePlugin gRPC 服务与
// go-plugin 握手 (magic cookie + stdout 握手行 + gRPC Health 服务)。
package pluginsdk

import (
	"context"
	"errors"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pb "changeme/pluginsdk/proto"
)

// 协议版本与握手魔数 (与宿主 pluginservice.go 保持一致)。
const (
	ProtocolVersion  = 1
	MagicCookieKey   = "ACESHELL_PLUGIN_COOKIE"
	MagicCookieValue = "aceshell-plugin-v1-handshake"
)

// DispenseName go-plugin 插件注册名 (宿主 Dispense 用)。
const DispenseName = "plugin"

// AcePlugin 插件实现的完整钩子集。
type AcePlugin interface {
	// Info 返回插件元数据与侧栏视图注册清单。宿主握手后立即调用。
	// locale 为宿主当前界面语言 (BCP-47, 如 zh-CN/en-US); 应据此返回本地化文案,
	// 不支持该语言时返回默认文案。宿主语言切换后会重新调用 Info 并刷新注册表。
	Info(ctx context.Context, locale string) (*PluginInfo, error)
	// OnLocaleChanged 用户切换界面语言 (宿主对每个运行中插件调用)。
	// 插件应更新自身展示文案; 宿主随后重新拉取 Info。
	OnLocaleChanged(ctx context.Context, locale string) error
	// Start 生命周期开始; host 携带宿主版本、插件私有数据目录与 HostService 端点/令牌。
	Start(ctx context.Context, host *HostContext) error
	// Shutdown 生命周期结束 (宿主退出/插件禁用/更新前), 应尽快收尾返回。
	Shutdown(ctx context.Context) error
	// OnViewVisible/OnViewHidden 侧栏面板显隐回调。
	OnViewVisible(ctx context.Context, viewID string) error
	OnViewHidden(ctx context.Context, viewID string) error
	// OnTabEvent 标签页事件: Kind = "opened" | "activated" | "closed"。
	OnTabEvent(ctx context.Context, ev *TabEvent) error
	// Rpc 通用业务通道: 插件前端 (经宿主转发) 调用插件后端, JSON 进出。
	Rpc(ctx context.Context, method, argsJSON string) (resultJSON string, err error)
}

// PluginInfo 插件元数据 (注册侧栏图标/视图)。
type PluginInfo struct {
	ID           string     `json:"id"`
	DisplayName  string     `json:"displayName"`
	Version      string     `json:"version"`
	Icon         string     `json:"icon,omitempty"`        // SVG 文本或 dataURI
	AccentColor  string     `json:"accentColor,omitempty"` // 可选主题色 (CSS color)
	Views        []ViewInfo `json:"views,omitempty"`
	Capabilities []string   `json:"capabilities,omitempty"` // 能力标签声明 (供宿主展示/门控)
}

// ViewInfo 侧栏视图注册项。
type ViewInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Icon        string `json:"icon,omitempty"`
	ComponentID string `json:"componentId"` // dist/entry.js 导出 components 的键
}

// HostContext Start 下发的宿主上下文。
type HostContext struct {
	HostVersion  string `json:"hostVersion"`
	DataDir      string `json:"dataDir"`
	HostEndpoint string `json:"hostEndpoint"`
	Token        string `json:"token"`
}

// TabEvent 标签页事件。
type TabEvent struct {
	TabKey string `json:"tabKey"`
	Kind   string `json:"kind"`
}

// ==================== go-plugin 适配 (插件进程侧) ====================

type grpcPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	impl AcePlugin
}

// GRPCServer 插件进程侧: 将 AcePlugin 服务注册到 go-plugin 的 gRPC Server。
func (p *grpcPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterAcePluginServer(s, &pbServer{impl: p.impl})
	return nil
}

// GRPCClient 宿主侧取用器在 hostside.go (宿主不实现 AcePlugin)。
func (p *grpcPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("AcePlugin 服务只能由插件进程实现")
}

// pbServer pb.AcePluginServer → AcePlugin 适配。
type pbServer struct {
	pb.UnimplementedAcePluginServer
	impl AcePlugin
}

func (s *pbServer) Info(ctx context.Context, req *pb.InfoRequest) (*pb.PluginInfo, error) {
	info, err := s.impl.Info(ctx, req.GetLocale())
	if err != nil {
		return nil, err
	}
	out := &pb.PluginInfo{
		Id:          info.ID,
		DisplayName: info.DisplayName,
		Version:     info.Version,
		Icon:        info.Icon,
		AccentColor: info.AccentColor,
		Capabilities: info.Capabilities,
	}
	for _, v := range info.Views {
		out.Views = append(out.Views, &pb.ViewInfo{Id: v.ID, Title: v.Title, Icon: v.Icon, ComponentId: v.ComponentID})
	}
	return out, nil
}

func (s *pbServer) OnLocaleChanged(ctx context.Context, req *pb.LocaleChangedRequest) (*pb.LocaleChangedResponse, error) {
	err := s.impl.OnLocaleChanged(ctx, req.GetLocale())
	return &pb.LocaleChangedResponse{}, err
}

func (s *pbServer) Start(ctx context.Context, req *pb.StartRequest) (*pb.StartResponse, error) {
	err := s.impl.Start(ctx, &HostContext{
		HostVersion:  req.GetHostVersion(),
		DataDir:      req.GetDataDir(),
		HostEndpoint: req.GetHostEndpoint(),
		Token:        req.GetToken(),
	})
	return &pb.StartResponse{}, err
}

func (s *pbServer) Shutdown(ctx context.Context, _ *pb.ShutdownRequest) (*pb.ShutdownResponse, error) {
	err := s.impl.Shutdown(ctx)
	return &pb.ShutdownResponse{}, err
}

func (s *pbServer) OnViewVisible(ctx context.Context, req *pb.ViewVisibleRequest) (*pb.ViewVisibleResponse, error) {
	err := s.impl.OnViewVisible(ctx, req.GetViewId())
	return &pb.ViewVisibleResponse{}, err
}

func (s *pbServer) OnViewHidden(ctx context.Context, req *pb.ViewHiddenRequest) (*pb.ViewHiddenResponse, error) {
	err := s.impl.OnViewHidden(ctx, req.GetViewId())
	return &pb.ViewHiddenResponse{}, err
}

func (s *pbServer) OnTabEvent(ctx context.Context, ev *pb.TabEvent) (*pb.TabEventResponse, error) {
	err := s.impl.OnTabEvent(ctx, &TabEvent{TabKey: ev.GetTabKey(), Kind: ev.GetKind()})
	return &pb.TabEventResponse{}, err
}

func (s *pbServer) Rpc(ctx context.Context, req *pb.RpcRequest) (*pb.RpcResponse, error) {
	result, err := s.impl.Rpc(ctx, req.GetMethod(), req.GetArgsJson())
	if err != nil {
		return &pb.RpcResponse{Error: err.Error()}, nil
	}
	return &pb.RpcResponse{ResultJson: result}, nil
}

// Serve 阻塞运行插件进程 (宿主断开或调用 Shutdown 后返回)。
func Serve(impl AcePlugin) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  ProtocolVersion,
			MagicCookieKey:   MagicCookieKey,
			MagicCookieValue: MagicCookieValue,
		},
		Plugins:     map[string]plugin.Plugin{DispenseName: &grpcPlugin{impl: impl}},
		GRPCServer:  plugin.DefaultGRPCServer,
		VersionedPlugins: nil,
	})
}
