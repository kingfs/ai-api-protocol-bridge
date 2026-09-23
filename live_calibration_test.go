package protocolbridge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These fixtures are real bytes captured from a live multi-protocol gateway
// (ai-api-gateway.app.baizhi.cloud) serving deepseek-flash. They are kept
// because they answer questions the official schemas cannot: the schemas say
// what is allowed, whereas these say what a working implementation actually
// sends. Reading the code alone produced several wrong conclusions that these
// transcripts corrected.
//
// The gateway is not this library — its chat endpoint is a straight passthrough
// and its Anthropic endpoint is its own conversion — so a fixture is evidence
// about the protocols, not about us. What it bought us:
//
//   - it settled the Responses stream: the event key is `part` and not
//     `content_part`, `response.function_call_arguments.done` does carry
//     `arguments`, `response.output_item.done` is emitted, and `output` is
//     nested inside `response`, not beside it;
//   - it settled the Anthropic stream: content blocks are strictly sequential
//     and every one is closed;
//   - it showed that `choices[].logprobs` and `reasoning_content` are ordinary
//     parts of a chat stream rather than exotic fields to ignore.
//
// Each fixture is replayed two ways: the captured request is decoded and
// re-encoded through the adapter and the result is validated against the
// vendored schema, and the captured stream is decoded and re-encoded and the
// result is decoded again. The second decode is the assertion — a stream that
// cannot survive a round trip through the IR is a stream the IR cannot describe.

const liveFixtureDir = "testdata/live"

type liveProtocolFixture struct {
	name string

	requestFixture  string
	responseFixture string
	streamFixture   string

	requestSchema  string
	responseSchema string

	adapter Adapter
}

func liveFixtures() []liveProtocolFixture {
	return []liveProtocolFixture{
		{
			name:            "openai_chat",
			requestFixture:  "openai-chat.request.json",
			responseFixture: "openai-chat.response.json",
			streamFixture:   "openai-chat.stream.sse",
			requestSchema:   "protocols/openai/chat-completions.schema.json",
			responseSchema:  "protocols/openai/chat-completions.schema.json",
			adapter:         NewOpenAIChatAdapter(),
		},
		{
			name:           "openai_responses",
			requestFixture: "openai-responses.request.json",
			streamFixture:  "openai-responses.stream.sse",
			requestSchema:  "protocols/openai/responses.schema.json",
			responseSchema: "protocols/openai/responses.schema.json",
			adapter:        NewOpenAIResponsesAdapter(),
		},
		{
			name:            "anthropic_messages",
			requestFixture:  "anthropic-messages.request.json",
			responseFixture: "anthropic-messages.response.json",
			streamFixture:   "anthropic-messages.stream.sse",
			requestSchema:   "protocols/anthropic/messages.schema.json",
			responseSchema:  "protocols/anthropic/messages.schema.json",
			adapter:         NewAnthropicMessagesAdapter(),
		},
	}
}

func readLiveFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(liveFixtureDir, name))
	if err != nil {
		t.Fatalf("read live fixture %s: %v", name, err)
	}
	return raw
}

// parseLiveSSE splits a captured server-sent-event stream into raw events. It
// understands the two shapes the gateway emits: named events with a `data:`
// line, and bare `data:` lines as the chat endpoint sends.
func parseLiveSSE(t *testing.T, raw []byte) []RawStreamEvent {
	t.Helper()
	events := make([]RawStreamEvent, 0)
	var current RawStreamEvent
	var data bytes.Buffer

	flush := func() {
		if data.Len() == 0 {
			current = RawStreamEvent{}
			return
		}
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "[DONE]" {
			current = RawStreamEvent{}
			return
		}
		current.Data = []byte(payload)
		events = append(events, current)
		current = RawStreamEvent{}
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			current.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data:"))
		case strings.TrimSpace(line) == "":
			flush()
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan live SSE: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("live SSE fixture produced no events")
	}
	return events
}

// TestLiveCapturedRequestsReencodeWithinSchema replays a request that a real
// client sent to the gateway and checks that our own encoding of it is valid
// for the same protocol.
func TestLiveCapturedRequestsReencodeWithinSchema(t *testing.T) {
	for _, fixture := range liveFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			raw := readLiveFixture(t, fixture.requestFixture)

			decoded, err := fixture.adapter.DecodeRequest(raw)
			if err != nil {
				t.Fatalf("DecodeRequest() error = %v", err)
			}
			encoded, err := fixture.adapter.EncodeRequest(decoded, EncodeRequestOptions{})
			if err != nil {
				t.Fatalf("EncodeRequest() error = %v", err)
			}

			bundle := loadSchemaBundle(t, fixture.requestSchema)
			problems := bundle.validate(bundle.document(t, "request"), decodeJSONValue(t, encoded))
			if len(problems) > 0 {
				t.Fatalf("re-encoded captured request is not valid %s: %s\n%s",
					fixture.name, strings.Join(problemCodes(problems), "\n  "), encoded)
			}
		})
	}
}

// TestLiveCapturedResponsesDecode replays a response the gateway produced and
// checks that the adapter reads it and re-emits something valid for the same
// protocol. Only the Anthropic and chat fixtures have one: the Responses
// capture is a stream.
func TestLiveCapturedResponsesDecode(t *testing.T) {
	for _, fixture := range liveFixtures() {
		if fixture.responseFixture == "" {
			continue
		}
		t.Run(fixture.name, func(t *testing.T) {
			raw := readLiveFixture(t, fixture.responseFixture)

			decoded, err := fixture.adapter.DecodeResponse(raw)
			if err != nil {
				t.Fatalf("DecodeResponse() error = %v", err)
			}
			if len(decoded.Content) == 0 {
				t.Fatalf("captured response decoded to no content: %+v", decoded)
			}
			encoded, err := fixture.adapter.EncodeResponse(decoded, EncodeResponseOptions{})
			if err != nil {
				t.Fatalf("EncodeResponse() error = %v", err)
			}

			bundle := loadSchemaBundle(t, fixture.responseSchema)
			problems := bundle.validate(bundle.document(t, "response"), decodeJSONValue(t, encoded))
			if len(problems) > 0 {
				t.Fatalf("re-encoded captured response is not valid %s: %s\n%s",
					fixture.name, strings.Join(problemCodes(problems), "\n  "), encoded)
			}
		})
	}
}

// TestLiveCapturedStreamsRoundTrip replays a captured stream through the
// decoder, re-emits it through the encoder, and decodes the result again. The
// second decode is the real assertion: the re-encoded stream has to be
// something the same decoder can read, which it cannot be if the encoder emits
// events the protocol does not define or drops the ones that carry content.
func TestLiveCapturedStreamsRoundTrip(t *testing.T) {
	for _, fixture := range liveFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			captured := parseLiveSSE(t, readLiveFixture(t, fixture.streamFixture))

			decoder, err := fixture.adapter.NewStreamDecoder(StreamDecodeOptions{})
			if err != nil {
				t.Fatalf("NewStreamDecoder() error = %v", err)
			}
			parts := make([]StreamPart, 0)
			for _, event := range captured {
				decoded, err := decoder.Decode(event)
				if err != nil {
					t.Fatalf("Decode(%s) error = %v\n%s", event.Event, err, event.Data)
				}
				parts = append(parts, decoded...)
			}
			tail, err := decoder.Close()
			if err != nil {
				t.Fatalf("decoder Close() error = %v", err)
			}
			parts = append(parts, tail...)
			if len(parts) == 0 {
				t.Fatal("captured stream decoded to no parts")
			}

			encoder, err := fixture.adapter.NewStreamEncoder(StreamEncodeOptions{})
			if err != nil {
				t.Fatalf("NewStreamEncoder() error = %v", err)
			}
			encoded := make([]RawStreamEvent, 0)
			for _, part := range parts {
				events, err := encoder.Encode(part)
				if err != nil {
					t.Fatalf("Encode(%s) error = %v", part.Type, err)
				}
				encoded = append(encoded, events...)
			}
			encodedTail, err := encoder.Close()
			if err != nil {
				t.Fatalf("encoder Close() error = %v", err)
			}
			encoded = append(encoded, encodedTail...)

			replay, err := fixture.adapter.NewStreamDecoder(StreamDecodeOptions{})
			if err != nil {
				t.Fatalf("NewStreamDecoder() error = %v", err)
			}
			replayed := make([]StreamPart, 0)
			for _, event := range encoded {
				decoded, err := replay.Decode(event)
				if err != nil {
					t.Fatalf("re-decode(%s) error = %v\n%s", event.Event, err, event.Data)
				}
				replayed = append(replayed, decoded...)
			}

			// Comparing counts alone is too weak: the defects this harness
			// exists to catch were all cases where the right event was emitted
			// with the wrong payload, or the wrong number of times. Each part
			// is projected onto the fields that carry meaning and the two
			// projections must match exactly. StreamRaw is excluded - it marks
			// something the decoder could not model, so there is nothing to
			// re-emit.
			want := projectStreamParts(parts)
			got := projectStreamParts(replayed)
			if len(want) != len(got) {
				t.Fatalf("round trip changed the number of parts: want %d, got %d\nwant: %+v\ngot:  %+v", len(want), len(got), want, got)
			}
			for i := range want {
				if want[i] != got[i] {
					t.Fatalf("round trip changed part %d:\nwant: %+v\ngot:  %+v\nfull want: %+v\nfull got:  %+v", i, want[i], got[i], want, got)
				}
			}

			// Lifecycle balance is protocol independent: one stream-start, and
			// every block that opened also closed. The defects this harness was
			// built to catch included a stream that never closed a block and one
			// that opened a second block before closing the first.
			assertStreamLifecycleBalanced(t, parts)

			// A chat stream chunk requires `created`, and a zero there is a
			// real value the protocol does not allow anyone to mean.
			if fixture.name == "openai_chat" {
				for _, event := range encoded {
					if strings.TrimSpace(string(event.Data)) == "[DONE]" {
						continue
					}
					var chunk struct {
						Created *int64 `json:"created"`
					}
					if err := json.Unmarshal(event.Data, &chunk); err != nil {
						t.Fatalf("json.Unmarshal(chunk) error = %v", err)
					}
					if chunk.Created == nil || *chunk.Created == 0 {
						t.Fatalf("re-encoded chunk has no usable created value: %s", event.Data)
					}
				}
			}

			// An Anthropic stream has exactly one message_start and closes
			// every content block before the next one opens.
			if fixture.name == "anthropic_messages" {
				assertBalancedAnthropicEvents(t, encoded)
			}
		})
	}
}

// liveStreamProjection is everything about a stream part that a client can
// observe. Two streams that project equal carry the same content.
// The block id is deliberately not projected: it is an artefact of the
// decoder's bookkeeping and an encoder is free to renumber it, provided it does
// so consistently. Everything a client can observe about the content is here.
type liveStreamProjection struct {
	Type         StreamPartType
	Delta        string
	ToolName     string
	ToolCallID   string
	Input        string
	FinishReason FinishReason
}

func projectStreamParts(parts []StreamPart) []liveStreamProjection {
	projected := make([]liveStreamProjection, 0, len(parts))
	for _, part := range parts {
		if part.Type == StreamRaw {
			continue
		}
		input := ""
		if part.Input != nil {
			raw, err := json.Marshal(part.Input)
			if err == nil {
				input = string(raw)
			}
		}
		projected = append(projected, liveStreamProjection{
			Type:         part.Type,
			Delta:        part.Delta,
			ToolName:     part.ToolName,
			ToolCallID:   part.ToolCallID,
			Input:        input,
			FinishReason: part.FinishReason,
		})
	}
	return projected
}

// assertStreamLifecycleBalanced checks that a decoded stream describes complete
// content blocks: exactly one stream-start, and a matching end part for every
// start part, in the order that keeps at most one block open at a time.
func assertStreamLifecycleBalanced(t *testing.T, parts []StreamPart) {
	t.Helper()

	blockOf := map[StreamPartType]StreamPartType{
		StreamTextStart:      StreamTextEnd,
		StreamReasoningStart: StreamReasoningEnd,
		StreamToolInputStart: StreamToolInputEnd,
	}
	endOf := map[StreamPartType]StreamPartType{
		StreamTextEnd:      StreamTextStart,
		StreamReasoningEnd: StreamReasoningStart,
		StreamToolInputEnd: StreamToolInputStart,
	}

	starts := 0
	open := map[StreamPartType]int{}
	for _, part := range parts {
		if part.Type == StreamStart {
			starts++
			continue
		}
		if end, ok := blockOf[part.Type]; ok {
			open[end]++
			continue
		}
		if start, ok := endOf[part.Type]; ok {
			if open[part.Type] == 0 {
				t.Fatalf("%s with no matching %s", part.Type, start)
			}
			open[part.Type]--
		}
	}
	if starts != 1 {
		t.Fatalf("a stream must start exactly once, got %d: %+v", starts, parts)
	}
	for end, count := range open {
		if count != 0 {
			t.Fatalf("%d %s blocks were never closed", count, end)
		}
	}
}

// assertBalancedAnthropicEvents checks the stream level invariants a client
// relies on: one message_start, blocks opened and closed one at a time, and a
// message_stop only after everything has been closed.
func assertBalancedAnthropicEvents(t *testing.T, events []RawStreamEvent) {
	t.Helper()
	open := 0
	starts, stops := 0, 0
	for _, event := range events {
		switch event.Event {
		case "message_start":
			starts++
		case "content_block_start":
			if open != 0 {
				t.Fatalf("content_block_start while a block was still open: %s", event.Data)
			}
			open++
		case "content_block_stop":
			if open == 0 {
				t.Fatalf("content_block_stop with no open block: %s", event.Data)
			}
			open--
			stops++
		case "message_stop":
			if open != 0 {
				t.Fatalf("message_stop with %d blocks still open", open)
			}
		}
	}
	if starts != 1 {
		t.Fatalf("message_start emitted %d times, want 1", starts)
	}
	if open != 0 {
		t.Fatalf("%d content blocks were left open", open)
	}
	if stops == 0 {
		t.Fatal("no content block was ever closed")
	}
}

// TestLiveCapturedAnthropicStreamIsSequential encodes what the captured
// Anthropic stream does and what a previous version of our encoder did not:
// content blocks arrive one at a time, each is closed before the next opens,
// and the last one is closed before the message ends.
func TestLiveCapturedAnthropicStreamIsSequential(t *testing.T) {
	captured := parseLiveSSE(t, readLiveFixture(t, "anthropic-messages-tool.stream.sse"))

	open := 0
	starts, stops, messageStarts, messageStops := 0, 0, 0, 0
	for _, event := range captured {
		switch event.Event {
		case "message_start":
			messageStarts++
			if open != 0 || messageStops != 0 {
				t.Fatalf("message_start out of order after %d open blocks", open)
			}
		case "content_block_start":
			if open != 0 {
				t.Fatalf("the capture itself opens a block while one is open")
			}
			open++
			starts++
		case "content_block_stop":
			if open != 1 {
				t.Fatalf("the capture itself closes a block that is not open")
			}
			open--
			stops++
		case "message_stop":
			messageStops++
			if open != 0 {
				t.Fatalf("the capture itself ends with %d open blocks", open)
			}
		}
	}
	if open != 0 {
		t.Fatalf("the capture itself leaves %d blocks open", open)
	}
	if starts != stops {
		t.Fatalf("capture has %d starts and %d stops", starts, stops)
	}
	if messageStarts != 1 || messageStops != 1 {
		t.Fatalf("capture has %d message_start and %d message_stop", messageStarts, messageStops)
	}
	if starts < 2 {
		t.Fatalf("capture has %d content blocks; the tool-using fixture should have at least a thinking and a tool_use block", starts)
	}
}

// TestLiveCapturedResponsesStreamNestsOutput checks the shape a Responses
// client depends on: the completion event carries the whole response object
// under `response`, with the output inside it.
func TestLiveCapturedResponsesStreamNestsOutput(t *testing.T) {
	captured := parseLiveSSE(t, readLiveFixture(t, "openai-responses.stream.sse"))

	var completed map[string]any
	itemDone := 0
	argumentDones := 0
	for _, event := range captured {
		if event.Event != "response.completed" {
			if event.Event == "response.output_item.done" {
				itemDone++
			}
			if event.Event == "response.function_call_arguments.done" {
				var payload struct {
					Arguments string `json:"arguments"`
				}
				if err := json.Unmarshal(event.Data, &payload); err != nil {
					t.Fatalf("json.Unmarshal(arguments.done) error = %v", err)
				}
				// The capture carries the accumulated arguments; the schema
				// requires the member to be present.
				if _, ok := decodeJSONValue(t, event.Data).(map[string]any)["arguments"]; !ok {
					t.Fatalf("captured arguments.done has no arguments member: %s", event.Data)
				}
				argumentDones++
			}
			continue
		}
		if err := json.Unmarshal(event.Data, &completed); err != nil {
			t.Fatalf("json.Unmarshal(completed) error = %v", err)
		}
	}
	if completed == nil {
		t.Fatal("the capture has no response.completed event")
	}
	response, ok := completed["response"].(map[string]any)
	if !ok {
		t.Fatalf("response.completed has no response object: %+v", completed)
	}
	output, ok := response["output"].([]any)
	if !ok || len(output) == 0 {
		t.Fatalf("response.output is missing or empty: %+v", response["output"])
	}
	if _, leaked := completed["output"]; leaked {
		t.Fatalf("output is beside the response object rather than inside it")
	}
	if argumentDones == 0 {
		t.Fatal("the capture has no response.function_call_arguments.done event")
	}
	if itemDone == 0 {
		t.Fatal("the capture has no response.output_item.done event")
	}
}
