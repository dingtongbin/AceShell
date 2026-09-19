package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	app "github.com/wailsapp/wails/v3/pkg/application"

	pluginsdk "changeme/pluginsdk"
	pb "changeme/pluginsdk/proto"
)

// PluginService 插件宿主服务: 发现/启动/注册表/能力转发/GitHub 安装。
//
// 架构:
//   - 插件为独立进程, 经 hashicorp/go-plugin(gRPC+AutoMTLS) 握手, 实现协议见 proto/aceshell.proto
//   - 宿主侧 loopback gRPC(HostService) 反向提供能力, 每插件独立令牌经 metadata 鉴权
//   - 插件前端产物(dist/)由本服务的资产处理器同源伺服 /plugins/<id>/..., 宿主前端动态 import
const (
	pluginAssetPort = 8941 // 插件资产回环端口(dev 下 Vite 代理指向此端口), 与 MCP 8940 同风格固定
)

// 插件状态。
const (
	pluginStatusStarting = "starting"
	pluginStatusRunning  = "running"
	pluginStatusStopped  = "stopped"
	pluginStatusError    = "error"
	pluginStatusDisabled = "disabled"
)

var pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// PluginService 主结构。
type PluginService struct {
	app         *app.App
	cfg         *ConfigService
	sessionFile *SessionFileService

	mu        sync.Mutex
	instances map[string]*pluginInstance
	tokens    map[string]string // 插件令牌 → 插件ID
	hostSrv   *grpc.Server

	// hostVueShim 宿主 Vue shim 源码(main.go 从内嵌前端产物注入;
	// 插件 ESM 经 import map 引用, 保证全页面唯一 Vue 实例)。
	hostVueShim string
	hostAddr  string
	assetLn   net.Listener
	logFile   *os.File
	logMu     sync.Mutex
	stopped   bool
}

type pluginInstance struct {
	id       string
	dir      string
	exePath  string
	manifest pluginManifest
	client   *plugin.Client
	api      *pluginsdk.PluginClient
	info     *pluginsdk.PluginInfo
	status   string
	errMsg   string
	done     chan struct{}
}

// pluginManifest 安装清单(安装期校验 + 元数据兜底;运行期真值以握手 Info() 为准)。
type pluginManifest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	MinHost string `json:"minHost"`
}

type pluginCandidate struct {
	id       string
	dir      string
	exePath  string
	manifest pluginManifest
}

// pluginSummary 注册表快照条目(前端消费)。
type pluginSummary struct {
	ID          string              `json:"id"`
	DisplayName string              `json:"displayName"`
	Version     string              `json:"version"`
	Icon        string              `json:"icon,omitempty"`
	AccentColor string              `json:"accentColor,omitempty"`
	Status      string              `json:"status"`
	Error       string              `json:"error,omitempty"`
	Bundled     bool                `json:"bundled"`
	Views       []pluginViewSummary `json:"views,omitempty"`
}

type pluginViewSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Icon        string `json:"icon,omitempty"`
	ComponentID string `json:"componentId"`
}

// NewPluginService 构造插件宿主服务。
// NewPluginService hostVueShim 为宿主 Vue shim 源码(main.go 从内嵌前端产物
// plugins-host-vue.js 读取; 构建时由 vite 插件从宿主安装的 vue 自动生成导出面;
// 空串则 AssetHandler 退回内置兜底清单)。
func NewPluginService(cfg *ConfigService, sessionFile *SessionFileService, hostVueShim string) *PluginService {
	return &PluginService{
		cfg:         cfg,
		sessionFile: sessionFile,
		instances:   map[string]*pluginInstance{},
		tokens:      map[string]string{},
		hostVueShim: hostVueShim,
	}
}

// SetApp 注入应用实例(事件推送依赖)。
func (s *PluginService) SetApp(app *app.App) { s.app = app }

// StartAll 启动反向服务并加载全部插件。单插件失败不阻塞其余插件与主窗口。
func (s *PluginService) StartAll() {
	_ = os.MkdirAll(PluginsDir(), 0700)

	// 捆绑插件落盘/升级(须在扫描之前)
	s.ensureBundledPlugins()

	// 宿主反向能力服务(HostService): loopback 随机端口, 每插件独立令牌
	if lis, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		s.hostSrv = grpc.NewServer(grpc.UnaryInterceptor(s.authInterceptor))
		pb.RegisterHostServiceServer(s.hostSrv, &hostServiceServer{svc: s})
		s.hostAddr = lis.Addr().String()
		go func() { _ = s.hostSrv.Serve(lis) }()
	} else {
		s.logLine("host gRPC 启动失败: " + err.Error())
	}

	// 插件资产回环服务: dev 模式 Vite 代理 /plugins → 此端口; 生产走 main.go 进程内直连
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", pluginAssetPort)); err == nil {
		s.assetLn = ln
		go func() { _ = http.Serve(ln, s.AssetHandler()) }()
	} else {
		s.logLine(fmt.Sprintf("资产端口 %d 占用, dev 代理不可用: %v", pluginAssetPort, err))
	}

	if f, err := os.OpenFile(filepath.Join(DataDir(), "plugin-runs.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
		s.logFile = f
	}

	candidates := s.scanPluginsDir()
	var wg sync.WaitGroup
	for _, cand := range candidates {
		if !s.cfg.PluginEnabled(cand.id) {
			s.mu.Lock()
			s.instances[cand.id] = &pluginInstance{
				id: cand.id, dir: cand.dir, exePath: cand.exePath,
				manifest: cand.manifest, status: pluginStatusDisabled,
				done: make(chan struct{}),
			}
			s.mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(c pluginCandidate) {
			defer wg.Done()
			_ = s.launch(c)
		}(cand)
	}
	wg.Wait()
	s.emitRegistry()
}

// StopAll 优雅收尾: 通知插件 Shutdown 后结束进程并关闭反向服务。
func (s *PluginService) StopAll() {
	s.mu.Lock()
	s.stopped = true
	instances := make([]*pluginInstance, 0, len(s.instances))
	for _, inst := range s.instances {
		instances = append(instances, inst)
	}
	assetLn, hostSrv := s.assetLn, s.hostSrv
	s.assetLn = nil
	s.mu.Unlock()

	for _, inst := range instances {
		s.stopInstance(inst)
	}
	if hostSrv != nil {
		hostSrv.Stop()
	}
	if assetLn != nil {
		_ = assetLn.Close()
	}
	s.mu.Lock()
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
	s.mu.Unlock()
}

// AssetHandler 返回插件前端资产处理器(同源伺服 /plugins/<id>/...)。
// 路径白名单式清洗;禁用目录列表;no-store 保证插件更新后前端拿到新模块。
// 每请求一行日志(plugin-runs.log), 供"插件组件加载失败"类问题定位。
func (s *PluginService) AssetHandler() http.Handler {
	root := PluginsDir()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		if r.URL.Path == hostVueShimPath {
			serveHostVueShim(rec, r, s.hostVueShim)
		} else {
			serveAssetFile(rec, r, root)
		}
		s.logLine(fmt.Sprintf("asset %s %s -> %d", r.Method, r.URL.Path, rec.status))
	})
}

// hostVueShimPath 插件共享依赖契约的伺服路径(见 frontend/index.html 的 import map)。
const hostVueShimPath = "/plugins/_host/vue.js"

// serveHostVueShim 伺服宿主 Vue shim(导出面由宿主构建时自动生成;
// 产物缺失时退回内置最小清单, 覆盖 SFC 编译产物的常用导入面)。
func serveHostVueShim(w http.ResponseWriter, r *http.Request, shim string) {
	if strings.TrimSpace(shim) == "" {
		shim = fallbackHostVueShim
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "vue.js", time.Time{}, strings.NewReader(shim))
}

// fallbackHostVueShim 兜底 shim(宿主构建产物缺失时使用; 名单 = SFC 编译产物
// 与常规插件代码的并集)。正常路径不经过此清单 —— 以构建生成的完整导出面为准。
const fallbackHostVueShim = `/* 宿主 Vue shim(内置兜底清单) */
const V = globalThis.__ACESHELL_VUE__
if (!V) throw new Error('[AceShell 插件] 宿主 Vue 未注册(__ACESHELL_VUE__), 宿主版本过旧, 请升级 AceShell')
export default V
export const EffectScope = V.EffectScope
export const Fragment = V.Fragment
export const ReactiveEffect = V.ReactiveEffect
export const Static = V.Static
export const Suspense = V.Suspense
export const Teleport = V.Teleport
export const Text = V.Text
export const Transition = V.Transition
export const TransitionGroup = V.TransitionGroup
export const KeepAlive = V.KeepAlive
export const BaseTransition = V.BaseTransition
export const Comment = V.Comment
export const camelize = V.camelize
export const capitalize = V.capitalize
export const callWithAsyncErrorHandling = V.callWithAsyncErrorHandling
export const callWithErrorHandling = V.callWithErrorHandling
export const cloneVNode = V.cloneVNode
export const compile = V.compile
export const computed = V.computed
export const createApp = V.createApp
export const createBlock = V.createBlock
export const createCommentVNode = V.createCommentVNode
export const createElementBlock = V.createElementBlock
export const createElementVNode = V.createElementVNode
export const createHydrationRenderer = V.createHydrationRenderer
export const createPropsRestProxy = V.createPropsRestProxy
export const createRenderer = V.createRenderer
export const createSSRApp = V.createSSRApp
export const createSlots = V.createSlots
export const createStaticVNode = V.createStaticVNode
export const createTextVNode = V.createTextVNode
export const createVNode = V.createVNode
export const customRef = V.customRef
export const defineAsyncComponent = V.defineAsyncComponent
export const defineComponent = V.defineComponent
export const defineCustomElement = V.defineCustomElement
export const defineEmits = V.defineEmits
export const defineExpose = V.defineExpose
export const defineModel = V.defineModel
export const defineOptions = V.defineOptions
export const defineProps = V.defineProps
export const defineSSRCustomElement = V.defineSSRCustomElement
export const defineSlots = V.defineSlots
export const devtools = V.devtools
export const effect = V.effect
export const effectScope = V.effectScope
export const getCurrentInstance = V.getCurrentInstance
export const getCurrentScope = V.getCurrentScope
export const getTransitionRawChildren = V.getTransitionRawChildren
export const guardReactiveProps = V.guardReactiveProps
export const h = V.h
export const handleError = V.handleError
export const hasInjectionContext = V.hasInjectionContext
export const hydrate = V.hydrate
export const hydrateOnIdle = V.hydrateOnIdle
export const hydrateOnInteraction = V.hydrateOnInteraction
export const hydrateOnMediaQuery = V.hydrateOnMediaQuery
export const hydrateOnVisible = V.hydrateOnVisible
export const initCustomFormatter = V.initCustomFormatter
export const initDirectivesForSSR = V.initDirectivesForSSR
export const inject = V.inject
export const isMemoSame = V.isMemoSame
export const isProxy = V.isProxy
export const isReactive = V.isReactive
export const isReadonly = V.isReadonly
export const isRef = V.isRef
export const isRuntimeOnly = V.isRuntimeOnly
export const isShallow = V.isShallow
export const isVNode = V.isVNode
export const markRaw = V.markRaw
export const mergeDefaults = V.mergeDefaults
export const mergeModels = V.mergeModels
export const mergeProps = V.mergeProps
export const nextTick = V.nextTick
export const nodeOps = V.nodeOps
export const normalizeClass = V.normalizeClass
export const normalizeProps = V.normalizeProps
export const normalizeStyle = V.normalizeStyle
export const onActivated = V.onActivated
export const onBeforeMount = V.onBeforeMount
export const onBeforeUnmount = V.onBeforeUnmount
export const onBeforeUpdate = V.onBeforeUpdate
export const onDeactivated = V.onDeactivated
export const onErrorCaptured = V.onErrorCaptured
export const onMounted = V.onMounted
export const onRenderTracked = V.onRenderTracked
export const onRenderTriggered = V.onRenderTriggered
export const onScopeDispose = V.onScopeDispose
export const onServerPrefetch = V.onServerPrefetch
export const onUnmounted = V.onUnmounted
export const onUpdated = V.onUpdated
export const onWatcherCleanup = V.onWatcherCleanup
export const openBlock = V.openBlock
export const patchProp = V.patchProp
export const popScopeId = V.popScopeId
export const provide = V.provide
export const proxyRefs = V.proxyRefs
export const pushScopeId = V.pushScopeId
export const queuePostFlushCb = V.queuePostFlushCb
export const reactive = V.reactive
export const readonly = V.readonly
export const ref = V.ref
export const registerRuntimeCompiler = V.registerRuntimeCompiler
export const render = V.render
export const renderList = V.renderList
export const renderSlot = V.renderSlot
export const resolveComponent = V.resolveComponent
export const resolveDirective = V.resolveDirective
export const resolveDynamicComponent = V.resolveDynamicComponent
export const resolveFilter = V.resolveFilter
export const resolveTransitionHooks = V.resolveTransitionHooks
export const setBlockTracking = V.setBlockTracking
export const setDevtoolsHook = V.setDevtoolsHook
export const setTransitionHooks = V.setTransitionHooks
export const shallowReactive = V.shallowReactive
export const shallowReadonly = V.shallowReadonly
export const shallowRef = V.shallowRef
export const ssrContextKey = V.ssrContextKey
export const stop = V.stop
export const toDisplayString = V.toDisplayString
export const toHandlerKey = V.toHandlerKey
export const toHandlers = V.toHandlers
export const toRaw = V.toRaw
export const toRef = V.toRef
export const toRefs = V.toRefs
export const toValue = V.toValue
export const transformVNodeArgs = V.transformVNodeArgs
export const triggerRef = V.triggerRef
export const unref = V.unref
export const useAttrs = V.useAttrs
export const useCssModule = V.useCssModule
export const useCssVars = V.useCssVars
export const useHost = V.useHost
export const useId = V.useId
export const useModel = V.useModel
export const useSSRContext = V.useSSRContext
export const useShadowRoot = V.useShadowRoot
export const useSlots = V.useSlots
export const useTemplateRef = V.useTemplateRef
export const useTransitionState = V.useTransitionState
export const vModelCheckbox = V.vModelCheckbox
export const vModelDynamic = V.vModelDynamic
export const vModelRadio = V.vModelRadio
export const vModelSelect = V.vModelSelect
export const vModelText = V.vModelText
export const vShow = V.vShow
export const version = V.version
export const warn = V.warn
export const watch = V.watch
export const watchEffect = V.watchEffect
export const watchPostEffect = V.watchPostEffect
export const watchSyncEffect = V.watchSyncEffect
export const withAsyncContext = V.withAsyncContext
export const withCtx = V.withCtx
export const withDefaults = V.withDefaults
export const withDirectives = V.withDirectives
export const withKeys = V.withKeys
export const withMemo = V.withMemo
export const withModifiers = V.withModifiers
export const withScopeId = V.withScopeId
`

// serveAssetFile 单个插件资产请求(路径清洗 + 目录拒绝 + no-store)。
func serveAssetFile(w http.ResponseWriter, r *http.Request, root string) {
	rel := strings.TrimPrefix(r.URL.Path, "/plugins/")
	if rel == "" || strings.HasSuffix(rel, "/") {
		http.NotFound(w, r)
		return
	}
	cleaned := path.Clean("/" + rel)
	cleaned = strings.TrimPrefix(cleaned, "/")
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, full)
}

// statusRecorder 捕获响应码(仅日志用)。
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

// ==================== 发现与启动 ====================

// scanPluginsDir 扫描插件目录: 每个子目录须含合法 plugin.json 与平台可执行文件。
func (s *PluginService) scanPluginsDir() []pluginCandidate {
	entries, err := os.ReadDir(PluginsDir())
	if err != nil {
		return nil
	}
	out := []pluginCandidate{}
	for _, e := range entries {
		if !e.IsDir() || !pluginIDRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(PluginsDir(), e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err != nil {
			s.logLine(e.Name() + ": 缺少 plugin.json, 跳过")
			continue
		}
		var mf pluginManifest
		if json.Unmarshal(raw, &mf) != nil || mf.ID != e.Name() || !pluginIDRe.MatchString(mf.ID) {
			s.logLine(e.Name() + ": plugin.json 无效或 ID 与目录名不一致, 跳过")
			continue
		}
		exeName := mf.ID
		if runtime.GOOS == "windows" {
			exeName += ".exe"
		}
		exePath := filepath.Join(dir, exeName)
		if st, err := os.Stat(exePath); err != nil || st.IsDir() {
			s.logLine(mf.ID + ": 缺少可执行文件 " + exeName + ", 跳过")
			continue
		}
		out = append(out, pluginCandidate{id: mf.ID, dir: dir, exePath: exePath, manifest: mf})
	}
	return out
}

// launch 启动单个插件进程并完成握手/注册。已存在同 ID 实例时先停止旧实例。
func (s *PluginService) launch(cand pluginCandidate) error {
	s.mu.Lock()
	if old := s.instances[cand.id]; old != nil {
		s.mu.Unlock()
		s.stopInstance(old)
	} else {
		s.mu.Unlock()
	}

	_ = os.MkdirAll(PluginDataDir(cand.id), 0700)

	token, err := randomToken()
	if err != nil {
		return err
	}

	cmd := exec.Command(cand.exePath)
	HideWindow(cmd)
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  pluginsdk.ProtocolVersion,
			MagicCookieKey:   pluginsdk.MagicCookieKey,
			MagicCookieValue: pluginsdk.MagicCookieValue,
		},
		Plugins:          map[string]plugin.Plugin{pluginsdk.DispenseName: &pluginsdk.HostGRPCPlugin{}},
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		AutoMTLS:         true,
		StartTimeout:     15 * time.Second,
		Logger:           hclog.NewNullLogger(),
		SyncStdout:       &pluginLogWriter{svc: s, prefix: "[" + cand.id + "] "},
		SyncStderr:       &pluginLogWriter{svc: s, prefix: "[" + cand.id + "][err] "},
	})

	inst := &pluginInstance{
		id: cand.id, dir: cand.dir, exePath: cand.exePath,
		manifest: cand.manifest, status: pluginStatusStarting,
		done: make(chan struct{}),
	}

	fail := func(err error) error {
		inst.status = pluginStatusError
		inst.errMsg = err.Error()
		client.Kill()
		s.mu.Lock()
		s.instances[cand.id] = inst
		s.mu.Unlock()
		s.emitRegistry()
		return err
	}

	rpcClient, err := client.Client()
	if err != nil {
		return fail(fmt.Errorf("握手失败: %w", err))
	}
	raw, err := rpcClient.Dispense(pluginsdk.DispenseName)
	if err != nil {
		return fail(fmt.Errorf("取用失败: %w", err))
	}
	api := raw.(*pluginsdk.PluginClient)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := api.Info(ctx)
	if err != nil {
		return fail(fmt.Errorf("Info 失败: %w", err))
	}
	if info.ID != cand.id {
		return fail(fmt.Errorf("插件上报 ID %q 与安装目录 %q 不一致", info.ID, cand.id))
	}
	if info.DisplayName == "" {
		info.DisplayName = cand.manifest.Name
	}
	if info.Version == "" {
		info.Version = cand.manifest.Version
	}

	sctx, scancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer scancel()
	s.mu.Lock()
	tokenRegistered := s.hostSrv != nil
	if tokenRegistered {
		s.tokens[token] = cand.id
	}
	hostAddr := s.hostAddr
	s.mu.Unlock()
	if err := api.Start(sctx, &pluginsdk.HostContext{
		HostVersion:  AppVersion,
		DataDir:      PluginDataDir(cand.id),
		HostEndpoint: hostAddr,
		Token:        token,
	}); err != nil {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return fail(fmt.Errorf("Start 失败: %w", err))
	}

	inst.client = client
	inst.api = api
	inst.info = info
	inst.status = pluginStatusRunning

	s.mu.Lock()
	s.instances[cand.id] = inst
	stoppedAlready := s.stopped
	s.mu.Unlock()
	if stoppedAlready {
		s.stopInstance(inst)
		return nil
	}

	// 进程退出监视: 非受控退出标记错误并刷新注册表(图标随视图消失)
	// (受控停止时 status 已先行置为 stopped/error 之外的值, 见 stopInstance)
	go func() {
		select {
		case <-api.Dead():
			s.mu.Lock()
			if inst.status == pluginStatusRunning || inst.status == pluginStatusStarting {
				inst.status = pluginStatusError
				inst.errMsg = "插件进程意外退出"
			}
			s.mu.Unlock()
			s.emitRegistry()
		case <-inst.done:
		}
	}()

	s.emitRegistry()
	return nil
}

// stopInstance 优雅停止单个插件(Shutdown 钩子 + 进程终止)。
// 先置终态再 Kill: 让 Dead() 监视者能区分受控停止与崩溃。
func (s *PluginService) stopInstance(inst *pluginInstance) {
	s.mu.Lock()
	if inst.status != pluginStatusDisabled {
		inst.status = pluginStatusStopped
	}
	inst.errMsg = ""
	// 吊销令牌
	for tok, id := range s.tokens {
		if id == inst.id {
			delete(s.tokens, tok)
		}
	}
	s.mu.Unlock()

	if inst.api != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = inst.api.Shutdown(ctx)
		cancel()
	}
	if inst.client != nil {
		inst.client.Kill()
	}
	if inst.done != nil {
		select {
		case <-inst.done:
		default:
			close(inst.done)
		}
	}
	s.mu.Lock()
	inst.api = nil
	inst.client = nil
	s.mu.Unlock()
}

// ==================== 注册表与事件 ====================

// snapshot 注册表快照(调用方持锁)。已卸载的捆绑插件以 "uninstalled" 伪状态出现,
// 供设置页展示"恢复"入口。
func (s *PluginService) snapshotLocked() []pluginSummary {
	out := make([]pluginSummary, 0, len(s.instances))
	for _, inst := range s.instances {
		sum := pluginSummary{
			ID:          inst.id,
			DisplayName: inst.manifest.Name,
			Version:     inst.manifest.Version,
			Status:      inst.status,
			Error:       inst.errMsg,
			Bundled:     s.isBundled(inst.id),
		}
		if inst.info != nil {
			sum.DisplayName = firstNonEmpty(inst.info.DisplayName, sum.DisplayName)
			sum.Version = firstNonEmpty(inst.info.Version, sum.Version)
			sum.Icon = inst.info.Icon
			sum.AccentColor = inst.info.AccentColor
			for _, v := range inst.info.Views {
				sum.Views = append(sum.Views, pluginViewSummary{ID: v.ID, Title: v.Title, Icon: v.Icon, ComponentID: v.ComponentID})
			}
		}
		out = append(out, sum)
	}
	// 已卸载捆绑插件(目录已删, 元数据来自内置载荷)
	for _, mf := range bundledManifests() {
		if _, alive := s.instances[mf.ID]; alive {
			continue
		}
		if !s.cfg.BundledUninstalled(mf.ID) {
			continue
		}
		out = append(out, pluginSummary{
			ID: mf.ID, DisplayName: mf.Name, Version: mf.Version,
			Status: "uninstalled", Bundled: true,
		})
	}
	return out
}

// PluginLoadReport 前端上报插件组件加载结果(诊断日志;ok=false 时 errMsg 为原因)。
func (s *PluginService) PluginLoadReport(pluginID string, componentID string, ok bool, errMsg string) string {
	if ok {
		s.logLine(fmt.Sprintf("component %s/%s -> ok", pluginID, componentID))
	} else {
		s.logLine(fmt.Sprintf("component %s/%s -> FAIL: %s", pluginID, componentID, errMsg))
	}
	return "{}"
}

// PluginList 返回注册表快照 JSON。
func (s *PluginService) PluginList() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(map[string]any{"plugins": s.snapshotLocked()})
	return string(data)
}

// emitRegistry 推送注册表变更。
func (s *PluginService) emitRegistry() {
	s.mu.Lock()
	data, err := json.Marshal(map[string]any{"plugins": s.snapshotLocked()})
	s.mu.Unlock()
	if err == nil {
		s.emit("plugin-registry-changed", string(data))
	}
}

// emit 事件推送(拷贝自 McpService 的房式约定)。
func (s *PluginService) emit(name string, payload any) {
	if s.app == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.app.Event.Emit(name, string(data))
}

// ==================== 前端绑定 ====================

// PluginCall 转发插件前端 → 插件后端的业务调用 (30s 超时)。
func (s *PluginService) PluginCall(pluginID string, method string, argsJSON string) string {
	inst := s.getInstance(pluginID)
	if inst == nil || inst.api == nil || inst.status != pluginStatusRunning {
		return `{"error":"插件未运行: ` + pluginID + `"}`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := inst.api.Rpc(ctx, method, argsJSON)
	if err != nil {
		return `{"error":` + mustJSONString(err.Error()) + `}`
	}
	if result == "" {
		return "{}"
	}
	return result
}

// PluginOpenTab 插件前端直达开签页入口 (与 HostService.OpenTab 同路)。
func (s *PluginService) PluginOpenTab(pluginID string, specJSON string) string {
	inst := s.getInstance(pluginID)
	if inst == nil || inst.status != pluginStatusRunning {
		return `{"error":"插件未运行: ` + pluginID + `"}`
	}
	var spec struct {
		TabKey      string         `json:"tabKey"`
		Title       string         `json:"title"`
		ComponentID string         `json:"componentId"`
		Props       map[string]any `json:"props"`
		Icon        string         `json:"icon"`
		Color       string         `json:"color"`
	}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil || spec.TabKey == "" || spec.ComponentID == "" {
		return `{"error":"spec 无效: 需要 tabKey 与 componentId"}`
	}
	tabID := pluginTabID(pluginID, spec.TabKey)
	s.emit("plugin-open-tab", map[string]any{
		"pluginID":    pluginID,
		"tabId":       tabID,
		"tabKey":      spec.TabKey,
		"title":       firstNonEmpty(spec.Title, inst.info.DisplayName),
		"componentId": spec.ComponentID,
		"props":       spec.Props,
		"icon":        spec.Icon,
		"color":       firstNonEmpty(spec.Color, inst.info.AccentColor),
	})
	return mustJSON(map[string]any{"tabId": tabID})
}

// PluginNotifyTabEvent 前端回传标签页生命周期事件 (opened/activated/closed)。
func (s *PluginService) PluginNotifyTabEvent(pluginID string, tabKey string, kind string) string {
	inst := s.getInstance(pluginID)
	if inst == nil || inst.api == nil || inst.status != pluginStatusRunning || tabKey == "" {
		return "{}"
	}
	switch kind {
	case "opened", "activated", "closed":
	default:
		return "{}"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = inst.api.OnTabEvent(ctx, &pluginsdk.TabEvent{TabKey: tabKey, Kind: kind})
	}()
	return "{}"
}

// PluginSetViewVisible 侧栏面板显隐上报。
func (s *PluginService) PluginSetViewVisible(pluginID string, viewID string, visible bool) string {
	inst := s.getInstance(pluginID)
	if inst == nil || inst.api == nil || inst.status != pluginStatusRunning || viewID == "" {
		return "{}"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if visible {
			_ = inst.api.OnViewVisible(ctx, viewID)
		} else {
			_ = inst.api.OnViewHidden(ctx, viewID)
		}
	}()
	return "{}"
}

// PluginSetEnabled 启用/禁用插件(持久化 + 热生效)。
func (s *PluginService) PluginSetEnabled(pluginID string, enabled bool) string {
	inst := s.getInstance(pluginID)
	if inst == nil {
		return `{"error":"未知插件: ` + pluginID + `"}`
	}
	out := s.cfg.SetPluginEnabled(pluginID, enabled)
	if enabled {
		cand := pluginCandidate{id: inst.id, dir: inst.dir, exePath: inst.exePath, manifest: inst.manifest}
		go func() {
			_ = s.launch(cand)
			s.emitRegistry()
		}()
	} else {
		// 显式置为 disabled(而非 stopped): 注册表/前端开关以该状态为唯一真值。
		// 若落成 stopped, 前端推导的开关恒为"已启用", 与配置脱节导致无法再开启。
		s.mu.Lock()
		inst.status = pluginStatusDisabled
		s.mu.Unlock()
		s.stopInstance(inst)
		s.emitRegistry()
	}
	return out
}

// PluginReload 重载单个插件(开发调试用)。
func (s *PluginService) PluginReload(pluginID string) string {
	inst := s.getInstance(pluginID)
	if inst == nil {
		return `{"error":"未知插件: ` + pluginID + `"}`
	}
	cand := pluginCandidate{id: inst.id, dir: inst.dir, exePath: inst.exePath, manifest: inst.manifest}
	s.stopInstance(inst)
	if !s.cfg.PluginEnabled(pluginID) {
		s.emitRegistry()
		return "{}"
	}
	go func() {
		_ = s.launch(cand)
		s.emitRegistry()
	}()
	return "{}"
}

// PluginOpenDir 打开插件安装目录(资源管理器)。
func (s *PluginService) PluginOpenDir() string {
	_ = os.MkdirAll(PluginsDir(), 0700)
	exec.Command("explorer.exe", PluginsDir()).Start()
	return "{}"
}

// getInstance 取运行中实例(快照)。
func (s *PluginService) getInstance(pluginID string) *pluginInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instances[pluginID]
}

// ==================== 宿主反向能力服务 ====================

type pluginIDKey struct{}

type hostServiceServer struct {
	pb.UnimplementedHostServiceServer
	svc *PluginService
}

// authInterceptor 令牌 → 插件ID 鉴权并注入上下文。
func (s *PluginService) authInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	tok := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vs := md.Get("x-aceshell-token"); len(vs) > 0 {
			tok = vs[0]
		}
	}
	s.mu.Lock()
	pluginID, ok := s.tokens[tok]
	s.mu.Unlock()
	if !ok || pluginID == "" {
		return nil, status.Error(codes.Unauthenticated, "无效插件令牌")
	}
	return handler(context.WithValue(ctx, pluginIDKey{}, pluginID), req)
}

func callerID(ctx context.Context) string {
	id, _ := ctx.Value(pluginIDKey{}).(string)
	return id
}

func (h *hostServiceServer) OpenTab(ctx context.Context, req *pb.OpenTabRequest) (*pb.OpenTabResponse, error) {
	spec := req.GetSpec()
	if spec.GetTabKey() == "" || spec.GetComponentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tabKey 与 componentId 必填")
	}
	pluginID := callerID(ctx)
	tabID := pluginTabID(pluginID, spec.GetTabKey())
	title := spec.GetTitle()
	color := spec.GetColor()
	var props map[string]any
	_ = json.Unmarshal([]byte(spec.GetPropsJson()), &props)
	h.svc.mu.Lock()
	if inst := h.svc.instances[pluginID]; inst != nil && inst.info != nil {
		if title == "" {
			title = inst.info.DisplayName
		}
		if color == "" {
			color = inst.info.AccentColor
		}
	}
	h.svc.mu.Unlock()
	h.svc.emit("plugin-open-tab", map[string]any{
		"pluginID":    pluginID,
		"tabId":       tabID,
		"tabKey":      spec.GetTabKey(),
		"title":       title,
		"componentId": spec.GetComponentId(),
		"props":       props,
		"icon":        spec.GetIcon(),
		"color":       color,
	})
	return &pb.OpenTabResponse{TabId: tabID}, nil
}

func (h *hostServiceServer) CloseTab(ctx context.Context, req *pb.CloseTabRequest) (*pb.CloseTabResponse, error) {
	pluginID := callerID(ctx)
	if req.GetTabKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "tabKey 必填")
	}
	h.svc.emit("plugin-tab-closed", map[string]any{"pluginID": pluginID, "tabKey": req.GetTabKey()})
	return &pb.CloseTabResponse{Closed: true}, nil
}

func (h *hostServiceServer) SetTabTitle(ctx context.Context, req *pb.SetTabTitleRequest) (*pb.SetTabTitleResponse, error) {
	pluginID := callerID(ctx)
	if req.GetTabKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "tabKey 必填")
	}
	h.svc.emit("plugin-tab-updated", map[string]any{"pluginID": pluginID, "tabKey": req.GetTabKey(), "title": req.GetTitle()})
	return &pb.SetTabTitleResponse{}, nil
}

func (h *hostServiceServer) ShowToast(ctx context.Context, req *pb.ShowToastRequest) (*pb.ShowToastResponse, error) {
	pluginID := callerID(ctx)
	level := req.GetLevel()
	switch level {
	case "info", "warning", "error", "success":
	default:
		level = "info"
	}
	h.svc.emit("plugin-toast", map[string]any{"pluginID": pluginID, "message": req.GetMessage(), "level": level})
	return &pb.ShowToastResponse{}, nil
}

func (h *hostServiceServer) ListSessions(ctx context.Context, _ *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	_ = callerID(ctx)
	return &pb.ListSessionsResponse{SessionsJson: h.svc.sessionFile.GetTree()}, nil
}

func (h *hostServiceServer) EmitUIEvent(ctx context.Context, req *pb.EmitUIEventRequest) (*pb.EmitUIEventResponse, error) {
	pluginID := callerID(ctx)
	if strings.TrimSpace(req.GetPayloadJson()) == "" {
		return nil, status.Error(codes.InvalidArgument, "payloadJson 必填")
	}
	// payload 原样透传(限长 256KB + 必须合法 JSON, 防误用拖垮前端事件通道)
	if len(req.GetPayloadJson()) > 256<<10 {
		return nil, status.Error(codes.InvalidArgument, "payload 过大")
	}
	if !json.Valid([]byte(req.GetPayloadJson())) {
		return nil, status.Error(codes.InvalidArgument, "payload 必须是合法 JSON")
	}
	h.svc.emit("plugin-event", map[string]any{"pluginID": pluginID, "payload": json.RawMessage(req.GetPayloadJson())})
	return &pb.EmitUIEventResponse{}, nil
}

// pluginTabID 确定性标签页 ID: tabKey 即幂等键。
func pluginTabID(pluginID, tabKey string) string {
	return "plugin://" + pluginID + "/" + tabKey
}

// ==================== 杂项 ====================

// pluginLogWriter 插件 stdout/stderr → 数据目录 plugin-runs.log。
type pluginLogWriter struct {
	svc    *PluginService
	prefix string
}

func (w *pluginLogWriter) Write(p []byte) (int, error) {
	w.svc.logMu.Lock()
	defer w.svc.logMu.Unlock()
	if w.svc.logFile == nil {
		return len(p), nil
	}
	// 每行时间戳: "面板白屏/卡死"类问题定位依赖请求与加载事件的精确时序。
	ts := time.Now().Format("01-02 15:04:05.000")
	if n, err := w.svc.logFile.WriteString(ts + " " + w.prefix + string(p)); err != nil {
		return n, err
	}
	return len(p), nil
}

func (s *PluginService) logLine(line string) {
	w := &pluginLogWriter{svc: s}
	_, _ = w.Write([]byte(line + "\n"))
}

// FrontendDiagLog 前端诊断事件落盘(JS 全局错误、插件模块 import 看门狗等),
// 与插件资产请求同写 plugin-runs.log, 供时序对账。
func (s *PluginService) FrontendDiagLog(msg string) {
	msg = strings.ReplaceAll(strings.TrimSpace(msg), "\n", " | ")
	if msg == "" {
		return
	}
	if len(msg) > 4096 {
		msg = msg[:4096]
	}
	s.logLine("[frontend] " + msg)
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func mustJSONString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(data)
}
