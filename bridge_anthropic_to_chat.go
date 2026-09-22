package protocolbridge

import "fmt"

// anthropicToOpenAIChatBridge serves an Anthropic-messages client against an
// OpenAI chat-completions upstream.
//
// It is the missing fourth cross-family cell. An Anthropic inbound has two
// possible OpenAI targets, so a caller that knows which one its upstream speaks
// must be able to ask for it by protocol rather than by family — see
// NewCrossFamilyBridgeForProtocol.
//
// The upstream half needs no hand-written encoder here: the caller has already
// decoded the Anthropic request into the neutral LLMRequest, and the chat
// adapter's own encoder is the authority on that wire format. Hand-building it,
// as the Responses bridge must, would only duplicate the adapter.
type anthropicToOpenAIChatBridge struct {
	adapter OpenAIChatAdapter
}

func (b anthropicToOpenAIChatBridge) InboundProtocol() Protocol {
	return ProtocolAnthropicMessages
}

func (b anthropicToOpenAIChatBridge) UpstreamProtocol() Protocol {
	return ProtocolOpenAIChat
}

func (b anthropicToOpenAIChatBridge) EncodeUpstreamRequest(req *LLMRequest, opts EncodeRequestOptions) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("encode anthropic to openai chat request: nil request")
	}
	return b.adapter.EncodeRequest(req, opts)
}

func (b anthropicToOpenAIChatBridge) DecodeUpstreamResponse(raw []byte) (*LLMResponse, error) {
	resp, err := b.adapter.DecodeResponse(raw)
	if err != nil {
		return nil, err
	}
	resp.Protocol = ProtocolOpenAIChat
	return resp, nil
}

func (b anthropicToOpenAIChatBridge) NewStreamDecoder(opts StreamDecodeOptions) (StreamDecoder, error) {
	return b.adapter.NewStreamDecoder(opts)
}

func (b anthropicToOpenAIChatBridge) NewStreamEncoder(opts StreamEncodeOptions) (StreamEncoder, error) {
	return &anthropicStreamEncoderForChatUpstream{ant: anthropicStreamEncoder{model: opts.Model}}, nil
}

// anthropicStreamEncoderForChatUpstream writes neutral stream parts as Anthropic
// SSE for a chat-completions upstream.
//
// The usage has to be rebased on the way out. The chat decoder reports prompt
// tokens the way OpenAI does — the total, with the cached portion broken out
// separately — while Anthropic expects input_tokens to exclude what was served
// from cache and reports the cached count in cache_read_input_tokens. Passing
// the chat numbers straight through would count every cached token twice.
type anthropicStreamEncoderForChatUpstream struct {
	ant anthropicStreamEncoder
}

func (e *anthropicStreamEncoderForChatUpstream) Encode(part StreamPart) ([]RawStreamEvent, error) {
	if part.Type == StreamStart || part.Type == StreamFinish || part.Type == StreamResponseMetadata {
		part.Usage = responsesUsageToAnthropicUsage(part.Usage)
	}
	if part.Type == StreamReasoningDelta {
		if signature, ok := part.ProviderMetadata["signature"].(string); ok && signature != "" && part.Delta == "" {
			return e.ant.Encode(StreamPart{Type: StreamReasoningDelta, ID: part.ID, ProviderMetadata: map[string]any{"signature": signature}})
		}
	}
	return e.ant.Encode(part)
}

func (e *anthropicStreamEncoderForChatUpstream) Close() ([]RawStreamEvent, error) {
	return e.ant.Close()
}

func (e *anthropicStreamEncoderForChatUpstream) EncodeError(err error) []RawStreamEvent {
	return e.ant.EncodeError(err)
}
