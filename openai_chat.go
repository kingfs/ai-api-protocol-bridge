package protocolbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

var ErrStreamUnsupported = errors.New("protocolbridge: stream conversion is not implemented")

type OpenAIChatAdapter struct{}

func NewOpenAIChatAdapter() OpenAIChatAdapter {
	return OpenAIChatAdapter{}
}

func (a OpenAIChatAdapter) Protocol() Protocol {
	return ProtocolOpenAIChat
}

func (a OpenAIChatAdapter) DecodeRequest(raw []byte) (*LLMRequest, error) {
	var request openAIChatRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("decode openai chat request: %w", err)
	}

	maxOutputTokens := request.MaxTokens
	if request.MaxCompletionTokens != nil {
		maxOutputTokens = request.MaxCompletionTokens
	}

	llmRequest := &LLMRequest{
		Protocol:          ProtocolOpenAIChat,
		Model:             request.Model,
		Prompt:            make([]Message, 0, len(request.Messages)),
		MaxOutputTokens:   maxOutputTokens,
		Temperature:       request.Temperature,
		StopSequences:     decodeOpenAIStop(asRawMessage(request.Stop)),
		TopP:              request.TopP,
		PresencePenalty:   request.PresencePenalty,
		FrequencyPenalty:  request.FrequencyPenalty,
		Seed:              request.Seed,
		CandidateCount:    request.N,
		ResponseFormat:    decodeOpenAIResponseFormat(asRawMessage(request.ResponseFormat)),
		Reasoning:         decodeReasoningEffort(request.ReasoningEffort),
		ReasoningEffort:   normalizeOpenAIReasoningEffort(request.ReasoningEffort),
		Tools:             decodeOpenAITools(request.Tools),
		ToolChoice:        decodeOpenAIToolChoice(asRawMessage(request.ToolChoice)),
		ParallelToolCalls: request.ParallelToolCalls,
		Stream:            request.Stream,
	}

	for _, message := range request.Messages {
		decoded, err := decodeOpenAIChatMessage(message)
		if err != nil {
			return nil, err
		}
		llmRequest.Prompt = append(llmRequest.Prompt, decoded)
	}

	return llmRequest, nil
}

func (a OpenAIChatAdapter) EncodeRequest(req *LLMRequest, opts EncodeRequestOptions) ([]byte, error) {
	if req == nil {
		return nil, errors.New("encode openai chat request: nil request")
	}

	model := req.Model
	if opts.Model != "" {
		model = opts.Model
	}

	request := openAIChatRequest{
		Model:               model,
		MaxCompletionTokens: positiveTokensOrNil(req.MaxOutputTokens),
		Temperature:         req.Temperature,
		Stop:                encodeOpenAIStop(req.StopSequences),
		TopP:                req.TopP,
		PresencePenalty:     req.PresencePenalty,
		FrequencyPenalty:    req.FrequencyPenalty,
		Seed:                req.Seed,
		N:                   req.CandidateCount,
		ResponseFormat:      encodeOpenAIResponseFormat(req.ResponseFormat),
		ReasoningEffort:     encodeOpenAIReasoningEffort(req.ReasoningEffort, req.Reasoning),
		StreamOptions:       encodeOpenAIStreamOptions(req.Stream),
		Tools:               encodeOpenAITools(req.Tools),
		ToolChoice:          encodeOpenAIToolChoice(req.ToolChoice),
		ParallelToolCalls:   req.ParallelToolCalls,
		Stream:              req.Stream,
	}
	for _, message := range req.Prompt {
		encoded, err := encodeOpenAIChatMessages(message)
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, encoded...)
	}

	return json.Marshal(request)
}

func (a OpenAIChatAdapter) DecodeResponse(raw []byte) (*LLMResponse, error) {
	var response openAIChatResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode openai chat response: %w", err)
	}
	if len(response.Choices) == 0 {
		return nil, errors.New("decode openai chat response: choices is empty")
	}

	choices := make([]LLMChoice, 0, len(response.Choices))
	for _, choice := range response.Choices {
		decoded, err := decodeOpenAIChatMessage(choice.Message)
		if err != nil {
			return nil, err
		}
		choices = append(choices, LLMChoice{Index: choice.Index, Role: decoded.Role, Content: decoded.Parts, FinishReason: decodeOpenAIFinishReason(choice.FinishReason)})
	}

	firstChoice := choices[0]
	if firstChoice.Role == "" {
		firstChoice.Role = RoleAssistant
	}
	decodedContent := firstChoice.Content

	decodedUsage := Usage{}
	if response.Usage != nil {
		decodedUsage = decodeOpenAIUsage(*response.Usage)
	}

	return &LLMResponse{
		Protocol:     ProtocolOpenAIChat,
		ID:           response.ID,
		Model:        response.Model,
		Role:         firstChoice.Role,
		Content:      decodedContent,
		Choices:      choices,
		FinishReason: firstChoice.FinishReason,
		Usage:        decodedUsage,
		ProviderMetadata: map[string]any{
			"object":  response.Object,
			"created": response.Created,
		},
	}, nil
}

func (a OpenAIChatAdapter) EncodeResponse(resp *LLMResponse, opts EncodeResponseOptions) ([]byte, error) {
	if resp == nil {
		return nil, errors.New("encode openai chat response: nil response")
	}

	model := resp.Model
	if opts.Model != "" {
		model = opts.Model
	}

	choices := make([]openAIChatChoice, 0)
	if len(resp.Choices) > 0 {
		for _, choice := range resp.Choices {
			message, err := encodeOpenAIAssistantMessage(choice.Content)
			if err != nil {
				return nil, err
			}
			choices = append(choices, openAIChatChoice{Index: choice.Index, Message: message, FinishReason: encodeOpenAIFinishReason(choice.FinishReason)})
		}
	} else {
		message, err := encodeOpenAIAssistantMessage(resp.Content)
		if err != nil {
			return nil, err
		}
		choices = append(choices, openAIChatChoice{Index: 0, Message: message, FinishReason: encodeOpenAIFinishReason(resp.FinishReason)})
	}

	usage := encodeOpenAIUsage(resp.Usage, resp.BillingUsage())

	response := openAIChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: currentTimestamp(),
		Model:   model,
		Choices: choices,
		Usage:   &usage,
	}

	return json.Marshal(response)
}

func (a OpenAIChatAdapter) NewStreamDecoder(StreamDecodeOptions) (StreamDecoder, error) {
	return &openAIChatStreamDecoder{}, nil
}

func (a OpenAIChatAdapter) NewStreamEncoder(opts StreamEncodeOptions) (StreamEncoder, error) {
	return &openAIChatStreamEncoder{model: opts.Model, toolIndexes: make(map[string]int)}, nil
}

type openAIChatStreamChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []openAIChatStreamChoice    `json:"choices"`
	Usage   *openAIChatStreamChunkUsage `json:"usage,omitempty"`
}

type openAIChatStreamChoice struct {
	Index        int                    `json:"index"`
	Delta        *openAIChatStreamDelta `json:"delta"`
	FinishReason *string                `json:"finish_reason"`
}

type openAIChatStreamDelta struct {
	Role      string                     `json:"role,omitempty"`
	Content   *string                    `json:"content,omitempty"`
	Reasoning *string                    `json:"reasoning_content,omitempty"`
	Refusal   *string                    `json:"refusal,omitempty"`
	ToolCalls []openAIChatStreamToolCall `json:"tool_calls,omitempty"`
}

type openAIChatStreamToolCall struct {
	Index    int                              `json:"index"`
	ID       string                           `json:"id,omitempty"`
	Type     string                           `json:"type,omitempty"`
	Function openAIChatStreamToolCallFunction `json:"function"`
}

type openAIChatStreamToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIChatStreamChunkUsage struct {
	PromptTokens            *int                           `json:"prompt_tokens,omitempty"`
	CompletionTokens        *int                           `json:"completion_tokens,omitempty"`
	TotalTokens             *int                           `json:"total_tokens,omitempty"`
	PromptTokensDetails     *openAIPromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAICompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// openAIChatStreamDecoder turns chat completion chunks into stream parts.
//
// A chat completion chunk is a bare delta: it never announces that a content
// block opened or that one closed. The IR stream protocol does have start and
// end parts, and every encoder relies on them to emit a well formed target
// stream (Responses needs `response.output_item.done`, Anthropic needs
// `content_block_stop`), so the decoder synthesises them here: a block is
// opened by the first delta that carries content for it and closed when the
// choice reports a finish reason, or at Close for a stream that ends without
// one. Block ids follow streamIndexID, so one choice's text, reasoning and tool
// call share an id and are closed together in the order they were opened.
type openAIChatStreamDecoder struct {
	started   bool
	openText  map[string]bool
	openThink map[string]bool
	openTools map[string]bool
}

// closeBlocks emits the end part for every block still open under id, oldest
// first, and forgets them.
func (d *openAIChatStreamDecoder) closeBlocks(id string, parts *[]StreamPart) {
	if d.openThink[id] {
		*parts = append(*parts, StreamPart{Type: StreamReasoningEnd, ID: id})
		delete(d.openThink, id)
	}
	if d.openText[id] {
		*parts = append(*parts, StreamPart{Type: StreamTextEnd, ID: id})
		delete(d.openText, id)
	}
	if d.openTools[id] {
		*parts = append(*parts, StreamPart{Type: StreamToolInputEnd, ID: id})
		delete(d.openTools, id)
	}
}

// closeAllBlocks is the Close-time sweep for a stream that never reported a
// finish reason.
func (d *openAIChatStreamDecoder) closeAllBlocks(parts *[]StreamPart) {
	ids := make([]string, 0, len(d.openThink)+len(d.openText)+len(d.openTools))
	for id := range d.openThink {
		ids = append(ids, id)
	}
	for id := range d.openText {
		if !d.openThink[id] {
			ids = append(ids, id)
		}
	}
	for id := range d.openTools {
		if !d.openThink[id] && !d.openText[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		d.closeBlocks(id, parts)
	}
}

func (d *openAIChatStreamDecoder) startText(id string, parts *[]StreamPart) {
	if !d.openText[id] {
		d.openText[id] = true
		*parts = append(*parts, StreamPart{Type: StreamTextStart, ID: id})
	}
}

func (d *openAIChatStreamDecoder) startReasoning(id string, parts *[]StreamPart) {
	if !d.openThink[id] {
		d.openThink[id] = true
		*parts = append(*parts, StreamPart{Type: StreamReasoningStart, ID: id})
	}
}

func (d *openAIChatStreamDecoder) Decode(event RawStreamEvent) ([]StreamPart, error) {
	if len(bytes.TrimSpace(event.Data)) == 0 {
		return nil, nil
	}
	if string(event.Data) == "[DONE]" {
		return nil, nil
	}

	var chunk openAIChatStreamChunk
	if err := json.Unmarshal(event.Data, &chunk); err != nil {
		return []StreamPart{{Type: StreamRaw, RawValue: string(event.Data)}}, nil
	}

	if d.openText == nil {
		d.openText = make(map[string]bool)
		d.openThink = make(map[string]bool)
		d.openTools = make(map[string]bool)
	}

	parts := make([]StreamPart, 0)
	// Exactly one StreamStart per stream. A chunk that carries delta.role used
	// to produce a second one, which downstream encoders turned into a
	// duplicate lifecycle event (two `message_start` events for one Anthropic
	// response).
	if !d.started {
		d.started = true
		part := StreamPart{Type: StreamStart, ID: chunk.ID, ProviderMetadata: map[string]any{"model": chunk.Model}}
		if chunk.Usage != nil {
			part.Usage = decodeOpenAIUsage(openAIUsage{
				PromptTokens:            chunk.Usage.PromptTokens,
				CompletionTokens:        chunk.Usage.CompletionTokens,
				TotalTokens:             chunk.Usage.TotalTokens,
				PromptTokensDetails:     chunk.Usage.PromptTokensDetails,
				CompletionTokensDetails: chunk.Usage.CompletionTokensDetails,
			})
		}
		parts = append(parts, part)
	}

	for _, choice := range chunk.Choices {
		if choice.Delta == nil {
			continue
		}
		id := streamIndexID(choice.Index)
		if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
			d.startReasoning(id, &parts)
			parts = append(parts, StreamPart{Type: StreamReasoningDelta, ID: id, Delta: *choice.Delta.Reasoning})
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			d.startText(id, &parts)
			parts = append(parts, StreamPart{Type: StreamTextDelta, ID: id, Delta: *choice.Delta.Content})
		}
		if choice.Delta.Refusal != nil && *choice.Delta.Refusal != "" {
			d.startText(id, &parts)
			parts = append(parts, StreamPart{Type: StreamTextDelta, ID: id, Delta: *choice.Delta.Refusal, ProviderMetadata: map[string]any{"refusal": true}})
		}
		for _, tc := range choice.Delta.ToolCalls {
			toolID := streamIndexID(tc.Index)
			if tc.ID != "" {
				if !d.openTools[toolID] {
					d.openTools[toolID] = true
					parts = append(parts, StreamPart{Type: StreamToolInputStart, ID: toolID, ToolCallID: tc.ID, ToolName: tc.Function.Name})
				}
			} else if tc.Function.Arguments != "" && !d.openTools[toolID] {
				// Argument-only chunks are the norm after the first one, so a
				// provider that never repeats the id still gets one open block.
				d.openTools[toolID] = true
				parts = append(parts, StreamPart{Type: StreamToolInputStart, ID: toolID, ToolName: tc.Function.Name})
			}
			if tc.Function.Arguments != "" {
				parts = append(parts, StreamPart{Type: StreamToolInputDelta, ID: toolID, ToolCallID: tc.ID, Delta: tc.Function.Arguments})
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			d.closeBlocks(id, &parts)
			parts = append(parts, StreamPart{Type: StreamFinish, FinishReason: decodeOpenAIFinishReason(*choice.FinishReason)})
		}
	}

	if chunk.Usage != nil && (chunk.Usage.PromptTokens != nil || chunk.Usage.CompletionTokens != nil) {
		parts = append(parts, StreamPart{
			Type: StreamResponseMetadata,
			Usage: decodeOpenAIUsage(openAIUsage{
				PromptTokens:            chunk.Usage.PromptTokens,
				CompletionTokens:        chunk.Usage.CompletionTokens,
				TotalTokens:             chunk.Usage.TotalTokens,
				PromptTokensDetails:     chunk.Usage.PromptTokensDetails,
				CompletionTokensDetails: chunk.Usage.CompletionTokensDetails,
			}),
		})
	}

	return parts, nil
}

// Close flushes any block a stream left open, so an aborted or unterminated
// chat stream still produces a balanced IR stream.
func (d *openAIChatStreamDecoder) Close() ([]StreamPart, error) {
	parts := make([]StreamPart, 0)
	d.closeAllBlocks(&parts)
	return parts, nil
}

type openAIChatStreamEncoder struct {
	model       string
	responseID  string
	started     bool
	finished    bool
	created     int64
	usage       Usage
	nextIndex   int
	toolIndexes map[string]int
	toolNames   map[string]string
	toolInputs  map[string]string
}

func (e *openAIChatStreamEncoder) Encode(part StreamPart) ([]RawStreamEvent, error) {
	if e.toolNames == nil {
		e.toolNames = make(map[string]string)
		e.toolInputs = make(map[string]string)
	}

	switch part.Type {
	case StreamStart:
		e.started = true
		if part.ID != "" {
			e.responseID = part.ID
		}
		mergeUsage(&e.usage, part.Usage)
		chunk := openAIChatStreamChunk{ID: part.ID, Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp()}
		role := "assistant"
		chunk.Choices = []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{Role: role}}}
		return singleOpenAIChatStreamEvent(chunk)
	case StreamTextDelta:
		content := part.Delta
		chunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{Content: &content}}}}
		return singleOpenAIChatStreamEvent(chunk)
	case StreamReasoningDelta:
		reasoning := part.Delta
		chunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{Reasoning: &reasoning}}}}
		return singleOpenAIChatStreamEvent(chunk)
	case StreamToolInputStart:
		idx := e.ensureToolIndex(part.ToolCallID)
		e.toolNames[part.ToolCallID] = part.ToolName
		e.toolInputs[part.ToolCallID] = ""
		tc := openAIChatStreamToolCall{Index: idx, ID: part.ToolCallID, Type: "function", Function: openAIChatStreamToolCallFunction{Name: part.ToolName}}
		chunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{ToolCalls: []openAIChatStreamToolCall{tc}}}}}
		return singleOpenAIChatStreamEvent(chunk)
	case StreamToolInputDelta:
		idx := e.ensureToolIndex(part.ToolCallID)
		e.toolInputs[part.ToolCallID] += part.Delta
		tc := openAIChatStreamToolCall{Index: idx, Function: openAIChatStreamToolCallFunction{Arguments: part.Delta}}
		chunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{ToolCalls: []openAIChatStreamToolCall{tc}}}}}
		return singleOpenAIChatStreamEvent(chunk)
	case StreamToolInputEnd:
		return nil, nil
	case StreamToolCall:
		return e.encodeToolCall(part)
	case StreamFinish:
		e.finished = true
		mergeUsage(&e.usage, part.Usage)
		return e.encodeFinish(part)
	case StreamResponseMetadata:
		mergeUsage(&e.usage, part.Usage)
		return nil, nil
	case StreamError:
		return e.encodeStreamError(part)
	case StreamRaw:
		chunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{Content: strPtr(fmt.Sprint(part.RawValue))}}}}
		return singleOpenAIChatStreamEvent(chunk)
	default:
		return nil, nil
	}
}

func (e *openAIChatStreamEncoder) Close() ([]RawStreamEvent, error) {
	events := make([]RawStreamEvent, 0, 3)
	if !e.finished {
		finishEvents, err := e.encodeFinish(StreamPart{Type: StreamFinish, FinishReason: FinishStop})
		if err != nil {
			return nil, err
		}
		events = append(events, finishEvents...)
	}
	usageEvents, err := e.encodeUsageSummary()
	if err != nil {
		return nil, err
	}
	events = append(events, usageEvents...)
	return append(events, RawStreamEvent{Data: []byte("[DONE]")}), nil
}

func (e *openAIChatStreamEncoder) EncodeError(err error) []RawStreamEvent {
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}
	part := StreamPart{Type: StreamError, Error: map[string]any{"message": message, "type": "protocol_bridge_error"}}
	events, _ := e.encodeStreamError(part)
	return events
}

func (e *openAIChatStreamEncoder) encodeToolCall(part StreamPart) ([]RawStreamEvent, error) {
	toolID := part.ToolCallID
	if toolID == "" {
		toolID = part.ID
	}
	idx := e.ensureToolIndex(toolID)
	name := part.ToolName
	if name == "" {
		name = e.toolNames[toolID]
	}
	input, err := encodeOpenAIToolInput(part.Input)
	if err != nil {
		return nil, err
	}
	var events []RawStreamEvent
	startChunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{ToolCalls: []openAIChatStreamToolCall{{Index: idx, ID: toolID, Type: "function", Function: openAIChatStreamToolCallFunction{Name: name}}}}}}}
	start, err := singleOpenAIChatStreamEvent(startChunk)
	if err != nil {
		return nil, err
	}
	events = append(events, start...)
	deltaChunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{ToolCalls: []openAIChatStreamToolCall{{Index: idx, Function: openAIChatStreamToolCallFunction{Arguments: input}}}}}}}
	delta, err := singleOpenAIChatStreamEvent(deltaChunk)
	if err != nil {
		return nil, err
	}
	events = append(events, delta...)
	delete(e.toolIndexes, toolID)
	return events, nil
}

func (e *openAIChatStreamEncoder) encodeFinish(part StreamPart) ([]RawStreamEvent, error) {
	reason := encodeOpenAIFinishReason(part.FinishReason)
	chunk := openAIChatStreamChunk{Object: "chat.completion.chunk", Model: e.model, Created: e.timestamp(), Choices: []openAIChatStreamChoice{{Index: 0, Delta: &openAIChatStreamDelta{}, FinishReason: &reason}}}
	return singleOpenAIChatStreamEvent(chunk)
}

func (e *openAIChatStreamEncoder) encodeUsageSummary() ([]RawStreamEvent, error) {
	if !hasUsage(e.usage) {
		return nil, nil
	}
	usage := encodeOpenAIUsage(e.usage, billingUsageForProtocol(ProtocolOpenAIChat, e.usage))
	chunk := openAIChatStreamChunk{
		ID:      e.responseID,
		Object:  "chat.completion.chunk",
		Model:   e.model,
		Created: e.timestamp(),
		Choices: []openAIChatStreamChoice{},
		Usage: &openAIChatStreamChunkUsage{
			PromptTokens:            usage.PromptTokens,
			CompletionTokens:        usage.CompletionTokens,
			TotalTokens:             usage.TotalTokens,
			PromptTokensDetails:     usage.PromptTokensDetails,
			CompletionTokensDetails: usage.CompletionTokensDetails,
		},
	}
	return singleOpenAIChatStreamEvent(chunk)
}

func (e *openAIChatStreamEncoder) encodeStreamError(part StreamPart) ([]RawStreamEvent, error) {
	raw, err := json.Marshal(openAIErrorResponse{Error: openAIError{Message: fmt.Sprint(part.Error), Type: "protocol_bridge_error"}})
	if err != nil {
		return nil, err
	}
	return []RawStreamEvent{{Data: raw}}, nil
}

func (e *openAIChatStreamEncoder) ensureToolIndex(id string) int {
	if idx, ok := e.toolIndexes[id]; ok {
		return idx
	}
	idx := e.nextIndex
	e.nextIndex++
	e.toolIndexes[id] = idx
	return idx
}

func singleOpenAIChatStreamEvent(chunk openAIChatStreamChunk) ([]RawStreamEvent, error) {
	raw, err := json.Marshal(chunk)
	if err != nil {
		return nil, err
	}
	return []RawStreamEvent{{Data: raw}}, nil
}

// currentTimestamp returns the Unix timestamp stamped on a chat completion and
// on every chunk of a chat completion stream. The OpenAI schema requires
// `created` on both, and the streaming contract requires every chunk of one
// response to carry the same value, so a response-scoped value must be captured
// once rather than recomputed per chunk. Callers that need a stable stamp
// across chunks should use newResponseTimestamp once per response.
func currentTimestamp() int64 {
	return time.Now().Unix()
}

// timestamp returns the response-scoped `created` value for this stream. The
// first chunk fixes it and every later chunk reuses it, so a stream never
// reports two different creation times.
func (e *openAIChatStreamEncoder) timestamp() int64 {
	if e.created == 0 {
		e.created = currentTimestamp()
	}
	return e.created
}

func strPtr(s string) *string {
	return &s
}

func (a OpenAIChatAdapter) EncodeError(err error) ([]byte, int) {
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}

	raw, marshalErr := json.Marshal(openAIErrorResponse{
		Error: openAIError{
			Message: message,
			Type:    "protocol_bridge_error",
		},
	})
	if marshalErr != nil {
		return []byte(`{"error":{"message":"failed to encode error","type":"protocol_bridge_error"}}`), http.StatusInternalServerError
	}
	return raw, http.StatusBadRequest
}

func decodeOpenAIChatMessage(message openAIChatMessage) (Message, error) {
	role := Role(message.Role)
	decoded := Message{Role: role}

	if strings.TrimSpace(message.Reasoning) != "" {
		decoded.Parts = append(decoded.Parts, Part{Type: PartReasoning, Reasoning: &ReasoningPart{Text: message.Reasoning}})
	}
	if message.Content != nil {
		parts, err := decodeOpenAIChatContent(asRawMessage(message.Content))
		if err != nil {
			return Message{}, err
		}
		decoded.Parts = append(decoded.Parts, parts...)
	}
	if message.Refusal != nil && strings.TrimSpace(*message.Refusal) != "" {
		decoded.Parts = append(decoded.Parts, Part{Type: PartRefusal, Refusal: &RefusalPart{Text: *message.Refusal}})
	}

	for _, toolCall := range message.ToolCalls {
		input, err := decodeOpenAIToolInput(toolCall.Function.Arguments)
		if err != nil {
			return Message{}, err
		}
		decoded.Parts = append(decoded.Parts, Part{
			Type: PartToolCall,
			ToolCall: &ToolCallPart{
				ToolCallID: toolCall.ID,
				ToolName:   toolCall.Function.Name,
				Input:      input,
			},
		})
	}

	if role == RoleTool {
		parts, err := decodeOpenAIChatContent(asRawMessage(message.Content))
		if err != nil {
			return Message{}, err
		}
		decoded.Parts = []Part{
			{
				Type: PartToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: message.ToolCallID,
					Output: ToolResultOutput{
						Type: ToolResultText,
						Text: joinTextParts(parts),
					},
				},
			},
		}
	}

	return decoded, nil
}

func decodeReasoningEffort(effort string) *bool {
	if strings.TrimSpace(effort) == "" {
		return nil
	}
	enabled := true
	return &enabled
}

// normalizeOpenAIReasoningEffort clamps an effort onto the enum the OpenAI
// schemas share, ["low", "medium", "high"]. An unrecognised or absent level
// yields "", so a caller that only knows reasoning is on falls back to a level
// instead of emitting an out-of-enum value such as "xhigh".
func normalizeOpenAIReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "minimal":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh":
		return "high"
	default:
		return ""
	}
}

// encodeOpenAIReasoningEffort prefers an explicit level over the boolean
// "reasoning is on" flag, because the level is what the caller actually asked
// for. It used to drop every level and always send "medium".
func encodeOpenAIReasoningEffort(effort string, reasoning *bool) string {
	if normalized := normalizeOpenAIReasoningEffort(effort); normalized != "" {
		return normalized
	}
	if reasoning != nil && *reasoning {
		return "medium"
	}
	return ""
}

func encodeOpenAIStreamOptions(stream bool) any {
	if !stream {
		return nil
	}
	return map[string]any{"include_usage": true}
}

func encodeOpenAIChatMessages(message Message) ([]openAIChatMessage, error) {
	if message.Role == RoleTool {
		return encodeOpenAIToolMessages(message), nil
	}

	// A tool result does not have to arrive as a tool-role message: Anthropic
	// expresses it as a user message containing a tool-result part. The loop
	// below reads only text, refusal and tool-call parts, so such a message
	// would encode as an empty user turn and the tool's output would be
	// dropped. Emit the tool messages first, which is where the chat protocol
	// requires them (directly after the assistant turn that made the call), and
	// encode anything else the message carries under its own role.
	if hasToolResultPart(message.Parts) {
		encoded := encodeOpenAIToolMessages(message)
		remaining := withoutToolResultParts(message)
		if len(remaining.Parts) == 0 {
			return encoded, nil
		}
		rest, err := encodeOpenAIChatMessages(remaining)
		if err != nil {
			return nil, err
		}
		return append(encoded, rest...), nil
	}

	encoded := openAIChatMessage{
		Role:    string(message.Role),
		Content: encodeOpenAITextContent(message.Parts),
	}

	for _, part := range message.Parts {
		if part.Type == PartRefusal && part.Refusal != nil {
			refusal := part.Refusal.Text
			encoded.Refusal = &refusal
			continue
		}
		if part.Type != PartToolCall || part.ToolCall == nil {
			continue
		}
		arguments, err := encodeOpenAIToolInput(part.ToolCall.Input)
		if err != nil {
			return nil, err
		}
		encoded.ToolCalls = append(encoded.ToolCalls, openAIChatToolCall{
			ID:   part.ToolCall.ToolCallID,
			Type: "function",
			Function: openAIChatToolCallFunction{
				Name:      part.ToolCall.ToolName,
				Arguments: arguments,
			},
		})
	}

	// A response message must carry both `content` and `refusal`, so a
	// tool-call-only assistant message keeps a null content instead of
	// dropping the key, and an absent refusal serialises as null.
	if encoded.Content == "" {
		encoded.Content = nil
	}

	return []openAIChatMessage{encoded}, nil
}

func hasToolResultPart(parts []Part) bool {
	for _, part := range parts {
		if part.Type == PartToolResult && part.ToolResult != nil {
			return true
		}
	}
	return false
}

// withoutToolResultParts returns a copy of the message with its tool-result
// parts removed, leaving the caller's message untouched.
func withoutToolResultParts(message Message) Message {
	parts := make([]Part, 0, len(message.Parts))
	for _, part := range message.Parts {
		if part.Type == PartToolResult && part.ToolResult != nil {
			continue
		}
		parts = append(parts, part)
	}
	message.Parts = parts
	return message
}

func encodeOpenAIToolMessages(message Message) []openAIChatMessage {
	encoded := make([]openAIChatMessage, 0)
	for _, part := range message.Parts {
		if part.Type != PartToolResult || part.ToolResult == nil {
			continue
		}
		encoded = append(encoded, openAIChatMessage{
			Role:       string(RoleTool),
			ToolCallID: part.ToolResult.ToolCallID,
			Content:    encodeOpenAIToolOutput(part.ToolResult.Output),
		})
	}
	return encoded
}

func encodeOpenAIAssistantMessage(content []Part) (openAIChatMessage, error) {
	message := Message{Role: RoleAssistant, Parts: content}
	encoded, err := encodeOpenAIChatMessages(message)
	if err != nil {
		return openAIChatMessage{}, err
	}
	if len(encoded) == 0 {
		return openAIChatMessage{Role: string(RoleAssistant)}, nil
	}
	return encoded[0], nil
}

func decodeOpenAIChatContent(raw json.RawMessage) ([]Part, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []Part{{Type: PartText, Text: &TextPart{Text: text}}}, nil
	}

	var parts []openAIChatContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("decode openai chat content: %w", err)
	}

	decoded := make([]Part, 0, len(parts))
	for _, part := range parts {
		if part.Type == "text" {
			decoded = append(decoded, Part{Type: PartText, Text: &TextPart{Text: part.Text}})
			continue
		}
		if part.Type == "image_url" && part.ImageURL != nil {
			file := decodeFileURL(part.ImageURL.URL, FileImage)
			file.Detail = part.ImageURL.Detail
			decoded = append(decoded, Part{Type: PartFile, File: file})
			continue
		}
		if part.Type == "file" && part.File != nil {
			file := &FilePart{Type: FileDocument, Data: part.File.FileData, FileID: part.File.FileID, Filename: part.File.Filename}
			decoded = append(decoded, Part{Type: PartFile, File: file})
		}
	}
	return decoded, nil
}

func encodeOpenAITextContent(parts []Part) any {
	encoded := make([]openAIChatContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Type == PartText && part.Text != nil {
			encoded = append(encoded, openAIChatContentPart{Type: "text", Text: part.Text.Text})
			continue
		}
		if part.Type == PartFile && part.File != nil && part.File.Type == FileImage {
			url := encodeFileURL(part.File)
			if url == "" {
				continue
			}
			encoded = append(encoded, openAIChatContentPart{Type: "image_url", ImageURL: &openAIChatImageURL{URL: url, Detail: part.File.Detail}})
			continue
		}
		if part.Type == PartFile && part.File != nil && part.File.Type == FileDocument {
			file := openAIChatFilePart{FileData: part.File.Data, FileID: part.File.FileID, Filename: part.File.Filename}
			if file.FileData == "" && file.FileID == "" {
				continue
			}
			encoded = append(encoded, openAIChatContentPart{Type: "file", File: &file})
		}
	}
	if len(encoded) == 0 {
		return ""
	}
	if len(encoded) == 1 && encoded[0].Type == "text" {
		return encoded[0].Text
	}
	return encoded
}

func joinTextParts(parts []Part) string {
	var builder strings.Builder
	for _, part := range parts {
		if part.Type == PartText && part.Text != nil {
			builder.WriteString(part.Text.Text)
		}
	}
	return builder.String()
}

func joinReasoningParts(parts []Part) string {
	var builder strings.Builder
	for _, part := range parts {
		if part.Type == PartReasoning && part.Reasoning != nil {
			builder.WriteString(part.Reasoning.Text)
		}
	}
	return builder.String()
}

func decodeOpenAIToolInput(arguments string) (any, error) {
	if strings.TrimSpace(arguments) == "" {
		return nil, nil
	}

	var input any
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return arguments, nil
	}
	return input, nil
}

func encodeOpenAIToolInput(input any) (string, error) {
	if input == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode openai tool input: %w", err)
	}
	return string(raw), nil
}

func encodeOpenAIToolOutput(output ToolResultOutput) any {
	switch output.Type {
	case ToolResultJSON, ToolResultErrorJSON:
		raw, err := json.Marshal(output.JSON)
		if err != nil {
			return "null"
		}
		return string(raw)
	case ToolResultContent:
		return joinTextParts(output.Content)
	default:
		return output.Text
	}
}

func decodeOpenAITools(tools []openAIChatTool) []Tool {
	decoded := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		decoded = append(decoded, Tool{
			Type:        ToolFunction,
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
			Strict:      tool.Function.Strict,
		})
	}
	return decoded
}

func encodeOpenAITools(tools []Tool) []openAIChatTool {
	encoded := make([]openAIChatTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != ToolFunction {
			continue
		}
		encoded = append(encoded, openAIChatTool{
			Type: "function",
			Function: openAIChatFunctionTool{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
				Strict:      tool.Strict,
			},
		})
	}
	return encoded
}

func decodeOpenAIToolChoice(raw json.RawMessage) *ToolChoice {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		switch value {
		case "auto":
			return &ToolChoice{Type: ToolChoiceAuto}
		case "none":
			return &ToolChoice{Type: ToolChoiceNone}
		case "required":
			return &ToolChoice{Type: ToolChoiceRequired}
		}
	}

	var object struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &object); err == nil && object.Type == "function" {
		return &ToolChoice{Type: ToolChoiceTool, ToolName: object.Function.Name}
	}

	return nil
}

func encodeOpenAIToolChoice(choice *ToolChoice) any {
	if choice == nil {
		return nil
	}
	switch choice.Type {
	case ToolChoiceAuto:
		return "auto"
	case ToolChoiceNone:
		return "none"
	case ToolChoiceRequired:
		return "required"
	case ToolChoiceTool:
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": choice.ToolName,
			},
		}
	default:
		return nil
	}
}

func decodeOpenAIResponseFormat(raw json.RawMessage) *ResponseFormat {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var object struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Schema      map[string]any `json:"schema"`
			Strict      *bool          `json:"strict"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil
	}

	switch object.Type {
	case "json_object":
		return &ResponseFormat{Type: ResponseFormatJSON}
	case "json_schema":
		return &ResponseFormat{
			Type:        ResponseFormatJSON,
			Schema:      object.JSONSchema.Schema,
			Name:        object.JSONSchema.Name,
			Description: object.JSONSchema.Description,
			Strict:      object.JSONSchema.Strict,
		}
	case "text":
		return &ResponseFormat{Type: ResponseFormatText}
	default:
		return nil
	}
}

func encodeOpenAIResponseFormat(format *ResponseFormat) any {
	if format == nil || format.Type == ResponseFormatText {
		return nil
	}
	if format.Type != ResponseFormatJSON {
		return nil
	}
	if format.Schema == nil {
		return map[string]any{"type": "json_object"}
	}
	jsonSchema := map[string]any{
		"name":   format.Name,
		"schema": format.Schema,
	}
	if format.Description != "" {
		jsonSchema["description"] = format.Description
	}
	// `strict` is a boolean in the schema; emitting null for an unset value
	// would be invalid, so it is omitted and the server default applies.
	if format.Strict != nil {
		jsonSchema["strict"] = *format.Strict
	}
	return map[string]any{
		"type":        "json_schema",
		"json_schema": jsonSchema,
	}
}

func decodeOpenAIStop(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return []string{value}
	}

	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values
	}
	return nil
}

func encodeOpenAIStop(stop []string) any {
	if len(stop) == 0 {
		return nil
	}
	if len(stop) == 1 {
		return stop[0]
	}
	return stop
}

func decodeOpenAIFinishReason(reason string) FinishReason {
	switch reason {
	case "stop":
		return FinishStop
	case "length":
		return FinishLength
	case "content_filter":
		return FinishContentFilter
	case "tool_calls", "function_call":
		return FinishToolCalls
	case "":
		return FinishUnknown
	default:
		return FinishOther
	}
}

func encodeOpenAIFinishReason(reason FinishReason) string {
	switch reason {
	case FinishStop:
		return "stop"
	case FinishLength:
		return "length"
	case FinishContentFilter:
		return "content_filter"
	case FinishToolCalls:
		return "tool_calls"
	default:
		return "stop"
	}
}

func decodeOpenAIUsage(usage openAIUsage) Usage {
	decoded := Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.PromptTokensDetails != nil {
		decoded.CachedInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	if usage.CompletionTokensDetails != nil {
		decoded.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	}
	return decoded
}

func encodeOpenAIUsage(usage Usage, billingUsage BillingUsage) openAIUsage {
	if hasBillingUsage(billingUsage) {
		inputTokens := billingUsage.InputTokens + billingUsage.CachedInputTokens
		outputTokens := billingUsage.OutputTokens
		cachedInputTokens := billingUsage.CachedInputTokens
		encoded := openAIUsage{
			PromptTokens:     &inputTokens,
			CompletionTokens: &outputTokens,
			TotalTokens:      calculateTotalTokens(&inputTokens, &outputTokens),
		}
		if cachedInputTokens > 0 {
			encoded.PromptTokensDetails = &openAIPromptTokensDetails{CachedTokens: &cachedInputTokens}
		}
		if usage.ReasoningTokens != nil {
			encoded.CompletionTokensDetails = &openAICompletionTokensDetails{ReasoningTokens: usage.ReasoningTokens}
		}
		return encoded
	}
	// CompletionUsage requires prompt_tokens, completion_tokens and
	// total_tokens whenever `usage` is present, so a missing counter is
	// reported as zero instead of being dropped.
	inputTokens := usage.InputTokens
	if inputTokens == nil {
		zero := 0
		inputTokens = &zero
	}
	outputTokens := usage.OutputTokens
	if outputTokens == nil {
		zero := 0
		outputTokens = &zero
	}
	encoded := openAIUsage{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      calculateTotalTokens(inputTokens, outputTokens),
	}
	if usage.CachedInputTokens != nil {
		encoded.PromptTokensDetails = &openAIPromptTokensDetails{CachedTokens: usage.CachedInputTokens}
	}
	if usage.ReasoningTokens != nil {
		encoded.CompletionTokensDetails = &openAICompletionTokensDetails{ReasoningTokens: usage.ReasoningTokens}
	}
	return encoded
}

type openAIChatRequest struct {
	Model               string              `json:"model"`
	Messages            []openAIChatMessage `json:"messages"`
	MaxTokens           *int                `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                `json:"max_completion_tokens,omitempty"`
	Temperature         *float64            `json:"temperature,omitempty"`
	Stop                any                 `json:"stop,omitempty"`
	TopP                *float64            `json:"top_p,omitempty"`
	PresencePenalty     *float64            `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64            `json:"frequency_penalty,omitempty"`
	Seed                *int64              `json:"seed,omitempty"`
	N                   *int                `json:"n,omitempty"`
	ResponseFormat      any                 `json:"response_format,omitempty"`
	ReasoningEffort     string              `json:"reasoning_effort,omitempty"`
	StreamOptions       any                 `json:"stream_options,omitempty"`
	Tools               []openAIChatTool    `json:"tools,omitempty"`
	ToolChoice          any                 `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool               `json:"parallel_tool_calls,omitempty"`
	Stream              bool                `json:"stream,omitempty"`
}

func (r *openAIChatRequest) UnmarshalJSON(raw []byte) error {
	type alias openAIChatRequest
	var decoded struct {
		alias
		Stop           json.RawMessage `json:"stop"`
		ResponseFormat json.RawMessage `json:"response_format"`
		ToolChoice     json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*r = openAIChatRequest(decoded.alias)
	r.Stop = decoded.Stop
	r.ResponseFormat = decoded.ResponseFormat
	r.ToolChoice = decoded.ToolChoice
	return nil
}

type openAIChatMessage struct {
	Role string `json:"role"`
	// Content is required by every message schema and is nullable, so it is
	// always emitted; a nil value serialises as `null`, which is what the
	// official API returns for an assistant message that only calls tools.
	Content    any                  `json:"content"`
	Reasoning  string               `json:"reasoning_content,omitempty"`
	Refusal    *string              `json:"refusal"`
	ToolCalls  []openAIChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
}

func (m *openAIChatMessage) UnmarshalJSON(raw []byte) error {
	var decoded struct {
		Role       string               `json:"role"`
		Content    json.RawMessage      `json:"content"`
		Reasoning  string               `json:"reasoning_content"`
		Refusal    *string              `json:"refusal"`
		ToolCalls  []openAIChatToolCall `json:"tool_calls"`
		ToolCallID string               `json:"tool_call_id"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	m.Role = decoded.Role
	m.Content = decoded.Content
	m.Reasoning = decoded.Reasoning
	m.Refusal = decoded.Refusal
	m.ToolCalls = decoded.ToolCalls
	m.ToolCallID = decoded.ToolCallID
	return nil
}

type openAIChatContentPart struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *openAIChatImageURL `json:"image_url,omitempty"`
	File     *openAIChatFilePart `json:"file,omitempty"`
}

type openAIChatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type openAIChatFilePart struct {
	FileData string `json:"file_data,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type openAIChatToolCall struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function openAIChatToolCallFunction `json:"function"`
}

type openAIChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatTool struct {
	Type     string                 `json:"type"`
	Function openAIChatFunctionTool `json:"function"`
}

type openAIChatFunctionTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      *bool          `json:"strict,omitempty"`
}

// openAIChatResponse is the non-streaming chat completion envelope. The schema
// requires id, object, created, model and choices on every response, so none of
// them may be omitted; usage is optional, so it stays a pointer.
type openAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIChatChoice `json:"choices"`
	Usage   *openAIUsage       `json:"usage,omitempty"`
}

type openAIChatChoice struct {
	Index        int               `json:"index"`
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
	// Logprobs is required by the schema but nullable; the IR has no
	// representation for token log probabilities, so this is always null.
	Logprobs any `json:"logprobs"`
}

type openAIUsage struct {
	PromptTokens            *int                           `json:"prompt_tokens,omitempty"`
	CompletionTokens        *int                           `json:"completion_tokens,omitempty"`
	TotalTokens             *int                           `json:"total_tokens,omitempty"`
	PromptTokensDetails     *openAIPromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAICompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type openAIPromptTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}

type openAICompletionTokensDetails struct {
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
}

type openAIErrorResponse struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func asRawMessage(value any) json.RawMessage {
	switch typed := value.(type) {
	case nil:
		return nil
	case json.RawMessage:
		return typed
	case []byte:
		return typed
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return nil
		}
		return raw
	}
}
