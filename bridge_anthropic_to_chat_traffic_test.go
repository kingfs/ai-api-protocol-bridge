package protocolbridge

import (
	"encoding/json"
	"strconv"
	"testing"
)

// The fixtures in this file are shaped like the traffic agent-compose actually
// relays when a coding agent that speaks Anthropic Messages is pointed at an
// upstream that speaks OpenAI Chat Completions. Endpoint hosts, credentials and
// account identifiers are removed; the bodies keep their real structure,
// including the fields a coding agent sets by convention — prompt caching
// markers, tool schemas, and the usage-only final stream chunk.

// claudeCodeRequest is one turn of a coding agent: a cached system preamble, two
// tool schemas, and a user instruction.
const claudeCodeRequest = `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 32000,
  "stream": true,
  "temperature": 1,
  "system": [
    {"type": "text", "text": "You are a coding agent running in a sandbox.", "cache_control": {"type": "ephemeral"}},
    {"type": "text", "text": "<env>\nWorking directory: /workspace\nPlatform: linux\n</env>", "cache_control": {"type": "ephemeral"}}
  ],
  "tools": [
    {"name": "Bash", "description": "Run a shell command in the sandbox.", "input_schema": {"type": "object", "properties": {"command": {"type": "string"}, "timeout": {"type": "number"}}, "required": ["command"]}},
    {"name": "Read", "description": "Read a file from disk.", "input_schema": {"type": "object", "properties": {"file_path": {"type": "string"}}, "required": ["file_path"]}}
  ],
  "messages": [
    {"role": "user", "content": [{"type": "text", "text": "List the files in the working directory.", "cache_control": {"type": "ephemeral"}}]}
  ]
}`

// claudeCodeToolResultRequest is the follow-up turn: the model asked for a tool,
// the runtime ran it, and the result goes back. A coding agent exercises this
// mapping on almost every turn.
const claudeCodeToolResultRequest = `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 32000,
  "stream": true,
  "system": [{"type": "text", "text": "You are a coding agent running in a sandbox."}],
  "messages": [
    {"role": "user", "content": [{"type": "text", "text": "List the files in the working directory."}]},
    {"role": "assistant", "content": [
      {"type": "text", "text": "I will list them."},
      {"type": "tool_use", "id": "toolu_01ABC", "name": "Bash", "input": {"command": "ls -1"}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_01ABC", "content": [{"type": "text", "text": "README.md\nmain.go\n"}]}
    ]}
  ]
}`

// TestAnthropicToOpenAIChatBridgeReplaysCodingAgentTurn takes the request above
// through the path agent-compose uses: decode the Anthropic body, then bridge
// the neutral request to a chat-completions upstream.
func TestAnthropicToOpenAIChatBridgeReplaysCodingAgentTurn(t *testing.T) {
	bridge, ok := NewCrossFamilyBridgeForProtocol(ProtocolAnthropicMessages, ProtocolOpenAIChat)
	if !ok {
		t.Fatal("NewCrossFamilyBridgeForProtocol() ok = false, want true")
	}
	req, err := NewAnthropicMessagesAdapter().DecodeRequest([]byte(claudeCodeRequest))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}

	raw, err := bridge.EncodeUpstreamRequest(req, EncodeRequestOptions{Model: "gpt-5.4"})
	if err != nil {
		t.Fatalf("EncodeUpstreamRequest() error = %v", err)
	}
	var out struct {
		Model               string `json:"model"`
		Stream              bool   `json:"stream"`
		MaxCompletionTokens *int   `json:"max_completion_tokens"`
		Messages            []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name       string         `json:"name"`
				Parameters map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// The upstream model is the resolved one, never the agent's declaration.
	if out.Model != "gpt-5.4" {
		t.Errorf("model = %q, want the resolved upstream model %q", out.Model, "gpt-5.4")
	}
	if !out.Stream {
		t.Error("stream = false, want true: the agent asked for a streamed turn")
	}
	if out.MaxCompletionTokens == nil || *out.MaxCompletionTokens != 32000 {
		t.Errorf("max_completion_tokens = %v, want 32000", out.MaxCompletionTokens)
	}

	// Both cached system blocks precede the user turn. The Anthropic
	// cache_control marker is a cache hint, not content, so it must not arrive
	// as a message or leak into the text.
	if len(out.Messages) != 2 {
		t.Fatalf("messages = %d, want a system message and a user message", len(out.Messages))
	}
	if out.Messages[0].Role != "system" {
		t.Errorf("messages[0].role = %q, want %q", out.Messages[0].Role, "system")
	}
	if got := string(out.Messages[0].Content); !containsAll(got, "coding agent", "Working directory: /workspace") {
		t.Errorf("system content = %s, want both system blocks merged", got)
	}
	if out.Messages[1].Role != "user" {
		t.Errorf("messages[1].role = %q, want %q", out.Messages[1].Role, "user")
	}
	if got := string(out.Messages[1].Content); !containsString(got, "List the files in the working directory.") {
		t.Errorf("user content = %s, want the instruction text", got)
	}

	if len(out.Tools) != 2 {
		t.Fatalf("tools = %d, want the two declared functions", len(out.Tools))
	}
	for i, tool := range out.Tools {
		if tool.Type != "function" {
			t.Errorf("tools[%d].type = %q, want %q", i, tool.Type, "function")
		}
	}
	if out.Tools[0].Function.Name != "Bash" || out.Tools[1].Function.Name != "Read" {
		t.Errorf("tool names = %q, %q, want Bash and Read", out.Tools[0].Function.Name, out.Tools[1].Function.Name)
	}
	// input_schema becomes parameters with its structure intact.
	props, ok := out.Tools[0].Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Bash parameters = %#v, want the decoded input_schema", out.Tools[0].Function.Parameters)
	}
	if _, ok := props["command"]; !ok {
		t.Errorf("Bash parameters = %#v, want the command property preserved", props)
	}
	required, ok := out.Tools[0].Function.Parameters["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "command" {
		t.Errorf("Bash required = %#v, want [command]", out.Tools[0].Function.Parameters["required"])
	}
}

// TestAnthropicToOpenAIChatBridgeReplaysCodingAgentToolResultTurn covers the
// mapping a coding agent exercises constantly: the assistant's tool_use becomes
// a chat tool_call, and the tool_result comes back as a tool-role message that
// references it by id.
func TestAnthropicToOpenAIChatBridgeReplaysCodingAgentToolResultTurn(t *testing.T) {
	bridge, ok := NewCrossFamilyBridgeForProtocol(ProtocolAnthropicMessages, ProtocolOpenAIChat)
	if !ok {
		t.Fatal("NewCrossFamilyBridgeForProtocol() ok = false, want true")
	}
	req, err := NewAnthropicMessagesAdapter().DecodeRequest([]byte(claudeCodeToolResultRequest))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}

	raw, err := bridge.EncodeUpstreamRequest(req, EncodeRequestOptions{Model: "gpt-5.4"})
	if err != nil {
		t.Fatalf("EncodeUpstreamRequest() error = %v", err)
	}
	var out struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   any    `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	roles := make([]string, 0, len(out.Messages))
	for _, message := range out.Messages {
		roles = append(roles, message.Role)
	}
	want := []string{"system", "user", "assistant", "tool"}
	if len(roles) != len(want) {
		t.Fatalf("message roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("message roles = %v, want %v", roles, want)
		}
	}

	assistant := out.Messages[2]
	if assistant.Role != "assistant" {
		t.Fatalf("messages[2].role = %q, want %q", assistant.Role, "assistant")
	}
	if got, _ := assistant.Content.(string); got != "I will list them." {
		t.Errorf("assistant content = %#v, want its text preserved alongside the call", assistant.Content)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(assistant.ToolCalls))
	}
	call := assistant.ToolCalls[0]
	if call.ID != "toolu_01ABC" {
		t.Errorf("tool_call id = %q, want the original tool_use id so the result can reference it", call.ID)
	}
	if call.Type != "function" || call.Function.Name != "Bash" {
		t.Errorf("tool_call = %+v, want a Bash function call", call)
	}
	// Arguments travel as a JSON *string* on the chat wire, carrying the input
	// the model produced.
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("tool_call arguments = %q, want a JSON-encoded object: %v", call.Function.Arguments, err)
	}
	if args["command"] != "ls -1" {
		t.Errorf("tool_call arguments = %#v, want the model's input preserved", args)
	}

	tool := out.Messages[3]
	if tool.Role != "tool" {
		t.Errorf("messages[3].role = %q, want %q", tool.Role, "tool")
	}
	if tool.ToolCallID != "toolu_01ABC" {
		t.Errorf("tool_call_id = %q, want it to match the assistant's call", tool.ToolCallID)
	}
	if got, _ := tool.Content.(string); !containsString(got, "main.go") {
		t.Errorf("tool content = %#v, want the command output", tool.Content)
	}
}

// codingAgentChatCompletion is an upstream reply that both answers in text and
// asks for another tool, with most of the prompt served from cache — the shape a
// cached coding-agent turn returns.
const codingAgentChatCompletion = `{
  "id": "chatcmpl-9xQ2m",
  "object": "chat.completion",
  "created": 1735689600,
  "model": "gpt-5.4",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "The directory holds three files.",
      "tool_calls": [{
        "id": "call_7f3a",
        "type": "function",
        "function": {"name": "Read", "arguments": "{\"file_path\":\"/workspace/main.go\"}"}
      }]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": {
    "prompt_tokens": 12000,
    "completion_tokens": 45,
    "total_tokens": 12045,
    "prompt_tokens_details": {"cached_tokens": 11000}
  }
}`

// TestAnthropicToOpenAIChatBridgeReplaysCodingAgentResponse decodes a real chat
// completion back into the Anthropic shape the coding agent expects, tool call
// and token accounting included.
func TestAnthropicToOpenAIChatBridgeReplaysCodingAgentResponse(t *testing.T) {
	bridge, ok := NewCrossFamilyBridgeForProtocol(ProtocolAnthropicMessages, ProtocolOpenAIChat)
	if !ok {
		t.Fatal("NewCrossFamilyBridgeForProtocol() ok = false, want true")
	}

	resp, err := bridge.DecodeUpstreamResponse([]byte(codingAgentChatCompletion))
	if err != nil {
		t.Fatalf("DecodeUpstreamResponse() error = %v", err)
	}
	if resp.Protocol != ProtocolOpenAIChat {
		t.Errorf("Protocol = %q, want %q", resp.Protocol, ProtocolOpenAIChat)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Errorf("finish reason = %q, want %q so the agent runs the tool", resp.FinishReason, FinishToolCalls)
	}

	text, call := codingAgentTextAndToolCall(resp)
	if text != "The directory holds three files." {
		t.Errorf("text = %q, want the assistant's answer", text)
	}
	if call == nil {
		t.Fatal("no tool call survived the decode")
	}
	if call.ToolName != "Read" {
		t.Errorf("tool call name = %q, want %q", call.ToolName, "Read")
	}
	if call.ToolCallID != "call_7f3a" {
		t.Errorf("tool call id = %q, want the upstream id", call.ToolCallID)
	}
	encoded, err := json.Marshal(call.Input)
	if err != nil {
		t.Fatalf("json.Marshal(tool call input) error = %v", err)
	}
	if !containsString(string(encoded), "/workspace/main.go") {
		t.Errorf("tool call input = %s, want the arguments", encoded)
	}

	// The usage is reported in the neutral OpenAI convention: prompt_tokens is a
	// total that already includes the cached portion. Anthopic's rebasing happens
	// on the way out, which the stream test below pins.
	if resp.Usage.InputTokens == nil || *resp.Usage.InputTokens != 12000 {
		t.Errorf("input tokens = %v, want the upstream total 12000", resp.Usage.InputTokens)
	}
	if resp.Usage.CachedInputTokens == nil || *resp.Usage.CachedInputTokens != 11000 {
		t.Errorf("cached input tokens = %v, want 11000", resp.Usage.CachedInputTokens)
	}
	if resp.Usage.OutputTokens == nil || *resp.Usage.OutputTokens != 45 {
		t.Errorf("output tokens = %v, want 45", resp.Usage.OutputTokens)
	}
}

// codingAgentChatStream is a streamed reply as an OpenAI-compatible upstream
// sends it, ending with the usage-only chunk a coding agent's client asks for.
// Almost the whole prompt was served from cache.
var codingAgentChatStream = []RawStreamEvent{
	{Data: []byte(`{"id":"chatcmpl-9xQ2m","object":"chat.completion.chunk","created":1735689600,"model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`)},
	{Data: []byte(`{"id":"chatcmpl-9xQ2m","object":"chat.completion.chunk","created":1735689600,"model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"The directory holds three files."},"finish_reason":null}]}`)},
	{Data: []byte(`{"id":"chatcmpl-9xQ2m","object":"chat.completion.chunk","created":1735689600,"model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_7f3a","type":"function","function":{"name":"Read","arguments":"{\"file_path\":"}}]},"finish_reason":null}]}`)},
	{Data: []byte(`{"id":"chatcmpl-9xQ2m","object":"chat.completion.chunk","created":1735689600,"model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/workspace/main.go\"}"}}]},"finish_reason":null}]}`)},
	{Data: []byte(`{"id":"chatcmpl-9xQ2m","object":"chat.completion.chunk","created":1735689600,"model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)},
	{Data: []byte(`{"id":"chatcmpl-9xQ2m","object":"chat.completion.chunk","created":1735689600,"model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":12000,"completion_tokens":45,"total_tokens":12045,"prompt_tokens_details":{"cached_tokens":11000}}}`)},
}

// TestAnthropicToOpenAIChatBridgeReplaysCodingAgentStream runs a whole streamed
// turn through the bridge and checks what the coding agent receives: text and
// tool-call deltas in Anthropic form, and usage that does not count the cached
// prompt twice.
func TestAnthropicToOpenAIChatBridgeReplaysCodingAgentStream(t *testing.T) {
	bridge, ok := NewCrossFamilyBridgeForProtocol(ProtocolAnthropicMessages, ProtocolOpenAIChat)
	if !ok {
		t.Fatal("NewCrossFamilyBridgeForProtocol() ok = false, want true")
	}
	decoder, err := bridge.NewStreamDecoder(StreamDecodeOptions{})
	if err != nil {
		t.Fatalf("NewStreamDecoder() error = %v", err)
	}
	encoder, err := bridge.NewStreamEncoder(StreamEncodeOptions{Model: "claude-sonnet-4-5"})
	if err != nil {
		t.Fatalf("NewStreamEncoder() error = %v", err)
	}

	var events []RawStreamEvent
	for i, chunk := range codingAgentChatStream {
		parts, err := decoder.Decode(chunk)
		if err != nil {
			t.Fatalf("Decode(chunk %d) error = %v", i, err)
		}
		for _, part := range parts {
			encoded, err := encoder.Encode(part)
			if err != nil {
				t.Fatalf("Encode(chunk %d) error = %v", i, err)
			}
			events = append(events, encoded...)
		}
	}

	var (
		text       string
		toolName   string
		toolArgs   string
		stopReason string
		usage      *anthropicUsage
	)
	for _, event := range events {
		var decoded anthropicStreamEvent
		if err := json.Unmarshal(event.Data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(%s) error = %v", event.Data, err)
		}
		switch {
		case decoded.Type == "content_block_start" && decoded.ContentBlock != nil && decoded.ContentBlock.Type == "tool_use":
			toolName = decoded.ContentBlock.Name
		case decoded.Delta != nil && decoded.Delta.Type == "text_delta":
			text += decoded.Delta.Text
		case decoded.Delta != nil && decoded.Delta.Type == "input_json_delta":
			toolArgs += decoded.Delta.PartialJSON
		case decoded.Delta != nil && decoded.Delta.StopReason != "":
			stopReason = decoded.Delta.StopReason
		}
		if decoded.Usage != nil && decoded.Usage.OutputTokens != nil {
			usage = decoded.Usage
		}
	}

	if text != "The directory holds three files." {
		t.Errorf("streamed text = %q, want the assistant's answer", text)
	}
	if toolName != "Read" {
		t.Errorf("streamed tool name = %q, want %q", toolName, "Read")
	}
	if !containsString(toolArgs, "/workspace/main.go") {
		t.Errorf("streamed tool arguments = %q, want the arguments", toolArgs)
	}
	if stopReason != "tool_use" {
		t.Errorf("stop reason = %q, want %q so the agent runs the tool", stopReason, "tool_use")
	}

	if usage == nil {
		t.Fatal("the stream reported no usage")
	}
	// 12000 prompt tokens of which 11000 were cached: Anthropic counts only the
	// uncached remainder as input and reports the rest as cache reads. Passing
	// the chat numbers through would say 12000 here and double-count the cache.
	if usage.InputTokens == nil || *usage.InputTokens != 1000 {
		t.Errorf("input_tokens = %s, want 1000 (12000 total minus 11000 cached)", formatUsageValue(usage.InputTokens))
	}
	if usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 11000 {
		t.Errorf("cache_read_input_tokens = %s, want 11000", formatUsageValue(usage.CacheReadInputTokens))
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != 45 {
		t.Errorf("output_tokens = %s, want 45", formatUsageValue(usage.OutputTokens))
	}
}

// codingAgentTextAndToolCall splits a decoded response into its assistant text
// and its first tool call.
func codingAgentTextAndToolCall(resp *LLMResponse) (string, *ToolCallPart) {
	var text string
	for _, part := range resp.Content {
		if part.Type == PartText && part.Text != nil {
			text += part.Text.Text
		}
		if part.Type == PartToolCall && part.ToolCall != nil {
			return text, part.ToolCall
		}
	}
	return text, nil
}

func formatUsageValue(value *int) string {
	if value == nil {
		return "<nil>"
	}
	return strconv.Itoa(*value)
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !containsString(value, needle) {
			return false
		}
	}
	return true
}
