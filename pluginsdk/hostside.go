package pluginsdk

import (
	"context"
	"errors"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pb "changeme/pluginsdk/proto"
)

// HostGRPCPlugin 宿主侧 Dispense 适配器: pluginservice 用它从插件进程取 AcePlugin 客户端。
type HostGRPCPlugin struct {
	plugin.NetRPCUnsupportedPlugin
}

// GRPCServer 宿主不实现 AcePlugin 服务。
func (p *HostGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	return nil
}

// GRPCClient 插件进程侧的 AcePlugin 服务客户端。
// go-plugin 在插件进程退出时取消传入的 ctx, 借此实现崩溃即时感知。
func (p *HostGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	dead := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(dead)
	}()
	return &PluginClient{raw: pb.NewAcePluginClient(c), dead: dead}, nil
}

// PluginClient 宿主持有的插件 API 客户端 (带默认超时封装)。
type PluginClient struct {
	raw  pb.AcePluginClient
	dead chan struct{}
}

// Dead 进程退出(或宿主主动 Kill)后关闭的通道。
func (c *PluginClient) Dead() <-chan struct{} { return c.dead }

// Raw 暴露生成的 gRPC 客户端 (需要自定义超时/拦截器时用)。
func (c *PluginClient) Raw() pb.AcePluginClient { return c.raw }

func (c *PluginClient) Info(ctx context.Context) (*PluginInfo, error) {
	resp, err := c.raw.Info(ctx, &pb.InfoRequest{})
	if err != nil {
		return nil, err
	}
	out := &PluginInfo{
		ID:          resp.GetId(),
		DisplayName: resp.GetDisplayName(),
		Version:     resp.GetVersion(),
		Icon:        resp.GetIcon(),
		AccentColor: resp.GetAccentColor(),
	}
	for _, v := range resp.GetViews() {
		out.Views = append(out.Views, ViewInfo{ID: v.GetId(), Title: v.GetTitle(), Icon: v.GetIcon(), ComponentID: v.GetComponentId()})
	}
	return out, nil
}

func (c *PluginClient) Start(ctx context.Context, host *HostContext) error {
	_, err := c.raw.Start(ctx, &pb.StartRequest{
		HostVersion:  host.HostVersion,
		DataDir:      host.DataDir,
		HostEndpoint: host.HostEndpoint,
		Token:        host.Token,
	})
	return err
}

func (c *PluginClient) Shutdown(ctx context.Context) error {
	_, err := c.raw.Shutdown(ctx, &pb.ShutdownRequest{})
	return err
}

func (c *PluginClient) OnViewVisible(ctx context.Context, viewID string) error {
	_, err := c.raw.OnViewVisible(ctx, &pb.ViewVisibleRequest{ViewId: viewID})
	return err
}

func (c *PluginClient) OnViewHidden(ctx context.Context, viewID string) error {
	_, err := c.raw.OnViewHidden(ctx, &pb.ViewHiddenRequest{ViewId: viewID})
	return err
}

func (c *PluginClient) OnTabEvent(ctx context.Context, ev *TabEvent) error {
	_, err := c.raw.OnTabEvent(ctx, &pb.TabEvent{TabKey: ev.TabKey, Kind: ev.Kind})
	return err
}

func (c *PluginClient) Rpc(ctx context.Context, method, argsJSON string) (string, error) {
	resp, err := c.raw.Rpc(ctx, &pb.RpcRequest{Method: method, ArgsJson: argsJSON})
	if err != nil {
		return "", err
	}
	if resp.GetError() != "" {
		return "", errors.New(resp.GetError())
	}
	return resp.GetResultJson(), nil
}
