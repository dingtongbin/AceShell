package services

// LLM 客户端(基于 OpenAI 官方 Go SDK github.com/openai/openai-go)。
// 对外保留轻量消息/工具类型(agentservice 与事件持久化使用),
// SDK 仅在 Chat/ListModels 内部出现。
//
// 兼容目标: OpenAI / DeepSeek / 通义千问(兼容模式) / 智谱 GLM /
// Kimi / 各类 OpenAI 兼容网关(Anthropic 亦经网关接入)。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const agentLLMCallTimeout = 5 * time.Minute // 单次 LLM 调用上限(流式长回答)

// agentClient OpenAI 兼容客户端(SDK 封装)。
type agentClient struct {
	client openai.Client
}

// newAgentClient 创建客户端(baseURL 形如 https://api.openai.com/v1)。
func newAgentClient(baseURL, apiKey string) *agentClient {
	return &agentClient{
		client: openai.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(strings.TrimRight(strings.TrimSpace(baseURL), "/")),
		),
	}
}

// chatMessage OpenAI 消息格式(服务层轻量表示)。
type chatMessage struct {
	Role       string         `json:"role"` // system / user / assistant / tool
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatToolCall OpenAI 工具调用格式(assistant 消息内)。
type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // 固定 "function"
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// agentToolCall 简化工具调用(事件持久化用)。
type agentToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatTool 工具定义。
type chatTool struct {
	Type     string     `json:"type"` // 固定 "function"
	Function chatToolFn `json:"function"`
}

type chatToolFn struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// chatRequest 对话请求。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

// chatUsage 单次 LLM 调用 token 用量(输入按缓存命中/未命中拆分)。
type chatUsage struct {
	PromptTokens     int64 // 输入合计(含缓存命中)
	CachedTokens     int64 // 缓存命中输入
	CompletionTokens int64 // 输出
	TotalTokens      int64 // 总量(判断网关是否返回 usage)
}

// chatResult 一次完整对话返回(流式增量累积后)。
type chatResult struct {
	Content   string
	Reasoning string // 思考内容(网关 reasoning_content;未返回则为空)
	ToolCalls []agentToolCall
	Usage     chatUsage
}

// toSDKMessages 轻量消息 → SDK 参数。
func toSDKMessages(msgs []chatMessage) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			out = append(out, openai.SystemMessage(m.Content))
		case "user":
			out = append(out, openai.UserMessage(m.Content))
		case "tool":
			out = append(out, openai.ToolMessage(m.Content, m.ToolCallID))
		case "assistant":
			ap := openai.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				ap.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(m.Content),
				}
			}
			for _, tc := range m.ToolCalls {
				ap.ToolCalls = append(ap.ToolCalls, openai.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &ap})
		}
	}
	return out
}

// toSDKTools 轻量工具定义 → SDK 参数。
func toSDKTools(tools []chatTool) []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		out = append(out, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Function.Name,
				Description: openai.String(t.Function.Description),
				Parameters:  shared.FunctionParameters(t.Function.Parameters),
			},
		})
	}
	return out
}

// Chat 发起流式对话。onDelta 实时回调文本增量(可为 nil)。
// ctx 取消时立即返回错误(用户中断)。
func (c *agentClient) Chat(ctx context.Context, req chatRequest, onDelta func(delta string)) (*chatResult, error) {
	ctx, cancel := context.WithTimeout(ctx, agentLLMCallTimeout)
	defer cancel()

	stream := c.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: toSDKMessages(req.Messages),
		Tools:    toSDKTools(req.Tools),
		// 请求流式 usage(最后一个 chunk 携带;部分网关不支持则为 0)
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
	})
	defer stream.Close()

	var content strings.Builder
	var reasoning strings.Builder
	var usage chatUsage
	toolAcc := make(map[int64]*agentToolCall)
	for stream.Next() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		chunk := stream.Current()
		if u := chunk.Usage; u.TotalTokens > 0 {
			usage = chatUsage{
				PromptTokens:     u.PromptTokens,
				CachedTokens:     u.PromptTokensDetails.CachedTokens,
				CompletionTokens: u.CompletionTokens,
				TotalTokens:      u.TotalTokens,
			}
		}
		for _, choice := range chunk.Choices {
			d := choice.Delta
			// 思考内容: 网关以 reasoning_content 扩展字段下发(SDK 未建模,经 ExtraFields 原样保留)
			if f, ok := d.JSON.ExtraFields["reasoning_content"]; ok {
				var piece string
				if raw := f.Raw(); raw != "" && raw != "null" && json.Unmarshal([]byte(raw), &piece) == nil && piece != "" {
					reasoning.WriteString(piece)
				}
			}
			if d.Content != "" {
				content.WriteString(d.Content)
				if onDelta != nil {
					onDelta(d.Content)
				}
			}
			for _, tc := range d.ToolCalls {
				acc := toolAcc[tc.Index]
				if acc == nil {
					acc = &agentToolCall{}
					toolAcc[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.ID = tc.ID
				}
				if tc.Function.Name != "" {
					acc.Name += tc.Function.Name
				}
				acc.Arguments += tc.Function.Arguments
			}
		}
	}
	if err := stream.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}

	// 按 index 排序组装工具调用
	idxes := make([]int64, 0, len(toolAcc))
	for i := range toolAcc {
		idxes = append(idxes, i)
	}
	sort.Slice(idxes, func(a, b int) bool { return idxes[a] < idxes[b] })
	var toolCalls []agentToolCall
	for _, i := range idxes {
		tc := toolAcc[i]
		if tc.Name == "" {
			continue
		}
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), i)
		}
		if tc.Arguments == "" {
			tc.Arguments = "{}"
		}
		toolCalls = append(toolCalls, *tc)
	}
	return &chatResult{Content: content.String(), Reasoning: reasoning.String(), ToolCalls: toolCalls, Usage: usage}, nil
}

// ListModels 拉取可用模型 ID 列表(下拉选择用;单页即可)。
func (c *agentClient) ListModels(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	page, err := c.client.Models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取模型列表失败: %w", err)
	}
	var ids []string
	for _, m := range page.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
		if len(ids) >= 200 { // 有界
			break
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Test 连通性测试(拉模型列表,成功即连通)。
func (c *agentClient) Test() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.client.Models.List(ctx)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	return nil
}
