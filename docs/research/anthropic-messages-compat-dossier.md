# Anthropic Messages API Compatibility Dossier

**Subject:** divergences of non-Anthropic `POST /v1/messages` implementations (Part A) and what Claude Code requires from a Messages-compatible upstream (Part B).

**Method.** Every claim below is labelled:

- **[documented]** — I fetched the cited URL and the claim is stated (or directly shown in a table/schema) on that page.
- **[community-reported]** — cited from an issue report, blog, or third-party guide; not vendor-official.
- **unknown** — I could not verify it. Nothing is inferred.

Where a GitHub issue body was unreadable (HTML truncation or API rate limiting) I say so explicitly rather than paraphrasing a title as fact.

---

## Part 0 — Official Anthropic baseline (the thing being diverged from)

Sources fetched:
[Messages API reference](https://platform.claude.com/docs/en/api/messages) ·
[Streaming messages](https://platform.claude.com/docs/en/build-with-claude/streaming) ·
[Errors](https://platform.claude.com/docs/en/api/errors) ·
[Stop reasons and fallback](https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons)

> Note: `docs.anthropic.com` 301-redirects to `platform.claude.com`, which the fetch tool could not reach; I retrieved these pages as Markdown (`.md` suffix) with `curl`. Both resolve to the same canonical docs.

### 0.1 Endpoint, auth, headers [documented]

| Item | Value |
| --- | --- |
| Inference | `POST /v1/messages` |
| Token counting | `POST /v1/messages/count_tokens` — separate documented endpoint; returns `MessageTokensCount { input_tokens }` = "total number of tokens across the provided list of messages, system prompt, and tools" |
| Auth | `x-api-key: $ANTHROPIC_API_KEY` (all cURL examples) |
| Version header | `anthropic-version: 2023-06-01` (all cURL examples) |
| Other request headers | `anthropic-beta` (comma-separated capability values), `anthropic-workspace-id`, `anthropic-user-profile-id` |
| Request size limit | Messages API **32 MB** (413 `request_too_large` beyond it) |

### 0.2 Request schema [documented]

**Required:** `model`, `max_tokens` (number, `minimum: 0`), `messages` (array of `MessageParam`).

| Field | Shape per the reference |
| --- | --- |
| `messages[].role` | `user` or `assistant` **only**. The reference states verbatim: *"there is no `"system"` role for input messages in the Messages API"*. Limit 100,000 messages. |
| `messages[].content` | `string` or array of `ContentBlockParam`; a string is shorthand for one `text` block |
| `system` | `optional string or array of TextBlockParam`; each block `{type:"text", text (minLength 1), cache_control?, citations?}` |
| `max_tokens` | `minimum: 0`; "Set to `0` to populate the prompt cache without generating a response" |
| `cache_control` | `CacheControlEphemeral` = `{type:"ephemeral", ttl?: "5m"\|"1h"}` (default `5m`). Placement: on content blocks, on `system` text blocks, **on tool definitions** (`Tool.cache_control`), and **top-level** ("Top-level cache control automatically applies a `cache_control` marker to the last cacheable block in the request"). |
| `thinking` | `{type:"enabled", budget_tokens (minimum 1024, must be < max_tokens), display?: "summarized"\|"omitted"}` \| `{type:"disabled"}` \| `{type:"adaptive", display?}`. "Requires a minimum budget of 1,024 tokens and counts towards your `max_tokens` limit." |
| `tool_choice` | `{type:"auto"\|"any"\|"tool"(+`name`)\|"none"}`; `disable_parallel_tool_use?: boolean` on `auto`/`any`/`tool` (**not** on `none`) |
| `tools[]` | `Tool`: `name` (pattern `^[a-zA-Z0-9_-]{1,128}$`), `description`, `input_schema` (`type:"object"`, `properties`, `required`), plus `cache_control`, `defer_loading`, `allowed_callers`, `type` |
| `stop_sequences` | `optional array of string` |
| `top_k` / `top_p` / `temperature` | all `optional number` |
| `metadata` | `{user_id?: string or null}` — maxLength 512 |
| `service_tier` | `optional "auto" or "standard_only"` |
| `container` | `optional ContainerParams or null` (`{id?, skills?[]}`) or a plain string |
| `output_config` | `{effort?: "low"\|"medium"\|"high"\|"xhigh"\|"max", format?: {type:"json_schema", schema}}` |
| `inference_geo` | `optional string or null` |
| `stream` | `optional boolean` |

### 0.3 Response envelope [documented]

`id`, `type` (`"message"`), `role`, `model`, `content` (array of content blocks), `stop_reason`, `stop_sequence`, `usage`; the stop-reasons page also shows `stop_details` and `container` on the response.

**`stop_reason` value set** — exactly these seven [documented]:

`end_turn`, `max_tokens`, `stop_sequence`, `tool_use`, `pause_turn`, `refusal`, `model_context_window_exceeded`.

Verbatim semantics note: *"In non-streaming mode this value is always non-null. In streaming mode, it is null in the `message_start` event and non-null otherwise."*

**`usage` members** [documented]:

`input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `cache_creation {ephemeral_1h_input_tokens, ephemeral_5m_input_tokens}`, `output_tokens_details {thinking_tokens}`, `server_tool_use {web_fetch_requests, web_search_requests}`, `inference_geo`.
Rule stated in the reference: *"Total input tokens in a request is the summation of `input_tokens`, `cache_creation_input_tokens`, and `cache_read_input_tokens`."*

### 0.4 Streaming [documented]

Documented event flow:

1. `message_start` — "contains a `Message` object with empty `content`"
2. per block: `content_block_start` → one or more `content_block_delta` → `content_block_stop`
3. one or more `message_delta` — "indicating top-level changes to the final `Message` object"
4. `message_stop`

Additional documented rules:

- *"Each content block has an `index` that corresponds to its index in the final Message `content` array."*
- *"Event streams may also include any number of `ping` events."*
- Warning in the docs: *"The token counts shown in the `usage` field of the `message_delta` event are **cumulative**."*
- Error events: `event: error` / `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`.
- *"In accordance with the versioning policy, new event types may be added, and your code should handle unknown event types gracefully."*

**Delta types** [documented]: `text_delta`, `input_json_delta` (`partial_json`), `thinking_delta`, `signature_delta` ("sent just before the `content_block_stop` event" for thinking blocks), and `citations_delta` (present in Domain types).

### 0.5 Errors [documented]

Envelope carries a top-level `type: "error"`, an `error` object with `type` and `message`, and a `request_id`:

```json
{ "type": "error",
  "error": { "type": "not_found_error", "message": "The requested resource could not be found." },
  "request_id": "req_011CSHoEeqs5C35K2UUqR7Fy" }
```

Documented error types: `invalid_request_error` (400), `authentication_error` (401), `billing_error` (402), `permission_error` (403), `not_found_error` (404), `conflict_error` (409), `request_too_large` (413), `rate_limit_error` (429), `api_error` (500), `timeout_error` (504), `overloaded_error` (529).

---

## Part A — Where non-Anthropic `/v1/messages` implementations diverge

### A.0 Endpoint / auth / capability matrix (all [documented] unless noted)

| Vendor | base_url | Messages path | `anthropic-version` | Auth | `count_tokens` |
| --- | --- | --- | --- | --- | --- |
| **DeepSeek** | `https://api.deepseek.com/anthropic` | `/anthropic/v1/messages` | **Ignored** | `x-api-key` (fully supported) | not mentioned → unknown |
| **Alibaba Bailian / Qwen** | `https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/apps/anthropic` | `/apps/anthropic/v1/messages` | not mentioned → unknown | `x-api-key` **or** `Authorization: Bearer` | not mentioned; **`/v1/models` explicitly absent (404)** |
| **Moonshot / Kimi** | `https://api.moonshot.cn/anthropic` | `/anthropic/v1/messages` | not mentioned → unknown | **`Authorization: Bearer` only** | not in the Anthropic-compat spec (Kimi has a native `POST /v1/estimate`) |
| **Zhipu GLM** | `https://open.bigmodel.cn/api/anthropic` | `/api/anthropic/v1/messages` | not mentioned → unknown | `x-api-key` | not documented |
| **MiniMax** | `https://api.minimax.io/anthropic` (intl) / `https://api.minimax.cn/anthropic` (CN) | `/anthropic/v1/messages` | not mentioned → unknown | Anthropic SDK default | **yes — `POST /anthropic/v1/messages/count_tokens`** |
| **SiliconFlow** | `https://api.siliconflow.cn/` | doc renders `POST /messages` (exact `/v1` prefix not confirmed) | not mentioned → unknown | **`Authorization: Bearer`** | not documented |
| **Volcengine Ark** | `https://ark.cn-beijing.volces.com/api/compatible` (also `/api/plan`) | `/api/compatible/v1/messages` | not mentioned → unknown | API Key | **yes — nav has "统计 Messages 请求 token 数"** |
| **Tencent Hunyuan** | `https://api.hunyuan.cloud.tencent.com/anthropic` | `/anthropic/v1/messages` | **not listed at all** | **`x-api-key` required** | not documented |
| **ModelScope** [community-reported] | `https://api-inference.modelscope.cn` | `/v1/messages` | **required: `2023-06-01`** | `x-api-key: ms-…` | not documented |

**`anthropic-beta` handling:** DeepSeek — *"Ignored for `/messages`"* (but **required** as `files-api-2025-04-14` for its Files API). Tencent Hunyuan — *"不处理此头部"* (this header is not processed). Everyone else — not mentioned → unknown.

---

### A.1 DeepSeek — the explicitly-unsupported-fields doc

Source: [Using the Anthropic API](https://api-docs.deepseek.com/guides/anthropic_api) — this is the section the task asked for; it is a three-table compatibility matrix.

**HTTP headers**

| Field | Support status |
| --- | --- |
| `anthropic-beta` | Ignored for `/messages`; required (`files-api-2025-04-14`) for Files API endpoints |
| `anthropic-version` | **Ignored** |
| `x-api-key` | Fully Supported |

**Simple fields**

| Field | Status |
| --- | --- |
| `model` | "Use DeepSeek Model Instead" |
| `max_tokens` | Fully Supported |
| `container` | **Ignored** |
| `mcp_servers` | **Ignored** |
| `metadata` | `user_id` supported, others ignored |
| `service_tier` | **Ignored** |
| `stop_sequences` | Fully Supported |
| `stream` | Fully Supported |
| `system` | Fully Supported |
| `temperature` | Fully Supported (range **[0.0 ~ 2.0]**) |
| `thinking` | Supported (**`budget_tokens` is ignored**) |
| `output_config` | Only `effort` is supported |
| `top_k` | **Ignored** |
| `top_p` | Only takes effect in thinking mode (lower bound `0.95`); in non-thinking mode fixed at `1.0` |

**Tool fields** — `tools[].name`/`input_schema`/`description` Fully Supported; **`tools[].cache_control` Ignored**. `tool_choice` `none`/`auto`/`any`/`tool` all supported but **`disable_parallel_tool_use` is ignored on every value**.

**Message content blocks**

| Variant | Status |
| --- | --- |
| `string`; `text` | Fully Supported (`cache_control` **Ignored**, `citations` **Ignored**) |
| `image` | Supported — `source.type` = base64 (jpeg/png/gif/webp), url, or file (file requires `anthropic-beta: files-api-2025-04-14`) |
| `document` | **Not Supported** |
| `search_result` | **Not Supported** |
| `thinking` | Supported |
| `redacted_thinking` | **Not Supported** |
| `tool_use` (id/input/name) | Fully Supported (`cache_control` Ignored) |
| `tool_result` (tool_use_id/content) | Fully Supported (`cache_control` and **`is_error` Ignored**) |
| `server_tool_use`, `web_search_tool_result` | Supported |
| `code_execution_tool_result`, `mcp_tool_use`, `mcp_tool_result`, `container_upload` | **Not Supported** |

**Model mapping** [documented]: `claude-opus*` → `deepseek-v4-pro`; `claude-haiku*` / `claude-sonnet*` → `deepseek-flash`; any unsupported model name is auto-mapped to `deepseek-flash`.

**No SSE event list is published for DeepSeek** → streaming ordering/ping behaviour is **unknown** from official docs.

---

### A.2 Alibaba Qwen / DashScope / Bailian Model Studio

Source: [Anthropic兼容-Messages](https://help.aliyun.com/zh/model-studio/anthropic-api-messages) (the English `alibabacloud.com/help/en/model-studio/anthropic-api-messages` page exists but failed to fetch).

Bailian publishes its own **"与 Anthropic 官方 API 的主要差异"** table [documented]:

| Difference | Detail |
| --- | --- |
| Base URL | `https://{WorkspaceId}.<region>.maas.aliyuncs.com/apps/anthropic` — **the path segment is `apps/anthropic`, not `anthropic`** |
| Auth | `x-api-key` **or** `Authorization: Bearer`, either one |
| Model names | must be Bailian model names (e.g. `qwen3.7-plus`) |
| **`temperature` range** | **[0, 2)** vs Anthropic's [0.0, 1.0] |
| **Interface scope** | **only Messages (`/v1/messages`); no `/v1/models`; client model-discovery requests return 404** |
| **Extension params** | **`output_config` (structured output + `effort`) is a Bailian extension not present in official SDK type definitions**, must be passed through in the body; **`thinking.budget_tokens` is being deprecated** in favour of `output_config.effort` |

Other documented specifics:

- `model` **required**; `max_tokens` **required**.
- `system`: `string 或 array` — the string form is equivalent to a single `text` block, and **the array form is required to place an explicit cache breakpoint**.
- `messages[].role`: `user`, `assistant`, **and `system`** — Bailian documents accepting the `system` role inside `messages`, which is exactly the shape that broke DeepSeek.
- Content blocks: `text` (+`cache_control`), `image` (url/base64), **`video` (url/base64 — non-standard block type)**, `tool_use` (+`cache_control`), `tool_result` (+`cache_control`). **No `document`; no `redacted_thinking`.**
- `tool_result.content` is documented as **`string`** only (not the string-or-block-array form).
- **`stop_sequences` divergence [documented]:** *"命中后，响应的 `stop_reason` 仍为 `end_turn`，响应不会回填命中的序列"* — on a stop-sequence hit the response still reports `end_turn`, and the matched sequence is **not** returned in `stop_sequence`.
- `thinking`: `{type: enabled|disabled, budget_tokens?}`; per-model default on/off behaviour documented.
- `tool_choice`: **all four** (`auto`, `any`, `none`, `tool`+name). `disable_parallel_tool_use` is **not documented**.
- `output_config.effort`: allowed values are **model-specific** (e.g. `glm-5.3`: low/high/max; `qwen3.8-max`: xhigh/medium/low; others map `low`/`medium`→`high`, `xhigh`→`max`).
- `cache_control` is documented only as `{type:"ephemeral"}` — **no `ttl` field**, unlike Anthropic's 5m/1h.
- `metadata`, `service_tier`, `container`: **not documented** → unknown/unsupported.

---

### A.3 Moonshot / Kimi — the most precise OpenAPI spec of any vendor

Source: [Messages API](https://platform.kimi.com/docs/api/messages) — a full OpenAPI 3.1 document, plus the integration guide [在 Claude Code 中使用 Kimi](https://platform.kimi.com/docs/guide/claude-code-kimi).

- Path `POST /anthropic/v1/messages`; base URL `https://api.moonshot.cn/anthropic` [documented].
- **Auth is `Authorization: Bearer` only** (`securitySchemes.bearerAuth`, "Authorization 请求头需要一个 Bearer 令牌"). **No `x-api-key` scheme is declared** → a meaningful divergence for clients that only send `x-api-key`.
- **Required: `model`, `messages`, `max_tokens`** (`max_tokens` marked 必填).
- `system`: `string` **or** array of `MessagesTextBlockParam` — **both accepted**; the text block has only `type`/`text` (no `cache_control`).
- `messages[].role`: **`user`, `assistant` only** — the spec says *"系统提示请使用顶层 `system` 字段"* (use the top-level `system` field for system prompts).
- Content blocks: `text`, `image` (base64 or `url` = `ms://<file_id>`), `thinking` (+`signature`), `tool_use`, `tool_result` (content = string, or text/image array). **No `document`, no `redacted_thinking`, no `cache_control` inside content blocks.**
- `tool_choice`: **only `auto` / `any` / `none` — there is no `tool` variant** [documented]. `disable_parallel_tool_use` is **absent**.
- `stop_sequences`: **max 5 items, each ≤ 32 bytes**.
- **`temperature` and `top_p` do not appear in the request schema at all**, and there is **no `top_k`** [documented] — a `thinking` config object is also absent; reasoning is controlled by `output_config.effort` (`low`/`high`/`max`, default `max`).
- `metadata.user_id` supported (the spec recommends passing a stable session id for coding agents).
- **Prompt caching uses a non-standard top-level `cache_control`**: `{type:"ephemeral", ttl?:"5m"|"1h"}`; *"`messages` 消息体内的 `cache_control` 标记会被忽略"* (cache_control inside messages is ignored). Billing is by TTL tier.
- `output_config`: `effort` **and** `format` (`{type:"json_schema", schema}`).
- **`stop_reason` enum: `end_turn`, `max_tokens`, `tool_use`, `refusal`, `null`.** There is **no `stop_sequence`** (the spec documents that hitting one yields `end_turn`), **no `pause_turn`**, and no `model_context_window_exceeded` [documented].
- `usage`: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`, `cache_creation {ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}`, `output_tokens_details.thinking_tokens` — this one matches Anthropic closely.
- **Streaming**: the spec's `MessagesStreamEvent` oneOf contains `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop` — **no `ping` event is defined**. Documented ordering: *"message_start → (content_block_start → content_block_delta… → content_block_stop)… → message_delta → message_stop"*. Each SSE frame's `event` equals `data.type`. Delta types: `text_delta`, `thinking_delta`, `signature_delta`, `input_json_delta`. `message_delta` carries `delta.stop_reason`, `delta.stop_sequence`, and `usage`.
- **Error envelope**: `{type:"error", error:{type,message}, request_id}` — matches Anthropic.
- Non-standard extras: request header `X-Msh-Request-Nonce`; response headers `Msh-Request-Timestamp`, `Msh-Request-Signature` (the latter enables `POST /v1/signatures/verify`).

---

### A.4 Zhipu GLM / 智谱

Sources: [Claude API 兼容](https://docs.bigmodel.cn/cn/guide/develop/claude/introduction) · [Claude Code](https://docs.bigmodel.cn/cn/guide/develop/claude) · [常见问题](https://docs.bigmodel.cn/cn/coding-plan/faq) · [使用须知](https://docs.bigmodel.cn/cn/coding-plan/usage-notes)

**Documented:**
- base_url `https://open.bigmodel.cn/api/anthropic`; the cURL example hits `https://open.bigmodel.cn/api/anthropic/v1/messages` with header `x-api-key: YOUR_API_KEY` (plus `content-type`; no `anthropic-version` shown).
- Claude Code integration (`~/.claude/settings.json` → `env`): `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic`, optional `ANTHROPIC_DEFAULT_HAIKU_MODEL` / `_SONNET_MODEL` / `_OPUS_MODEL`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: 1`, `API_TIMEOUT_MS`.
- Model-name suffix `[1m]` (e.g. `glm-5.2[1m]`) enables the 1M context window, paired with `CLAUDE_CODE_AUTO_COMPACT_WINDOW: "1000000"`.
- `/effort` mapping documented: `low, medium, high` → GLM `high`; `xhigh, max, ultracode` → GLM `max`.
- Zhipu states it validated against **Claude Code 2.1.140**.
- The coding-plan FAQ states the Claude Code base URL must be exactly `https://open.bigmodel.cn/api/anthropic` for plan quota to apply.

**Gap (important):** Zhipu publishes **no field-level compatibility matrix**. The only statement is a warning: *"某些场景下智谱与 Claude 接口仍存在差异，但不影响整体兼容性"* ("in some scenarios there are still differences from the Claude interface, but they don't affect overall compatibility"). Whether GLM accepts `system` as an array, `cache_control`, `output_config`, the `thinking.adaptive` tag, `service_tier`, or which SSE events it emits is **not documented** — I could not verify any of it. GLM's divergences are therefore **unknown**, not "none".

---

### A.5 MiniMax

Source: [Anthropic SDK](https://platform.minimax.io/docs/api-reference/text-anthropic-api) · [Claude Code (Token Plan)](https://platform.minimax.io/docs/token-plan/claude-code)

- base_url `https://api.minimax.io/anthropic` (international) or `https://api.minimax.cn/anthropic` (China).
- **`POST /anthropic/v1/messages/count_tokens` exists** — "for `MiniMax-M3` token estimation… returns input token usage without generating model output" [documented].
- Supported: `model`, `messages` (partial), `max_tokens`, `stream`, `system`, `temperature` **[0, 2]**, `tool_choice`, `tools`, `top_p` (default 0.95 for M3 / 0.9 for M2.x), `metadata`, `thinking`, `service_tier`.
- **`service_tier` values are `standard` and `priority`** — priority is billed at 1.5× standard [documented]. Anthropic's values are `auto`/`standard_only`, so this is a value-set divergence.
- **Ignored fields** [documented]: `top_k`, **`stop_sequences`**, `mcp_servers`, `context_management`, `container`.
- Messages field support: `text` (all models), `image` (**M3 only**; URL or base64; JPEG/PNG/GIF/WEBP), **`video` (M3 only; URL, base64, or `mm_file://{file_id}`; MP4/AVI/MOV/MKV — non-standard block type)**, `tool_use`, `tool_result`, `thinking`. **No `document`, no `redacted_thinking`, no `cache_control`.**
- **Thinking control is non-standard**: `thinking: {"type": "adaptive"}` **enables** thinking on M3 (the vendor says "for MiniMax-M3, `adaptive` is equivalent to thinking on"); `{"type":"disabled"}` keeps it off; M2.x cannot disable thinking. **No `budget_tokens` form is documented**, so Anthropic's `{type:"enabled", budget_tokens:N}` is **unknown/likely unsupported**.
- Multi-turn rule: *"the complete model response (i.e., the assistant message) must be append to the conversation history"* — all of thinking/text/tool_use must be replayed.
- Claude Code setup requires clearing `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_BASE_URL` first, then setting them in `settings.json`; `CLAUDE_CODE_AUTO_COMPACT_WINDOW` = 1000000.
- **No SSE event list or `ping` behaviour is documented.**

---

### A.6 SiliconFlow

Sources: [创建对话请求（Anthropic）](https://docs.siliconflow.cn/docs/api/messages-post) · [Claude Code](https://docs.siliconflow.cn/docs/usercases/use-siliconcloud-in-ClaudeCode)

- Claude Code guide exports `ANTHROPIC_BASE_URL="https://api.siliconflow.cn/"`, `ANTHROPIC_MODEL`, `ANTHROPIC_API_KEY`.
- API reference renders the endpoint as `POST /messages`; **the exact `/v1` prefix is not shown on the fetched page — treat the full path as unconfirmed**.
- Auth: **`Authorization: Bearer <账户 API Key>`** [documented].
- `system`: `string | array<Text>` — **both accepted** [documented].
- `max_tokens` **required**; `temperature` documented as 0–2 (`value <= 2`); `top_p <= 1`; **`top_k` supported (`value <= 100`)**.
- `tool_choice`: **`Auto | Tool | None` — there is no `any`** [documented]. `disable_parallel_tool_use` is supported.
- `thinking`: `{type:"disabled"}` or `{type:"enabled", budget_tokens}` (budget_tokens required in the enabled form).
- `stop_sequences` behaviour matches Anthropic [documented]: hitting one sets `stop_reason = "stop_sequence"` and `stop_sequence` holds the matched string.
- **Streaming terminates with `data: [DONE]`** — *"流式传输通常以 `data: [DONE]` 结束"* [documented]. **Anthropic's protocol has no `[DONE]` sentinel**, so this is a wire-level divergence.
- Non-standard extras: request header `X-Trace-Id`; response header `x-siliconcloud-trace-id`.
- Response envelope begins `id`, `type` ("message"), `role` ("assistant"), `content` (the page was truncated after this) — full envelope and `stop_reason`/`usage` sets **unverified**.

---

### A.7 Volcengine 火山方舟 (Ark)

Sources: [创建 Message](https://docs.volcengine.com/docs/82379/2655179?lang=zh) · [接入三方工具](https://docs.volcengine.com/docs/82379/2160841?lang=zh)

- Anthropic-compatible base URL for Claude Code: **`https://ark.cn-beijing.volces.com/api/compatible`**; Agent Plan variant **`https://ark.cn-beijing.volces.com/api/plan`**.
- Endpoint: **`POST https://ark.cn-beijing.volces.com/api/compatible/v1/messages`** [documented]. The API-reference nav also lists **"统计 Messages 请求 token 数"**, i.e. a token-counting endpoint exists.
- Auth: "API Key 鉴权" (see the platform's "Base URL 及鉴权" page).
- **`messages[].role` accepts `user`, `system`, `assistant`, and `developer`** [documented] — this vendor explicitly allows the mid-conversation `system` role that broke DeepSeek. `developer` is a non-standard extra role.
- **`max_tokens` is listed without the 必选 (required) marker** → optional [documented].
- `metadata.user_id` supported.
- Content blocks: `text`, `thinking` (the doc says **`signature` is required when replaying a thinking block**, and the block must be returned unchanged and in order), `image` (url/base64), **`document`** (url, base64, text, or `content`), `tool_use`, `tool_result` (string or text/image block array, plus **`tool_reference`**), `server_tool_use`, `web_search_tool_result` (+ `web_search_tool_result_error`).
- **`output_config.effort` accepts `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`** — `none` and `minimal` are non-standard additions to Anthropic's five-value set.
- **`output_format`** is a **non-standard field name**: `{type: "text"|"json_object"|"json_schema", schema?}`. Anthropic's equivalent lives at `output_config.format`.
- **`service_tier` values are `default` and `flex`** — **not** Anthropic's `auto`/`standard_only` [documented].
- `stop_sequences`: a single string or **up to 4** sequences.
- **SSE events documented**: `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, plus `message_start`/`message_stop` and an `error` event. Delta types: `text_delta`, `thinking_delta`, **`signature_delta`**, `input_json_delta`. `message_delta` carries `delta.stop_reason`, `delta.stop_sequence`, and `usage`. **No `ping` event is documented.**
- **Error envelope has extra members**: `MessagesErrorResponse` = `{type:"error", error:{code, message, param, type}}` — `code` and `param` are additions to Anthropic's `{type,message}`.
- `top_k`: **not present in the doc** → likely unsupported. `cache_control`: **not present** → not documented.

---

### A.8 Tencent Hunyuan — full compatibility tables

Source: [混元 Anthropic API 兼容接口相关调用示例](https://cloud.tencent.com/document/product/1729/127293) — base_url `https://api.hunyuan.cloud.tencent.com/anthropic`, full path `https://api.hunyuan.cloud.tencent.com/anthropic/v1/messages`. Models: `hunyuan-2.0-thinking-20251109`, `hunyuan-2.0-instruct-20251111`.

**HTTP Headers** [documented]

| Header | Required | Note |
| --- | --- | --- |
| `content-type` | yes | `application/json` |
| `x-api-key` | **yes** | authentication |
| `anthropic-beta` | no | **"不处理此头部"** — not processed |

`anthropic-version` **does not appear anywhere** in the page.

**Request fields** [documented]

| Field | Required | Note |
| --- | --- | --- |
| `model` | yes | |
| **`max_tokens`** | **no** | maximum output tokens — **optional, unlike Anthropic** |
| `messages` | yes | |
| `messages[].role` | yes | **"支持'user'或'assistant'"** — no `system` role |
| `messages[].content` | yes | string or list |
| `messages[].content[].type` | yes | **only `text`, `thinking`, `tool_use`, `tool_result`** |
| `thinking` block | | `thinking` (required), `signature` (optional) |
| `tool_use` block | | `id`, `name`, `input` |
| `tool_result` block | | `tool_use_id`, `content` (string), `is_error` |
| `metadata.user_id`, `service_tier`, `stop_sequences`, `stream`, `temperature`, `top_k`, `top_p` | no | all present |
| `system` | no | string or list; blocks support **`text` only** |
| `thinking` | no | **`type` = `enabled` or `disabled`**; `budget_tokens` |
| `tools` | no | `name` (req), `description`, `input_schema` |
| `tool_choice` | no | **`auto`, `any`, `tool`, `none`** |

**Streaming** [documented] — supported events are exactly: `message_start`, `message_delta`, `message_stop`, `content_block_start`, `content_block_delta`, `content_block_stop`. **There is no `ping` event.**

- `message_start.message`: `id`, `model`, `type` (fixed `message`), `role` (fixed `assistant`), `content` (fixed `[]`), **`stop_reason` fixed empty**, **`stop_sequence` fixed empty**, `usage {input_tokens, output_tokens, service_tier}` (note `service_tier` living inside `usage` — non-standard placement).
- `message_delta`: `delta.stop_reason` possible values **`end_turn`, `max_tokens`, `stop_sequence`, `tool_use`, `sensitive`**. **`sensitive` replaces Anthropic's `refusal`**; `pause_turn` and `model_context_window_exceeded` are absent. `delta.stop_sequence`; `usage {input_tokens, output_tokens}` (no cache fields).
- `content_block_start.content_block.type`: `text`, `thinking`, `tool_use`, `tool_result`; for text/thinking the payload fields are **fixed empty**.
- `content_block_delta.delta.type`: **`thinking_delta`, `text_delta`, `input_json_delta` — no `signature_delta`**. The thinking signature is instead delivered on `content_block_start` (documented as fixed empty).
- Non-streaming response: `id`, `model`, `role`, `content` (types `text`, `thinking`, `tool_use`), `type`.
- Platform notice: Hunyuan features are migrating to TokenHub; the existing endpoint keeps working.

---

### A.9 ModelScope — community-reported only

Source: [ModelScope（魔搭社区）接入指南](https://raw.githubusercontent.com/wanshuiyin/Auto-claude-code-research-in-sleep/main/docs/MODELSCOPE_GUIDE.md) **[community-reported]**

- Base URL `https://api-inference.modelscope.cn`; the guide's verification command posts to `https://api-inference.modelscope.cn/v1/messages` with headers `x-api-key: ms-…` **and** `anthropic-version: 2023-06-01`.
- One `ms-…` key serves both an Anthropic-compatible endpoint (base URL without `/v1`) and an OpenAI-compatible endpoint (`…/v1`).
- The guide claims ModelScope "兼容 OpenAI 和 Anthropic 两大主流 API 协议".
- **I found no official ModelScope documentation page for the Anthropic-compatible endpoint.** Everything above is a third-party guide. Field-level behaviour is **unknown**.

---

### A.10 Cross-vendor field matrix (documented rows only)

`✔` = documented supported · `✖` = documented unsupported/ignored · `?` = not documented / unknown

| Field | Anthropic | DeepSeek | Bailian | Kimi | GLM | MiniMax | SiliconFlow | Ark | Hunyuan |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `max_tokens` required | yes | ? | **yes** | **yes** | ? | ? | **yes** | **no** | **no** |
| `system` as array | ✔ | ? (doc says "Fully Supported"; array behaviour community-reported broken) | ✔ | ✔ | ? | ? | ✔ | ✔ | ✔ |
| `messages[].role = "system"` | **✖ (not a valid role)** | ✖ (400) | **✔** | ✖ | ? | ? | ? | **✔** | ✖ |
| `temperature` range | [0,1] | [0,2] | [0,2) | field absent | ? | [0,2] | [0,2] | ? | ✖? (present) |
| `top_k` | ✔ | ✖ ignored | ✔ | **absent** | ? | ✖ ignored | ✔ | **absent** | ✔ |
| `stop_sequences` | ✔ | ✔ | ✔ (but `end_turn`, no echo) | ✔ (max 5, ≤32B) | ? | **✖ ignored** | ✔ (Anthropic-like) | ✔ (max 4) | ✔ |
| `tool_choice: tool` | ✔ | ✔ | ✔ | **✖** | ? | ? | ✔ | ✔ | ✔ |
| `tool_choice: any` | ✔ | ✔ | ✔ | ✔ | ? | ? | **✖** | ✔ | ✔ |
| `disable_parallel_tool_use` | ✔ | ✖ ignored | ? | **absent** | ? | ? | ✔ | ? | ? |
| `thinking.budget_tokens` | ✔ (≥1024) | **✖ ignored** | ✔ (deprecated) | **absent** | ? | **absent** | ✔ | ? | ✔ |
| `thinking.type: adaptive` | ✔ | ? | ✖ (enabled/disabled only) | n/a (`output_config.effort`) | ? | **✔ (non-standard meaning: "on")** | ✖ | ? | ✖ |
| `cache_control` on content | ✔ | ✖ ignored | ✔ ({type:ephemeral} only, **no ttl**) | **✖ (ignored inside messages)** | ? | **absent** | ? | absent | **absent** |
| top-level `cache_control` | ✔ | ? | ? | **✔ (the only honoured placement)** | ? | ? | ? | ? | ? |
| `document` block | ✔ | ✖ | ✖ | ✖ | ? | ✖ | ? | **✔** | ✖ |
| `redacted_thinking` | ✔ | ✖ | ✖ | ✖ | ? | ✖ | ? | ? | ✖ |
| `image` block | ✔ | ✔ | ✔ | ✔ | ? | ✔ (M3) | ? | ✔ | ✖ |
| `video` block (non-standard) | ✖ | ✖ | **✔** | ✖ | ✖ | **✔ (M3)** | ✖ | ✖ | ✖ |
| `metadata.user_id` | ✔ | ✔ | ? | ✔ | ? | ✔ | ? | ✔ | ✔ |
| `service_tier` | ✔ (`auto`/`standard_only`) | ✖ ignored | ? | absent | ? | ✔ (`standard`/`priority`) | ? | ✔ (**`default`/`flex`**) | present |
| `container` | ✔ | ✖ ignored | ? | absent | ? | ✖ ignored | ? | ? | ? |
| `output_config.effort` | ✔ | ✔ (only `effort`) | ✔ (extension) | ✔ | ? | ? | ? | ✔ (+`none`,`minimal`) | ✖? (`output_config` absent) |
| `count_tokens` | ✔ | ? | absent | not in compat spec | ? | **✔** | ? | **✔** | ? |
| emits `ping` | ✔ (may) | ? | ? | **✖ not in spec** | ? | ? | ? | **✖** | **✖** |
| `signature_delta` | ✔ | ? | ? | ✔ | ? | ? | ? | ✔ | **✖** |
| `stop_reason` = `stop_sequence` | ✔ | ? | **✖ (reports `end_turn`)** | **✖ (reports `end_turn`)** | ? | ? | ✔ | ? | ✔ |
| `stop_reason` = `refusal` | ✔ | ? | ? | ✔ | ? | ? | ? | ? | **✖ (uses `sensitive`)** |
| `stop_reason` = `pause_turn` | ✔ | ? | ? | ✖ | ? | ? | ? | ? | ✖ |
| `stop_reason` = `model_context_window_exceeded` | ✔ | ? | ? | ✖ | ? | ? | ? | ? | ✖ |
| `usage.cache_creation` breakdown | ✔ | ? | ? | ✔ | ? | ? | ? | ? | ✖ |
| `usage.output_tokens_details.thinking_tokens` | ✔ | ? | ? | ✔ | ? | ? | ? | ? | ✖ |
| error envelope shape | `{type,error{type,message},request_id}` | ? | ? | same | ? | ? | ? | **+ `code`, `param`** | ? |

---

### A.11 The `system`-as-content-block-array incompatibility (the headline case)

Two distinct things are conflated in the ecosystem; separating them matters:

1. **Top-level `system` sent as an array of text blocks** (valid Anthropic, and what Claude Code uses to attach `cache_control`).
2. **A `messages[]` entry with `role: "system"`** (Claude Code appends mid-conversation system/skill/hook context this way). Anthropic's own reference says there is no `system` role for input messages — so this is a Claude Code behaviour that only works because the first-party API tolerates it, and it is rejected by anything that validates the documented role enum.

**Evidence — [community-reported]:**

- [anthropics/claude-code#63366](https://github.com/anthropics/claude-code/issues/63366) — *"[BUG] 2.1.154 appears to send messages[].role="system" to Anthropic-compatible providers, causing schema parse failures"*. The raw request dump captured with `OTEL_LOG_RAW_API_BODIES` shows `has_top_level_system: true`, `top_level_system_type: "array"`, `message_roles: ["user","system"]`. Error: ``400 Failed to deserialize the JSON body into the target type: messages[1].role: unknown variant `system`, expected `user` or `assistant` ``. Labelled `bug`, `has repro`, `area:api`, `regression`, `area:providers`; **closed as completed on 2026-06-05**. I read the full issue body via the GitHub API; the 3 comments were not retrievable (rate limit).
- [deepseek-ai/DeepSeek-V3#1369](https://github.com/deepseek-ai/DeepSeek-V3/issues/1369) — *"[BUG] Anthropic endpoint: system field as content block array causes 'unknown variant system' error with Claude Code v2.1.154+"*. The reporter's root-cause claim: DeepSeek's endpoint appears to only handle `system` as a string, and its Anthropic→OpenAI conversion **places the text content blocks into `messages` as `role: "system"` entries**, which then fails validation. Workaround given: `npm i -g @anthropic-ai/claude-code@2.1.153`. Comments (read via the API) include a user writing a local proxy to hoist `system` out of the `messages` array, and a note that the array form is part of the Anthropic spec. **Closed as stale / not planned** (bot-closed 2026-07-30).
- [bitrouter/bitrouter#227](https://github.com/bitrouter/bitrouter/issues/227) — a gateway tracker item titled *"fix(anthropic): accept system field as both string and array of content blocks"*, i.e. the same fix must be applied in intermediary proxies. (Title only — I did not read the body.)

**Which vendors reject the array form — documented:**

- **Reject `role: "system"` in `messages`:** DeepSeek (community-reported 400), **Kimi** (spec declares role enum `user`/`assistant` and says to use top-level `system`), **Tencent Hunyuan** (role is 'user' or 'assistant').
- **Accept `role: "system"` in `messages`:** **Alibaba Bailian** (documents `system` as a valid role), **Volcengine Ark** (documents `user`/`system`/`assistant`/`developer`).
- **Accept `system` as a top-level array:** Kimi, Bailian, SiliconFlow, Ark (with `document`/`thinking` blocks), Hunyuan (text blocks only) — all documented. DeepSeek documents `system` as "Fully Supported" but its array handling is community-reported broken.
- **Not documented either way:** GLM, MiniMax, ModelScope.

---

## Part B — What Claude Code requires from a Messages-compatible upstream

Primary source: **[Claude Code gateway compatibility guide](https://code.claude.com/docs/en/llm-gateway-protocol)** — this is Anthropic's own statement of what Claude Code sends to an `ANTHROPIC_BASE_URL` endpoint and what breaks when it's stripped. Supporting:
[Other LLM gateways](https://code.claude.com/docs/en/llm-gateway) ·
[Connect Claude Code to an LLM gateway](https://code.claude.com/docs/en/llm-gateway-connect) ·
[Claude Code errors](https://code.claude.com/docs/en/errors)

### B.1 Endpoints Claude Code calls [documented]

| Endpoint | Required? | Notes |
| --- | --- | --- |
| `POST /v1/messages` | yes | *"Inference requests post to `/v1/messages?beta=true`"* — **match on the path, not the full URL**, because of the query string |
| `POST /v1/messages/count_tokens` | **optional** | *"Token-counting endpoints are the only optional ones: when they're absent, Claude Code falls back to a character-based estimate of context usage."* Symptom when missing: no error, but `/context` shows approximate counts |
| `GET /v1/models?limit=1000` | optional | Gateway model discovery, off by default (`CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`); 3 s default timeout; **any redirect is treated as failure**; Claude Code keeps only entries whose `id` contains `claude` or `anthropic`, case-insensitively |
| `HEAD /api/hello` | no | connection-warming probe; can be rejected without breaking anything |

**Headers that must be forwarded unchanged** [documented]: `anthropic-beta` **and** `anthropic-version` (currently `2023-06-01`) — explicit in the API-formats table and restated in the request-headers section. `anthropic-workspace-id` too, when the upstream is Claude Platform on AWS.

Other headers Claude Code sends (may be consumed, need not be forwarded): `Authorization`, `x-api-key`, `x-claude-code-session-id`, `x-claude-code-agent-id`, `x-claude-code-parent-agent-id`, plus anything in `ANTHROPIC_CUSTOM_HEADERS`.

**Credential mapping** [documented in the connect guide]: `ANTHROPIC_AUTH_TOKEN` → `Authorization` bearer; `ANTHROPIC_API_KEY` → `x-api-key`; if you weren't told which, use `ANTHROPIC_AUTH_TOKEN`. So **a provider that only reads `x-api-key` will fail the default configuration**, and vice versa.

### B.2 Request features Claude Code uses [documented]

- **`system` as an array of blocks.** The gateway guide's system-prompt-attribution section states *"The strip is positional, so it only works when the gateway forwards the `system` array unchanged"*, and warns that prepending, reordering, **or converting it to a single string** defeats the strip.
- **`cache_control` ephemeral breakpoints.** Verbatim: *"Claude Code attaches `cache_control` markers to `system` blocks and to `messages` entries, **including `role: "system"` entries appended mid-conversation**."* There is **no beta pairing** for prompt caching.
- **Explicit instruction not to normalise block form:** the remediation column says *"Forward `cache_control` unchanged wherever it appears, and **don't convert block-form `system` or message content to plain strings**."*
- **`thinking`.** *"Claude Code sends `thinking: {"type": "adaptive"}` for Claude 4.6 and later, and treats model names it doesn't recognize, such as gateway aliases, as current models that receive the field."* No beta header pairs with it.
- **`context_management`** — beta header + body field pair; symptom when broken: `400` with `Extra inputs are not permitted`.
- **`output_config`** — carries effort, structured-output format, and task budget; each pairs with its own beta header; symptom: `400` naming `output_config`.
- **Tool definitions with `input_schema`**, plus beta tool schema fields `strict` and `defer_loading` (paired with tool-related beta headers); symptom when the body passes without its header: `400` naming the unrecognized tool schema field.
- **Beta headers only**, no body field: extended/interleaved context — silently unavailable if stripped.
- **Parallel tool use** — the gateway guide documents parallel tool calls reporting their own timing, and Claude Code's tool loop depends on standard `tool_use`/`tool_result` block round-tripping.
- **`stop_sequences`** — Claude Code's use is not called out in the gateway guide; **unknown** as a hard requirement.
- **Streaming** — required in practice: *"Stream inference responses. Claude Code reads the stream as it arrives, so if your gateway buffers complete responses before relaying them, Claude Code stalls."*

### B.3 Streaming: what must appear, and what breaks [documented]

- **`ping` (or any bytes) during silence is functionally required.** Verbatim: *"Claude Code counts every byte your gateway relays, including SSE `ping` events and comment lines, and aborts a stream that goes silent for 300 seconds by default. The upstream's pings are the only traffic during long thinking pauses, so if your gateway strips or buffers them, Claude Code aborts the stream during those pauses… An upstream that sends no pings at all, such as Amazon Bedrock's binary event-stream, leaves those pauses with nothing to forward. **When translating from such an upstream, emit your own `ping` events during silent gaps.**"*

  This is the single most actionable finding for vendors that emit no `ping` — Kimi, Volcengine Ark, and Tencent Hunyuan all omit it from their documented event sets.

- **Buffering / non-streaming fallback.** If a streaming request fails, Claude Code retries it non-streaming; if that returns HTTP 200 with a body that isn't a Claude API message it ends the turn with *"API returned an empty or malformed response (HTTP 200) — check for a proxy or gateway intercepting the request."* The diagnostic reports **how many stream events arrived** and how long the stream had been silent.
- **Content-type:** return `text/event-stream` on streamed Anthropic Messages responses.
- **What exactly breaks if `message_start`, `content_block_stop`, or `message_delta.usage` are missing or duplicated:** Anthropic's gateway guide does **not** enumerate per-event consequences, and I could not retrieve the body of the errors-doc section titled *"Streaming response ended before any complete data was received"* (repeated TLS failures to `code.claude.com`). What *is* documented: events are consumed incrementally, silence is fatal after 300 s, and a stream that produced events before failing is reported by event count. **Per-event failure semantics are therefore unknown from official sources** — I am not going to invent them.
- **Community-reported stream/ordering symptoms** (title-level only; I read these from the GitHub search API and did **not** read the bodies):
  - [claude-code#84404](https://github.com/anthropics/claude-code/issues/84404) — "Regression after 2.1.139: streaming connection resets after first SSE chunk and retries 10 times" (open).
  - [claude-code#92596](https://github.com/anthropics/claude-code/issues/92596) — "Windows: assistant text deltas arrive but are not painted until `message_stop` (thinking streams fine) - v2.1.263" (open).
  - [claude-code#75298](https://github.com/anthropics/claude-code/issues/75298) — "Claude Code CLI reports 'Truncated event message received' on Bedrock Opus 4.8 streams that are byte-complete" (closed).
  - [claude-code#59074](https://github.com/anthropics/claude-code/issues/59074) — VS Code webview throws on an unknown stream-event/delta type (closed).

### B.4 Does Claude Code need `anthropic-beta`? [documented]

It **sends** `anthropic-beta` and the upstream must receive it verbatim: *"Forward the header verbatim; don't allowlist individual values, because the set changes with Claude Code releases."* The guide's *"Forward as open lists"* section says to pass `anthropic-*` headers and request body fields through unchanged rather than pinning an observed list. It also notes that with a claude.ai login the header carries an OAuth capability whose removal fails requests with `401`.

Crucially, **capabilities are header+body pairs**: *"A gateway that strips the header while passing the body, or forwards an Anthropic-format body to an upstream with a different schema, produces hard `400` errors; only when both halves are absent together does the feature turn off quietly."*

Claude Code also has **graceful degradation** [documented]: *"When the upstream rejects the `thinking` field, a mid-conversation system message, or the `cache_control` marker on such a message, Claude Code retries the request and disables the rejected capability for the rest of the conversation."* It does **not** retry rejections of context management or tool schema fields. And *"The retry logic matches on the upstream's error wording, so forward error response bodies unmodified"* — a gateway that re-wraps errors breaks the recovery path.

### B.5 Does Claude Code call `/v1/messages/count_tokens`? [documented]

Yes, but it is optional and failure is silent (character-based fallback; `/context` becomes approximate). Separate from that, `GET /v1/models` powers the `/model` picker and is off by default.

### B.6 Most common reported failures pointing Claude Code at third-party providers

| Failure reported | Root cause | Source |
| --- | --- | --- |
| ``400 … messages[1].role: unknown variant `system`, expected `user` or `assistant` `` | Claude Code ≥ 2.1.154 serialises session/skill/hook context as a `messages[]` entry with `role:"system"`, and sends top-level `system` as an array of blocks. Providers whose schema only allows `user`/`assistant` reject it at parse time, before model routing. | [claude-code#63366](https://github.com/anthropics/claude-code/issues/63366) (closed completed); [DeepSeek-V3#1369](https://github.com/deepseek-ai/DeepSeek-V3/issues/1369) (closed stale) — both **[community-reported]** |
| DeepSeek specifically: `system` array mishandled | Reporter's diagnosis: DeepSeek's Anthropic→OpenAI conversion moves the `system` text blocks into `messages` as `role:"system"` entries, which then fail its own validation. Workaround: pin Claude Code to 2.1.153, or insert a local proxy that hoists `system` back to the top level. | [DeepSeek-V3#1369](https://github.com/deepseek-ai/DeepSeek-V3/issues/1369) + its comments **[community-reported]** |
| `400 thinking options type cannot be disabled when reasoning_effort is set` on `Agent()` spawn / WebSearch / WebFetch against `https://api.deepseek.com/anthropic` | Reporter verified with 11 direct cURL calls that DeepSeek's endpoint accepts `thinking:{type:"disabled"}` and returns 200, so the failure is specific to Claude Code's sub-agent request path rather than the endpoint's `thinking` support. Main conversation worked. | [claude-code#65863](https://github.com/anthropics/claude-code/issues/65863) (closed not_planned) **[community-reported]** |
| Subagents / structured-output requests `400` against third-party endpoints that require a `thinking` field | Claude Code omits `thinking` on those request paths. | [claude-code#69379](https://github.com/anthropics/claude-code/issues/69379) — **title only; body not read** (GitHub API rate limit) **[community-reported]** |
| `400` on every request because a tool's `input_schema.pattern` uses `\p{...}` Unicode property escapes (Artifact tool, Claude Code 2.1.265–2.1.267) | The Anthropic API accepts the regex; third-party gateways/upstreams validate the pattern with their own regex engine and reject the whole request. | Documented in the [gateway connect troubleshooting table](https://code.claude.com/docs/en/llm-gateway-connect); also issues [#92964](https://github.com/anthropics/claude-code/issues/92964), [#93029](https://github.com/anthropics/claude-code/issues/93029) (z.ai) **[documented + community-reported]** |
| `400` with `Extra inputs are not permitted` naming `context_management` or `output_config` | Gateway forwards Anthropic-format fields to an upstream with a different schema, or strips the beta header while passing the body. | [gateway connect troubleshooting](https://code.claude.com/docs/en/llm-gateway-connect); [gateway protocol, feature pass-through](https://code.claude.com/docs/en/llm-gateway-protocol) **[documented]** |
| `400` naming `thinking` or the `adaptive` tag (e.g. `Input tag 'adaptive' found`) | Claude Code requests adaptive reasoning for Claude 4.6+ and for unrecognised/gateway model aliases; the upstream model build doesn't accept it. | [gateway connect troubleshooting](https://code.claude.com/docs/en/llm-gateway-connect) **[documented]** |
| `400` with a context/token limit in the gateway's own words (`ContextWindowExceededError`, `prompt token count of N exceeds the limit of M`) | The gateway enforces a smaller context than the model's native window and rewrites the error, so the too-long recovery (auto-compact) never fires. | [gateway connect troubleshooting](https://code.claude.com/docs/en/llm-gateway-connect) **[documented]** |
| `400` rejecting an unrecognised tool type, e.g. `Input tag 'advisor_20260301'` (v2.1.275) | A gradual-rollout advisor tool entry reaches strict tool-type validators. | [gateway connect troubleshooting](https://code.claude.com/docs/en/llm-gateway-connect) **[documented]** |
| `401` when signed in with a claude.ai login against a gateway | The upstream requires the OAuth capability carried in `anthropic-beta`, which the gateway stripped. | [gateway protocol, request headers](https://code.claude.com/docs/en/llm-gateway-protocol) **[documented]** |
| Conversation bills as fully uncached input every turn, high `input_tokens`, no cache activity | Provider ignores or strips `cache_control`, or the gateway converts block-form `system`/content to strings. **No error is raised.** | [gateway protocol, feature pass-through](https://code.claude.com/docs/en/llm-gateway-protocol) **[documented]** |
| `/context` shows approximate counts | `count_tokens` endpoint absent. **No error.** | [gateway protocol](https://code.claude.com/docs/en/llm-gateway-protocol) **[documented]** |
| ~10× per-turn latency regression against a custom `ANTHROPIC_BASE_URL` (vLLM) — prefix cache stops hitting | Claimed client-side prefix instability. | [claude-code#87227](https://github.com/anthropics/claude-code/issues/87227) — **title only; body not read** **[community-reported]** |
| `400 "text content blocks must be non-empty"` from a custom `ANTHROPIC_BASE_URL`; empty text block persisted and replayed | Claimed client-side persistence of an empty block. | [claude-code#88536](https://github.com/anthropics/claude-code/issues/88536) — **title only; body not read** **[community-reported]** |
| Unrecognised models misinterpret Claude Code's tool/skill/agent payloads | No explicit `behavesAs` mapping for a third-party model id. | [claude-code#92449](https://github.com/anthropics/claude-code/issues/92449) — **title only; body not read** **[community-reported]** |
| CLI reports "Truncated event message received" for byte-complete Bedrock streams | Gateway transforms the binary event-stream; wrong content-type. | [claude-code#75298](https://github.com/anthropics/claude-code/issues/75298) / [errors doc](https://code.claude.com/docs/en/errors) **[documented + community-reported]** |

### B.7 Client-side knobs the gateway/vendor ecosystem relies on [documented]

- `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1` — suppresses most pre-release capabilities and their body fields (not adaptive reasoning, not the OAuth capability).
- `CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING=1` — documented fallback on Opus 4.6 / Sonnet 4.6 when the upstream rejects `adaptive`.
- `CLAUDE_CODE_ATTRIBUTION_HEADER=0` — makes Claude Code omit the system-prompt attribution block, for gateways that must reshape `system`.
- `CLAUDE_CODE_AUTO_COMPACT_WINDOW` — documented remedy when a gateway enforces a smaller context; clamped to ≥100,000 tokens and ≤ the model's window.
- `CLAUDE_CODE_DISABLE_NONSTREAMING_FALLBACK=1` — avoids the malformed-response path when only the non-streaming route through a gateway is broken.
- Model capability variables `ANTHROPIC_DEFAULT_*_MODEL_SUPPORTED_CAPABILITIES` work **only** in the `CLAUDE_CODE_USE_BEDROCK`/`_VERTEX`/`_FOUNDRY`/`_MANTLE` provider configurations and **have no effect behind an `ANTHROPIC_BASE_URL` gateway**.
- Unrecognised model ids: Claude Code assumes a 200K context window, or 1M when the id carries `[1m]` — which is exactly why GLM documents `glm-5.2[1m]` plus `CLAUDE_CODE_AUTO_COMPACT_WINDOW`.

---

## Minimum viable Messages subset for Claude Code

A server that satisfies only this list will run Claude Code reliably; everything beyond it is capability upside.

**Transport**
1. `POST /v1/messages` (tolerate the `?beta=true` query string) returning `text/event-stream` when `stream: true`.
2. Use HTTPS and a stable host; do not redirect `/v1/messages` or `/v1/models`.
3. Accept `Authorization: Bearer` **and** `x-api-key` — Claude Code sends one or the other depending on whether the user set `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_API_KEY`, and you cannot control which.
4. Accept and do not choke on `anthropic-version: 2023-06-01` and the comma-separated `anthropic-beta` header. You may ignore `anthropic-beta` values; you must not error on their presence.
5. Tolerate (and ideally ignore) the extra `x-claude-code-*` headers and `HEAD /api/hello`.

**Request**
6. Parse `model`, `max_tokens`, `messages`, `system`, `tools`, `tool_choice`, `stream`, `stop_sequences`, `temperature`, `top_p`, `metadata.user_id`.
7. **Accept `system` as a plain string AND as an array of `{type:"text", text, cache_control?}` blocks.** Never coerce block form to a string.
8. **Accept a `messages[]` entry with `role: "system"`** (or otherwise tolerate it), even though the documented Anthropic role enum is `user`/`assistant`. This is the single most common breakage. Bailian and Volcengine Ark document accepting it; Kimi and Hunyuan do not.
9. Accept **both** `messages[].content` as a string and as an array of blocks.
10. **Accept and ignore `cache_control: {"type":"ephemeral"}`** wherever it appears — on `system` blocks, on message content blocks, and on tool definitions. Accept `ttl: "5m" | "1h"` without erroring. Ignoring it silently is acceptable (you lose caching, Claude Code does not error); **rejecting it is not**.
11. Accept `thinking: {"type":"adaptive"}` on the request without a `400` — Claude Code sends it for Claude 4.6+ models *and* for unrecognised gateway aliases. Also tolerate `{"type":"enabled","budget_tokens":N}` and `{"type":"disabled"}`.
12. Accept `tool_choice` with all four types `auto` / `any` / `tool` (+`name`) / `none`, and tolerate `disable_parallel_tool_use`.
13. Accept `tools[]` entries with `name`, `description`, `input_schema` (JSON Schema, `type:"object"`). Tolerate unknown tool-schema keys such as `strict` and `defer_loading`, and tolerate odd `pattern` regexes (`\p{...}`).
14. Tolerate unknown/unimplemented top-level fields (`output_config`, `context_management`, `service_tier`, `container`, `mcp_servers`, `metadata` beyond `user_id`) rather than returning `400`. Where you do reject one, say so in the error `message` using wording Claude Code can match — it degrades gracefully on `thinking`, mid-conversation system messages, and `cache_control`.
15. Do at least one full tool-call round trip: emit `tool_use` with `id`/`name`/`input`, and accept `tool_result` with `tool_use_id`/`content`. Support parallel `tool_use` blocks (Claude Code issues parallel tool calls).

**Response**
16. Return the envelope `{id, type:"message", role:"assistant", model, content[], stop_reason, stop_sequence, usage}`.
17. Return `stop_reason` from the standard set — minimum `end_turn`, `max_tokens`, `tool_use`, `stop_sequence`; `refusal` if you have a safety stop. Do **not** invent values like `sensitive`.
18. Return `usage` with at least `input_tokens` and `output_tokens`. Include `cache_creation_input_tokens`, `cache_read_input_tokens`, `cache_creation.{ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}`, and `output_tokens_details.thinking_tokens` if you report cache/thinking accounting.

**Streaming**
19. Emit, in order: `message_start` (with `content: []` and `stop_reason: null`) → for each block `content_block_start` + `content_block_delta`* + `content_block_stop` (closing each block before opening the next, with a correct 0-based `index`) → `message_delta` (carrying `stop_reason`, `stop_sequence`, and cumulative `usage`) → `message_stop`. Set the SSE `event:` name equal to the payload's `type`.
20. Support at least `text_delta` and `input_json_delta` (`partial_json` for tool args); add `thinking_delta` + `signature_delta` if you emit thinking blocks.
21. **Emit keep-alive traffic during long silences** — real `ping` events or SSE comment lines. Claude Code aborts a stream after **300 s** without bytes, which is exactly the length of a long thinking pause. Vendors that document no `ping` event (Kimi, Volcengine Ark, Tencent Hunyuan) will need a translation layer that injects one.
22. Do not use a `data: [DONE]` sentinel in place of a proper `message_stop` (SiliconFlow documents `[DONE]`; Claude Code expects `message_stop`).
23. On a mid-stream failure, emit `event: error` with the standard error payload rather than truncating silently.

**Errors**
24. Return `{"type":"error","error":{"type":"<type>","message":"<msg>"}}` with the correct HTTP status, and include `request_id` if you have one. Use Anthropic's type vocabulary at least for `invalid_request_error`, `authentication_error`, `rate_limit_error`, `api_error`, and `overloaded_error`. Do not wrap upstream errors in your own envelope if you are a gateway — Claude Code's retry/degradation logic matches on the upstream's wording.

**Optional but valuable**
25. `POST /v1/messages/count_tokens` returning `{input_tokens}` — makes `/context` exact instead of approximate. MiniMax and Volcengine Ark document one; Bailian and DeepSeek do not.
26. `GET /v1/models?limit=1000` returning `{data:[{id, display_name?, description?}]}` with ids containing `claude` or `anthropic` — powers the `/model` picker. Bailian explicitly returns 404 here, which is a documented, tolerable gap.

**Known unknown / do not assume:** exactly which failures follow a missing or duplicated `message_start`, `content_block_stop`, or `message_delta.usage` is **not documented** by Anthropic, and I could not retrieve the `code.claude.com` errors-doc section on incomplete streams. GLM and ModelScope publish no field-level compatibility data at all. Treat "no documented divergence" for those two as "unmeasured", not "compatible".
