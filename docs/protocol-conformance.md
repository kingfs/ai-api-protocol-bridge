# Protocol conformance and cross-protocol convertibility

This document answers two questions with evidence rather than opinion:

1. **Is our per-protocol parsing correct?** For each of the three protocols this
   library implements, how much of the official message format does the adapter
   actually decode, and is what it emits still valid according to the official
   format?
2. **Given correct per-protocol handling, what actually survives a conversion
   between families?** Which fields convert exactly, which convert lossily, and
   which cannot be expressed in the target protocol at all?

The standard is the checked-in schema in [`protocols/`](../protocols/README.md),
extracted from the official OpenAI OpenAPI document and the official Anthropic
API reference. Section [Reproducing the evidence](#reproducing-the-evidence)
lists the commands; the `TestOfficial*` / `TestEncoded*` tests in
[`schema_conformance_test.go`](../schema_conformance_test.go) enforce the parts
of this document that are mechanically checkable, and
[`schema_support_test.go`](../schema_support_test.go) implements the small JSON
Schema subset validator they use.

**Status of this document.** It reports the current implementation as found. No
`.go` implementation file was changed to produce it — see
[Recommended remediation](#recommended-remediation).

---

## 1. How the standard was built

Every `protocols/*.schema.json` bundle is self-contained (every `$ref` resolves
inside its own bundle), carries `source.url` + `source.sha256` provenance, and
inlines the official examples for the same endpoint. Nothing in the library
imports these files; they exist for tests and for this audit.

| Protocol | Bundle | Upstream | Kind |
| --- | --- | --- | --- |
| OpenAI Chat Completions | `openai/chat-completions.schema.json` (51 defs) | OpenAI OpenAPI 3.0, `info.version` 2.3.0, digest `6a6c681b…67cad9` | official OpenAPI document |
| OpenAI Responses | `openai/responses.schema.json` (115 defs) | same document | official OpenAPI document |
| Anthropic Messages | `anthropic/messages.schema.json` (231 defs) | `platform.claude.com/docs/en/api/messages.md`, digest `b5d3e670…7ae8ae` | official API reference (no OpenAPI exists) |

Two properties of the standard matter when reading the findings below:

- **OpenAI sets no `additionalProperties: false` anywhere** in either bundle
  (only two occurrences in the whole extraction). An emitted property that the
  schema does not document is therefore *undocumented*, not *rejected*.
  Missing **required** properties, by contrast, are hard violations, and those
  are what the tests assert.
- **The Anthropic bundle is derived from prose.** `required` there means "the
  reference does not mark this member optional." It is evidence about the
  documented shape, not proof that the live API rejects a missing field. Every
  Anthropic "missing required" finding below is flagged accordingly.

---

## 2. What the executed tests prove

`go test ./...` passes. The conformance tests assert three kinds of statement:

| Test | Claim |
| --- | --- |
| `TestOfficialSchemaBundlesAreWellFormed` | every bundle is closed, every `$ref` resolves, `documents` point at real `$defs`, `oneOf`/`anyOf` arms exist |
| `TestOfficialExamplePayloadsDecode` | every official example *request* and *response* decodes without error |
| `TestOfficialExampleRequestsReencodeWithinSchema` | re-encoding a decoded official request stays within the request schema — **except** for the exact problem lists pinned in the test |
| `TestEncodedResponsesWithinSchema` | an encoded response built from a hand-made IR sample reports exactly the listed schema violations |
| `TestAnthropicOfficialStreamTranscriptsDecode` | the complete official SSE transcripts decode |

The response tests intentionally **pin the current violations**: if a gap is
fixed, the test fails and tells you to update this document and `protocols/`
together. A passing suite therefore does *not* mean the encoders are conformant —
read the pinned lists as a to-do list, reproduced in §3.

Observed results:

- **All official examples decode without error.** Chat: 5 requests, 4 responses.
  Responses: 7 requests, 6 responses plus a streaming example with `response: null`.
  Anthropic: 1 request/response pair. Streaming examples carry no response body
  and are skipped by the response tests.
- **Request re-encode is clean except for one case.** Chat (all 5) and Anthropic
  (1) re-encode with zero problems. Responses fails on the tool example because
  the official `FunctionTool` requires `strict` and the adapter omits it when the
  IR leaves it nil.
- **Response re-encode is not conformant for any of the three protocols.** The
  pinned violation lists are in §3.2, §3.4 and §3.6.

---

## 3. Per-protocol conformance

### 3.1 OpenAI Chat Completions — request

Decode covers 16 of the 31 documented `CreateChatCompletionRequest` properties.
Decoded: `messages`, `model`, `temperature`, `top_p`, `max_completion_tokens`,
`max_tokens`, `frequency_penalty`, `presence_penalty`, `response_format`,
`stop`, `n`, `seed`, `stream`, `tools`, `tool_choice`, `parallel_tool_calls`.

Documented and not decoded (no wire field, so the value is dropped silently):
`metadata`, `user`, `service_tier`, `modalities`, `web_search_options`,
`top_logprobs`, `logprobs`, `audio`, `store`, `logit_bias`, `prediction`,
`function_call`, `functions`.

Message-level gaps:

- `name` on any request role is dropped — the wire message struct has no `Name`.
- `content[].type = "input_audio"` and `content[].type = "refusal"` on assistant
  messages have no decode branch and no `Part` type; those content parts are
  dropped. `role: "function"` has no IR constant.
- `message.audio`, `message.function_call` (deprecated) are dropped.
- `tools[].type != "function"` is skipped silently, so a non-function tool in the
  request becomes fewer tools rather than an error.
- `stream_options.include_usage` is never read on decode.
- `reasoning_effort` is collapsed to `*bool` (`decodeReasoningEffort`), discarding
  the `low`/`medium`/`high` value. The IR already has `LLMRequest.ReasoningEffort`;
  this adapter never populates it.
- `reasoning_content` on a message is read as reasoning even though it is not a
  documented Chat property (it is a DeepSeek/vLLM-style extension).

Request encode violations (hard):

| # | Violation | Cause |
| --- | --- | --- |
| CR1 | `"messages": null` when `req.Prompt` is empty | `messages` is a required array with `minItems: 1` and not nullable |
| CR2 | `"schema": null` inside `response_format.json_schema` when the IR schema is nil | `ResponseFormatJsonSchemaSchema` is `type: object` |
| CR3 | `n` is not range-checked | official bounds are `minimum: 1`, `maximum: 128` |
| CR4 | `stop` array is not capped at 4 entries | `StopConfiguration` is `string \| array` with `maxItems: 4` |
| CR5 | `tools` is not capped at 128, and non-function tools are dropped rather than rejected | `maxItems: 128` |

Request encode defects (undocumented output):

- `refusal` is written on messages of any role; only assistant messages document it.
- `image_url` / `file` content parts are emitted for system, developer, assistant
  and tool roles, but those role schemas allow text only (assistant: text and
  refusal).
- IR `PartReasoning` is dropped entirely on request encode.
- `max_tokens` is never emitted, only `max_completion_tokens`, so older endpoints
  that require the legacy name cannot be targeted.
- A missing token limit is invented as `4096` (`maxOutputTokensOrDefault`), and
  because the adapter always emits the field, decoding a request that omitted it
  and re-encoding changes its semantics.

### 3.2 OpenAI Chat Completions — response

Decode losses: `choices[].logprobs` (the object is required by the schema, has no
wire field, and is dropped), `message.annotations`, `message.function_call`,
`message.audio`, `service_tier`, `system_fingerprint`,
`usage.prompt_tokens_details.audio_tokens`,
`usage.completion_tokens_details.{audio,accepted_prediction,rejected_prediction}_tokens`
(the IR `Usage` has no slots for these), and `usage.total_tokens` (dropped, then
recomputed on encode).

Encode violations — the pinned list produced by `TestEncodedResponsesWithinSchema`:

```text
missing-required (root)/created
missing-required /choices/0/logprobs
missing-required /choices/0/message/refusal
```

plus, for a tool-call-only assistant message, `missing-required /choices/0/message/content`.

| # | Violation | Cause |
| --- | --- | --- |
| CS1 | `created` never emitted | the wire field is `omitempty` and is never assigned; the schema requires it |
| CS2 | `choices[].logprobs` never emitted | the wire choice has only index/message/finish_reason; required by the schema |
| CS3 | `message.content` omitted for tool-call-only messages | the encoder nulls content when tool calls are present; the schema requires the property (nullable, but present) |
| CS4 | `message.refusal` omitted whenever empty | `omitempty` on a required, nullable property |
| CS5 | `id` omitted when the response ID is empty | `omitempty` on a required property |
| CS6 | `"usage": {}` is reachable | struct-tagged `omitempty` does not omit a struct; an empty `CompletionUsage` violates its required token triple |
| CS7 | `response_format.json_schema` emitted with `"strict": null` | the schema requires a boolean when the property is present |
| CS8 | unknown finish reasons are rewritten to `"stop"` | `FinishUnknown` / `FinishOther` / `FinishError` all become a false natural stop |
| CS9 | `reasoning_effort` round-trip is broken | decode discards the value, encode always writes `"medium"`; `LLMRequest.ReasoningEffort` is dead for this adapter |

### 3.3 OpenAI Chat Completions — streaming

The stream chunk schema requires `id`, `object`, `created`, `model`, `choices`.
Encode violations:

- `created` is **always `0`** — `currentTimestamp()` is a stub returning 0.
- `id` is `""` on every content, tool and finish chunk even though the response ID
  is known; the field has no `omitempty`.
- `delta.reasoning_content` is emitted for reasoning deltas. It is **not** a
  documented `ChatCompletionStreamResponseDelta` property (the documented delta has
  only `content`, `function_call`, `tool_calls`, `role`, `refusal`).

Decode defects:

- A first chunk carrying `delta.role` produces **two** `StreamStart` parts (one
  from the `!d.started` branch, one from the role branch). Downstream encoders
  that map `StreamStart` to a lifecycle event emit it twice.
- `delta.tool_calls[].type` is never validated; the tool name is only kept when
  the same delta also carries an `id`.
- Empty `delta.content` and `delta.refusal` strings are dropped (this matches the
  official first chunk, which sends `"content":""`).
- A usage chunk carrying only `total_tokens` is dropped, because the metadata part
  is gated on `prompt_tokens`/`completion_tokens` being present.
- `delta.function_call` (deprecated) is ignored.
- The decoder never emits `StreamTextStart`/`End`, `StreamReasoningStart`,
  `StreamToolInputEnd` or `StreamToolCall`, and `Close()` emits nothing; the
  encoder discards `StreamToolInputEnd`.
- Stream errors are stringified from a Go map into the `message` field.

Lossy by design (asserted by existing tests): `metadata`, `user`, `service_tier`,
`logprobs`, `top_logprobs`, `store`, `prediction`, `modalities`, `audio` are
treated as provider-specific and stripped in both directions; `max_tokens` is
never emitted; `total_tokens` is recomputed.

### 3.4 OpenAI Responses — request

Decode covers `model`, `input`, `instructions`, `max_output_tokens`,
`temperature`, `top_p`, `previous_response_id`, `reasoning` (partially),
`text`, `tools`, `tool_choice` (partially), `include`, `parallel_tool_calls`,
`stream`. Not decoded: `metadata`, `user`, `service_tier`, `truncation`, `store`.

Notable losses and inventions:

- All system and developer messages are concatenated into one `instructions`
  string joined with `"\n"`; message boundaries and the `developer` role are lost
  and cannot round-trip.
- `reasoning.generate_summary` is ignored.
- `tool_choice` of a provider type (`file_search`, `web_search_preview`,
  `computer_use_preview`) decodes to `nil`.
- `include` is passed through without validating against `Includable`.
- `conversation` is accepted even though it is outside the extracted scope.
- A missing `max_output_tokens` is invented as `4096`.
- `FunctionTool` is emitted without `strict` and without `parameters` when the IR
  leaves them empty, both of which the official tool object requires. This is the
  one pinned request-side failure:
  `Functions: no-branch /tools/0 oneOf` (closest branch: missing required `strict`).

### 3.5 OpenAI Responses — response

`OutputItem` is a union of six members. The adapter handles `message`,
`reasoning` and `function_call`, plus several types that are *not* output item
members (`function_call_output`, `output_text`, `custom_tool_call`,
`image_generation_call`, `input_image`). It silently drops the provider tool
calls that *are* members: `file_search_call`, `web_search_call`,
`computer_call`, taking their annotations (`url_citation`, `file_citation`) with
them. `ItemReferenceParam` inputs are dropped.

The required `Response` property list is
`[id, object, created_at, error, incomplete_details, instructions, model, tools,
output, parallel_tool_calls, metadata, tool_choice, temperature, top_p]`.
`EncodeResponse` sets only `id`, `object`, `status`, `model`, `output_text`,
`output`, `usage`. The pinned violations:

```text
missing-required (root)/created_at
missing-required (root)/error
missing-required (root)/incomplete_details
missing-required (root)/instructions
missing-required (root)/metadata
missing-required (root)/parallel_tool_calls
missing-required (root)/temperature
missing-required (root)/tool_choice
missing-required (root)/tools
missing-required (root)/top_p
missing-required /usage/input_tokens_details
missing-required /usage/output_tokens_details
no-branch /output/1 anyOf
```

`no-branch /output/1 anyOf` is the **message** item: its `output_text` part omits
the required `annotations` array, so it matches no `OutputItem` arm. (Verified by
encoding the sample and reading the payload: `output[0]` is the reasoning item and
`output[1]` the message; the reasoning item passes because it carries `id`,
`summary` and `type`.)

Other emitted-output defects:

- The stream events `response.content_part.added` and `response.content_part.done`
  are emitted with the key **`content_part`**; the official events call it `part`
  (and require it). As a consequence the decoder's own `content_part.added`
  branch, which looks for `content_part`, is dead for real upstream payloads.
- Several emitted events omit members the official event requires:
  `response.output_text.done` without `text`,
  `response.function_call_arguments.done` without `arguments`,
  `response.reasoning_summary_text.delta` without `summary_index`,
  the message content part without `annotations`, the function-call item without
  `arguments`, the reasoning item without `summary`.
- **Every `output[]` item carries `"usage": {}`.** `openAIResponsesOutputItem.Usage`
  (and the stream item equivalent) is a non-pointer struct tagged `omitempty`,
  which never omits a struct. So each message, reasoning and function-call item
  gains an undocumented `usage` key holding an object that violates
  `ResponseUsage`'s required token triple. The same non-pointer-struct mistake
  appears in `Response.usage` when the IR usage is empty.
- `ResponseUsage` omits `input_tokens_details` / `output_tokens_details` unless the
  IR happens to carry the corresponding counters, though both are required.
- `reasoning.effort` can be emitted as `"xhigh"`, which is outside the documented
  `low` / `medium` / `high` enum.
- The stream `Response` objects in `response.created`, `response.in_progress`,
  `response.completed`, `response.incomplete` and `response.failed` carry the same
  missing required properties as the non-stream encoder.
- Failure handling reads the wrong object: `ResponseFailedEvent` carries the error
  inside `response.error`, but the decoder reads the event-level `error`; the
  `error` event is emitted with an `error` object where the schema requires
  top-level `code` / `message` / `param`.
- A `raw` SSE event type is emitted that is not a `ResponseStreamEvent` member.
- `custom_tool_call` items, `response.custom_tool_call_input.*` events,
  `cache_write_tokens`, `text_tokens`, `image_tokens` and `file_url` are emitted
  but absent from the official schema.
- 18 of the 36 `ResponseStreamEvent` variants fall through to `StreamRaw`:
  the audio, code interpreter, file search, web search,
  `response.output_text.annotation.added` and
  `response.reasoning_summary_part.*` families.
- The reasoning stream sequence skips the official summary-part events and goes
  straight from delta to `output_item.done`.

### 3.6 Anthropic Messages — request

Decode covers `model`, `messages`, `system` (text only), `max_tokens`,
`temperature`, `top_p`, `top_k`, `stop_sequences`, `stream`, `metadata.user_id`,
`tool_choice` (including `disable_parallel_tool_use`), `thinking` (partially) and
`tools` (function tools only).

The dominant finding is the **tool union**: `decodeAnthropicTools` accepts only
`type: ""` and `type: "custom"` and skips everything else, so **20 of the 21
documented `ToolUnion` branches are dropped silently** — all server tools. The IR
already has a `ToolProviderDefined` slot with a `Config` map, and the Responses
adapter uses it; the Anthropic adapter does not. On the encode side only
`ToolFunction` is emitted. The `unsupportedAnthropicToolWarnings` helper exists
but is wired only into the Responses→Anthropic bridge, so the Chat→Anthropic path
drops them without a word.

Content block coverage: 7 of the 16 `ContentBlockParam` variants decode
(text, image, document, thinking, redacted thinking, tool_use, tool_result).
Dropped: `search_result`, `server_tool_use`, `web_search_tool_result`,
`web_fetch_tool_result`, `code_execution_tool_result`,
`bash_code_execution_tool_result`, `text_editor_code_execution_tool_result`,
`tool_search_tool_result`, `container_upload`.

Other request findings:

- Top-level `cache_control` has no wire field; only block-level `cache_control` is
  sniffed, and only as a boolean. Placement and TTL are lost, and re-encoding
  re-applies the marker to the last system block or the last message.
- `container`, `inference_geo`, `service_tier` and `output_config.effort` are not
  modelled.
- `thinking.display` is ignored; the encoder can only produce
  `{"type":"enabled","budget_tokens":N}`.
- `output_config.format.strict` is emitted although `JSONOutputFormat` documents
  only `type` and `schema`.
- `tools[].input_schema` is `omitempty` although `Tool` requires it, so a tool with
  a nil schema is emitted without it. The one official request example re-encodes
  cleanly only because its tool has a schema.
- A missing `max_tokens` is invented as `4096`.
- `"messages": null` is reachable when the prompt is empty.
- A `thinking` budget below 1024 or at/above `max_tokens` silently drops the
  thinking configuration, and `sanitizeAnthropicToolChoice` silently downgrades
  `required` and named-tool choices to `auto` whenever thinking is enabled. Both
  are defensible protocol workarounds, but neither carries a comment explaining
  why.
- The Claude Code system-prompt stripping regex is likewise uncommented.

### 3.7 Anthropic Messages — response

The pinned encode violations:

```text
missing-required (root)/container
missing-required (root)/stop_details
missing-required (root)/stop_sequence
missing-required /usage/cache_creation
missing-required /usage/cache_creation_input_tokens
missing-required /usage/inference_geo
missing-required /usage/output_tokens_details
missing-required /usage/server_tool_use
missing-required /usage/service_tier
no-branch /content/0 anyOf
no-branch /content/2 anyOf
```

Reading them with the §1 caveat in mind (the Anthropic bundle derives `required`
from prose, and the docs list members such as `usage.service_tier` that older
responses do not send):

- `stop_sequence` and `container` have no field in the response struct at all, so
  they are never decoded and never emitted.
- `Message.id` is `omitempty`, so an empty ID is omitted.
- `usage` is emitted as a struct with `omitempty`, so `"usage": {}` is reachable.
- `stop_details` is only produced for a content filter, and its `category` is
  never set.
- `content` blocks fail the union because text blocks omit `citations` and
  `tool_use` blocks omit the newly documented `caller`.

The clearest *internal* inconsistency: **`decodeAnthropicUsage` never sets
`Usage.ReasoningTokens`**, although it reads `output_tokens` and the IR field
exists and is populated by both OpenAI adapters. Anthropic's
`usage.output_tokens_details.thinking_tokens` is dropped, so reasoning token
counts reported by Anthropic never reach the IR.

Other response findings:

- The response `ContentBlock` union has 12 members and the adapter decodes 4 of
  them (text, thinking, redacted thinking, tool_use). The other 8 —
  `server_tool_use` and the seven server-tool-result / `container_upload` blocks —
  are dropped.
- `TextBlock.citations` is dropped in both directions.
- `Usage.cache_creation` (the 1h/5m breakdown), `server_tool_use`,
  `service_tier`, `inference_geo` and `output_tokens_details` are not modelled.
- `StopReason` values `pause_turn` and `model_context_window_exceeded` collapse to
  `FinishOther` and are then re-encoded as `end_turn`.
- Tool blocks are emitted with `omitempty` on required members (`tool_use.id`,
  `tool_use.name`, `tool_use.input`, `tool_result.tool_use_id`,
  `thinking.signature`).
- `tool_result.content` is always emitted as an array, though the documented union
  is `string | single block`; a single-block object on decode becomes empty text.
- Base64 `media_type` is forwarded without validating the documented enum.
- `image` sources of `type: "file"` decode but have no encode branch, so they
  round-trip out as a warning text block.

### 3.8 Anthropic Messages — streaming

Decode handles all six `RawMessageStreamEvent` branches plus `ping` and `error`
(the latter two appear in real transcripts but are not in the event union).
`content_block_start` for anything other than text/thinking/redacted
thinking/tool_use becomes `StreamRaw`, as does `citations_delta`.

Encode:

- There is no guard requiring `message_start` before `content_block_start`; an
  existing test encodes `StreamTextStart` with no prior `StreamStart`.
- On a Chat upstream, `Close()` only flushes the finish event and never closes
  active text/reasoning/tool blocks, and the Chat decoder emits no `*-End` parts
  in the first place — so those streams end with unterminated content blocks.
- Combined with the duplicate `StreamStart` in §3.3, a Chat-upstream stream emits
  **two `message_start` events**.
- `redacted_thinking_delta` is emitted as a delta type not in
  `RawContentBlockDelta`, and a `raw` event/delta pair is emitted that is not
  official.
- Required delta members are `omitempty`, so an empty first `partial_json` or an
  empty `text` is omitted even though official transcripts send `""`.
- `ping` is never emitted.

---

## 4. Cross-protocol convertibility

### 4.1 Which conversions exist

`NewCrossFamilyBridge` serves only pairs *between* families; every same-family
pair returns `(nil, false)`.

| Inbound | Upstream family | Bridge | Available |
| --- | --- | --- | --- |
| OpenAI Chat | Anthropic | hand-built `anthropicRequest` | yes |
| OpenAI Responses | Anthropic | hand-built `anthropicRequest` | yes |
| Anthropic Messages | OpenAI | `OpenAIResponsesAdapter` | yes |
| Anthropic Messages | OpenAI Chat | `OpenAIChatAdapter` | yes, but only via `NewCrossFamilyBridgeForProtocol` |
| OpenAI Chat | OpenAI Responses | — | **no** — gap, not an impossibility |
| OpenAI Responses | OpenAI Chat | — | **no** — gap, not an impossibility |
| same family | same family | — | no (not needed) |

`NewCrossFamilyBridge(ProtocolAnthropicMessages, FamilyOpenAI)` always returns the
**Responses** bridge. A caller whose upstream speaks Chat gets the wrong encoder
unless it uses `NewCrossFamilyBridgeForProtocol`. The doc comment warns about
this; it is a sharp edge worth an API-level fix rather than a comment.

### 4.2 Convertible exactly

Text; images (base64 and URL, modulo `detail`); documents to Responses; function
tool declarations; all four tool-choice modes (`auto`, `none`, required, named);
`parallel_tool_calls` ↔ `disable_parallel_tool_use`; tool call IDs; tool results
with text and images (except into Chat); `temperature` / `top_p`; stop sequences
(except into Responses); `json_schema` response format; streamed text,
reasoning, signature and tool-input deltas; `metadata.user_id` (native Anthropic
adapter only); response state into Responses.

### 4.3 Convertible but lossy

| Feature | Loss |
| --- | --- |
| `developer` / `system` roles → Anthropic | all such messages collapse into one `system` string; role identity and boundaries lost |
| `tool` role → Anthropic | becomes a `user` message |
| Consecutive assistant/tool messages → Anthropic | merged only in the tool-use cases; Anthropic's strict alternation is not otherwise normalised |
| `refusal` | Anthropic gains `stop_details`; Chat has no equivalent, and in a Chat stream a refusal arrives as a plain text delta with metadata |
| finish reasons | unknown values collapse to `stop` (Chat) or `end_turn` (Anthropic); Responses content filtering is `status:"completed"` non-streamed but `response.incomplete` streamed |
| reasoning text → Anthropic | unsigned reasoning is dropped; only signed/redacted survives |
| reasoning budget ↔ effort | budget→effort is quantised (`>=8192` → the out-of-enum `xhigh`); effort→budget does not exist and defaults to 1024; Chat flattens effort to a boolean and always re-emits `medium` |
| forced / named tool choice with thinking | silently downgraded to `auto` |
| strict tool schemas → Responses | `additionalProperties` is forced to `false` and `required` is rewritten to every property, mutating the declared schema |
| prompt caching → Anthropic | only presence round-trips; placement and TTL are lost, and a marker is applied at a fixed position |
| token limits | absent limits are invented as 4096 on every path |
| `max_tokens` naming | Chat decode prefers `max_completion_tokens`, encode emits only it |
| tool-result JSON and error flags | JSON-ness is stringified; `is_error` exists only in Anthropic |
| usage / cache accounting | cache-write tokens are folded into `prompt_tokens` for Chat, and emitted as a non-standard `cache_write_tokens` for Responses |
| streamed usage | rebased per direction; cache-write visibility is lost |
| timestamps | Chat stream chunks always report `created: 0` |
| Responses item identity | non-stream `function_call` items omit `id`; the IR has no item-ID slot for tool calls |
| Anthropic `stop_sequence` | not modelled in the IR at all |

### 4.4 Not expressible in the target protocol

1. Reasoning signatures / redacted / encrypted content → **Chat**. There is no
   request field, and the encoder never writes `reasoning_content`.
2. `n` and multiple `choices` → **Anthropic** and **Responses**; only the first
   choice is ever emitted.
3. `presence_penalty`, `frequency_penalty`, `seed` → **Anthropic** and
   **Responses**.
4. `top_k` → **Chat** and **Responses**.
5. `stop` sequences → **Responses** (no field). Note the adapter-level hard error
   for this is unreachable from the bridges, because the bridge builds its own
   request struct and drops the value instead.
6. `developer` role → **Anthropic**.
7. `cache_control` markers → **Chat** and **Responses**; a separate cache-write
   counter → **Chat**.
8. Response state (`previous_response_id`, `conversation`) → **Anthropic** and
   **Chat**.
9. Tool-result `is_error` and images → **Chat** (its tool messages are strings).
10. Responses per-item `id` / `status` and annotations → the IR, hence any target.
11. Anthropic server tools ↔ OpenAI tool models: dropped at Anthropic decode, and
    the Chat tool model only holds functions.
12. Anthropic `stop_sequence` (the matched string) and `thinking.display` → the IR.

### 4.5 Expressible but not implemented

These are the actionable conversion gaps, ordered by payoff:

1. **Chat ↔ Responses conversion is absent** even though both protocols are fully
   modelled. The natural implementation is a same-family bridge that goes through
   the IR, which already has every needed field.
2. **`Metadata` is never copied by any cross-family bridge**, although the IR and
   the Anthropic request model both support it.
3. **`reasoning.effort` → Anthropic `budget_tokens`** mapping. Only the reverse
   exists.
4. **`include: ["reasoning.encrypted_content"]` is never set** for a Responses
   upstream driven by an Anthropic client, so encrypted reasoning may never come
   back and thinking cannot round-trip.
5. **`json_object` → Anthropic** is dropped instead of becoming a permissive
   schema.
6. **Provider-defined tools → Anthropic** are dropped in the Chat path and replaced
   by a system-prompt warning in the Responses path. The IR slot exists.
7. **`top_k` is never assigned by the two bridges that encode to Anthropic**,
   although the native encoder supports it.
8. **User/assistant alternation is not normalised** for Anthropic targets.
9. **Responses annotations and Anthropic `stop_sequence`** are modelled in
   unrelated structs but never surfaced in the IR.

---

## 5. Recommended remediation

Nothing below has been applied. The suggested order:

**Tier 1 — hard schema violations in encoder output.** `created` in Chat
responses and stream chunks; `choices[].logprobs`; `content` and `refusal` on
assistant messages; `id` on Chat responses; `messages: null`; `schema: null`;
`"usage": {}`; the Responses `content_part` → `part` key and the required event
members; the ten missing required `Response` properties; Anthropic
`stop_sequence` / `container` / required usage members; the `xhigh` effort value.

**Tier 2 — internal correctness bugs with no schema angle.** `currentTimestamp()`
returning 0; the duplicate `StreamStart` / duplicate `message_start`; the missing
content-block close on the Chat-upstream Anthropic path; `decodeAnthropicUsage`
not setting `ReasoningTokens`; the dead `content_part.added` decode branch;
unknown finish reasons silently becoming `stop`.

**Tier 3 — decode coverage.** Anthropic's 20 dropped `ToolUnion` branches (use the
existing `ToolProviderDefined` slot); Anthropic's 9 dropped content-block
variants; Responses' dropped `file_search_call` / `web_search_call` /
`computer_call` output items and annotations; Chat `metadata`, `logprobs`,
`store` and the remaining documented request fields; the Chat `name` field and
`role: "function"`.

**Tier 4 — conversion gaps.** Chat ↔ Responses bridge; `Metadata` propagation;
`effort` → budget; `include: ["reasoning.encrypted_content"]`; `top_k`;
alternation normalisation.

**Tier 5 — clarity.** Comment the deliberate workarounds (system-prompt
stripping, forced-tool-choice downgrade, thinking-budget clamping, thinking
suppression on unsigned tool continuations, signature-delta filtering) and
consider making the invented defaults (4096 token limit, default
`cache_control`, warning text blocks) opt-in rather than silent. The default
`cache_control` is the most surprising: it is applied whenever the IR `Cache` is
nil, so every bridged request is cache-marked unless the caller cannot express it.

---

## Reproducing the evidence

```bash
# Conformance tests (the sandbox's default GOCACHE is read-only).
GOCACHE=$PWD/.scratch/gocache go test -run 'TestOfficial|TestEncoded|TestAnthropicOfficial' -v .

# Full suite.
GOCACHE=$PWD/.scratch/gocache go test ./...

# Re-extract the standard from the pinned sources.
python3 protocols/tools/extract_openai_schema.py \
    --spec <openapi.documented-2026-06-02.yml> \
    --paths <openai-path-items> \
    --out protocols/openai
protocols/tools/fetch_anthropic_docs.sh .scratch/raw
python3 protocols/tools/extract_anthropic_schema.py \
    --messages .scratch/raw/anthropic-messages-api.md \
    --streaming .scratch/raw/anthropic-streaming.md \
    --out protocols/anthropic
```

Two caveats on the evidence:

- The OpenAI findings in §3.1–§3.5 were produced by reading the adapters against
  the extracted bundle and by executing the pinned tests. The Anthropic findings
  in §3.6–§3.8 and the conversion analysis in §4 combine the executed tests with
  static reading of `anthropic_messages.go` and the four `bridge_*.go` files;
  claims about `omitempty` behaviour in those sections follow from the struct tags
  and the encoder code rather than from a per-field execution.
- One official Anthropic transcript is marked `"complete": false` in
  `protocols/anthropic/streaming.examples.json` because the guide abbreviates an
  identifier (`msg_01G...`). The decode test uses only complete transcripts.
