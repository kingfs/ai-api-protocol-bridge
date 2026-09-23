# Responses API (`POST /v1/responses`) compatibility dossier

**Method.** All web claims below were retrieved with `curl`/`web_fetch` on 2026-09-23 and are linked. Codex
claims come from a shallow clone of `openai/codex` at commit `0a2eb4696c26ac33204bcd255721ab30220a4774`
(`main`, 2026-09-23). Live endpoint probes were run unauthenticated against each vendor's public base URL
(method + caveat in §A.4). Labels: **[D]** = documented by the vendor/spec (cited), **[I]** = inferred from
source code or issue reports (cited), **[U]** = unknown / not established.

---

## A.0 The canonical shape, so "subset" is measurable

Request body (OpenAI): `model`, `input` (`string | InputItem[]`), `instructions`, `previous_response_id`,
`conversation`, `store`, `include`, `reasoning{mode,effort,summary,context,generate_summary}`,
`text{format{type:json_schema|json_object|text,name,schema,strict},verbosity}`, `tools`, `tool_choice`,
`parallel_tool_calls`, `truncation`, `max_output_tokens`, `max_tool_calls`, `temperature`, `top_p`,
`stream`, `stream_options{include_obfuscation}`, `metadata`, `prompt_cache_key`, `service_tier`,
`safety_identifier`, `background`, `user`, `top_logprobs`, `prompt_cache_options`. **[D]**
[openai-openapi `CreateResponse`](https://github.com/openai/openai-openapi/blob/master/openapi.yaml#L40483),
[`InputItem`](https://github.com/openai/openai-openapi/blob/master/openapi.yaml#L46576) —
`InputItem` is a discriminated union over `EasyInputMessage`, `Item`, `ItemReferenceParam`,
`CompactionTriggerItemParam`, `ProgramItemParam`, `ProgramOutputItemParam`.

Stream events: the spec's `ResponseStreamEvent` is an `anyOf` of **59** event schemas — 58 with a
`response.*` type plus one bare `error` event. The complete canonical set **[D]**
[openapi.yaml ResponseStreamEvent](https://github.com/openai/openai-openapi/blob/master/openapi.yaml#L65421):

`response.audio.delta`, `response.audio.done`, `response.audio.transcript.delta`,
`response.audio.transcript.done`, `response.code_interpreter_call_code.delta`,
`response.code_interpreter_call_code.done`, `response.code_interpreter_call.completed`,
`response.code_interpreter_call.in_progress`, `response.code_interpreter_call.interpreting`,
`response.compaction.compacting`, `response.completed`, `response.content_part.added`,
`response.content_part.done`, `response.created`, **`error`**, `response.file_search_call.completed`,
`response.file_search_call.in_progress`, `response.file_search_call.searching`,
`response.function_call_arguments.delta`, `response.function_call_arguments.done`,
`response.shell_call_command.added`, `response.shell_call_command.delta`,
`response.shell_call_command.done`, `response.shell_call_output_content.delta`,
`response.shell_call_output_content.done`, `response.in_progress`, `response.failed`,
`response.incomplete`, `response.output_item.added`, `response.output_item.done`,
`response.reasoning_summary_part.added`, `response.reasoning_summary_part.done`,
`response.reasoning_summary_text.delta`, `response.reasoning_summary_text.done`,
`response.reasoning_text.delta`, `response.reasoning_text.done`, `response.refusal.delta`,
`response.refusal.done`, `response.output_text.delta`, `response.output_text.done`,
`response.web_search_call.completed`, `response.web_search_call.in_progress`,
`response.web_search_call.searching`, `response.image_generation_call.completed`,
`response.image_generation_call.generating`, `response.image_generation_call.in_progress`,
`response.image_generation_call.partial_image`, `response.mcp_call_arguments.delta`,
`response.mcp_call_arguments.done`, `response.mcp_call.completed`, `response.mcp_call.failed`,
`response.mcp_call.in_progress`, `response.mcp_list_tools.completed`, `response.mcp_list_tools.failed`,
`response.mcp_list_tools.in_progress`, `response.output_text.annotation.added`, `response.queued`,
`response.custom_tool_call_input.delta`, `response.custom_tool_call_input.done`.

`obfuscation` is a per-delta field controlled by `stream_options.include_obfuscation` (default **true**).
**[D]** [openapi.yaml `ResponseStreamOptions`](https://github.com/openai/openai-openapi/blob/master/openapi.yaml#L65480).

**Nothing outside OpenAI implements all of this.** No implementation surveyed emits `response.queued`,
`response.compaction.compacting`, `response.shell_call_*`, `response.output_text.annotation.added`, or the
`audio`/`image_generation` families.

---

## A.1 Chinese vendors

| | DashScope 百炼 | DeepSeek | Kimi/Moonshot | Zhipu GLM | MiniMax | Ark/Doubao | SiliconFlow |
|---|---|---|---|---|---|---|---|
| Path | `/compatible-mode/v1/responses` **[D]** | `/responses` (base `https://api.deepseek.com`) **[D]** | `/v1/responses` **[D]** | `/api/v1/responses` **[D]** | `/v1/responses` **[D]** | `/api/v3/responses` **[D]** | **absent** (404) **[I]** |
| `input` string / item array | ✅ / ✅ **[D]** | ✅ / ✅ **[D]** | ✅ / ✅ **[D]** | ✅ / ✅ **[D]** | ✅ / ✅ **[D]** | ✅ / ✅ **[D]** | n/a |
| `instructions` | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | n/a |
| `previous_response_id` | ✅ (7-day id) **[D]** | ❌ ignored **[D]** | accepted, response fixed `null` **[D]** | ✅ (7-day id) **[D]** | ❌ absent **[D]** | ✅ **[D]** | n/a |
| `store` | ✅ default **true** **[D]** | ❌ ignored, response fixed `false` **[D]** | accepted, response fixed `false` **[D]** | ✅ default **false** **[D]** | ❌ absent; response `false` **[D]** | ✅ default **true**, TTL 3 d (≤7 d), 1000-item cap **[D]** | n/a |
| `conversation` | ✅ (mutually exclusive with `previous_response_id`) **[D]** | ❌ **[D]** | ❌ response fixed `null` **[D]** | ❌ absent **[D]** | ❌ absent **[D]** | ❌ not documented **[U]** | n/a |
| `include` / `reasoning.encrypted_content` | ❌ not documented → ignored **[D]** | ❌ ignored **[D]** | ✅ but only `web_search_call.results` / `web_search_call.action.sources` **[D]** | ❌ absent **[D]** | ❌ absent **[D]** | ❌ not documented **[U]** | n/a |
| `reasoning.effort` / `.summary` | ✅ effort (7 levels) / summary ✗ **[D]** | ✅ effort / summary **accepted but never generated** **[D]** | ✅ effort `low\|high\|max` / summary ✗ **[D]** | ✅ (via OpenAI-compatible `reasoning`) **[D]** | ✅ effort `minimal\|low\|medium\|high\|none`, does not change depth **[D]** | ✅ effort; `thinking{type}` is the native switch **[D]** | n/a |
| `text.format` json_schema | ❌ not documented → ignored **[D]** | ✅ `format` fully supported **[D]** | ✅ `json_schema` + `name` + `strict` **[D]** | ✅ `text` present in spec **[D]** | ⚠️ `format.type` enum is only `text` **[D]** | ✅ json_schema exists (but structured output is unsupported in prefill mode) **[D]** | n/a |
| `tools` function | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ + namespace + custom **[D]** | ✅ **[D]** | ✅ **[D]** | n/a |
| built-in tools | `web_search`, `file_search`, `code_interpreter`, `mcp`, `web_extractor`, `web_search_image`, `image_search` **[D]** | ❌ all built-ins ignored except `{"type":"custom","name":"apply_patch"}` **[D]** | `web_search` **[D]** | `web_search`, namespace, custom **[D]** | not enumerated **[U]** | web search + apply_patch + function **[D]** | n/a |
| `tool_choice` | ✅ `auto\|none\|required\|allowed_tools` **[D]** | ✅ incl. `{"type":"function","name":…}` **[D]** | ✅ **[D]** | ✅ **[D]** | ⚠️ only `none\|auto` **[D]** | ✅ **[D]** | n/a |
| `parallel_tool_calls` | ❌ not a request param (echoed `false`) **[D]** | ❌ ignored, always on **[D]** | ✅ request + response **[D]** | ❌ absent **[D]** | ❌ absent; response `true` **[D]** | ❌ not documented **[U]** | n/a |
| `truncation` | ❌; auto-truncates at ~80 % of window, no error **[D]** | ❌; over-long input → 400 **[D]** | ❌ absent **[D]** | ❌ absent **[D]** | ❌ request-side; response `disabled` **[D]** | ❌ | n/a |
| `max_output_tokens` | ✅ (min 16) **[D]** | ✅ **[D]** | ✅ (default 131072, max 1048576) **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ | n/a |
| `temperature` / `top_p` | ✅ / ✅ **[D]** | ✅ (no effect in thinking mode) / ✅ (lower bound 0.95 in thinking mode) **[D]** | ✅ / ✅ **[D]** | ✅ / ✅ **[D]** | ✅ (0,1] / ✅ (0,1] **[D]** | ✅ / ✅ | n/a |
| `stream` | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ **[D]** | ✅ | n/a |
| `metadata` | ❌ (not documented) **[D]** | ❌ **[D]** | ✅ **[D]** | ❌ absent **[D]** | ✅ string map **[D]** | ❌ **[U]** | n/a |
| `prompt_cache_key` | ❌ (uses header `x-dashscope-session-cache`) **[D]** | ❌ **[D]** | ✅ + `prompt_cache_options{mode,ttl}` **[D]** | ✅ **[D]** | ✅ (cache routing) **[D]** | ❌ (uses `caching{type,prefix}`) **[D]** | n/a |
| `sequence_number` on events | ✅ from 0 **[D]** | ✅ monotonic **[D]** | ✅ from 0 **[D]** | ✅ **[D]** | **[U]** | ✅ | n/a |
| `obfuscation` | ❌ **[D]** | ❌ **[D]** | ❌ **[D]** | ❌ **[D]** | ❌ **[D]** | ❌ **[D]** | n/a |
| `response.failed` / `response.incomplete` | ✅ / ✅ **[D]** | ✅ / ✅ (+ `[DONE]` explicitly **not** sent) **[D]** | ✅ / ✅ (+ `error`) **[D]** | ✅ / ✅ (+ `error`) **[D]** | **[U]** | ✅ | n/a |

### Per-vendor notes that change integration behaviour

**Alibaba DashScope / 百炼** — "请求将仅处理本文档明确列出的参数，任何未提及的 OpenAI 参数都会被忽略" (only
documented params are processed; every unlisted OpenAI param is ignored). `background` explicitly
unsupported. Input context is capped at ~80 % of the model window and **truncated silently, without an
error**. Response items echo `parallel_tool_calls: false`. `instructions` is *not* carried across a
`previous_response_id` boundary. **[D]**
[创建响应 (zh)](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-responses)

**DeepSeek** — the only vendor with an explicit, complete unsupported-parameter table: "Unsupported
parameters are silently ignored and do not cause errors, so existing Responses API clients can connect
without modification." Stateless by construction; `store` is fixed `false`; `previous_response_id` is
always `null`; `parallel_tool_calls` always `true`. It supports `custom_tool_call`/`custom_tool_call_output`
**only** for `{"type":"custom","name":"apply_patch"}` — other custom names are a 400 — a Codex-specific
accommodation. It ships an official Codex integration (see §B.8). **[D]**
[Compatibility Details](https://api-docs.deepseek.com/guides/responses_api),
[Responses API reference](https://api-docs.deepseek.com/api/create-response),
[Integrate with Codex](https://api-docs.deepseek.com/quick_start/agent_integrations/codex)

**Moonshot / Kimi** — the most complete Chinese implementation. Only non-OpenAI endpoint found that
documents `response.reasoning_summary_text.delta`/`.done` and `response.reasoning_summary_part.added`/`.done`
plus `response.custom_tool_call_input.delta/done`, `response.web_search_call.*`, `response.incomplete`,
`response.failed` and a bare `error` event. `include` exists but is narrowed to two web-search values.
`store`, `previous_response_id` and `conversation` are accepted but pinned to `false`/`null`/`null`. **[D]**
[Responses API](https://platform.kimi.com/docs/api/responses),
[在 Codex 中使用 Kimi K3](https://platform.kimi.com/docs/guide/codex-kimi)

**Zhipu GLM** — the Responses API lives on a *different base* from Chat Completions:
`https://open.bigmodel.cn/api/v1` (not `/api/paas/v4`). `store` defaults to **false**, no `data: [DONE]`,
no cancel endpoint. Request schema is exactly: `model, input, instructions, stream, temperature, top_p,
max_output_tokens, stop, tools, tool_choice, reasoning, text, prompt_cache_key, previous_response_id,
store`. Stream event enum is 19 entries (`response.created`, `in_progress`, `completed`, `failed`,
`incomplete`, `output_item.added/done`, `content_part.added/done`, `output_text.delta/done`,
`function_call_arguments.delta/done`, `reasoning_text.delta/done`, `web_search_call.in_progress/searching/
completed`, `error`). **[D]**
[intro](https://docs.bigmodel.cn/cn/guide/develop/responses/introduction),
[OpenAPI](https://docs.bigmodel.cn/openapi/openapi-responses.json)

**MiniMax** — strictest subset of the group. `tool_choice` only `none|auto`; `text.format.type` enum is
only `text` (no json_schema); no `previous_response_id`/`store`/`conversation`/`include`/`truncation` in the
request at all. `reasoning.effort` for M3 only gates whether reasoning items are emitted, not depth. **[D]**
[对话生成](https://platform.minimaxi.com/docs/api-reference/responses-create),
[OpenAPI](https://platform.minimaxi.com/docs/api-reference/text/api/openapi-responses.json)

**Volcengine Ark / Doubao** — all LLMs from version `250615` onward support Responses by default. Stored
responses: TTL 3 days (max 7), 1000-item conversation cap, chain-of-thought is **not** stored. Emits
`response.reasoning_summary_part.added` + `response.reasoning_summary_text.delta` with `summary_index`.
Adds non-standard `thinking`/`caching` params and `tool_usage`/`tool_usage_details` in `usage`.
Structured output (`json_object`/`json_schema`) is declared unsupported in prefill/continuation mode. **[D]**
[使用 Responses API 文本生成](https://www.volcengine.com/docs/82379/1958520)

**SiliconFlow** — no `/v1/responses`. **[I]** Unauthenticated `POST https://api.siliconflow.cn/v1/responses`
→ `404 Not Found`, while `/v1/chat/completions` → `401 Token is invalid` and an unknown path
`/v1/zzz-not-a-real-endpoint` → `404 Not Found`. The route-vs-auth ordering is therefore decidable here and
the 404 is conclusive. Chat Completions is documented at
[docs.siliconflow.cn](https://docs.siliconflow.cn/cn/api-reference/chat-completions/chat-completions).

---

## A.2 Open-source inference servers

### vLLM

Endpoints: `POST /v1/responses`, `GET /v1/responses/{id}`, `POST /v1/responses/{id}/cancel`, plus
`/v1/responses/render`. **No longer labelled experimental/alpha in current docs** — the supported-API list
simply reads "Responses API (`/v1/responses`, …) — Only applicable to text generation models". **[D]**
[openai_compatible_server.md](https://github.com/vllm-project/vllm/blob/main/docs/serving/online_serving/openai_compatible_server.md),
[api_router.py](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/openai/responses/api_router.py)

Request model [`ResponsesRequest`](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/openai/responses/protocol.py#L138):
`background, include, input, instructions, max_output_tokens, max_tool_calls, metadata, model, logit_bias,
parallel_tool_calls, previous_response_id, prompt, reasoning, include_reasoning, service_tier, store,
stream, temperature, text, tool_choice, tools, top_logprobs, top_p, top_k, truncation, user` + vLLM extras.

Gaps and quirks:
- **`prompt_cache_key` is accepted but explicitly dead**: *"A key that was used to read from or write to the
  prompt cache. Note: This field has not been implemented yet and vLLM will ignore it."* **[D]** (same file)
- **No `conversation`.** **[I]** (absent from the model)
- **`include` is validated against a closed literal set** of six values (including
  `reasoning.encrypted_content`) — anything else is a schema error. **[D]** (same file, L142–154)
- **`store` silently downgrades**: if `store=True` and the store is disabled, vLLM *rewrites the request to
  `store=False` and processes it anyway* rather than erroring. Only `store=True` + `background` returns a
  400 telling you to set `VLLM_ENABLE_RESPONSES_API_STORE=1`. **[I]**
  [serving.py L330–345](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/openai/responses/serving.py#L330)
- **`previous_response_id` requires the store**: lookup goes through an in-memory `response_store` that only
  exists when `VLLM_ENABLE_RESPONSES_API_STORE=1`; otherwise the id misses and you get a not-found error. The
  in-flight PR for a store-free path states the failure directly: *"users with OpenCode/Codex CLI agents fail
  on turn 2 because previous_response_id requires `VLLM_ENABLE_RESPONSES_API_STORE=1`"*. **[I]**
  [PR #35740](https://github.com/vllm-project/vllm/pull/35740)
- **Missing events:** streaming events are the OpenAI SDK's own types plus two local
  `response.reasoning_part.added/done`. Grepping the whole responses module for `reasoning_summary` yields
  **zero** hits, and `response.failed` yields **zero** hits. So: **no `response.reasoning_summary_text.*`, no
  `response.failed`, no `obfuscation`.** Emitted subset: `response.created`, `response.in_progress`,
  `response.completed`, `output_item.added/done`, `content_part.added/done`, `output_text.delta/done`,
  `reasoning_text.delta/done`, `reasoning_part.added/done`, `function_call_arguments.delta/done`,
  `code_interpreter_call*`, `web_search_call*`, `mcp_call*`. **[I]**
  [streaming_events.py](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/openai/responses/streaming_events.py)
- vLLM itself publishes a **Codex** integration page using `wire_api = "responses"`, and warns only "ensure
  your vLLM version supports the OpenAI Responses API". **[D]**
  [docs/serving/integrations/codex.md](https://github.com/vllm-project/vllm/blob/main/docs/serving/integrations/codex.md)

### SGLang

Request model is a near-copy of vLLM's — same field set including `include` with the same six literals,
`parallel_tool_calls`, `previous_response_id`, `store`, `truncation`, `text` — **plus** it accepts loose
dict-shaped input items "for replayed shapes that don't satisfy every openai TypedDict", and its base model
uses `ConfigDict(extra="allow")` so unknown fields do not 422. **No `conversation`, no `prompt_cache_key`,
no `logit_bias`.** **[I]**
[protocol.py L1622](https://github.com/sgl-project/sglang/blob/main/python/sglang/srt/entrypoints/openai/protocol.py#L1622)

SGLang is the **most Codex-oriented** of the OSS servers: it emits
`response.reasoning_summary_text.delta/done` and `response.reasoning_summary_part.added/done` **and** has an
actively-developed Codex compatibility surface. PR #35216 ("Responses API Codex compatibility") adds
namespace-tool flattening, `agent_message` rendering, array-shaped tool output (preserving `input_image`),
and custom/freeform tool round-tripping (`custom_tool_call` items + `response.custom_tool_call_input.delta/
done`, with a note that `format` lark/regex grammars are ignored). **[I]**
[PR #35216](https://github.com/sgl-project/sglang/pull/35216),
[serving_responses.py](https://github.com/sgl-project/sglang/blob/main/python/sglang/srt/entrypoints/openai/serving_responses.py)

### Ollama

`POST /v1/responses`, added in **v0.13.3**. Documented position: *"Ollama supports the OpenAI Responses
API. Only the non-stateful flavor is supported (i.e., there is no `previous_response_id` or `conversation`
support)."* Supported features are listed as Streaming, Tools (function calling), Reasoning summaries,
Stateful requests. Documented request fields: `model, input, instructions, tools, stream, temperature,
top_p, max_output_tokens, reasoning.effort, think (Ollama extension), previous_response_id, conversation,
truncation`. Cloud (`ollama.com/v1`) additionally does **not** support stateful Responses, built-in web
search through `/v1/responses`, or custom/freeform tool-call replay. **[D]**
[OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility)

From source (`openai/responses.go`): `background` — "not supported"; `conversation` — "not supported";
`include` — "ignored"; per-event `sequence_number` is emitted; **no `obfuscation`**. Emitted event set
includes `response.reasoning_summary_text.delta/done`, `response.output_item.added/done`,
`response.function_call_arguments.delta/done`, `response.web_search_call.*`, `response.failed`. **[I]**
[openai/responses.go](https://github.com/ollama/ollama/blob/main/openai/responses.go) ·
[server/routes.go L1974](https://github.com/ollama/ollama/blob/main/server/routes.go#L1974)

Known defect: `/v1/responses` accepts `previous_response_id` but a follow-up carrying a valid
`function_call_output` returns HTTP 200 with an **empty** completed message and zero tokens — Codex then
ends the turn after tools run. The reporter's ask is either to reconstruct state or to reject the param.
**[I]** [ollama#18419](https://github.com/ollama/ollama/issues/18419)

### LM Studio

`POST http://localhost:1234/v1/responses`, documented as "streaming, reasoning, prior response state, and
optional Remote MCP tools". It documents `previous_response_id` stateful follow-up explicitly, SSE events
"such as `response.created`, `response.output_text.delta`, and `response.completed`", `reasoning.effort`,
JSON-schema structured output, and `tools` of type `mcp` (`server_label`, `server_url`, `allowed_tools`).
The event-type inventory beyond those three examples is not published. **[D]**
[Responses](https://lmstudio.ai/docs/developer/openai-compat/responses) ·
(an events page at `/openai-compat/streaming-events` returned "Page not found", so full event coverage is **[U]**)

### llama.cpp

`POST /v1/responses` and `/responses` exist and "this endpoint works by converting Responses request into
Chat Completions request" — so most Responses-only semantics are simply absent. **[D]**
[server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md#L1461)

Source confirms the hard limits: `previous_response_id` is a hard error — *"llama.cpp does not support
'previous_response_id'."*; `instructions` is folded into a leading `system` message; `input` accepts a string
or an array of items with `input_text` / `input_image` / `input_file` content parts, and `input_file` raises
*"'input_file' is not supported by llamacpp at this moment"*; anything whose type is not one of those three
raises. **[I]**
[server-chat.cpp L6–L110](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/server-chat.cpp#L6)

Emitted stream events (from `server-task.cpp`): `response.created`, `response.in_progress`,
`response.output_item.added`, `response.content_part.added`, `response.output_text.delta`,
`response.reasoning_text.delta`, `response.output_item.done`, `response.function_call_arguments.delta`,
`response.output_text.done`, `response.content_part.done`, `response.completed`. Notable absences:
**no `sequence_number`, no `obfuscation`, no `response.reasoning_summary_text.*`, no
`response.function_call_arguments.done`, no `response.failed`.** Positively, `response.output_item.done` for a
tool call carries the complete `function_call` item (`arguments` string, `call_id`, `name`), which is what
Codex actually consumes. **[I]**
[server-task.cpp](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/server-task.cpp)

---

## A.3 Aggregators

**OpenRouter — yes, with a native (not pass-through) `/v1/responses`.** Base URL
`https://openrouter.ai/api/v1/responses`. It is explicitly **stateless only**: *"Requests that set
`store: true` or a non-null `previous_response_id` are rejected with a 400 error."* Documented core
parameters: `model`, `input` (string or message array), `stream`, `max_output_tokens`, `temperature`,
`top_p`; plus dedicated pages for reasoning, tool calling and web search. Reasoning output carries
`encrypted_content` and a `summary` array. **[D]**
[Overview](https://openrouter.ai/docs/api_reference/responses/overview) ·
[Basic Usage](https://openrouter.ai/docs/api_reference/responses/basic-usage) ·
[Reasoning](https://openrouter.ai/docs/api_reference/responses/reasoning) ·
[Tool Calling](https://openrouter.ai/docs/api_reference/responses/tool-calling) ·
[Web Search](https://openrouter.ai/docs/api_reference/responses/web-search) ·
[Error Handling](https://openrouter.ai/docs/api_reference/responses/error-handling)

Because it is a normalisation layer rather than a transparent proxy, a provider-specific field Codex sends
(notably `client_metadata`) is dropped or ignored rather than forwarded. **[I]** (no fetchable doc states the
forwarding policy; treated as **[U]**)

*Contrast (outside the brief but useful as calibration):* xAI (`api.x.ai/v1/responses`) and Mistral
(`/v1/responses` → `no Route matched with those values`) were probed; xAI's route exists, Mistral's does
not. Google's OpenAI-compat shim has no `/responses`. **[I]** (§A.4)

---

## A.4 Live endpoint probes (method, results, caveat)

Unauthenticated `POST` with a dummy bearer token to each vendor's public base URL, plus a **control probe**
to the same base with a deliberately nonexistent path. Only when the gateway distinguishes the two can the
probe prove route existence.

| Base URL | `POST …/responses` | `POST …/chat/completions` | control `POST …/zzz-not-a-real-endpoint` | verdict |
|---|---|---|---|---|
| `https://api.siliconflow.cn/v1` | **404 Not Found** | 401 Token is invalid | 404 Not Found | ✔ **route absent** (route-first) |
| `https://api.moonshot.cn/v1` | 401 | 401 | 404 `url.not_found` | ✔ route exists (route-first) |
| `https://open.bigmodel.cn/api/v1` | 401 body `{"code":401,…}` | — | 500 body `404 NOT_FOUND` | ✔ route exists (route-first) |
| `https://openrouter.ai/api/v1` | 401 | 401 | 404 | ✔ route exists (route-first) |
| `https://dashscope.aliyuncs.com/compatible-mode/v1` | 401 InvalidApiKey | 401 InvalidApiKey | **404** | ✔ route exists (route-first) |
| `https://api.minimax.chat/v1` (also `api.minimaxi.com`) | 401 | 401 | 404 `404 page not found` | ✔ route exists (route-first) |
| `https://api.deepseek.com/v1` | 401 | 401 | **401** | ✖ inconclusive (auth-first) |
| `https://open.bigmodel.cn/api/paas/v4` | 401 | 401 | **401** | ✖ inconclusive (chat base, not responses base) |
| `https://ark.cn-beijing.volces.com/api/v3` | 401 | 401 | **401** | ✖ inconclusive (auth-first) |

Caveat: an auth-first gateway answers 401 for *every* path, so a 401 there proves nothing about routing —
which is exactly why the control column exists. Only the route-first rows are evidence. **[I]** (own probes,
2026-09-23)

---

# B. What Codex CLI actually requires

Repository state: `openai/codex` `main` @ `0a2eb469` (2026-09-23).

## B.1 Transport contract: `responses` is the only wire API

`WireApi` has exactly one variant, and deserialization maps `"chat"` to a hard error string, not a fallback:

```
"responses" => Ok(Self::Responses),
"chat" => Err(serde::de::Error::custom(CHAT_WIRE_API_REMOVED_ERROR)),
_ => Err(serde::de::Error::unknown_variant(&value, &["responses"])),
```

with `CHAT_WIRE_API_REMOVED_ERROR = "`wire_api = \"chat\"` is no longer supported.\nHow to fix: set
`wire_api = \"responses\"` in your provider config.\nMore info: …/discussions/7782"`. **[I]**
[model-provider-info/src/lib.rs L97–L131](https://github.com/openai/codex/blob/main/codex-rs/model-provider-info/src/lib.rs#L97)

This is official, not just source: the config reference states *"`wire_api` — responses is the only
supported value, and it is the default when omitted."* **[D]**
[Codex config reference](https://developers.openai.com/codex/config-reference) — and the removal was
announced with a Feb-2026 deadline in [discussion #7782](https://github.com/openai/codex/discussions/7782)
("Full removal is slated for early February 2026", "In February 2026, this will transition to a hard
error").

The legacy `ollama-chat` provider id is likewise removed with its own error: *"`ollama-chat` is no longer
supported. … replace `ollama-chat` with `ollama`"*. **[I]** (same file, L98–L99)

A whole-tree grep of `codex-rs` for `chat/completions`, `WireApi::Chat` and `fn chat_completions` returns
**zero** hits. **[I]**

**Consequence for a bridge:** the Codex-facing side must always speak Responses. A Chat-Completions-only
upstream cannot be reached by configuring Codex differently — it has to be translated by a proxy.

## B.2 The request body Codex sends every turn

From [`ResponsesApiRequest`](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/common.rs#L259)
and its construction in
[core/src/client.rs L985–L1002](https://github.com/openai/codex/blob/main/codex-rs/core/src/client.rs#L985):

| field | value | unconditional? |
|---|---|---|
| `model` | catalog slug | always |
| `instructions` | system prompt | **omitted when empty** (`skip_serializing_if = "String::is_empty"`) |
| `input` | `Vec<ResponseItem>` — always an **array**, never a bare string | always |
| `tools` | array; **omitted entirely when there are no tools** | conditional |
| `tool_choice` | the literal string `"auto"` | **always** |
| `parallel_tool_calls` | bool (`prompt.parallel_tool_calls && !use_responses_lite`) | **always** |
| `reasoning` | `Option<Reasoning>` — populated from model effort/summary | **always** (Some) |
| `store` | **`false`** | **always** |
| `stream` | **`true`** | **always** |
| `stream_options` | only when concurrent reasoning summaries are on *and* provider is OpenAI-family | rare |
| `include` | **`vec!["reasoning.encrypted_content"]`** | **always** |
| `prompt_cache_key` | derived from `responses_metadata` | **always** (Some) |
| `service_tier` | model/provider dependent | conditional |
| `text` | `{verbosity, format:{type:"json_schema", strict, schema, name:"codex_output_schema"}}` when a schema/verbosity exists; omitted otherwise | conditional |
| `client_metadata` | **`Some(HashMap)` — non-OpenAI field.** Contains `x-codex-installation-id`, `session_id`, `thread_id`, `x-codex-window-id`, `turn_id`, sometimes `x-openai-subagent`, `x-codex-turn-metadata` | **always** |
| `access_programs` | Codex-internal feature flag | conditional |

**[I]** for all rows; the construction site and the serde attributes are the citation. `client_metadata`
presence is additionally pinned by tests
([core/tests/suite/client.rs L259–L288](https://github.com/openai/codex/blob/main/codex-rs/core/tests/suite/client.rs#L259)).

Notably **absent** from the HTTP body: `temperature`, `top_p`, `max_output_tokens`, `truncation`,
`metadata`, `conversation`, `previous_response_id`, `background`, `safety_identifier`, `user`. Codex does
not send them, so a server that only lacks those is still Codex-compatible.

Practical reading: an upstream that tolerates unknown/extra JSON keys and ignores `include`, `store`,
`prompt_cache_key` and `client_metadata` will pass the request stage. An upstream that rejects unknown keys
fails on `client_metadata` (and possibly `prompt_cache_key`) on **every** request.

## B.3 The stream events Codex needs — and the ones it ignores

Dispatch table: [`process_responses_event`](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/sse/responses.rs#L353).

**Consumed (change behaviour):**

| event | effect | required? |
|---|---|---|
| `response.output_item.done` | deserializes `item` into a `ResponseItem`; **this is the only path that produces assistant text, reasoning, `function_call` and `custom_tool_call` items** | **mandatory** — without it, a turn produces no content and no tool call |
| `response.output_text.delta` | live UI text delta (only applied while an item is "active") | needed for streaming text, not for correctness |
| `response.output_item.added` | sets the active item that `output_text.delta` is applied to | needed for live text streaming |
| `response.reasoning_summary_text.delta` | reasoning summary delta; **requires BOTH `delta` and `summary_index`** or it is dropped | needed for reasoning display |
| `response.reasoning_summary_text.done` | requires `item_id`, `text`, `summary_index` | optional |
| `response.reasoning_summary_part.added` | requires `summary_index` | optional |
| `response.reasoning_text.delta` | requires `delta` + `content_index` | optional (this is what vLLM/llama.cpp emit instead of summaries) |
| `response.custom_tool_call_input.delta` | freeform-tool input delta; needs `item_id` or `call_id` | needed only for custom tools |
| `response.created` | extracts `response.id` into turn state | optional but cheap |
| `response.completed` | **ends the turn**; must deserialize as `{id, usage?, usage_metadata?, end_turn?}` | **mandatory** |
| `response.failed` | turns the stream into an `ApiError` (special-cases `context_length_exceeded`, `insufficient_quota`, `cyber_policy`, …) | optional but the only sane error channel |
| `response.incomplete` | **always an error** — `"Incomplete response returned, reason: {reason}"` | n/a |

**Explicitly unhandled (no-op, trace-logged)** — the source lists them by name:
`codex.response.metadata`, `response.content_part.added`, `response.content_part.done`,
`response.custom_tool_call_input.done`, **`response.function_call_arguments.delta`**,
**`response.function_call_arguments.done`**, `response.in_progress`, `response.metadata`,
`response.output_text.done`, `response.reasoning_summary_part.done`, `responsesapi.websocket_timing`; any
other `*.delta`; and everything else via the default arm. **[I]**
[responses.rs L531–L552](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/sse/responses.rs#L531)

Three consequences worth stating plainly:

1. **Function-call arguments are never assembled from deltas.** Codex reads them from the completed item in
   `response.output_item.done`. A server may stream `function_call_arguments.delta` freely, or not at all;
   what matters is that `output_item.done` carries `{"type":"function_call","name":…,"arguments":"<json
   string>","call_id":…}`. A server that *only* streams deltas and never emits the item is unusable.
2. **A bare `error` event is ignored.** It falls through to the default arm. Errors must arrive as
   `response.failed` (or as an HTTP error status).
3. **A missing `response.completed` is fatal**: the reader emits
   `ApiError::Stream("stream closed before response.completed")` **[I]**
   [responses.rs L611](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/sse/responses.rs#L611),
   which reaches the user as `stream disconnected before completion: stream closed before
   response.completed` **[I]**
   [core/tests/suite/subagent_notifications.rs L2532](https://github.com/openai/codex/blob/main/codex-rs/core/tests/suite/subagent_notifications.rs#L2532).
   Codex then retries per `stream_max_retries` (default 5) with an idle timeout of
   `stream_idle_timeout_ms` (default **300 000 ms = 5 min**); the retry loop is
   [responses_retry.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/responses_retry.rs) and
   the retry behaviour is asserted by
   [core/tests/suite/stream_no_completed.rs](https://github.com/openai/codex/blob/main/codex-rs/core/tests/suite/stream_no_completed.rs)
   ("Verifies that the agent retries when the SSE stream terminates before delivering a `response.completed`
   event").
   It is therefore not an infinite hang, but the user sees repeated "Reconnecting… n/5" followed by failure.

`response.completed` payload requirements, from
[`ResponseCompleted`](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/sse/responses.rs#L118):
`id: String` is **required (no serde default)** — a `response.completed` without `response.id` fails to
parse and becomes an error. `usage`, `usage_metadata` and `end_turn` are optional. Inside `usage`,
`input_tokens`, `output_tokens`, `total_tokens` are required; `input_tokens_details.cached_tokens` and
`output_tokens_details.reasoning_tokens` are read when present. Codex's own mock server emits the minimum as
`{"type":"response.completed","response":{"id":…,"usage":{…}}}` **[I]**
[core/tests/common/responses.rs L732–L745](https://github.com/openai/codex/blob/main/codex-rs/core/tests/common/responses.rs#L732).

Codex sends `Accept: text/event-stream` and posts to `{base_url}/responses` **[I]**
[codex-api/src/endpoint/responses.rs L139–L147](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/endpoint/responses.rs#L139).
`sequence_number`, `obfuscation` and per-event ordering are **not** consumed by Codex at all — no code path
reads them. **[I]**

## B.4 `previous_response_id` and `store`

- **HTTP path: Codex does *not* use `previous_response_id`.** It sends the entire `input` array every turn.
  The only construction site of `previous_response_id` is `ResponseCreateWsRequest`, built from a
  `WebsocketContinuation` **[I]**
  [client.rs L1945–L1948](https://github.com/openai/codex/blob/main/codex-rs/core/src/client.rs#L1945), and
  the WebSocket transport is gated on the provider advertising `supports_websockets` (the config key
  `model_providers.<id>.supports_websockets`), which is not set for ordinary custom providers. **[I]**
- **`store: false` is unconditional** on every request **[I]** (client.rs L993). Codex never asks the
  upstream to persist anything, and never reads anything back by id on the HTTP path. A server that ignores
  `store` entirely is fine; a server that *rejects* `store: false` is not.
- Corollary: an upstream that implements `previous_response_id` but not stateless replay gains nothing from
  Codex, and an upstream that *requires* a stored response to answer correctly (Ollama's bug in §A.2) will
  silently truncate turns.

## B.5 What happens against a Chat-Completions-shaped upstream

- **Config time (current builds): hard failure.** `wire_api = "chat"` is rejected with the migration error
  above; there is no Chat branch left in the client (`.chat_completions()` does not exist; there is no chat
  converter module). **[I]** (whole-repo grep for `WireApi::Chat` / `chat/completions` in `core`,
  `codex-api`, `ollama`, `lmstudio` returns nothing)
- **Runtime, if a server returns Chat-Completions-shaped JSON (or non-SSE) from `POST /responses`:** the SSE
  reader sees no parseable events, hits end-of-stream with no `response.completed`, and Codex reports
  `stream disconnected before completion: stream closed before response.completed`, retries up to
  `stream_max_retries`, then fails the turn. **[I]** (derived from responses.rs L611 + the retry loop)
- **Historical:** the Chat wire API existed and was deprecated 2025-12-09 with removal "early February 2026"
  **[D]** [discussion #7782](https://github.com/openai/codex/discussions/7782). Anything written before then
  about "Codex works with OpenAI-compatible chat endpoints" is now stale.

## B.6 Tool-schema surface Codex puts in `tools`

[`ToolSpec`](https://github.com/openai/codex/blob/main/codex-rs/tools/src/tool_spec.rs#L20) is a
`#[serde(tag = "type")]` enum with five wire shapes:

| wire `type` | shape | notes |
|---|---|---|
| `function` | `{name, description, strict, parameters, defer_loading?}` | `strict` is a real bool field, defaulting to `false` in most specs **[I]** [responses_api.rs L32](https://github.com/openai/codex/blob/main/codex-rs/tools/src/responses_api.rs#L32) |
| `custom` | `{name, description, format:{type, syntax, definition}, defer_loading?}` | freeform tools; the Codex `apply_patch` tool uses this shape when the model catalog sets `apply_patch_tool_type: "freeform"` |
| `namespace` | `{name, description, tools:[function|custom]}` | **not an OpenAI Responses API type** — a Codex-internal wrapper for MCP/multi-agent tool families; OpenAI/Azure expand it server-side |
| `tool_search` | `{execution, description, parameters}` | emitted when deferred tool exposure is active |
| `web_search` | `{external_web_access?, indexed_web_access?, filters?, user_location?, search_context_size?, search_content_types?}` | built-in; disabled by the `web_search` config key |

The `namespace` shape is the single biggest hidden incompatibility. Observed behaviour: *"Codex serializes
each MCP server's tools inside a proprietary `{"type":"namespace",…}` wrapper, which is not part of the
standard OpenAI Responses API. OpenAI and Azure expand the wrapper and reach the nested tools, but other
Responses API backends pass it through unchanged or reject any tool whose `type` is not `function`, so the
model only ever sees a single non-callable `mcp__<server>` tool."* **[I]**
[openai/codex#26234](https://github.com/openai/codex/issues/26234) — which proposes a `namespace_tools`
provider capability defaulting to `requires_openai_auth`.

Second hidden incompatibility: **custom/freeform tools.** Code Mode and freeform `apply_patch` are sent as
`type:"custom"`. A provider that accepts only `function` gives the user *"Unsupported custom tool: 'exec'.
Only 'apply_patch' is supported."* **[I]** [openai/codex#37825](https://github.com/openai/codex/issues/37825).
DeepSeek's whitelist for exactly `apply_patch` (§A.1) exists for this reason.

## B.7 Input item shapes Codex replays that break strict servers

Codex replays its own history verbatim, so the *input* array contains item types the standard set does not
cover:

- `function_call_output` **without `call_id`** — `Failed to deserialize the JSON body into the target type:
  input: missing field 'call_id'`. *"Strict deserializers (e.g. DeepSeek) reject the entire request at the
  wire layer, so a single dangling item breaks every message in the thread."* **[I]**
  [openai/codex#42088](https://github.com/openai/codex/issues/42088)
- Missing `id` on `reasoning` items and missing `status` on assistant `message` items, because id
  re-attachment is guarded by an Azure-only condition — *"any strictly-conforming server will reject them
  with a 400 validation error."* **[I]** [openai/codex#12230](https://github.com/openai/codex/issues/12230)
- `agent_message` items from multi-agent threads — `Unsupported Responses API input item type:
  'agent_message'` **[I]** [sglang#35216](https://github.com/sgl-project/sglang/pull/35216)
- `additional_tools` items (Code Mode declares tools this way, with `tools: []` at top level) **[I]**
  [sglang#35216](https://github.com/sgl-project/sglang/pull/35216)
- array-shaped `function_call_output.output` carrying `input_image` parts (from `view_image`) — servers that
  string-join text fields drop the image, and some 400 **[I]**
  [sglang#35216](https://github.com/sgl-project/sglang/pull/35216)
- Because `tools` is **omitted** when empty while `additional_tools` may carry the real definitions, a
  server that requires `tools` to be present will see `tools: []` or no `tools` at all. **[I]**

## B.8 Failure reports and root causes

| symptom | upstream | root cause | source |
|---|---|---|---|
| `Reconnecting… 5/5`, `Stream disconnected before completion: … (http://localhost:8000/v1/responses)`, turn recorded with `last_agent_message: null` | vLLM | **`localhost` vs `127.0.0.1`** in `base_url` — worked after switching to `127.0.0.1` | **[I]** [openai/codex#21773](https://github.com/openai/codex/issues/21773) |
| `stream closed before response.completed` on a `/responses` endpoint | any | server never sent `response.completed` (or sent non-SSE / chat-shaped body) | **[I]** source + [stream_no_completed.rs](https://github.com/openai/codex/blob/main/codex-rs/core/tests/suite/stream_no_completed.rs) |
| 400 `missing field 'call_id'` on resume / into a thread with a prior tool call | DeepSeek (and any strict serde) | dangling `function_call_output` in replayed input | **[I]** [openai/codex#42088](https://github.com/openai/codex/issues/42088) |
| 400 on turn 2 for reasoning/multi-turn threads | vLLM, any strict server | missing `id` on `reasoning`, missing `status` on assistant `message`, missing `id` on user/system `message` | **[I]** [openai/codex#12230](https://github.com/openai/codex/issues/12230) |
| MCP tools never called (model sees one opaque tool) | Ollama, LM Studio, OpenRouter, Bedrock Mantle | `{"type":"namespace"}` is not expanded by non-OpenAI backends | **[I]** [openai/codex#26234](https://github.com/openai/codex/issues/26234) |
| `Unsupported custom tool: 'exec'. Only 'apply_patch' is supported.` | DeepSeek | Code Mode `exec` is emitted as a freeform `custom` tool; provider only whitelists `apply_patch` | **[I]** [openai/codex#37825](https://github.com/openai/codex/issues/37825) |
| `tool_search aborts with 'unsupported payload'` | Ollama, Bifrost | `tool_search` / namespace stubs are not understood | **[I]** [openai/codex#20574](https://github.com/openai/codex/issues/20574) |
| Agent turn ends after tools run; assistant message empty and usage zero | Ollama `/v1/responses` | endpoint accepts `previous_response_id` but returns an empty completed response instead of state or a 4xx | **[I]** [ollama#18419](https://github.com/ollama/ollama/issues/18419) |
| Turn 2 fails against vLLM Responses | vLLM | `previous_response_id` needs `VLLM_ENABLE_RESPONSES_API_STORE=1`; otherwise the id misses | **[I]** [vllm#35740](https://github.com/vllm-project/vllm/pull/35740) |
| Premature turn completion: model emits future-intent text and no tool call, stream still `completed` | custom Responses provider | model behaviour, not protocol; the reporter's recovery proxy re-issued with `previous_response_id` + `tool_choice: "required"` | **[I]** [openai/codex#45096](https://github.com/openai/codex/issues/45096) |
| First-token latency / overload rejection surfaces too late in a proxy | any | upstream sends `response.created → response.in_progress → response.output_item.added` **before** any token; a proxy that treats `output_item.added` as "output has started" commits headers too early | **[I]** [CLIProxyAPI#5724](https://github.com/router-for-me/CLIProxyAPI/pull/5724) |

Reference configurations that are known to work end-to-end (both first-party vendor docs):

- **DeepSeek** — `[model_providers.deepseek] base_url = "https://api.deepseek.com/"`, `wire_api =
  "responses"`, plus a custom `~/.codex/models.json` model catalog, `web_search = "disabled"`. The catalog
  sets `apply_patch_tool_type: "freeform"` (hence the `custom`/`apply_patch` accommodation) and
  `prefer_websockets: false`. **[D]**
  [Integrate with Codex](https://api-docs.deepseek.com/quick_start/agent_integrations/codex)
- **Kimi K3** — `base_url = "https://api.moonshot.cn/v1"`, `wire_api = "responses"`,
  `model_context_window = 1048576`. The page notes web search "开箱即用" (works out of the box) because
  Codex's default request includes the `web_search` tool. **[D]**
  [在 Codex 中使用 Kimi K3](https://platform.kimi.com/docs/guide/codex-kimi)
- **vLLM** — `base_url = "http://localhost:8000/v1"`, `wire_api = "responses"`, dummy `env_key`. **[D]**
  [vLLM Codex integration](https://github.com/vllm-project/vllm/blob/main/docs/serving/integrations/codex.md)

---

# Minimum viable Responses subset for Codex

A server is Codex-usable iff **all** of the following hold.

**Request handling**
1. `POST {base_url}/responses`, JSON body, `Accept: text/event-stream`.
2. Accept and ignore at least: `include` (any values), `store: false`, `prompt_cache_key`,
   `client_metadata` (a non-OpenAI object), `tool_choice: "auto"`, `parallel_tool_calls`, `reasoning`,
   `service_tier`. Silently ignoring unknown keys is strictly safer than rejecting them.
3. `input` is **always a JSON array** of items. Must handle at minimum: `message`
   (`user`/`assistant`/`system`/`developer`; string or `input_text`/`output_text` content),
   `function_call`, `function_call_output` (with and without `id`, and with the `call_id` present),
   `reasoning` (often without `id`), `custom_tool_call`, `custom_tool_call_output`. Ignoring unknown item
   types is safer than 400-ing.
4. Accept `tools` entries of `type: "function"` (with `strict`, `parameters`), `type: "custom"`
   (`name` + `format.syntax`/`format.definition`), `type: "web_search"`, `type: "namespace"` (or flatten
   it). Rejecting `namespace`/`custom` silently disables MCP and Code Mode respectively.
5. `stream: true` must produce SSE, not a JSON body.

**Response / stream**
6. Emit `response.output_item.done` for **every** output item, with the item complete:
   `message` (with `content: [{type:"output_text", text}]`), `reasoning`, and — critically —
   `function_call` with `name`, `call_id` and `arguments` as a **JSON string**.
   Optionally `response.output_item.added` for live text, and `response.output_text.delta`.
7. Emit `response.completed` exactly once, as the last event, containing `response.id` (required) and
   ideally `usage.input_tokens`/`output_tokens`/`total_tokens`.
8. Never end the stream without one of `response.completed` / `response.failed` / `response.incomplete`.
9. Emit errors as `response.failed` with `response.error.{code,message}` — a bare `error` event is ignored.

**Explicitly optional for Codex** (nice to have, not required): `sequence_number`, `obfuscation`,
`response.in_progress`, `response.content_part.*`, `response.output_text.done`,
`response.function_call_arguments.delta/done`, `response.reasoning_summary_text.done`,
`response.created`, `status` values beyond `completed`, `metadata`, `conversation`,
`previous_response_id`, `store`, `truncation`, `max_output_tokens`, `temperature`, `top_p`.

**Two things that will bite even if everything above passes**
- `response.function_call_arguments.delta/done` alone is **not** enough — without
  `response.output_item.done` carrying the assembled call, Codex sees no tool call.
- Reasoning summaries are only shown if `response.reasoning_summary_text.delta` carries **both**
  `delta` and `summary_index`. A server that emits reasoning only as `response.reasoning_text.delta`
  (vLLM, llama.cpp) still works, but the reasoning UI path differs.

---

# Open questions / not established

- **LM Studio** full event inventory (its streaming-events doc page 404s). Documented minimum only.
- **MiniMax / Ark** exact SSE event inventories beyond what their examples show; Ark's `text.format`
  json_schema support outside prefill mode.
- **OpenRouter**'s exact policy for Codex-specific fields (`client_metadata`, `include`,
  `parallel_tool_calls`) — its docs describe the supported surface but not the drop/forward rule.
- Whether any of these vendors **rejects** unknown JSON keys rather than ignoring them. DeepSeek and
  DashScope both state they ignore unlisted params **[D]**; for the rest this is **[U]**.
- Codex behaviour on a `/responses` endpoint that returns HTTP 200 with `Content-Type: application/json`:
  inferred to fail with `stream closed before response.completed`, not directly tested.
- Any per-model behaviour of `use_responses_lite` / `namespace_tools` capabilities for third-party
  providers: the public config reference exposes neither key; `namespace_tools` is inferred from
  `requires_openai_auth` per [#26234](https://github.com/openai/codex/issues/26234).
