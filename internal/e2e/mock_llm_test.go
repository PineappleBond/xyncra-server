package e2e_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Mock LLM Server — OpenAI-compatible HTTP mock for agent E2E tests
// ---------------------------------------------------------------------------
//
// mockLLMServer provides a fake OpenAI API that agent E2E tests use instead of
// a real LLM provider. It supports:
//   - Non-streaming chat completions (POST /v1/chat/completions)
//   - Streaming chat completions via SSE (stream: true)
//   - Tool call responses (finish_reason: "tool_calls")
//   - Content-based response routing (e.g. "error_trigger" → HTTP 500)
//   - Call counting for assertions
//   - Custom response overrides

// mockChatRequest represents the relevant fields of an OpenAI chat completion
// request that the mock server inspects for routing decisions.
type mockChatRequest struct {
	Model    string            `json:"model"`
	Messages []mockChatMessage `json:"messages"`
	Stream   bool              `json:"stream"`
	Tools    []mockToolDef     `json:"tools"`
}

// mockChatMessage is a single message in a chat completion request.
type mockChatMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// mockToolDef is a tool definition in the request's tools array.
type mockToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// mockToolResponse holds a custom tool call response configuration.
type mockToolResponse struct {
	ToolName  string
	Arguments string
	Result    string
}

// triggerToolCall holds a tool call configuration triggered by message content.
type triggerToolCall struct {
	toolName string
	args     string
}

// ToolCallStep represents one step in a multi-turn tool call sequence.
// If ToolName is non-empty, the step produces a tool_call response.
// If ToolName is empty, the step produces a text response (using Text).
type ToolCallStep struct {
	ToolName  string // empty for text response
	Arguments string
	Result    string // for tool result (informational; not used by mock directly)
	Text      string // for text response in the final round
}

// llmWeakNetConfig controls fault injection in the mock LLM server.
// Zero value means all fault injection is disabled (backward compatible).
type llmWeakNetConfig struct {
	// ResponseDelay adds a delay before sending the response.
	ResponseDelay time.Duration
	// BlackHoleTimeout causes the server to never respond, triggering client timeout.
	BlackHoleTimeout bool
	// StreamDisconnectAfter closes the stream after N chunks (0 = disabled).
	// Only applies to streaming responses.
	StreamDisconnectAfter int
	// RateLimitFirstN returns HTTP 429 for the first N requests (0 = disabled).
	RateLimitFirstN int
}

// mockLLMServer is an OpenAI-compatible mock LLM for agent E2E tests.
// It routes requests based on message content and supports both streaming
// and non-streaming responses, including tool calls.
type mockLLMServer struct {
	server            *httptest.Server
	callCount         int32 // atomic
	toolCallCount     int32 // atomic
	mu                sync.Mutex
	responses         map[string]string // pattern → response text
	toolResponses     map[string]*mockToolResponse
	requestMsgCnt     []int                      // message count per request (for context assertions)
	triggerToToolCall map[string]triggerToolCall // trigger string → tool call config
	sequenceSteps     []ToolCallStep             // multi-step sequence
	sequenceIndex     int                        // current step index
	recordedTools     [][]mockToolDef            // tools from each request
	lastMessages      []mockChatMessage          // messages from last request
	weakNet           llmWeakNetConfig
	weakNetMu         sync.Mutex
}

// newMockLLMServer creates and starts an OpenAI-compatible mock LLM server.
// Default responses are configured for common test patterns.
func newMockLLMServer() *mockLLMServer {
	m := &mockLLMServer{
		responses:         make(map[string]string),
		toolResponses:     make(map[string]*mockToolResponse),
		triggerToToolCall: make(map[string]triggerToolCall),
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	// Pre-populate default responses.
	m.responses["hello"] = "Hello! I'm the test bot. How can I help you?"
	m.responses["context"] = "I can see the context from our conversation."
	m.responses["default"] = "This is a mock response from the test LLM."
	// Default tool call response.
	m.toolResponses["get_weather"] = &mockToolResponse{
		ToolName:  "get_weather",
		Arguments: `{"location":"Beijing"}`,
		Result:    `{"temperature":"22°C","condition":"sunny"}`,
	}
	return m
}

// URL returns the base URL of the mock server (e.g. "http://127.0.0.1:12345").
// Use this as the base_url in agent configs; the OpenAI ChatModel appends
// /v1/chat/completions automatically.
func (m *mockLLMServer) URL() string {
	return m.server.URL
}

// CallCount returns the total number of chat completion requests received.
func (m *mockLLMServer) CallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

// ToolCallCount returns the number of responses that included tool_calls.
func (m *mockLLMServer) ToolCallCount() int {
	return int(atomic.LoadInt32(&m.toolCallCount))
}

// ResetCounters resets callCount and toolCallCount to zero.
func (m *mockLLMServer) ResetCounters() {
	atomic.StoreInt32(&m.callCount, 0)
	atomic.StoreInt32(&m.toolCallCount, 0)
}

// RequestMessageCounts returns the number of messages in each chat completion
// request received so far, in chronological order. Useful for verifying that
// context management passes the correct history to the LLM.
func (m *mockLLMServer) RequestMessageCounts() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]int, len(m.requestMsgCnt))
	copy(result, m.requestMsgCnt)
	return result
}

// SetResponse registers a custom response for the given pattern. When a user
// message contains pattern as a substring, response is returned instead of the
// default. Pass pattern="default" to change the fallback response.
func (m *mockLLMServer) SetResponse(pattern, response string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[pattern] = response
}

// SetToolCallResponse registers a custom tool call response. When the request
// triggers a tool call for toolName, the mock responds with the specified
// function name and arguments.
func (m *mockLLMServer) SetToolCallResponse(toolName, args, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolResponses[toolName] = &mockToolResponse{
		ToolName:  toolName,
		Arguments: args,
		Result:    result,
	}
}

// Close shuts down the mock HTTP server.
func (m *mockLLMServer) Close() {
	m.server.Close()
}

// SetWeakNetConfig configures fault injection for weak network simulation.
// Pass a zero-value llmWeakNetConfig{} to disable all fault injection.
func (m *mockLLMServer) SetWeakNetConfig(cfg llmWeakNetConfig) {
	m.weakNetMu.Lock()
	defer m.weakNetMu.Unlock()
	m.weakNet = cfg
}

// ResetWeakNet disables all weak network fault injection.
// Equivalent to SetWeakNetConfig(llmWeakNetConfig{}).
func (m *mockLLMServer) ResetWeakNet() {
	m.SetWeakNetConfig(llmWeakNetConfig{})
}

// applyWeakNetFaults checks the current weak net config and applies fault
// injection. Returns true if the fault was handled (response already written),
// false if normal processing should continue.
func (m *mockLLMServer) applyWeakNetFaults(w http.ResponseWriter, r *http.Request) bool {
	m.weakNetMu.Lock()
	cfg := m.weakNet
	m.weakNetMu.Unlock()

	// Rate limit: return HTTP 429 for the first N requests.
	if cfg.RateLimitFirstN > 0 {
		currentCall := int(atomic.LoadInt32(&m.callCount))
		if currentCall <= cfg.RateLimitFirstN {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return true
		}
	}

	// Black hole: accept the TCP connection but never send an HTTP response,
	// causing the client to time out. Hijack the connection and close it
	// immediately so that no HTTP headers (not even a default 200) are sent.
	if cfg.BlackHoleTimeout {
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close() // abrupt close — no HTTP response at all
			}
		}
		return true
	}

	// Response delay: sleep before processing.
	if cfg.ResponseDelay > 0 {
		time.Sleep(cfg.ResponseDelay)
	}

	return false
}

// handle is the HTTP handler for all mock endpoints.
func (m *mockLLMServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
		m.handleModels(w)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		m.handleChatCompletions(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleModels returns a minimal model list.
func (m *mockLLMServer) handleModels(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id":       "gpt-4",
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "mock",
			},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// activeToolCall returns the name and arguments for the tool call response.
// It prefers a non-default entry in toolResponses (configured via SetToolCallResponse),
// falling back to "get_weather" defaults. Caller must hold m.mu.
func (m *mockLLMServer) activeToolCall() (name, args string) {
	for key, tr := range m.toolResponses {
		if key != "get_weather" {
			return tr.ToolName, tr.Arguments
		}
	}
	if tr := m.toolResponses["get_weather"]; tr != nil {
		return tr.ToolName, tr.Arguments
	}
	return "get_weather", `{"location":"Beijing"}`
}

// handleChatCompletions processes chat completion requests.
func (m *mockLLMServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&m.callCount, 1)

	// Apply weak net fault injection before processing the request.
	if m.applyWeakNetFaults(w, r) {
		return // fault handled (rate limit, black hole, etc.)
	}

	var req mockChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	// Record the message count for this request (used by context tests).
	// Also record tools and messages for DynamicToolProvider verification.
	m.mu.Lock()
	m.requestMsgCnt = append(m.requestMsgCnt, len(req.Messages))
	toolsCopy := make([]mockToolDef, len(req.Tools))
	copy(toolsCopy, req.Tools)
	m.recordedTools = append(m.recordedTools, toolsCopy)
	msgsCopy := make([]mockChatMessage, len(req.Messages))
	copy(msgsCopy, req.Messages)
	m.lastMessages = msgsCopy
	m.mu.Unlock()

	// Determine response type based on message content.
	responseType, responseText, step := m.selectResponse(req)

	if responseType == "error" {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if req.Stream {
		m.writeStreamResponse(w, req, responseType, responseText, step)
		return
	}
	m.writeNonStreamResponse(w, req, responseType, responseText, step)
}

// selectResponse inspects the request messages and returns a response type
// ("text", "tool_call") and the text content for text responses. If the
// response is a sequence step, the step is returned as well.
func (m *mockLLMServer) selectResponse(req mockChatRequest) (responseType, text string, step *ToolCallStep) {
	// Check sequence steps first (highest priority).
	if s, ok := m.consumeStep(); ok {
		if s.ToolName != "" {
			return "tool_call_step", "", &s
		}
		return "text_step", s.Text, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check trigger-based tool calls (second priority).
	lastUserContent := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUserContent = req.Messages[i].Content
			break
		}
	}
	for trigger, tc := range m.triggerToToolCall {
		if strings.Contains(lastUserContent, trigger) && len(req.Tools) > 0 {
			_ = tc // ensure tc is captured
			return "tool_call_trigger", "", nil
		}
	}

	// If the message history contains tool results, this is the second turn
	// after a tool call — return a normal text response.
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			return "text", m.responses["default"], nil
		}
	}

	// Error trigger → HTTP 500.
	if strings.Contains(lastUserContent, "error_trigger") {
		return "error", "", nil
	}

	// Empty/whitespace-only message → HTTP 500 (D-091: reject with error).
	if strings.TrimSpace(lastUserContent) == "" {
		return "error", "", nil
	}

	// Tool call trigger: request has tools defined and content triggers it.
	// If a custom tool call response has been configured via SetToolCallResponse,
	// use it. Otherwise fall back to the default get_weather trigger.
	if len(req.Tools) > 0 && strings.Contains(lastUserContent, "tool_weather") {
		if len(m.toolResponses) > 0 {
			return "tool_call", "", nil
		}
		return "tool_call", "", nil
	}

	// Pattern-based text responses.
	if strings.Contains(lastUserContent, "hello") || strings.Contains(lastUserContent, "hi") {
		return "text", m.responses["hello"], nil
	}
	if strings.Contains(lastUserContent, "context") {
		return "text", m.responses["context"], nil
	}
	return "text", m.responses["default"], nil
}

// writeNonStreamResponse writes a standard (non-streaming) chat completion
// response in OpenAI JSON format. step is used only when responseType is
// "tool_call_step"; it may be nil for other response types.
func (m *mockLLMServer) writeNonStreamResponse(w http.ResponseWriter, req mockChatRequest, responseType, text string, step *ToolCallStep) {
	w.Header().Set("Content-Type", "application/json")

	if responseType == "tool_call_step" {
		atomic.AddInt32(&m.toolCallCount, 1)
		toolName := step.ToolName
		toolArgs := step.Arguments
		if toolArgs == "" {
			toolArgs = "{}"
		}

		resp := map[string]any{
			"id":      "chatcmpl-mock-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": nil,
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     50,
				"completion_tokens": 20,
				"total_tokens":      70,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	if responseType == "tool_call_trigger" {
		atomic.AddInt32(&m.toolCallCount, 1)
		toolName, toolArgs := m.activeToolCallFromTrigger()

		resp := map[string]any{
			"id":      "chatcmpl-mock-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": nil,
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     50,
				"completion_tokens": 20,
				"total_tokens":      70,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	if responseType == "tool_call" {
		atomic.AddInt32(&m.toolCallCount, 1)
		m.mu.Lock()
		toolName, toolArgs := m.activeToolCall()
		m.mu.Unlock()

		resp := map[string]any{
			"id":      "chatcmpl-mock-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": nil,
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     50,
				"completion_tokens": 20,
				"total_tokens":      70,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Normal text response.
	resp := map[string]any{
		"id":      "chatcmpl-mock-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     30,
			"completion_tokens": 10,
			"total_tokens":      40,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// writeStreamResponse writes a streaming (SSE) chat completion response.
// For text responses, tokens are emitted with ~10ms intervals. For tool_call
// responses, the tool call delta is emitted in a single chunk. step is used
// only when responseType is "tool_call_step"; it may be nil for other types.
func (m *mockLLMServer) writeStreamResponse(w http.ResponseWriter, req mockChatRequest, responseType, text string, step *ToolCallStep) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if responseType == "tool_call_step" {
		atomic.AddInt32(&m.toolCallCount, 1)
		toolName := step.ToolName
		toolArgs := step.Arguments
		if toolArgs == "" {
			toolArgs = "{}"
		}

		chunk := map[string]any{
			"id":      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_1",
								"type":  "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	if responseType == "tool_call_trigger" {
		atomic.AddInt32(&m.toolCallCount, 1)
		toolName, toolArgs := m.activeToolCallFromTrigger()

		chunk := map[string]any{
			"id":      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_1",
								"type":  "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	if responseType == "tool_call" {
		atomic.AddInt32(&m.toolCallCount, 1)
		m.mu.Lock()
		toolName, toolArgs := m.activeToolCall()
		m.mu.Unlock()

		// Emit tool call chunk.
		chunk := map[string]any{
			"id":      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_1",
								"type":  "function",
								"function": map[string]any{
									"name":      toolName,
									"arguments": toolArgs,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		// Emit [DONE].
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Text streaming: split response into tokens and emit with delay.
	tokens := splitIntoTokens(text)

	// Get weak net config for stream disconnect.
	m.weakNetMu.Lock()
	disconnectAfter := m.weakNet.StreamDisconnectAfter
	m.weakNetMu.Unlock()

	chunkCount := 0
	for _, token := range tokens {
		chunk := map[string]any{
			"id":      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": token,
					},
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		chunkCount++
		time.Sleep(10 * time.Millisecond)

		// Stream disconnect: abruptly close connection after N chunks.
		if disconnectAfter > 0 && chunkCount >= disconnectAfter {
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					conn.Close()
				}
			}
			return
		}
	}

	// Final chunk with finish_reason.
	finalChunk := map[string]any{
		"id":      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", atomic.LoadInt32(&m.callCount)),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			},
		},
	}
	data, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	// [DONE] sentinel.
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// splitIntoTokens splits text into small chunks to simulate token-by-token
// streaming. Each token is roughly 3-5 characters.
func splitIntoTokens(text string) []string {
	var tokens []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		word := scanner.Text()
		tokens = append(tokens, word+" ")
	}
	if len(tokens) == 0 {
		tokens = []string{text}
	}
	return tokens
}

// ---------------------------------------------------------------------------
// Phase 7 extensions: trigger-based tool calls, sequences, recording
// ---------------------------------------------------------------------------

// SetToolCallForTrigger configures the mock to return a tool_call for the given
// tool when the user message contains the trigger string. This is more flexible
// than the hardcoded "tool_weather" trigger.
func (m *mockLLMServer) SetToolCallForTrigger(trigger, toolName, args string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggerToToolCall[trigger] = triggerToolCall{
		toolName: toolName,
		args:     args,
	}
}

// SetToolCallSequence configures a sequence of responses. Steps are consumed
// in order: first step for first request, second step for second request, etc.
// After all steps are consumed, falls back to default behavior.
func (m *mockLLMServer) SetToolCallSequence(steps []ToolCallStep) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sequenceSteps = steps
	m.sequenceIndex = 0
}

// RecordedTools returns the tool definitions from each request, in order.
// Used to verify DynamicToolProvider injected the correct client functions.
func (m *mockLLMServer) RecordedTools() [][]mockToolDef {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]mockToolDef, len(m.recordedTools))
	for i, tools := range m.recordedTools {
		toolsCopy := make([]mockToolDef, len(tools))
		copy(toolsCopy, tools)
		result[i] = toolsCopy
	}
	return result
}

// LastRequestMessages returns the messages from the last request.
func (m *mockLLMServer) LastRequestMessages() []mockChatMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockChatMessage, len(m.lastMessages))
	copy(result, m.lastMessages)
	return result
}

// consumeStep returns the next sequence step and advances the index.
// Returns (step, true) if a step was available, (zero, false) otherwise.
// Exported fields of the returned step are safe to use without holding the lock.
func (m *mockLLMServer) consumeStep() (ToolCallStep, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sequenceIndex >= len(m.sequenceSteps) {
		return ToolCallStep{}, false
	}
	step := m.sequenceSteps[m.sequenceIndex]
	m.sequenceIndex++
	return step, true
}

// activeToolCallFromTrigger returns the tool name and arguments from the first
// configured trigger-based tool call. Caller must NOT hold m.mu.
func (m *mockLLMServer) activeToolCallFromTrigger() (name, args string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tc := range m.triggerToToolCall {
		return tc.toolName, tc.args
	}
	return "unknown_tool", "{}"
}
