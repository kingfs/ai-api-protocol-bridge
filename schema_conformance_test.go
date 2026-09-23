package protocolbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file checks the adapters against the vendored official protocol schemas
// in protocols/. See protocols/README.md for the provenance of those files and
// docs/protocol-conformance.md for the audit that explains every expected
// problem pinned here.
//
// The expectations below are deliberately exact. They encode "this is how far
// the implementation currently is from the official schema", so that fixing a
// gap fails the test and forces the report and the expectation to be updated
// together.

var (
	chatSchemaPath       = filepath.Join("protocols", "openai", "chat-completions.schema.json")
	chatExamplePath      = filepath.Join("protocols", "openai", "chat-completions.examples.json")
	responsesSchemaPath  = filepath.Join("protocols", "openai", "responses.schema.json")
	responsesExamplePath = filepath.Join("protocols", "openai", "responses.examples.json")
	anthropicSchemaPath  = filepath.Join("protocols", "anthropic", "messages.schema.json")
	anthropicExamplePath = filepath.Join("protocols", "anthropic", "messages.examples.json")
	anthropicStreamPath  = filepath.Join("protocols", "anthropic", "streaming.examples.json")
)

func TestOfficialSchemaBundlesAreWellFormed(t *testing.T) {
	for _, path := range []string{chatSchemaPath, responsesSchemaPath, anthropicSchemaPath} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			bundle := loadSchemaBundle(t, path)

			if bundle.Protocol == "" || bundle.Endpoint == "" || bundle.Dialect == "" {
				t.Fatalf("bundle metadata incomplete: protocol=%q endpoint=%q dialect=%q", bundle.Protocol, bundle.Endpoint, bundle.Dialect)
			}
			if source, ok := bundle.Source["sha256"]; !ok || source == nil {
				t.Fatalf("bundle has no source digest")
			}

			keys := make([]string, 0, len(bundle.Defs))
			for name := range bundle.Defs {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			if strings.Join(keys, ",") != strings.Join(bundle.Components, ",") {
				t.Fatalf("components_in_scope does not match $defs keys")
			}

			for _, slot := range []string{"request", "response"} {
				if _, ok := bundle.Documents[slot]; !ok {
					t.Fatalf("missing document %q", slot)
				}
				bundle.resolve(t, bundle.document(t, slot))
			}

			for name, definition := range bundle.Defs {
				walkSchema(t, definition, func(node map[string]any) {
					if expr, ok := node["x-unparsed-expr"]; ok {
						t.Fatalf("%s: unparsed type expression %v in %s", path, expr, name)
					}
					ref, ok := node["$ref"].(string)
					if !ok {
						return
					}
					const prefix = "#/$defs/"
					if !strings.HasPrefix(ref, prefix) {
						t.Fatalf("%s: non-local ref %q in %s", path, ref, name)
					}
					if _, ok := bundle.Defs[strings.TrimPrefix(ref, prefix)]; !ok {
						t.Fatalf("%s: unresolved ref %q in %s", path, ref, name)
					}
				})
			}
		})
	}
}

type officialExample struct {
	Title    string          `json:"title"`
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

func loadExamples(t *testing.T, path string) []officialExample {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read examples %s: %v", path, err)
	}
	var bundle struct {
		Examples []officialExample `json:"examples"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode examples %s: %v", path, err)
	}
	return bundle.Examples
}

func walkSchema(t *testing.T, value any, visit func(map[string]any)) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			walkSchema(t, child, visit)
		}
	case []any:
		for _, child := range typed {
			walkSchema(t, child, visit)
		}
	}
}

// TestOfficialExamplePayloadsDecode feeds every payload published by the
// upstream specification through the matching adapter's decoder.
func TestOfficialExamplePayloadsDecode(t *testing.T) {
	for _, item := range []struct {
		name    string
		path    string
		adapter Adapter
	}{
		{"openai_chat", chatExamplePath, NewOpenAIChatAdapter()},
		{"openai_responses", responsesExamplePath, NewOpenAIResponsesAdapter()},
		{"anthropic_messages", anthropicExamplePath, NewAnthropicMessagesAdapter()},
	} {
		t.Run(item.name, func(t *testing.T) {
			examples := loadExamples(t, item.path)
			if len(examples) == 0 {
				t.Fatalf("no official examples found in %s", item.path)
			}
			decoded := 0
			for _, example := range examples {
				if len(example.Request) > 0 && string(example.Request) != "null" {
					if _, err := item.adapter.DecodeRequest(example.Request); err != nil {
						t.Errorf("%q: decode request: %v", example.Title, err)
					} else {
						decoded++
					}
				}
				if len(example.Response) > 0 && string(example.Response) != "null" {
					if _, err := item.adapter.DecodeResponse(example.Response); err != nil {
						t.Errorf("%q: decode response: %v", example.Title, err)
					} else {
						decoded++
					}
				}
			}
			if decoded == 0 {
				t.Fatalf("no decodable official payloads in %s", item.path)
			}
		})
	}
}

type schemaCase struct {
	name    string
	bundle  string
	example string
	slot    string
	adapter Adapter
	// wantProblems lists the normalized schema violations this encoder is
	// expected to produce. An empty list means the encoder is conformant.
	wantProblems []string
}

// TestOfficialExampleRequestsReencodeWithinSchema decodes each official
// example request and validates the adapter's re-encoding against the official
// request schema.
func TestOfficialExampleRequestsReencodeWithinSchema(t *testing.T) {
	cases := []schemaCase{
		{name: "openai_chat", bundle: chatSchemaPath, example: chatExamplePath, slot: "request", adapter: NewOpenAIChatAdapter()},
		{
			name: "openai_responses", bundle: responsesSchemaPath, example: responsesExamplePath, slot: "request",
			adapter: NewOpenAIResponsesAdapter(),
			// The "Functions" example declares a function tool. The official
			// FunctionTool marks `strict` as required; the encoder omits it.
			wantProblems: []string{"Functions: no-branch /tools/0 oneOf"},
		},
		{name: "anthropic_messages", bundle: anthropicSchemaPath, example: anthropicExamplePath, slot: "request", adapter: NewAnthropicMessagesAdapter()},
	}
	runSchemaCases(t, cases)
}

// TestEncodedResponsesWithinSchema validates the adapter's re-encoding of a
// representative unified response against each official response schema.
func TestEncodedResponsesWithinSchema(t *testing.T) {
	cases := []schemaCase{
		{
			// Fixed: `created`, `choices[].logprobs`, `message.content` and
			// `message.refusal` are all now emitted, and `usage` no longer
			// serialises as an empty object.
			name: "openai_chat", bundle: chatSchemaPath, slot: "response", adapter: NewOpenAIChatAdapter(),
			wantProblems: nil,
		},
		{
			name: "openai_responses", bundle: responsesSchemaPath, slot: "response", adapter: NewOpenAIResponsesAdapter(),
			wantProblems: []string{
				"missing-required (root)/created_at",
				"missing-required (root)/error",
				"missing-required (root)/incomplete_details",
				"missing-required (root)/instructions",
				"missing-required (root)/metadata",
				"missing-required (root)/parallel_tool_calls",
				"missing-required (root)/temperature",
				"missing-required (root)/tool_choice",
				"missing-required (root)/tools",
				"missing-required (root)/top_p",
				"missing-required /usage/input_tokens_details",
				"missing-required /usage/output_tokens_details",
				"no-branch /output/1 anyOf",
			},
		},
		{
			name: "anthropic_messages", bundle: anthropicSchemaPath, slot: "response", adapter: NewAnthropicMessagesAdapter(),
			wantProblems: []string{
				"missing-required (root)/container",
				"missing-required (root)/stop_details",
				"missing-required (root)/stop_sequence",
				"missing-required /usage/cache_creation",
				"missing-required /usage/cache_creation_input_tokens",
				"missing-required /usage/inference_geo",
				"missing-required /usage/output_tokens_details",
				"missing-required /usage/server_tool_use",
				"missing-required /usage/service_tier",
				"no-branch /content/0 anyOf",
				"no-branch /content/2 anyOf",
			},
		},
	}
	runResponseSchemaCases(t, cases)
}

func runResponseSchemaCases(t *testing.T, cases []schemaCase) {
	t.Helper()
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			bundle := loadSchemaBundle(t, item.bundle)
			raw, err := item.adapter.EncodeResponse(sampleResponseFor(item.name), EncodeResponseOptions{Model: "model-1"})
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			problems := problemCodes(bundle.validate(bundle.document(t, item.slot), decodeJSONValue(t, raw)))
			assertProblems(t, problems, item.wantProblems)
		})
	}
}

func assertProblems(t *testing.T, got []string, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("schema violations changed.\n got:\n  %s\n want:\n  %s\n\nIf a gap was fixed, update protocols/ and docs/protocol-conformance.md and remove the entry here.",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func runSchemaCases(t *testing.T, cases []schemaCase) {
	t.Helper()
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			bundle := loadSchemaBundle(t, item.bundle)
			problems := make([]string, 0)
			checked := 0
			for _, example := range loadExamples(t, item.example) {
				if len(example.Request) == 0 || string(example.Request) == "null" {
					continue
				}
				decoded, err := item.adapter.DecodeRequest(example.Request)
				if err != nil {
					t.Fatalf("%s: decode request: %v", example.Title, err)
				}
				encoded, err := item.adapter.EncodeRequest(decoded, EncodeRequestOptions{})
				if err != nil {
					t.Fatalf("%s: encode request: %v", example.Title, err)
				}
				for _, code := range problemCodes(bundle.validate(bundle.document(t, item.slot), decodeJSONValue(t, encoded))) {
					problems = append(problems, example.Title+": "+code)
				}
				checked++
			}
			if checked == 0 {
				t.Fatalf("%s: no decodable official request example", item.example)
			}
			assertProblems(t, problems, item.wantProblems)
		})
	}
}

func sampleResponseFor(protocol string) *LLMResponse {
	switch protocol {
	case "openai_chat":
		return &LLMResponse{
			ID: "chatcmpl-1", Model: "model-1", Role: RoleAssistant, FinishReason: FinishStop,
			Usage: Usage{InputTokens: intPtr(10), OutputTokens: intPtr(5)},
			Content: []Part{
				{Type: PartText, Text: &TextPart{Text: "hello"}},
				{Type: PartToolCall, ToolCall: &ToolCallPart{ToolCallID: "call_1", ToolName: "lookup", Input: map[string]any{"a": 1}}},
			},
		}
	case "openai_responses":
		return &LLMResponse{
			ID: "resp_1", Model: "model-1", Role: RoleAssistant, FinishReason: FinishToolCalls,
			Usage: Usage{InputTokens: intPtr(10), OutputTokens: intPtr(5)},
			Content: []Part{
				{Type: PartText, Text: &TextPart{Text: "hello"}},
				{Type: PartReasoning, Reasoning: &ReasoningPart{Text: "thinking", Signature: "sig"}},
				{Type: PartToolCall, ToolCall: &ToolCallPart{ToolCallID: "call_1", ToolName: "lookup", Input: map[string]any{"a": 1}}},
			},
		}
	default:
		return &LLMResponse{
			ID: "msg_1", Model: "claude-sonnet-5", Role: RoleAssistant, FinishReason: FinishStop,
			Usage: Usage{InputTokens: intPtr(10), OutputTokens: intPtr(5)},
			Content: []Part{
				{Type: PartText, Text: &TextPart{Text: "hello"}},
				{Type: PartReasoning, Reasoning: &ReasoningPart{Text: "thinking", Signature: "sig"}},
				{Type: PartToolCall, ToolCall: &ToolCallPart{ToolCallID: "toolu_1", ToolName: "lookup", Input: map[string]any{"a": 1}}},
			},
		}
	}
}

// TestAnthropicOfficialStreamTranscriptsDecode replays the SSE transcripts
// published in the official streaming guide through the Anthropic stream
// decoder.
func TestAnthropicOfficialStreamTranscriptsDecode(t *testing.T) {
	raw, err := os.ReadFile(anthropicStreamPath)
	if err != nil {
		t.Fatalf("read %s: %v", anthropicStreamPath, err)
	}
	var bundle struct {
		Transcripts []struct {
			Title    string `json:"title"`
			Complete bool   `json:"complete"`
			Events   []struct {
				Event string `json:"event"`
				Data  string `json:"data"`
			} `json:"events"`
		} `json:"transcripts"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode %s: %v", anthropicStreamPath, err)
	}

	full := 0
	for _, transcript := range bundle.Transcripts {
		// `complete` is false for transcripts the official guide abbreviated.
		if !transcript.Complete || len(transcript.Events) == 0 || transcript.Events[0].Event != "message_start" {
			continue
		}
		full++
		t.Run(transcript.Title, func(t *testing.T) {
			decoder, err := NewAnthropicMessagesAdapter().NewStreamDecoder(StreamDecodeOptions{})
			if err != nil {
				t.Fatalf("new stream decoder: %v", err)
			}
			parts := 0
			for _, event := range transcript.Events {
				decoded, err := decoder.Decode(RawStreamEvent{Event: event.Event, Data: []byte(event.Data)})
				if err != nil {
					t.Fatalf("%s: decode %s: %v", transcript.Title, event.Event, err)
				}
				parts += len(decoded)
			}
			if parts == 0 {
				t.Fatalf("%s: decoded no stream parts", transcript.Title)
			}
		})
	}
	if full == 0 {
		t.Fatalf("no full official transcript found in %s", anthropicStreamPath)
	}
}

// problemCodes reduces a validator problem string to a stable, comparable code.
// The diagnostic tail of anyOf/oneOf failures is dropped on purpose: it is
// useful when reading a failure but too brittle to pin.
func problemCodes(problems []string) []string {
	codes := make([]string, 0, len(problems))
	for _, problem := range problems {
		if index := strings.Index(problem, "; closest branch:"); index >= 0 {
			problem = problem[:index]
		}
		switch {
		case strings.Contains(problem, ": missing required property "):
			path, name, _ := strings.Cut(problem, ": missing required property ")
			codes = append(codes, "missing-required "+joinPath(path, strings.Trim(name, `"`)))
		case strings.Contains(problem, ": unexpected property "):
			path, name, _ := strings.Cut(problem, ": unexpected property ")
			codes = append(codes, "unexpected-property "+joinPath(path, strings.Trim(name, `"`)))
		case strings.Contains(problem, ": matches no anyOf branch"):
			path, _, _ := strings.Cut(problem, ": matches no")
			codes = append(codes, "no-branch "+path+" anyOf")
		case strings.Contains(problem, ": matches no oneOf branch"):
			path, _, _ := strings.Cut(problem, ": matches no")
			codes = append(codes, "no-branch "+path+" oneOf")
		default:
			codes = append(codes, problem)
		}
	}
	sort.Strings(codes)
	return codes
}

func joinPath(path string, name string) string {
	if path == "(root)" || path == "" {
		return "(root)/" + name
	}
	return path + "/" + name
}
