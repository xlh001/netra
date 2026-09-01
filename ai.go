package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

var aiHTTPClient = &http.Client{Timeout: 20 * time.Second}

type chatCompletionRequest struct {
	Model         string                  `json:"model"`
	Messages      []chatCompletionMessage `json:"messages"`
	MaxTokens     int                     `json:"max_tokens,omitempty"`
	Stream        bool                    `json:"stream"`
	StreamOptions *streamOptions          `json:"stream_options,omitempty"`
	Tools         []toolSpec              `json:"tools,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatCompletionMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []toolCallOut `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type toolCallOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolSpec struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message      chatCompletionMessage `json:"message"`
		FinishReason *string               `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func chatCompletionOnce(baseURL, apiKey, model string, messages []chatCompletionMessage, maxTokens int) (chatCompletionResponse, error) {
	var parsed chatCompletionResponse
	if baseURL == "" {
		return parsed, fmt.Errorf("baseURL is required")
	}
	if model == "" {
		return parsed, fmt.Errorf("model is required")
	}

	body, err := json.Marshal(chatCompletionRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    false,
	})
	if err != nil {
		return parsed, fmt.Errorf("encode request: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return parsed, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := aiHTTPClient.Do(req)
	if err != nil {
		return parsed, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return parsed, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return parsed, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return parsed, fmt.Errorf("provider response was not valid JSON: %w", err)
	}
	if parsed.Error != nil {
		return parsed, fmt.Errorf("provider returned an error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return parsed, fmt.Errorf("provider response had no choices")
	}
	return parsed, nil
}

func testAIProvider(baseURL, apiKey, model string) error {
	_, err := chatCompletionOnce(baseURL, apiKey, model, []chatCompletionMessage{{Role: "user", Content: "回复\"OK\"以确认连接正常"}}, 10)
	return err
}

func summarizeAlertForNotify(cfg ConfigDTO, a ThreatAlert, ts time.Time) (string, error) {
	prompt := fmt.Sprintf(
		"以下是Netra刚触发的一条威胁感知告警的原始字段。用不超过2句话的中文说明这条告警可能意味着什么、以及建议的下一步排查动作。不要使用markdown格式，不要逐字复述下面的字段。\n类型：%s\nIP：%s\n%s\n时间：%s",
		alertKindLabel(a.Kind), formatIPLabel(a.IP, a.Label), alertDetailLine(a), ts.Format("2006-01-02 15:04:05"),
	)
	resp, err := chatCompletionOnce(cfg.AIBaseURL, cfg.AIAPIKey, cfg.AIModel, []chatCompletionMessage{{Role: "user", Content: prompt}}, 0)
	if err != nil {
		return "", err
	}
	choice := resp.Choices[0]
	if choice.FinishReason != nil && *choice.FinishReason == "length" {
		return "", fmt.Errorf("AI response truncated by max_tokens")
	}
	return strings.TrimSpace(choice.Message.Content), nil
}

const maxConcurrentAlertSummaries = 8

var alertSummarySem = make(chan struct{}, maxConcurrentAlertSummaries)

func summarizeAlertForNotifyLimited(cfg ConfigDTO, a ThreatAlert, ts time.Time) (summary string, ok bool) {
	select {
	case alertSummarySem <- struct{}{}:
	case <-time.After(3 * time.Second):
		return "", false
	}
	defer func() { <-alertSummarySem }()

	s, err := summarizeAlertForNotify(cfg, a, ts)
	if err != nil {
		return "", false
	}
	return s, true
}

const toolResultLimit = 20

var builtinChatTools = []toolSpec{
	{
		Type: "function",
		Function: toolFunction{
			Name:        "get_traffic_report",
			Description: "获取指定时间范围内的流量总览：总包数/总字节数、协议占比、Top流量五元组、Top IP、Top端口、Top域名。适合回答\"某段时间流量情况怎么样\"这类问题。",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from": {"type": "string", "description": "起始时间，RFC3339格式，如 2026-07-30T00:00:00+08:00"},
					"to": {"type": "string", "description": "结束时间，RFC3339格式"}
				},
				"required": ["from", "to"]
			}`),
		},
	},
	{
		Type: "function",
		Function: toolFunction{
			Name:        "get_timeseries",
			Description: "获取指定时间范围内按协议拆分的流量趋势时间序列，适合回答\"是否有流量突增/异常波动\"这类问题。",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from": {"type": "string", "description": "起始时间，RFC3339格式"},
					"to": {"type": "string", "description": "结束时间，RFC3339格式"}
				},
				"required": ["from", "to"]
			}`),
		},
	},
	{
		Type: "function",
		Function: toolFunction{
			Name:        "get_threat_alerts",
			Description: "获取指定时间范围内的威胁感知告警（端口扫描/DDoS疑似/单IP大流量），适合回答\"最近有没有异常/攻击\"这类问题。",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from": {"type": "string", "description": "起始时间，RFC3339格式"},
					"to": {"type": "string", "description": "结束时间，RFC3339格式"}
				},
				"required": ["from", "to"]
			}`),
		},
	},
	{
		Type: "function",
		Function: toolFunction{
			Name: "get_flows",
			Description: fmt.Sprintf(
				"查询指定时间范围内的具体五元组流量明细，可选按IP过滤，最多返回%d条（按流量从大到小排序）。适合回答\"某个IP具体访问了哪些地址\"这类需要明细的问题。",
				toolResultLimit),
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from": {"type": "string", "description": "起始时间，RFC3339格式"},
					"to": {"type": "string", "description": "结束时间，RFC3339格式"},
					"ip": {"type": "string", "description": "可选，按源或目的IP过滤"}
				},
				"required": ["from", "to"]
			}`),
		},
	},
}

func buildChatTools(mcpMgr *mcpManager) []toolSpec {
	tools := append([]toolSpec{}, builtinChatTools...)
	if mcpMgr != nil {
		tools = append(tools, mcpMgr.listChatTools()...)
	}
	return tools
}

func executeToolCall(ctx context.Context, store *Store, ipTags *ipTagCache, iocList *iocCache, mcpMgr *mcpManager, name, argsJSON string) (string, error) {
	if mcpMgr != nil && mcpMgr.isMCPTool(name) {
		return mcpMgr.callTool(ctx, name, argsJSON)
	}
	if store == nil {
		return `{"error": "历史数据查询需要启动时开启 -db 持久化"}`, nil
	}

	var args struct {
		From string `json:"from"`
		To   string `json:"to"`
		IP   string `json:"ip"`
	}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid tool arguments: %w", err)
		}
	}
	from, err := time.Parse(time.RFC3339, args.From)
	if err != nil {
		return "", fmt.Errorf("invalid 'from' time %q: %w", args.From, err)
	}
	to, err := time.Parse(time.RFC3339, args.To)
	if err != nil {
		return "", fmt.Errorf("invalid 'to' time %q: %w", args.To, err)
	}
	if !to.After(from) {
		return "", fmt.Errorf("'to' must be after 'from'")
	}

	switch name {
	case "get_traffic_report":
		report, err := store.QueryReportRange(from, to, toolResultLimit)
		if err != nil {
			return "", err
		}
		annotateIPTagsReport(ipTags, &report)
		b, err := json.Marshal(report)
		return string(b), err
	case "get_timeseries":
		ts, err := store.QueryTimeSeries(from, to)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(ts)
		return string(b), err
	case "get_threat_alerts":
		alerts, total, err := store.QueryThreatAlertsRange(from, to, 0, toolResultLimit, "")
		if err != nil {
			return "", err
		}
		annotateIPTagsAlerts(ipTags, alerts)
		annotateIOCAlerts(iocList, alerts)
		b, err := json.Marshal(struct {
			Total  int                 `json:"total"`
			Alerts []ThreatAlertRecord `json:"alerts"`
		}{total, alerts})
		return string(b), err
	case "get_flows":
		flows, total, err := store.QueryFlowsPaged(from, to, 0, toolResultLimit, FlowFilter{Q: args.IP})
		if err != nil {
			return "", err
		}
		annotateIPTagsFlows(ipTags, flows)
		b, err := json.Marshal(struct {
			Total int        `json:"total"`
			Flows []FlowStat `json:"flows"`
		}{total, flows})
		return string(b), err
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func buildSystemPrompt(now time.Time) string {
	weekdays := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return fmt.Sprintf(`你是Netra的流量分析助手。Netra是一款基于eBPF的网络流量可视化工具，你可以调用工具查询它采集并持久化的真实网络流量与威胁感知数据来回答用户问题。

当前时间：%s（%s），换算"今天"、"昨天"、"上周"等相对时间表达时以此为准。

可用工具：
- get_traffic_report：某段时间的流量总览（总量、协议占比、Top IP/端口/域名/五元组）
- get_timeseries：某段时间的流量趋势（按协议拆分的时间序列，用于判断是否有突增）
- get_threat_alerts：某段时间内的端口扫描/DDoS疑似/单IP大流量告警
- get_flows：某段时间内具体的五元组流量明细，可按IP过滤，最多返回%d条

严格规则：
1. 任何涉及具体数字（流量大小、IP、端口、告警数量等）的回答，必须先调用对应工具获取真实数据，禁止编造或估算。
2. 工具返回的是JSON数据，请提炼关键信息、用简洁自然的语言组织回答，不要直接把原始JSON贴给用户。
3. 时间范围换算成绝对时间传给工具时，统一使用RFC3339格式（如 2026-07-30T00:00:00+08:00）。
4. 你不局限于回答Netra流量数据本身——用户问及网络安全、网络工程、编程等其他领域的一般性问题时，运用你自身的知识正常作答，不要以"只能回答流量相关问题"为由拒绝；只有当问题明确需要用到Netra采集的具体流量/告警数据时，才必须调用工具获取真实数据（规则1），不能凭知识编造。
5. IP相关数据里如果已经带了label字段，说明这个IP已经配置了资产标签，直接使用即可；只有当用户需要更详细的信息（比如资产归属团队、工单记录）且有对应的外部工具（名字以mcp_开头）可用时，才需要额外调用外部工具查询，不要为了确认同一个信息重复调用。`,
		now.Format(time.RFC3339), weekdays[now.Weekday()], toolResultLimit)
}

const (
	chatEventToken    = "token"
	chatEventToolCall = "tool_call"
)

type chatEventSink func(kind string, payload any)

const maxToolRounds = 4

var chatStreamHTTPClient = &http.Client{}

func runChatTurn(ctx context.Context, cfg ConfigDTO, store *Store, ipTags *ipTagCache, iocList *iocCache, mcpMgr *mcpManager, messages []chatCompletionMessage, sink chatEventSink) (finalText string, toolsUsed []string, promptTokens, completionTokens int, err error) {
	tools := buildChatTools(mcpMgr)
	for round := 0; round < maxToolRounds; round++ {
		roundStart := time.Now()
		text, calls, finishReason, pt, ct, err := streamOneCompletion(ctx, cfg, messages, tools, sink)
		log.Printf("ai chat: round %d LLM call took %v (model=%s)", round, time.Since(roundStart), cfg.AIModel)
		promptTokens += pt
		completionTokens += ct
		if err != nil {
			return "", toolsUsed, promptTokens, completionTokens, err
		}
		if len(calls) == 0 || finishReason != "tool_calls" {
			return text, toolsUsed, promptTokens, completionTokens, nil
		}

		assistantMsg := chatCompletionMessage{Role: "assistant", Content: text, ToolCalls: calls}
		messages = append(messages, assistantMsg)

		for _, c := range calls {
			sink(chatEventToolCall, map[string]string{"name": c.Function.Name})
			toolsUsed = append(toolsUsed, c.Function.Name)
			toolStart := time.Now()
			result, toolErr := executeToolCall(ctx, store, ipTags, iocList, mcpMgr, c.Function.Name, c.Function.Arguments)
			log.Printf("ai chat: round %d tool %q took %v", round, c.Function.Name, time.Since(toolStart))
			if toolErr != nil {
				result = fmt.Sprintf(`{"error": %q}`, toolErr.Error())
			}
			messages = append(messages, chatCompletionMessage{Role: "tool", ToolCallID: c.ID, Content: result})
		}
	}
	return "", toolsUsed, promptTokens, completionTokens, fmt.Errorf("exceeded maximum tool-call rounds (%d)", maxToolRounds)
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string                `json:"content"`
			ToolCalls []streamToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`

	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolCallAccumulator struct {
	id   string
	name string
	args strings.Builder
}

func streamOneCompletion(ctx context.Context, cfg ConfigDTO, messages []chatCompletionMessage, tools []toolSpec, sink chatEventSink) (text string, calls []toolCallOut, finishReason string, promptTokens, completionTokens int, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	reqBody, err := json.Marshal(chatCompletionRequest{
		Model:         cfg.AIModel,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Tools:         tools,
	})
	if err != nil {
		return "", nil, "", 0, 0, fmt.Errorf("encode request: %w", err)
	}

	url := strings.TrimRight(cfg.AIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", nil, "", 0, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.AIAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AIAPIKey)
	}

	resp, err := chatStreamHTTPClient.Do(req)
	if err != nil {
		return "", nil, "", 0, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", nil, "", 0, 0, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var textBuilder strings.Builder
	toolAccum := map[int]*toolCallAccumulator{}
	var toolOrder []int

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		if payload == "" {
			continue
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			textBuilder.WriteString(choice.Delta.Content)
			sink(chatEventToken, map[string]string{"text": choice.Delta.Content})
		}
		for _, tc := range choice.Delta.ToolCalls {
			acc, ok := toolAccum[tc.Index]
			if !ok {
				acc = &toolCallAccumulator{}
				toolAccum[tc.Index] = acc
				toolOrder = append(toolOrder, tc.Index)
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name += tc.Function.Name
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, "", 0, 0, fmt.Errorf("read stream: %w", err)
	}

	for _, idx := range toolOrder {
		acc := toolAccum[idx]
		var c toolCallOut
		c.ID = acc.id
		c.Type = "function"
		c.Function.Name = acc.name
		c.Function.Arguments = acc.args.String()
		calls = append(calls, c)
	}

	return textBuilder.String(), calls, finishReason, promptTokens, completionTokens, nil
}
