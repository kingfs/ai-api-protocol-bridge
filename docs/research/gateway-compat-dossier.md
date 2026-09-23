# 聚合网关协议保真度档案：OpenRouter / LiteLLM / Portkey / Cloudflare AI Gateway

> **本文是主报告 §8 的证据底稿。** 主报告给出结论与设计含义，这里保留逐条原始发现。
>
> **证据分级**：**[文档]** = 实际抓取了该 URL 且结论陈述于该页 · **[实测]** = 本机发起的
> 无鉴权 HTTP 探测 · **[实况]** = 实际抓取的 issue / 社区报告 · **未确定** = 在抓取到的
> 文档中找不到，**未编造**。
>
> **方法说明**：OpenRouter 的 Mintlify 文档在 `<page>.md` 提供 Markdown 版本，
> 并发布完整 `llms.txt` 索引与 [OpenAPI spec](https://openrouter.ai/docs/openapi/openapi.yaml)；
> 本档案优先使用它们而非 JS 渲染的 HTML。LiteLLM 与 Portkey 使用各自的 `llms-full.txt`。
>
> **一个贯穿全文的现象：OpenRouter 的散文式指南与它自己的 OpenAPI spec 在若干处互相矛盾。**
> 遇到矛盾时本档案**并列陈述**，不替读者选边。

---

## 0. 路由探测的判定方法（为什么 404 有意义）

无鉴权探测要能区分"路由不存在"和"鉴权失败"，否则结论无效。判据是
**该网关是否"先路由后鉴权"**：

| 网关 | 行为 | 404 是否可判定 |
| --- | --- | --- |
| OpenRouter | 先路由 | **可判定** |
| Cloudflare | **先鉴权**（任何路径都 401） | **不可判定** |

因此本档案对 Cloudflare 的路径存在性**不作任何断言**——其全部探测都是 401，
包括已文档化的路径。

---

## 1. OpenRouter 端点清单

### 1.1 存在的路由 [文档]

| 路由 | 状态 |
| --- | --- |
| `POST /api/v1/chat/completions` | 核心，完整文档化 |
| `POST /api/v1/responses` | OpenAI 兼容，**仅无状态** |
| `POST /api/v1/messages` | Anthropic 形态 |
| `POST /api/v1/embeddings` | 文档化 |
| `POST /api/v1/rerank` | 文档化 |
| `POST /api/v1/audio/speech`、`/api/v1/audio/transcriptions`、`/api/v1/images` | 在 OpenAPI 中 |
| `GET /api/v1/generation?id=` | 事后用量/成本 |
| `GET /api/v1/models`、`/api/v1/key`、`/api/v1/credits` | 目录与配额 |
| `POST /api/v1/presets/{slug}/{chat/completions,messages,responses}` | 预设变体 |

### 1.2 `/api/v1/completions` —— 活着，但在契约之外

- **[文档]（散文）**：Router Metadata 页把 `/api/v1/completions`（legacy text
  completions）列在"every public completion route"里；Streaming 页说 generation ID 头
  对 "chat completions, completions, responses, and messages" 都返回。
- **[文档]（缺失）**：**OpenAPI spec 里没有任何 `/completions` 路径**，
  也没有任何页面描述其请求/响应 schema。其形状只能从 overview 里的
  `NonChatChoice`（`{finish_reason, text, error?}`）推测。
- **[实测]**：`POST /api/v1/completions` 返回
  `401 {"error":{"message":"No cookie auth credentials found","code":401}}` ——
  **路由真实存在且受鉴权保护，不是 404**。

**教训**：**不要把 OpenAPI spec 当端点清单。** 它是结构来源，不是存在性来源。

### 1.3 `/api/v1/messages/count_tokens` —— 不存在（决定性）

- **[文档]（缺失）**：OpenAPI spec 无此路由，无任何页面提及。
- **[实测]**：`POST /api/v1/messages/count_tokens` → **404**
  `{"error":{"message":"Not Found","code":404}}`，
  而同级路由返回 401。**该网关先路由后鉴权，故 404 具有判定意义。**
- **[实况]**：这会打断 Anthropic Agent SDK / Claude Code 路径——
  [claude-agent-sdk-python#789](https://github.com/anthropics/claude-agent-sdk-python/issues/789)
  显示用户会收到 `400 No endpoints available that support Anthropic's context
  management features (context-management-2025-06-27)`。

> **与主报告 §3.2.2 的衔接**：Anthropic 官方已把 `count_tokens` 定义为**可选**
> （缺失时退化为字符估算）。所以对 **Claude Code** 这个 404 不致断链；
> 但对**主动调用该路由的 SDK** 会失败。

### 1.4 GA 与 beta

- 文档**没有**给 `/chat/completions`、`/responses`、`/messages` 本身贴 GA/beta 标签。
- **`/messages` 在 OpenAPI 里被标 `x-speakeasy-ignore: true`** ——
  意味着它**被排除在生成的 SDK 之外**，支持层级低于 `/chat/completions`。
- 文档里的 "Beta" 标签针对的是**功能**（Files API、Containers），不是这三条路由。
- **[实测]** `https://openrouter.ai/api/beta/chat/completions` → 404，
  已无遗留 `/api/beta/*` 面。

---

## 2. Chat Completions 与 OpenAI 的保真度

### 2.1 模型命名：目录变体 vs 路由变体 [文档]

| 类别 | 后缀 | 是否在 `/api/v1/models` 里 |
| --- | --- | --- |
| **目录变体** | `:free`（活跃）、`:batch`（活跃）、`:thinking`（**已废弃**）、`:extended`（**已废弃**，"No model currently offers it"） | **在** |
| **路由变体** | `:nitro`、`:floor`、`:exacto`、`:online` | **永远不在**，但对任何模型 id 合法 |

原文：*"`GET /api/v1/models` is a catalog of models and catalog variants. It is not an
exhaustive list of every model string a request can use. `openai/gpt-5.2:nitro` is a
valid request `model` even though no entry with that `id` exists."*

**[实测]**：454 个条目，只有 `batch`(71) 与 `free`(21)；
**没有任何路由变体** —— 与文档完全一致。条目含 `canonical_slug`、
`supported_parameters`，以及 `reasoning:{mandatory, default_enabled,
supported_efforts, default_effort}`。

**陷阱**：给一个模型加它没有的**目录**后缀，**不会回退到基础模型**——
单模型查询 404，endpoints 查询返回 200 但 `"endpoints": []`，推理失败。

### 2.2 `provider` 路由偏好对象 [文档]

字段：`order`、`allow_fallbacks`（默认 true）、`require_parameters`（默认 false）、
`data_collection`（allow/deny）、`zdr`、`enforce_distillable_text`、`only`/`ignore`、
`quantizations`、`sort`（`by`/`partition`，可按价格/吞吐/延迟）、
`preferred_min_throughput`/`preferred_max_latency`（p50/p75/p90/p99）、`max_price`。

`require_parameters: true` 把路由限制到支持**全部**请求参数的 provider。
为 false 时未列参数被静默忽略，但对 `tools`/`response_format`/`verbosity`
已存在**软偏好**：有 provider 支持就只路由到它们；**全都不支持时参数被忽略，
且该模型不会被移出候选**。

### 2.3 响应体：`id` / `model` / `provider` / `system_fingerprint`

| 字段 | 结论 |
| --- | --- |
| `id` | **[文档]** 真实格式是 **`gen-…`**；OpenAPI 的 `ChatResult` 示例仍写 `chatcmpl-123`，**是过期占位符**。**不要按 `chatcmpl-` 做模式匹配。** |
| `model` | **[文档]** 回显的是**实际回答的模型**，不是你发的字符串。要查"我请求了什么"得读 `openrouter_metadata.requested`（文档原文："May differ from the provider/model that actually served the request"）。 |
| `provider` | **[文档] 不可靠**：它**不在**文档化的响应类型里，也不在 OpenAPI 的 `ChatResult`/`ChatChoice`/`ChatStreamChunk` 中；**只出现在错误样例里**。 |
| `X-Provider-Name` | **[实测]** 真实存在（出现在 CORS expose 列表），但**未文档化**。路由后的 provider 走这个头，不走 body。 |
| `system_fingerprint` | **[文档矛盾]** overview 说 `optional`（"Only present if the provider supports it"），OpenAPI 把它列在 `required` 且类型 `string\|null`。**安全读法：可能缺失。** |
| `service_tier` | **[文档] 位置随 skin 变**：Chat/Responses 在**顶层**（"matching OpenAI's native format"），Messages 在 **`usage` 里**（"matching Anthropic's native format"）。取值也不同：Chat/Responses 是 `"default"`，Messages 是 `"standard"`。 |

### 2.4 `usage` 形状 [文档]

```ts
usage: {
  prompt_tokens, completion_tokens, total_tokens,
  prompt_tokens_details?: { cached_tokens, cache_write_tokens?, audio_tokens?, video_tokens? },
  completion_tokens_details?: { reasoning_tokens?, audio_tokens?, image_tokens?,
                                accepted_prediction_tokens?, rejected_prediction_tokens? },
  cost?: number,                 // 以 credits 计
  is_byok?: boolean,
  cost_details?: { upstream_inference_cost?, upstream_inference_prompt_cost,
                   upstream_inference_completions_cost, server_tool_cost? },
  server_tool_use?: { web_search_requests? },
  server_tool_use_details?: { tool_calls_requested, tool_calls_executed }
}
```

`cost`/`is_byok`/`cost_details` **确实在 Chat Completions 的 `usage` 里**。
`server_tool_cost` 文档描述为"Matches the billed checkpoint and settlement amounts exactly"。

**[文档] 陷阱**：走 `GET /api/v1/generation` 取同样数字时，
*"`upstream_inference_cost` is only available for BYOK requests.
For all other requests it will be 0 or null."*

### 2.5 响应头

- **[文档] 重要偏离**：*"**Successful inference responses do not include
  `X-RateLimit-*` headers.**"* 只有当 **OpenRouter 自身**因平台限额返回 429 时，
  错误响应才带 `X-RateLimit-Limit`/`-Remaining`/`-Reset`；当所有尝试的 provider
  都给了重试提示时，还会带 `Retry-After`。
  **OpenAI 是每个成功响应都带。** 要查配额只能 `GET /api/v1/key`。
- **[实测]** CORS expose 列表里确实没有 `x-ratelimit-*`，与文档一致。
- `X-Generation-Id`：**[文档]** 对所有端点返回，配合 `GET /api/v1/generation` 使用。

### 2.6 事后查询端点 `GET /api/v1/generation?id=` [文档]

返回 `data` 含 `id`、`model`、`provider_name`、`router`、`finish_reason`、
`native_finish_reason`、`streamed`、`cancelled`、`latency`、`generation_time`、
`total_cost`、`upstream_inference_cost`、`cache_discount`、`is_byok`、
`tokens_prompt`、`tokens_completion`、`native_tokens_*`、`num_search_results`、
`service_tier`、`data_region`、`external_user`、`session_id`、`preset_id` 等。

**[文档] 命名碰撞陷阱**：在该记录里 **`"usage": 0.0015` 是金额**，
**不是** token 用量对象——而 chat 响应里的 `usage` **是**对象。
**同一个 key、不同类型、不同端点。**

### 2.7 流式 [文档]

- **存在保活**：*"The SSE stream will occasionally contain a 'comment' payload,
  which you should ignore."*
  但**字面量 `:OPENROUTER PROCESSING` 未在任何抓取到的页面出现** → **未确定**。
- **最终 usage chunk 是无条件的**，且这是最尖锐的流式偏离：
  > *"When streaming, usage is returned exactly once in the final chunk before the
  > `[DONE]` message. **Unlike OpenAI's spec, this chunk contains a non-empty
  > `choices` array**: a choice with a content-free delta that repeats the
  > `finish_reason` of the stream."*

  OpenAI 只在设了 `stream_options.include_usage` 时才发，且 `choices: []`。
- `data: [DONE]`：**[文档]** OpenAPI 上标了 `x-speakeasy-sse-sentinel: '[DONE]'`，
  **`/chat/completions` 与 `/messages` 都有**（注意：上游 Anthropic 并不发 `[DONE]`）。
- **流内错误**——HTTP 200 已提交，错误搭在 chunk 里：

```text
data: {"id":"cmpl-abc123","object":"chat.completion.chunk","created":1234567890,
 "model":"openai/gpt-4o","provider":"openai",
 "error":{"code":429,"message":"Rate limit exceeded"},
 "choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}
```

- **按 API 分的错误行为**：*"OpenAI Chat Completions API: Returns `ErrorResponse`
  directly if no chunks were processed, or includes error information in the response
  if some chunks were processed"*；*"OpenAI Responses API: May transform certain error
  codes (like `context_length_exceeded`) into a successful response with
  `finish_reason: "length"` instead of treating them as errors."*
- **非流式下上游中途失败也不是 HTTP 错误**——错误嵌在 choice 里：

```json
{ "choices": [{ "message": {"role":"assistant","content":"partial output..."},
  "finish_reason": "error",
  "error": { "code": 502, "message": "Provider disconnected mid-stream",
             "metadata": { "error_type": "provider_unavailable" } } }] }
```

- **finish_reason 归一化**：归一为 `tool_calls | stop | length | content_filter | error`；
  上游原值保留在 **`native_finish_reason`**。
- **`debug` 选项**：可回显发给上游的真实请求体；有 fallback 时**每个尝试过的
  provider 各发一个 debug chunk**。文档明确"不要用于生产"。

### 2.8 会被丢弃/无法转发的参数 [文档]

> *"**Non-standard parameters** — If the chosen model doesn't support a request
> parameter (such as `logit_bias` in non-OpenAI models, or `top_k` for OpenAI),
> then the parameter is **ignored**. The rest are forwarded to the underlying model API."*

所以 `logit_bias`、`logprobs`、`top_logprobs`、`top_k`、`min_p`、
`repetition_penalty`、`top_a`、`frequency_penalty`、`seed`、`structured_outputs`、
`prediction`、`service_tier` 等在模型/provider 不支持时
**在传输层被接受、在语义上被静默丢弃**。唯一机制是路由级 `require_parameters`——
**没有文档化的按请求"严格模式"**。**HTTP 200 + 参数被忽略是正常失败形态。**

`ChatRequest` 在 OpenAPI 里是显式白名单（`cache_control`、`debug`、
`frequency_penalty`、`image_config`、`logit_bias`、`logprobs`、
`max_completion_tokens`、`max_tokens`（"deprecated, use `max_completion_tokens`.
Note: some providers enforce a minimum of 16"）、`messages`、`metadata`
（≤16 对、key ≤64、value ≤512）、`min_p`、`modalities`、`model`、`models`、
`parallel_tool_calls`、`plugins`、`prediction`、`presence_penalty`、
`prompt_cache_key`、`prompt_cache_options`、`provider`、`reasoning`、
`reasoning_effort`、`repetition_penalty`、`response_format`、`route`、`seed`、
`service_tier`、`session_id`、`stop`（≤4）、`stop_server_tools_when`、`stream`、
`stream_options`、`temperature`、`tool_choice`、`tools`、`top_a`、`top_k`、
`top_logprobs`、`top_p`、`trace`、`user`）。

注意 **`session_id` 兼作粘性路由键**（*"routing all requests in the session to the
same provider to maximize prompt cache hits"*），可被 `x-session-id` 头覆盖。

### 2.9 已退休/已改名的字段

| 字段 | 状态 |
| --- | --- |
| `transforms` | **[文档] 已从 OpenAPI 与参数文档中彻底消失。** 功能由 `plugins:[{id:"context-compression"}]` 承担。**老客户端发它会怎样，文档没说** → **未确定**（不一定是错误） |
| `route` | **[文档] 已废弃**，是 `provider.sort.partition` 的兼容别名（`"fallback"`→`"model"`，`"sort"`→`"none"`） |
| `include_reasoning` | **[文档]** 是 `reasoning.exclude` 的**废弃别名** |
| `reasoning_effort` | **[文档]** 是顶层简写 |

### 2.10 `reasoning` 统一对象 [文档 + 文档矛盾]

文档化的成员：`effort`（`max|xhigh|high|medium|low|minimal|none`）、
`max_tokens`（Anthropic 式预算）、`exclude`（仍然推理、隐藏输出、
**仍然计费且仍占用 `max_tokens`**）、`enabled`。
文档给出 effort 百分比（max/xhigh ~95%、high 80%、medium 50%、low 20%、minimal 10%），
且 OpenRouter **会在两套词汇之间双向翻译**（只支持 `effort` 的模型用
`max_tokens` 值推断 effort 档，反之亦然）。
每模型能力通过 `GET /api/v1/models` 的
`reasoning:{supported_efforts, default_effort, default_enabled, supports_max_tokens,
mandatory}` 暴露——**[实测] 确实存在**。
`reasoning_details` 保留加密/摘要推理以便多轮回放。

**[文档矛盾]** OpenAPI 的 `ChatRequest.reasoning` schema **只列了 `effort` 与
`summary`** —— **没有 `max_tokens`、`exclude`、`enabled`**。

### 2.11 其他请求面

- **`plugins`**：`{id, …}` 数组。OpenAPI union 里的 id：
  `auto-router`、`auto-beta-router`、`moderation`、`web`（**已废弃**，
  改用 `openrouter:web_search` 服务端工具）、`web-fetch`、`file-parser`、
  `response-healing`、`context-compression`、`pareto-router`、`fusion`、
  `switchyard-router`。
  **文档化区别**：plugins 启用后**恰好运行一次**；**服务端工具**由模型调用 0–N 次。
- **`web_search_options`**：*"Configures native web search options for models and
  providers that support web-connected answers."*
- **provider 专有透传**：*"OpenRouter will also transmit some provider-specific
  parameters, such as `safe_prompt` for Mistral or `raw_mode` for Hyperbolic
  directly to the respective providers if specified."* —— **显式开放式通道**，
  额外 JSON key 不一定被拒。
- **[文档] 不注入默认值**：*"When a sampling parameter is absent from your request,
  OpenRouter omits it upstream rather than substituting a hardcoded value, so the
  provider applies its own default. The 'Default' listed for each parameter below is
  the conventional value, not one OpenRouter injects. **Explicitly sending it
  (e.g. `temperature: 1.0`) is still forwarded and may differ from omitting it
  (for example, it can affect provider-side cache keys).**"*

### 2.12 错误信封与状态码 [文档]

```ts
type ErrorResponse = { error: { code: number; message: string; metadata?: Record<string, unknown> } };
```

1. **HTTP 状态只在"推理前"问题（非法请求、余额不足）上镜像 `error.code`。**
   否则 **HTTP 是 200**，失败在 body 或 SSE 事件里 —— 与 OpenAI 契约相反。
2. `code` 是**数字**（`502`），不是字符串（OpenAI 是 `string | null`）。
3. 增加了非 OpenAI 状态码：`402`（余额不足）、`408`、`413`、`422`、
   `502`（provider 不可用/响应非法）、`524`（边缘超时）、**`529` Overloaded**。
4. **provider 错误被包装**，原始细节移到 `error.metadata`：
   `error_type`（有类型、稳定）、`provider_code`（*"omitted on 500s"*）、
   `provider_name`、`raw`。
   **500 类错误上 `error.message` 被换成通用串且省略 `provider_code`**，以免泄漏上游细节。
5. `error_type` 文档称为*"the stable field"*，跨三个 skin 一致，含完整映射表
   （`rate_limit_exceeded`、`provider_overloaded`、`provider_unavailable`、
   `invalid_request`、`invalid_prompt`、`not_found`、`precondition_failed`、
   `payload_too_large`、`unprocessable`、`content_policy_violation`、`refusal`、
   `invalid_image`、`image_too_large`、`image_too_small`、`unsupported_image_format`、
   `image_not_found`、`image_download_failed`、`server`、`timeout`、`unmapped`……）。
6. **按 skin 的有损重映射**：Responses 把许多内部类型塌缩成
   `server_error`/`invalid_prompt`；Messages 塌缩成 `api_error`/`invalid_request_error`。
   为恢复真实原因，OpenRouter 在 **Responses skin 加顶层 `error_type`**，
   在 **Anthropic skin 加非标准的 `error.error_type`**（在 error 对象内部）。
7. **限流类错误被转成成功**：`context_length_exceeded`、`max_tokens_exceeded`、
   `token_limit_exceeded`、`string_too_long` → **成功响应 + finish reason `length`**。
8. **策略错误**：`content_policy_violation` 与 `refusal` **都是 HTTP 403**
   （*"regardless of the provider's own wire status"*）。
   而模型**自己产出**的 refusal **不是错误**，保持原生成功形状
   （Chat：`message.refusal` + `finish_reason: "content_filter"`；
   Responses：`status: "completed"` + `refusal` content part）。
9. **可选路由遥测**：`X-OpenRouter-Metadata: enabled`（旧名
   `X-OpenRouter-Experimental-Metadata`）会在 body 里加 `openrouter_metadata`；
   流式时落在 `[DONE]` 前的最后一个 chunk，Messages 则落在 `message_stop`。

---

## 3. `/api/v1/messages` 的保真度

### 3.1 接受的 Anthropic 特性 [文档]

| 特性 | 接受 | 备注 |
| --- | --- | --- |
| `system` 为字符串**或**块数组 | ✅ | `anyOf: string \| AnthropicTextBlockParam[]`，故 system 块上的 `cache_control` 可表达 |
| `cache_control` | ✅ | **请求级**（`AnthropicCacheControlDirective`）**以及**每块/每工具 |
| `thinking` | ✅ | `{type: enabled, budget_tokens, …}`，另含 adaptive/display/block_binding 变体 |
| `tool_choice` | ✅ | `auto`/`any`/`none`/`tool` |
| `tools` | ✅ | 含 Anthropic 服务端工具：`bash_20250124`、`text_editor_20250124`、带 `allowed_domains`/`excluded_domains` 的 web search |
| `context_management` | ✅ | `edits[]` 含 `clear_at_least`、`clear_tool_inputs`、`exclude_tools`、`keep`、`trigger` |
| `max_tokens`/`temperature`/`top_p`/`top_k`/`stop_sequences`/`metadata.user_id` | ✅ | |
| `output_config`/`safeguards`/`speed`/`service_tier` | ✅ | 扩展面 |
| `anthropic-version` 头 | ❌ 非必需 | 未声明为操作参数 |
| **`anthropic-beta` 头** | **未确定** | 文档化的 beta 通道是**另一个头名**：`x-anthropic-beta`，**仅**在 Provider Routing 页为 **Anthropic 模型**记录，含两个具名值（`interleaved-thinking-2025-05-14`、`structured-outputs-2025-11-13`，可逗号组合）。**裸 `anthropic-beta` 是否被尊重未文档化。** |
| `count_tokens` | ❌ | 路由 404（§1.3） |

### 3.2 OpenRouter 在 Messages body 上的私有扩展 [文档]

`models`、`provider`（同一个 ProviderPreferences **对象**）、`plugins`、`route`、
`session_id`、`user`、`trace`、`stop_server_tools_when`、`fallbacks`。
其中 `fallbacks`：*"Handled by OpenRouter multi-model routing rather than Anthropic
server-side fallbacks; cannot be combined with `models`. Each entry accepts only
`model`. Maximum of 3 entries."*

### 3.3 响应形状的两处偏离

- **`usage` 里没有 `cost`。** 它带 `input_tokens`、`output_tokens`、
  `output_tokens_details`、`cache_creation`、`cache_creation_input_tokens`、
  `cache_read_input_tokens`、`server_tool_use`、`inference_geo`、`service_tier`——
  **只有 `service_tier` 与 `inference_geo` 是新增**。
  `cost`/`is_byok`/`cost_details` **只存在于 Chat 与 Responses skin**。
  想在 Anthropic skin 上要成本，只能 `X-Generation-Id` + `GET /api/v1/generation`。
- **`service_tier` 坐在 `usage` 里**，且取值是 `"standard"` 而非 `"default"`。

### 3.4 流式事件名 [文档]

OpenAPI 的 `MessagesStreamEvents` union 就是 Anthropic 的那一套：
`message_start`、`message_delta`、`message_stop`、`content_block_start`、
`content_block_delta`、`content_block_stop`、`ping`、`error`。
`message_delta` 带 `usage`。
**一处怪异**：OpenAPI 把流的哨兵标为 `[DONE]`
（`x-speakeasy-sse-sentinel: '[DONE]'`），**而上游 Anthropic 不发 `[DONE]`**。

### 3.5 错误 skin —— 已实测确认

```json
{ "type": "error",
  "error": { "type": "authentication_error", "message": "...", "error_type": "authentication" },
  "request_id": null }
```

**[实测]** 无鉴权 `POST /api/v1/messages` 返回的正是这个，HTTP 401。
`request_id` 在路由前是 `null`，路由后是 `gen-…` ID（文档化）。

### 3.6 文档化的接入路径

Anthropic Agent SDK 指南设 `ANTHROPIC_BASE_URL="https://openrouter.ai/api"`、
`ANTHROPIC_AUTH_TOKEN=$OPENROUTER_API_KEY`、`ANTHROPIC_API_KEY=""`。
由于该 SDK 还会调 `count_tokens` 并发送 Anthropic beta 特性，
**这条路径在实践中只能部分工作**——见缺失的路由与
[claude-agent-sdk-python#789](https://github.com/anthropics/claude-agent-sdk-python/issues/789)
里的 context-management 400。

---

## 4. Responses API 支持

**[文档]** `POST https://openrouter.ai/api/v1/responses`，
"designed to be a drop-in replacement for OpenAI's Responses API"。
文档化的子集由一条硬限制定义：

> **Stateless Only** — This API is **stateless** — each request is independent and
> no conversation state is persisted between requests. You must include the full
> conversation history in each request. Requests that set `store: true` or a
> non-null `previous_response_id` are **rejected with a `400` error**.

错误体是 `{"error":{"code":"invalid_prompt","message":"…"},"metadata":null}` ——
**这里的 `code` 是字符串**（Responses 风格），与 Chat Completions 的数字不同，
且多一个顶层 `metadata`。

**未确定**：其余 Responses 字段哪些转发、哪些丢弃，文档没有枚举。

> **与主报告 §3.1 的衔接**：Codex 在 HTTP 上**恒发 `store:false`**
> 且**不使用 `previous_response_id`**，所以这条"仅无状态"限制
> **恰好不阻碍 Codex**。这是一个"上游限制与客户端行为意外互补"的正面案例。

---

## 5. LiteLLM

**[文档]** 暴露的形状：`/chat/completions` 与 `/v1/chat/completions`、
`/completions`、`/embeddings`、`/responses`、`/v1/messages`、
`/v1/messages/count_tokens`，另有 files、batches、audio、fine-tuning、assistants、
vector stores、`/v1/models`、`/health`。
`/v1/management/*` 是**独立** API，返回 RFC 9457 `application/problem+json`。

### 5.1 `/v1/messages` 默认是翻译器，不是透传

> *"When a deployment's provider has no native Anthropic Messages support, LiteLLM
> translates each `/v1/messages` request into the provider's own API: `openai/`
> deployments go through the OpenAI Responses API and everything else goes through
> `/v1/chat/completions`. That translation only keeps what the target API can
> express: **`cache_control` blocks are dropped**, `thinking` is mapped to the
> provider's own reasoning parameter, and **other Anthropic-only request details are
> approximated or lost**."*

**原生透传是 opt-in**（v1.92.0+）：`model_info.supported_endpoints: ["/v1/messages"]`。
此后 Anthropic body 原样转发 *"apart from `cache_control`"*——
默认每个 `cache_control` 被削成 `{"type":"ephemeral"}`（**剥掉 `ttl` 成员**，
因为 Claude Code 会发 `{"type":"ephemeral","ttl":"1h"}` 而严格实现会拒），
要保留得再设 **`model_info.cache_control_ttl: true`**。
LiteLLM 默认 `anthropic-version: 2023-06-01` 并**转发 `anthropic-beta` 头**
（调用方发的和它自己加的都会转）。
有原生 Anthropic 支持的 provider（`anthropic/`、`bedrock/`、`vertex_ai/`……）
**总是原生转发并忽略该 opt-in**。

### 5.2 桥接路径的文档化损失

- `system` 为**列表**时：文本块用 `\n` 连接，**非文本块被忽略**。
- **`stop_sequences` 与 `top_k` 被 "Dropped silently"**。
- `thinking.budget_tokens` 被分桶成 effort 字符串（≥10000 `high`、≥5000 `medium`、
  ≥2000 `low`、<2000 `minimal`），`summary` **总是被强制成 `"detailed"`**。
- `thinking.type != "enabled"` 时**根本不发 `reasoning`**。
- `tool_result`/`tool_use` 块被**提升**出 messages，变成顶层
  `function_call_output`/`function_call` 条目。
- 响应侧 `type:"message"`、`role:"assistant"`、`stop_sequence:null` 是**硬编码**的；
  `usage` 只带 `input_tokens`/`output_tokens`（该路径上无缓存字段）。

### 5.3 `/v1/responses`

`openai/` 前缀的 deployment 原生；**`custom_openai/` 被桥接到
`/v1/chat/completions`**（*"Responses-only request fields are approximated or dropped"*），
除非用 `model_info.supported_endpoints: ["/v1/responses"]` opt-in（v1.102.0+）。
`previous_response_id` 在原生路径上按原样转发（**无状态后端会 400，且该错误原样返回**），
在桥接路径上从 spend logs 解析（需 `store_prompts_in_spend_logs: true`）。

### 5.4 统一 `usage` 与流式

> *"LiteLLM returns the OpenAI compatible usage object across all providers:
> `{prompt_tokens, completion_tokens, total_tokens}`."*

流式遵循 **OpenAI 契约**：只在 `stream_options={"include_usage": True}` 时，
且 *"an additional chunk will be streamed before the `data: [DONE]` message...
the `choices` field will always be an empty array."*
代理可用 `general_settings.always_include_stream_usage: true` 强制。
**与 OpenRouter 的"恒发且 `choices` 非空"恰好相反。**

### 5.5 模型别名与 `drop_params`

- `model_list[].model_name` 是客户端调用的名字，`litellm_params.model` 是发给上游的。
  文档原文：*"Response body `model` often `my-chat-model` — Often restamped to match
  the client; upstream id stays in config."*
  头 `x-litellm-model-group` 是客户端可见的组名，`x-litellm-model-id` 是 deployment 行。
- **`drop_params` 默认值与 OpenRouter 相反**：
  *"By default, LiteLLM raises an exception if you send a parameter to a model that
  doesn't support it."* 设 `drop_params: true` 才丢弃，或用
  `additional_drop_params: [...]`（支持 JSONPath 式嵌套，如 `tools[*].input_examples`）。

### 5.6 注入的头

OpenAI 兼容的 `x-ratelimit-limit/remaining-requests|tokens` 与
`x-ratelimit-reset-requests|tokens`（从后端标准化，**provider 没发则为 `None`**）；
`x-litellm-response-duration-ms`、`-overhead-duration-ms`；
`x-litellm-attempted-retries`、`-attempted-fallbacks`、`-max-fallbacks`；
`x-litellm-response-cost` 及各分量 `-cost-input`、`-cost-output`、`-cost-cache-read`、
`-cost-cache-creation`、`-cost-reasoning`、`-cost-tool-usage`；
`x-litellm-key-spend`；`x-litellm-call-id`、`-model-id`、`-model-api-base`、
`-version`、`-model-group`。
**provider 的头会被重新发出并加 `llm_provider-` 前缀**
（如 `llm_provider-x-ratelimit-limit-requests`）。
**分量成本头只出现在非流式响应上。**

### 5.7 错误

*"Every failed request through the LiteLLM AI Gateway returns an OpenAI-compatible
JSON error body"* —— `{error: {message, type, param, code}}`，
但文档明确警告：**"`error.code` is a string, not an integer"**（`"429"`），
另有非标准的 `error.provider_specific_fields`。

**三个文档化的非 OpenAI 例外**：
未处理内部异常 → `{"error":{"message":"Internal server error","type":"internal_server_error"}}`；
未知路由 → **`{"detail":"Not Found"}`**（框架形状）；
`/v1/management/*` → RFC 9457 problem+json。

### 5.8 `count_tokens`

**存在**，但只覆盖文档化的 provider 子集："Anthropic, Vertex AI (Claude),
Bedrock (Claude), Gemini"，*"Auto-routes to provider-specific token counting APIs"*，
返回 `{"input_tokens": 14}`。

**[实况]** [BerriAI/litellm#15006](https://github.com/BerriAI/litellm/issues/15006)：
该路由曾**只有代理管理员可达**（`is_llm_api_route()` 只允许 `["/v1/messages"]`），
普通用户会收到 "Only proxy admin can be used to generate, delete, update info for
new keys/users/teams."。**已 Closed（#15034 修复）**，故属历史而非现状。

---

## 6. Portkey

**[文档]** 三种一等格式面向所有 provider：
*"Use OpenAI's Chat Completions, Responses API, or Anthropic's Messages format —
Portkey translates between them all"* —— `POST /v1/chat/completions`、
`POST /v1/responses`、`POST /v1/messages`。另有 `/v1/messages/count_tokens`、
`/v1/completions`、`/v1/embeddings`、images/audio/files/batches/fine-tuning/assistants、
`/v1/prompts/{promptId}/completions`。
`/v1/responses` 声称 **"fully Open Responses compliant"**，并扩展到
*"every provider and model in Portkey's catalog — including Anthropic, Gemini,
Bedrock, and 60+ other providers that don't natively support it"*。

### 6.1 显著偏离

- **模型命名是 `@provider/model`**，不是 `author/model`：
  `@anthropic-provider/claude-sonnet-4-5-20250514`、`@openai-provider/gpt-4.1`。
  provider 也可由 `x-portkey-provider` 给。
- **`provider` 在 Portkey 是请求体字段，且是字符串。**
  文档把 `x-portkey-provider / provider` 描述为
  *"Specifies the provider you're using (e.g. `openai`, `anthropic`, `vertex-ai`)"*。
  **这与 OpenRouter 的 `provider`（路由偏好对象）正面冲突。**
  为一个网关写的 body 被另一个**误读**，而两边文档都没提到对方。
- **`x-portkey-strict-open-ai-compliance`**：*"By default, all the responses sent
  back from Portkey are compliant with the OpenAI specification."*
  provider 特有字段（文档举例：Perplexity 的字段）会被**剥掉**，
  除非传 `x-portkey-strict-open-ai-compliance: false`。
  **注意反转**：SDK 默认 `false`，HTTP 默认"合规=true"。
  该头也是**看到原生 thinking 块的必要条件**——每个 thinking 示例都把它设为 `false`。
- **Anthropic Messages 保真度**。两种文档化模式：
  *"**Native providers** — Requests pass through directly. All Anthropic-specific
  features work (`thinking`, `cache_control`, `top_k`, etc.)"* 与适配器路径。
  **适配器路径的文档化损失**：
  **`thinking` — "Silently dropped on adapter providers"**；
  **`cache_control` — "stripped during message transformation"**；
  还有 *"`container`, `mcp_servers`, `service_tier`, `anthropic_beta`"*
  被映射成 Chat Completions 的等价物。
  以及 *"Provider-specific parameters (e.g. Gemini's `safety_settings`, Bedrock
  guardrail configs) **cannot be passed through the Messages adapter**."*
  **即 `anthropic_beta` 明确无法在翻译中存活。**
- **一个 beta 头会改变适配目标**：默认 `/v1/messages` → 非 Anthropic provider 走
  Chat Completions；`x-portkey-beta: use-responses-api-2026-07-30` 把变换目标
  改成 Responses API。**因此"哪些参数能存活"取决于一个 beta 头。**
- **`output_config` 是 Portkey 扩展**，不是 Anthropic 原生：
  `output_config.effort` 是跨 provider 的推理控制，
  且 *"Only `json_schema` is supported — `json_object` is not available via the
  adapter."*

### 6.2 头

**请求**：`x-portkey-api-key`、`x-portkey-provider`、`x-portkey-config`、
`x-portkey-virtual-key`、`x-portkey-trace-id`、`x-portkey-metadata`、
`x-portkey-strict-open-ai-compliance`、`x-portkey-beta`、`x-portkey-anthropic-beta`、
`x-portkey-custom-host`、`x-portkey-forward-headers`、
`x-portkey-cache-force-refresh`/`-cache-namespace`、`x-portkey-request-timeout`、
`x-portkey-retry-attempt-count`、`x-portkey-nitro-mode`、`x-portkey-debug`。
**响应**：`x-portkey-trace-id`、`x-portkey-cache-status`。
`x-portkey-nitro-mode` 会把 body **原样不翻译地**转发，
因此 *"the provider **must** be specified through headers, not in the request body"*，
且任何需要读 payload 的网关功能都不可用。

### 6.3 流式 usage 偏离 [文档 changelog]

> *"**OpenAI**: Streaming chat completions now default to
> `stream_options.include_usage: true`, so usage is reported on the stream unless a
> custom host is configured."*

即**网关替客户端改了它没要求改的请求体**——这是**第三种**行为，
与 OpenAI（opt-in）、OpenRouter（恒发、`choices` 非空）、LiteLLM（opt-in、
`choices:[]`）都不同。

### 6.4 流中途 Anthropic 过载

文档解释：SSE 流内的 Anthropic `overloaded_error`
*"By default, the gateway treats this as a successful (status 200) response and
streams the error directly to the client, which means retry, fallback, and circuit
breaker strategies do not activate"*。
可按集成开启 "Catch Overloaded Error on Stream"：读第一个 chunk 并转成 HTTP **529**。

### 6.5 错误

**文档只给状态码**：`408`、`412`（预算耗尽）、`429`、
**`446`**（护栏检查失败，请求被拒）、
**`246`**（护栏检查失败，**请求成功**），
外加 *"Provider-specific error codes are passed through by Portkey."*
**`446`/`246` 是真正的协议扩展：`246` 是一个"成功"的 HTTP 状态却在表示策略失败**——
只检查非 2xx 的客户端会漏掉它。

**错误体形状基本未确定。** 网关上 OpenAI 式信封**在我读到的页面里没有文档化**。
唯一找到的网关信封是权限失败（AB03）：
`{"success": false, "data": {"message": "…", "errorCode": "AB03"}}`。
另外，被护栏拦截的 **MCP 工具调用**返回 JSON-RPC 错误
`{"jsonrpc":"2.0","id":1,"error":{"code":-32446,…}}`。
**故 Portkey 至少有三种互不兼容的错误信封，拿哪一种取决于失败路径。**
上游 provider 普通失败的信封 → **未确定**。

---

## 7. Cloudflare AI Gateway

### 7.1 两个面（搞清这点是主要难点）

**(a) `gateway.ai.cloudflare.com/v1/{account_id}/{gateway_id}/…`** ——
provider 原生路径，基本透传，网关功能叠加在上面：

| 路径 | 备注 |
| --- | --- |
| `/{provider}/chat/completions`、`/{provider}/responses` | 如 OpenAI 的 `/openai/responses` |
| `/anthropic/v1/messages` | *"The Anthropic endpoint exposes the same `/v1/messages` API that Claude Code expects"*；需 `anthropic-version: 2023-06-01` 与 `x-api-key`（或 `cf-aig-authorization` + BYOK/Unified Billing） |
| `/compat/chat/completions` | OpenAI 兼容；**"Deprecated for single-model calls"** 但 **"Required for dynamic routing"**（`model: "dynamic/{route}"`） |
| 裸 `/{account_id}/{gateway_id}` | **Universal Endpoint**，标 **"(Deprecated)"**，用非 OpenAI 信封 `{provider, endpoint, authorization, query, config}`，**整个 provider payload 塞在 `query` 里**，fallback 是这种信封的数组 |

**(b) `api.cloudflare.com/client/v4/accounts/{account_id}/ai/…`** ——
**REST API**，新集成推荐，且是**唯一有统一协议形状端点**的面：

| 端点 | 格式 |
| --- | --- |
| `POST /ai/run` | 含 `model` + `input` 的信封（全模态） |
| `POST /ai/v1/chat/completions` | OpenAI chat completions |
| `POST /ai/v1/responses` | OpenAI Responses API |
| `POST /ai/v1/messages` | Anthropic Messages API |

*"The `/ai/v1/messages` endpoint strictly uses Anthropic's API schema and supports
routing to Anthropic and other third-party models."*

**模型命名随面不同**：compat/统一层是 `{provider}/{model}`
（`openai/gpt-5.2`、`anthropic/claude-4-5-sonnet`、
`google-ai-studio/gemini-2.5-flash`、`workers-ai/@cf/…`、`dynamic/…`），
provider 原生路径则是 provider **自己的、不带前缀的**名字。

### 7.2 显著偏离

- **鉴权随面不同**：`gateway.ai.cloudflare.com` 用 `cf-aig-authorization`；
  `api.cloudflare.com` 用标准 `Authorization: Bearer <CF API token>`。
  REST API 需要 **Workers AI → Read**，
  *"A token that holds only an `AI Gateway` permission returns `401` with error
  code `10000`."*
- **错误信封**。[实测] 每个发往 `api.cloudflare.com/.../ai/v1/*` 的无鉴权 POST
  都返回 **Cloudflare v4 信封**，**不是 OpenAI 的**：

```json
{"result":null,"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"messages":[]}
```

  **重要保留意见**：Cloudflare 在路由**之前**校验鉴权，所以这个探测对**每条**路径
  都返回 401——包括 `/ai/v1/messages/count_tokens`、`/ai/v1/embeddings`、
  `/ai/v1/completions`、`/ai/v1/rerank`。
  **探测对路径存在性不可判定**，而这四条都无文档。
  因此本档案**不断言它们存在**。
- **按请求的控制头**：`cf-aig-skip-cache`、`cf-aig-cache-ttl`、`cf-aig-cache-key`、
  `cf-aig-collect-log`、`cf-aig-collect-log-payload`、`cf-aig-request-timeout`、
  `cf-aig-max-attempts`（≤5）、`cf-aig-retry-delay`（≤60000 ms）、
  `cf-aig-backoff`（`constant|linear|exponential`）、`cf-aig-metadata`（JSON 字符串）。
  另见 `cf-aig-byok-alias`、`cf-aig-custom-cost`、`cf-aig-zdr`、`cf-aig-dlp`、
  `cf-aig-gateway-id`（Workers AI 必需）。
  **旧名 `cf-skip-cache`/`cf-cache-ttl` 已改名为 `cf-aig-*`。**
- **通过响应头暴露 fallback**：`cf-aig-step: 0` = 主模型服务；
  `1`、`2`、… = 哪个 fallback 成功。
  与 OpenRouter/LiteLLM 不同，**没有文档化的成本或路由元数据 body 字段**。
- **缓存默认**：*"caching is based on **exact match** of the entire request.
  Any difference in the body — including messages, tools, or model parameters —
  will result in a separate cache entry."* 按请求用 `cf-aig-cache-key` opt-in；
  TTL 最小 60 秒、最大一个月。**与"透明代理"有实质差别。**
- **返回的 provider 响应形状**：`/compat` 端点 OpenAI 兼容，`/ai/v1/*` 声称
  OpenAI/Anthropic 兼容，但文档**没有**说会跨 provider 归一 `usage`、
  加 `cost` 字段或追加流式 usage chunk → **未确定**。
- **Anthropic 保真度**：文档断言 schema 兼容，并展示了 Anthropic SDK 打
  `/ai/v1/messages`、Claude Code 打 `/anthropic/v1/messages`。
  **未文档化 / 未确定**：两条路径上是否支持 `cache_control`、`thinking`、
  `anthropic-beta`、`count_tokens`——在我抓取到的 Cloudflare AI Gateway 文档里
  **`anthropic-beta` 这个词根本没出现**。

---

## 8. 跨网关对照

| 行为 | OpenAI（原生） | Anthropic（原生） | OpenRouter | LiteLLM | Portkey | Cloudflare AI GW |
| --- | --- | --- | --- | --- | --- | --- |
| 统一 `/v1/messages` | – | 原生 | **翻译，非透传** | **默认翻译**，opt-in 原生透传 | **默认翻译**，原生 provider 除外 | `/ai/v1/messages`（统一）+ `/anthropic/v1/messages`（原生） |
| 统一 `/v1/responses` | 原生 | – | ✅ 但**仅无状态**（`store`/`previous_response_id` → 400） | ✅ `openai/` 原生，其余桥接 | ✅ 声称 Open-Responses 合规，全 provider | ✅ `/ai/v1/responses` |
| `/v1/completions`（legacy） | 已废弃 | – | **活着但只在散文文档里，OpenAPI 里没有** | ✅ | ✅ | 无文档 |
| `/messages/count_tokens` | – | 原生 | ❌ **404（实测）** | ✅ 限部分 provider | ✅ | 无文档 |
| 不支持参数 | 400 | 400 | **静默忽略**（`require_parameters` 只能绕开） | **默认抛异常**；`drop_params` 才忽略 | `strict_open_ai_compliance` 控制响应附加字段；适配器静默丢弃 Anthropic 专有参数 | 无文档 |
| `usage` 附加字段 | – | – | **Chat/Responses 有 `cost`/`is_byok`/`cost_details`；Anthropic skin 没有** | 只有 `{prompt,completion,total}`；成本走头/`_hidden_params` | 文档称成本在 `usage`；thinking 需 `strict…: false` | 无文档 |
| 流式 usage chunk | opt-in `include_usage`；`choices: []` | `message_delta.usage` | **恒发，且 `choices` 非空**（文档承认偏离） | opt-in（`include_usage` 或 `always_include_stream_usage`）；`choices: []` | OpenAI 流式**默认帮你开** `include_usage` | 无文档 |
| 成功响应带 `x-ratelimit-*` | 是 | 是（`anthropic-ratelimit-*`） | **否**——仅平台 429 | 是（标准化；provider 静默则为 `None`） | 无文档 | 无文档 |
| 响应 `model` 回显 | 请求的模型 | 请求的模型 | **实际回答的模型** | **重写成客户端别名** | 按发送的 `@provider/model` | 原样 |
| 错误 `code` 类型 | 字符串 | 字符串 | **数字** | **字符串** | 仅状态码；另有 `{success,data}` / JSON-RPC | Cloudflare v4 信封 `{result,success,errors}` |
| HTTP 状态能否代表失败 | 非 2xx | 非 2xx | **HTTP 200 + body/SSE 内错误**（一旦开始路由） | 非 2xx（OpenAI 兼容） | **`246` 是"成功"状态却在表示护栏失败** | 非 2xx |
| 事后用量查询 | – | – | ✅ `GET /v1/generation?id=`（+ `X-Generation-Id`） | spend logs | logs | logs |

---

## 9. 未确定的开放问题

- OpenRouter 的 `/api/v1/messages` 是否尊重标准 **`anthropic-beta`** 头
  （只有 `x-anthropic-beta`、且只在 routing 文档页记录）。
- OpenRouter 是否会在 body 里返回 **`provider`**，以及 **`X-Provider-Name`**
  是否有保证（未文档化，但在实测响应里 CORS 可见）。
- OpenRouter 对**未知请求字段**（如已退休的 `transforms`）的处理：丢弃、转发还是 400。
- **`:online`** 是否正式废弃（模型变体状态表在抓取时于 routing 变体边界被截断；
  另有独立的 Online Variant 页面存在）。
- OpenRouter **`/api/v1/completions`** 的请求/响应 schema；只有 `NonChatChoice` 提示。
- OpenRouter **Responses API 各字段**哪些丢弃、哪些转发
  （只有 `store`/`previous_response_id` 被文档化为拒绝）。
- OpenRouter 流上**保活注释的确切字符串**（`:OPENROUTER PROCESSING` 被广泛引用，
  但**不在**我抓取到的文档里）。
- LiteLLM `/v1/messages` 在**桥接**路径上的**流式事件保真度**——
  映射页详细记录了非流式响应映射，但没有枚举发出的 SSE 事件名。
- Portkey 上游 provider 普通失败的**网关错误信封**；
  只有 `{success,data}`（AB03）与 JSON-RPC（MCP 护栏）被文档化。
- Portkey `/v1/rerank` 与 `/v1/embeddings` 的归一化——不在任何我抓取的页面里。
- Cloudflare `/ai/v1/*` 上的 **`usage` 归一化、成本字段、流式 usage chunk**。
- Cloudflare `/ai/v1/messages` 对 **`cache_control`、`thinking`、`anthropic-beta`、
  `count_tokens`** 的支持——均无文档；路由探测因先鉴权而不可判定。
- Cloudflare `/ai/v1/embeddings`、`/ai/v1/completions`、`/ai/v1/rerank`——
  无文档且无凭据时不可验证。

---

## 10. 直接可用的结论：对 OpenRouter 不能假设的 17 件事

1. **不要假设 HTTP 状态代表成功。** 一旦开始路由，provider 失败以 **HTTP 200**
   到达，错误在 body 里（Chat：`choice.error` + `finish_reason:"error"`；
   流式：带顶层 `error` 与 `finish_reason:"error"` 的 chunk）。**要看 payload，不是状态行。**
2. **不要假设 `error.code` 是字符串，也不要假设它等于 HTTP 状态。**
   它是**数字**，且只在推理前失败时两者一致。跨 skin 稳定的字段是 `error_type`
   （或 `error.metadata.error_type`），而 Responses 与 Anthropic skin 都会丢细节，
   **所以这个额外字段才是必须读的。**
3. **不要假设成功响应有 `x-ratelimit-*`。** 没有。要查配额用 `GET /api/v1/key`。
4. **不要假设 `stream_options.include_usage` 控制 usage chunk。**
   OpenRouter **总是**在 `[DONE]` 前的最后一个 chunk 追加 usage，
   **且该 chunk 的 `choices` 非空**——文档明确承认这是对 OpenAI 的偏离。
   按 `choices: []` 契约写的客户端会解析错。
5. **不要假设 `id` 形如 `chatcmpl-…`。** 它是 `gen-…`（OpenAPI 示例是过期的）。
6. **不要假设响应 `model` 等于你请求的模型。** 它是实际回答的模型；
   别名、`models` fallback、auto-router、`~…-latest` 都会破坏相等性。
   要查"我请求了什么"读 `openrouter_metadata.requested`。
7. **不要假设 body 里有 `provider`，也不要假设有 `system_fingerprint`。**
   两者都不在文档化的响应类型里；`provider` 只出现在错误样例，
   真实路由信息走未文档化的 `X-Provider-Name` 头。
8. **不要假设不支持的参数会报错。** 它被**静默忽略**——没有严格模式；
   `provider.require_parameters` 只改变路由。
9. **不要假设 OpenRouter 注入默认值。** 缺失参数被省略、由 provider 用自己的默认值；
   文档里的 "Default" 是描述性的，不是它发送的。
   **显式发 `temperature: 1.0` 与省略它不等价**（可能影响 provider 侧缓存键）。
10. **不要假设 `transforms` 还存在。** 它已从 OpenAPI 与参数文档消失；
    消息变换现在是 `plugins:[{id:"context-compression"}]`，
    `route` 只是 `provider.sort.partition` 的废弃别名。
11. **不要假设 Anthropic skin 有 `cost` 字段。** `/api/v1/messages` 的 `usage`
    没有 `cost`/`is_byok`/`cost_details`，且 `service_tier` 嵌在 `usage` 里、
    取值是 `"standard"` 而非 `"default"`。
    要用 `X-Generation-Id` + `GET /api/v1/generation`——**其中 `usage` 是金额，不是 token 对象**。
12. **不要假设 `usage` 语义跨端点一致**，也不要在非 BYOK 时依赖生成记录里的
    `upstream_inference_cost`（否则是 0 或 null）。
13. **不要假设 `/api/v1/messages` 是忠实的 Anthropic 透传。** 它是一个**翻译** skin，
    有自己的扩展（`models`/`provider`/`plugins`/`fallbacks`/`session_id`）、
    非标准的 `error.error_type`，以及——关键的——**完全没有 `count_tokens` 路由（404）**。
14. **不要假设对 Anthropic 有效的提示缓存或 beta 头在这里有效。**
    文档化的 beta 通道是 `x-anthropic-beta`（不是 `anthropic-beta`），
    只对 Anthropic 模型、且值列表很短；用户报告 context-management beta 会 400
    "No endpoints available that support Anthropic's context management features"
    （[claude-agent-sdk-python#789](https://github.com/anthropics/claude-agent-sdk-python/issues/789)）。
15. **不要假设 Responses API 是有状态的。** `store:true` 与非空
    `previous_response_id` 会被 **400 拒绝**；每轮都要重发完整历史。
16. **不要假设文档与 OpenAPI spec 一致，也不要把 spec 当端点清单**：
    `/api/v1/completions` 活着且只在散文里；`/messages` 被 `x-speakeasy-ignore`
    排除出 SDK；`system_fingerprint` 散文说可选、spec 说必需；
    `reasoning.max_tokens`/`exclude`/`enabled` 指南里有、spec 里没有。
    **行为信散文，结构信 spec，两个都要读。**
17. **不要假设一个网关的请求字段在另一个网关里意思相同。**
    OpenRouter 的 `provider` 是**对象**；Portkey 的 `provider` 是**字符串**。
    把一个网关的 body 发给另一个，**意思会静默改变**。

---

## 11. 主要来源

**OpenRouter**：`llms.txt`、`openapi/openapi.yaml`、`api_reference/overview.md`、
`parameters.md`、`streaming.md`、`errors-and-debugging.md`、`limits.md`、
`api_reference/responses/{overview,basic-usage,reasoning,tool-calling,web-search,error-handling}.md`、
`guides/routing/{provider-selection,model-fallbacks,model-variants/*,routers/auto-router}.md`、
`guides/best-practices/{reasoning-tokens,prompt-caching}.md`、
`guides/features/{message-transforms,plugins,plugins/web-search,response-healing,router-metadata,presets,service-tiers,server-tools/web-search,structured-outputs,guardrails,zero-completion-insurance,files-api,containers}.md`、
`guides/overview/models.md`、`guides/community/{openai-sdk,anthropic-agent-sdk,vercel-ai-sdk}.md`、
`api/api-reference/{chat/create-a-chat-completion,anthropic-messages/create-a-message,responses/create-a-response,embeddings/submit-an-embedding-request,generations/get-stored-prompt-completion-and-error-content-for-a-generation,rerank/submit-a-rerank-request}.md`、
`cookbook/administration/usage-accounting.md`、`faq.md`。

**LiteLLM**：`llms.txt`、`llms-full.txt`、`docs/anthropic_unified`、
`anthropic_unified/native_passthrough`、`anthropic_unified/messages_to_responses_mapping`、
`docs/response_api`、`proxy/response_headers`、`completion/usage`、`completion/token_usage`、
`completion/drop_params`、`completion/prompt_caching`、`proxy/error_reference`、
`anthropic_count_tokens`、`count_tokens`、`proxy/health`。

**Portkey**：`llms.txt`、`llms-full.txt`（内含 Universal API、Chat Completions、
Messages API、Responses API、Strict OpenAI Compliance、Beta Features、Headers、
Error codes、Guardrails capabilities、AB03 help）。

**Cloudflare**：`ai-gateway/llms.txt`、`ai-gateway/llms-full.txt`，
以及 `usage/`、`usage/chat-completion/`、`usage/universal/`、`usage/rest-api/`、
`usage/providers/{anthropic,openai}/`、`configuration/{request-handling,authentication,fallbacks,custom-providers}/`、
`features/{dynamic-routing,caching,rate-limiting}/`、`observability/{custom-metadata,costs}/`、
`reference/troubleshooting/`、`integrations/coding-agents/claude-code/` 的分页 Markdown。

**社区报告**：[anthropics/claude-agent-sdk-python#789](https://github.com/anthropics/claude-agent-sdk-python/issues/789)、
[OpenRouterTeam/ai-sdk-provider#341](https://github.com/OpenRouterTeam/ai-sdk-provider/issues/341)、
[BerriAI/litellm#15006](https://github.com/BerriAI/litellm/issues/15006)。

**实测探测**：`POST /api/v1/{chat/completions,responses,messages,messages/count_tokens,embeddings,completions,rerank}`、
`POST /api/beta/chat/completions`、`GET /api/v1/generation`、`GET /api/v1/models`、
响应头 dump；`POST api.cloudflare.com/client/v4/accounts/{id}/ai/{v1/chat/completions,v1/responses,v1/messages,v1/messages/count_tokens,v1/embeddings,v1/completions,v1/rerank,run}`（**不可判定**）。
