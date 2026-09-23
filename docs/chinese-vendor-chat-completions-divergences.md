# Chinese LLM vendors vs. OpenAI Chat Completions — field-by-field divergence dossier

**Target under study:** `POST /v1/chat/completions` (OpenAI Chat Completions).
**Method:** primary documentation fetched directly from each vendor (official API references and
OpenAPI specs where published), plus GitHub issue trackers where a divergence is *observed* rather
than *documented*. Every claim below carries a link to the page it came from.
**Labelling convention:** `[DOC]` = stated in vendor documentation; `[OBS]` = community/GitHub-reported
behaviour; `[NOT DOC]` = the vendor's published reference does not mention the field at all (which is
**not** proof it is rejected — absence of documentation, not documented rejection).

**A caution on reading "supported" lists:** several vendors publish an explicit allow-list of accepted
parameters (Zhipu, SiliconFlow, Alibaba's compat page). For those vendors, a field that is absent from
the allow-list should be treated as *at best undefined*; the strongest documented statement is Alibaba's
and MiniMax's, which say out loud that parameters are **ignored** rather than rejected.

---

## 1. Baseline — what OpenAI Chat Completions actually guarantees

The divergences below are only meaningful against a fixed reference. OpenAI's shape, as assumed
throughout:

| Aspect | OpenAI reference behaviour |
|---|---|
| Endpoint | `POST https://api.openai.com/v1/chat/completions` |
| Auth | `Authorization: Bearer <key>` |
| `stop` | Stops **before** the matched sequence; the sequence is **not** returned |
| Penalties | `presence_penalty` / `frequency_penalty`, range `[-2.0, 2.0]` |
| Length | `max_tokens` (deprecated) / `max_completion_tokens` (mutually exclusive) |
| Roles | `system`, `developer`, `user`, `assistant`, `tool` |
| `tool_choice` | `"none"`, `"auto"`, `"required"`, or `{"type":"function","function":{"name":...}}` |
| `finish_reason` | `stop`, `length`, `tool_calls`, `content_filter`, `function_call` |
| `system_fingerprint` | String on every response |
| `usage.prompt_tokens_details.cached_tokens` | Present where caching applies |
| Streaming | `choices[].delta`, `finish_reason: null` on non-final chunks, `data: [DONE]` terminator |
| Errors | `{"error": {"message": ..., "type": ..., "code": ..., "param": ...}}` |

---

## 2. Endpoint, base URL and authentication — the first place everything breaks

| Vendor | Base URL | Path | `/v1`? | Auth | Extra required headers |
|---|---|---|---|---|---|
| **OpenAI** | `https://api.openai.com` | `/v1/chat/completions` | yes | `Authorization: Bearer` | – |
| **DeepSeek** | `https://api.deepseek.com` (beta: `https://api.deepseek.com/beta`) | `/chat/completions` | **no `/v1`** | `Authorization: Bearer <TOKEN>` | `Content-Type: application/json` |
| **Alibaba Qwen / Model Studio** | `https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` (also legacy `https://dashscope.aliyuncs.com/compatible-mode/v1`, `https://dashscope-us.aliyuncs.com/compatible-mode/v1`, `https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1`, `https://{WorkspaceId}.ap-northeast-1.maas.aliyuncs.com/compatible-mode/v1`) | `/chat/completions` | yes, **plus a mandatory `/compatible-mode` path segment** | `Authorization: Bearer <DASHSCOPE key>` | – (but see `X-DashScope-Async` below) |
| **Moonshot / Kimi** | `https://api.moonshot.cn/v1` (CN) — CN and `platform.kimi.ai` (intl) accounts/keys are **fully isolated**, mixing returns 401 | `/chat/completions` | yes | `Authorization: Bearer $MOONSHOT_API_KEY` | – |
| **Zhipu GLM** | `https://open.bigmodel.cn/api/` | `/paas/v4/chat/completions` | **no `/v1`; version is `/v4`** | `Authorization: Bearer <API key>` | – |
| **MiniMax** | `https://api.minimax.cn/v1` (OpenAI-compat) | `/chat/completions` | yes | `Authorization: Bearer` | – |
| **ByteDance Doubao / Volcengine Ark** | CN: `https://ark.cn-beijing.volces.com/api/v3`; intl (BytePlus ModelArk): `https://ark.ap-southeast.bytepluses.com/api/v3` | `/chat/completions` | **no `/v1`; version is `/v3`** | `Authorization: Bearer $ARK_API_KEY` | optional `X-Client-Request-Id` for log correlation |
| **SiliconFlow** | `https://api.siliconflow.com/v1` (`.../cn` for the CN site) | `/chat/completions` | yes | `Authorization: Bearer <key>` | – |
| **Tencent Hunyuan** | `https://api.hunyuan.cloud.tencent.com/v1` | `/chat/completions` | yes | `Authorization: Bearer $HUNYUAN_API_KEY` | – |
| **Baidu ERNIE / Qianfan** | Qianfan v2 OpenAI-compatible surface; request body reference published under the Qianfan API docs | `/v2/chat/completions` | **`/v2`** | `Authorization: Bearer` | – |
| **iFlytek Spark** | `https://spark-api-open.xf-yun.com/v1/chat/completions` (`base_url` for OpenAI SDK: `https://spark-api-open.xf-yun.com/v1/`) | `/chat/completions` | yes | `Authorization: Bearer <APIPassword>` — **the credential is the console "APIPassword", not an "API key", and differs per model version** | – |
| **StepFun** | `https://api.stepfun.com/v1`; Step Plan channel: `https://api.stepfun.com/step_plan/v1` | `/chat/completions` | yes | `Authorization: Bearer` | – |

Sources: [DeepSeek Chat Completions API](https://api-docs.deepseek.com/api/create-chat-completion) ·
[Alibaba OpenAI Chat 接口兼容](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope) ·
[Kimi API 概述](https://platform.kimi.com/docs/api/overview) ·
[Kimi Chat Completions API](https://platform.kimi.com/docs/api/chat) ·
[Zhipu 对话补全](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8) ·
[MiniMax OpenAI SDK](https://platform.minimaxi.com/docs/api-reference/text-openai-api) ·
[Ark 兼容 OpenAI SDK](https://www.volcengine.com/docs/82379/1330626) ·
[ModelArk Base URL and authentication](https://docs.byteplus.com/en/docs/ModelArk/1298459) ·
[SiliconFlow 创建对话请求（OpenAI）](https://docs.siliconflow.com/cn/api-reference/chat-completions/chat-completions) ·
[腾讯混元 OpenAI 兼容接口](https://cloud.tencent.com/document/product/1729/111007) ·
[讯飞星火 HTTP 调用文档](https://www.xfyun.cn/doc/spark/HTTP%E8%B0%83%E7%94%A8%E6%96%87%E6%A1%A3.html) ·
[StepFun Chat Completions API](https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create).

### Documented auth/endpoint traps

- **[DOC] Alibaba: the API key is region-bound.** Calling the Virginia base URL with a Beijing key returns
  HTTP 401 with `Incorrect API key provided` / code `invalid_api_key`. The docs explicitly say this means
  *key/endpoint region mismatch*, not an invalid key — a message that will send a client into an
  "invalid credentials" retry loop
  ([Alibaba compat page](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope)).
- **[DOC] Alibaba: `X-DashScope-Async: enable` can cause an immediate 429** on models that do not support
  async invocation (e.g. `qwen-image-3.0-pro`) — a *single* request returns 429. Clients that set this
  header globally will misread it as rate limiting
  ([Alibaba 错误码](https://help.aliyun.com/zh/model-studio/error-code)).
- **[DOC] Alibaba: Qwen-Audio does not support the OpenAI-compatible protocol at all** — DashScope-native
  only ([Alibaba compat page](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope)).
- **[DOC] Ark: the base URL carries the major version `v3`, not `v1`.** The Ark docs also warn that
  Coding Plan uses a *different* base URL, and using the wrong one causes extra billing
  ([ModelArk Base URL and authentication](https://docs.byteplus.com/en/docs/ModelArk/1298459)).
- **[DOC] Kimi:** keys are scoped to a platform. `platform.kimi.com` (CN) and `platform.kimi.ai` (intl)
  have separate accounts, balances and keys; mixing returns 401
  ([Kimi 常见错误码](https://platform.kimi.com/docs/api/errors)).

---

## 3. DeepSeek

Sources: [Chat Completions API](https://api-docs.deepseek.com/api/create-chat-completion) ·
[Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode) ·
[Tool Calls](https://api-docs.deepseek.com/guides/tool_calls) ·
[JSON Output](https://api-docs.deepseek.com/guides/json_mode) ·
[Error Codes](https://api-docs.deepseek.com/quick_start/error_codes) ·
[Rate Limit & Isolation](https://api-docs.deepseek.com/quick_start/rate_limit).

### 3.1 Request fields

| Field | Status | Detail |
|---|---|---|
| `model` | `[DOC]` | Enum documented as `deepseek-flash`, `deepseek-v4-pro`. |
| `messages` | `[DOC]` | Roles: **`system`, `user`, `assistant`, `tool` only**. |
| `thinking` | `[DOC]` **vendor-only** | `{"type": "enabled" \| "disabled"}`, **default `enabled`**. The docs state plainly that with the OpenAI SDK you must pass it in `extra_body`. |
| `reasoning_effort` | `[DOC]` | `none` \| `low` \| `high` \| `max`. Documented compatibility aliases: `minimal`→`low`, `medium`→`high`, `xhigh`→`high`, `ultra`→`max`. **`minimal`/`medium`/`xhigh`/`ultra` are accepted, not rejected** — a deliberate silent remap. |
| `max_tokens` | `[DOC]` | Range 1–393216. Defaults: 8K non-thinking, 64K thinking, 128K at `reasoning_effort="max"`. |
| `max_completion_tokens` | `[NOT DOC]` | Absent from the request schema. |
| `response_format` | `[DOC]` | **`text` and `json_object` only — no `json_schema`.** JSON Output additionally requires the word "json" in a system/user message. The JSON Output page carries a candid warning that the API "may occasionally return empty content". |
| `temperature` | `[DOC]` | ≤ 2. **"Has no effect in thinking mode."** |
| `top_p` | `[DOC]` | In thinking mode clamped to `0.95`–`1.0` (values below 0.95 silently become 0.95). **In non-thinking mode it is fixed at 1.0 and the value you pass is ignored.** |
| `presence_penalty` / `frequency_penalty` | `[DOC]` | **Not supported in thinking mode; explicitly stated to be silently ignored** ("setting these parameters will not trigger an error but will also have no effect"). |
| `stop` | `[DOC]` | `string` or `array`. |
| `stream` / `stream_options.include_usage` | `[DOC]` | Supported; see §3.5 for the unusual chunk layout. |
| `tools[].function.strict` | `[DOC]` | Supported as a **Beta** feature, default `false`, and **requires `base_url="https://api.deepseek.com/beta"`**. |
| `tool_choice` | `[DOC]` | `none` \| `auto` \| `required` \| named object. **`required` and named tool choices return HTTP 400 in thinking mode** ("Disable thinking mode first to use them"). |
| `logprobs` / `top_logprobs` | `[DOC]` | `top_logprobs` ≤ 20. |
| `n`, `seed`, `logit_bias` | `[NOT DOC]` | Not in the published request schema. |
| `parallel_tool_calls` | `[NOT DOC]` | Not in the published request schema. |

### 3.2 Message/content parts

- **`role: "developer"` is not a supported role** `[DOC]` — the schema enumerates only
  `system`/`user`/`assistant`/`tool`. **`[OBS]`** It is rejected in practice with a serde error naming the
  expected variants:
  `unknown variant 'developer', expected one of 'system', 'user', 'assistant', 'tool', 'latest_reminder'`
  ([opencode-go-cliproxyapi#5](https://github.com/massiveits/opencode-go-cliproxyapi/issues/5)).
- `role: "tool"` is supported, with `tool_call_id`.
- Multimodal user/tool content parts: `text`, `image_url`, `file`. **`image_url.detail` enum is
  `low`/`high`/`original`/`auto`** — `original` is a non-OpenAI value. The `file` part uses
  `file_data` + `filename`.
- **`reasoning_content` on request-side assistant messages**: `[DOC]` present, marked Beta, described as
  input for the CoT in the *last* assistant message when using Chat Prefix Completion together with
  `prefix: true`. **`[OBS]`** In thinking mode with tool calls it is effectively **required**, not
  optional — replaying an assistant message that carries `tool_calls` without its original
  `reasoning_content` produces
  `400 {"error":{"message":"The \"reasoning_content\" in the thinking mode must be passed back to the API","type":"invalid_request_error"}}`
  ([fennara-godot-ai#151](https://github.com/fennaraOfficial/fennara-godot-ai/issues/151)).

### 3.3 Response envelope

- `id`, `object` (`chat.completion`), `created`, `model`, `system_fingerprint` — all present and required
  `[DOC]`. `system_fingerprint` is a real value such as `fp_7a09fdf9c2`, not an empty string.
- `choices[].logprobs` is present, and its schema contains a **non-standard `reasoning_content` array
  alongside `content`** `[DOC]`.
- **`finish_reason` value set diverges**: documented values are
  `stop`, `length`, `content_filter`, `tool_calls`, **`insufficient_system_resource`**, **`aborted`** `[DOC]`.
  The last two do not exist in OpenAI.
- `usage` is *superset* of OpenAI: `prompt_tokens`, `completion_tokens`, `total_tokens`,
  `prompt_tokens_details.cached_tokens` **plus vendor-only** `prompt_cache_hit_tokens`,
  `prompt_cache_miss_tokens`, and `completion_tokens_details.reasoning_tokens` `[DOC]`.
  The docs define `prompt_tokens = prompt_cache_hit_tokens + prompt_cache_miss_tokens`.

### 3.4 Streaming

- `data: [DONE]` terminator: **present** `[DOC]`.
- **Usage arrives on the last content chunk, not in a separate usage-only chunk.** The docs are explicit:
  "no separate usage-only chunk is emitted: the statistics ride on the last content chunk, whose `choices`
  array always contains exactly one element that carries no new content and a non-null `finish_reason`."
  With `include_usage: true`, every chunk carries a `usage` field that is `null` except the last `[DOC]`.
- Non-final chunks set `finish_reason: null` and emit `delta.content` as an empty string on the first chunk
  `[DOC]` (sample stream shows `{"content": "", "role": "assistant"}`). The closing chunk has
  `"role": null`.
- **Keep-alive:** non-streaming requests receive continuous empty lines; **streaming requests receive SSE
  keep-alive comments `: keep-alive`**. The docs warn that if you parse HTTP yourself you must handle
  these. The server closes the connection if inference has not started within 10 minutes `[DOC]`
  ([Rate Limit & Isolation](https://api-docs.deepseek.com/quick_start/rate_limit)).
- `delta.reasoning_content` is the thinking field (same level as `content`), **not** `delta.reasoning` `[DOC]`.

### 3.5 Tool calling

- Arguments are returned as a **JSON string** in `function.arguments` `[DOC]`.
- **Tool-call ids look like `call_00_kw66qNnNto11bSfJVIdlV5Oo`** `[DOC]` (shown in the Thinking Mode sample) —
  longer and differently shaped than OpenAI's `call_...` but the same `call_` prefix.
- **In streaming, the tool-call delta carries `index`, `id`, `type` and `function.name` together** `[DOC]`;
  i.e. the id is not guaranteed to appear only on the first fragment per the published schema.
- `strict` mode imposes a **restricted JSON-Schema subset** `[DOC]`: only `object`, `string`, `number`,
  `integer`, `boolean`, `array`, `enum`, `anyOf` are supported. For every `object`, **all properties must
  be listed in `required` and `additionalProperties` must be `false`**. For strings, `pattern` and
  `format` (`email`, `hostname`, `ipv4`, `ipv6`, `uuid`) are supported but **`minLength` and `maxLength`
  are not**. Schemas outside the subset produce an error. Requires the `/beta` base URL.

### 3.6 Errors and rate limits

- Status codes documented: `400` invalid format, `401` auth, **`402` insufficient balance** (non-OpenAI
  status usage), **`422` invalid parameters**, `429` rate limit, `500`, `503` `[DOC]`.
  The documented remedy text for 429 is unusual: "we also advise users to temporarily switch to the APIs
  of alternative LLM service providers".
- **[NOT DOC]** The published error-code page is a status/description table only — it does **not** specify
  the JSON error envelope shape or any rate-limit response headers. Community reports quote
  `{"error":{"message": ..., "type": "invalid_request_error"}}` `[OBS]` (above), consistent with OpenAI but
  not documented by DeepSeek.
- Rate limiting is **concurrency-based**, not RPM/TPM `[DOC]`. `user_id` is used for per-user isolation
  (`deepseek-flash` 2500, `deepseek-v4-pro` 500 concurrency per `user_id`).

---

## 4. Alibaba Qwen / DashScope / 百炼 Model Studio

Sources: [OpenAI Chat 接口兼容](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope) ·
[Function Calling](https://help.aliyun.com/zh/model-studio/qwen-function-calling) ·
[错误码](https://help.aliyun.com/zh/model-studio/error-code).

### 4.1 Request fields

The compat page publishes an explicit allow-list titled "输入参数与 OpenAI 的接口参数对齐，当前已支持的参数如下".

| Field | Status | Detail |
|---|---|---|
| `messages` | `[DOC]` | Roles listed: **`system`, `user`, `assistant`**. Additional documented constraints: **only `messages[0]` may be `system`**; user and assistant **must alternate**; **the last element's role must be `user`**. |
| `n` | `[DOC]` | Range **1–4** (OpenAI allows up to 128), **only supported on `qwen-plus`**, and **fixed to 1 when `tools` is passed**. |
| `seed` | `[DOC]` | Supported, **unsigned 64-bit**. |
| `presence_penalty` | `[DOC]` | Supported, `[-2.0, 2.0]`, "**currently only on commercial Qwen models and open-source qwen1.5 and later**". |
| `frequency_penalty` | `[NOT DOC]` | **`frequency_penalty` does not appear in the compat parameter table at all** — only `presence_penalty` does. |
| `stop` | `[DOC]` **extended** | Accepts `string` or `array`; array elements may be **strings, `token_id`s, or arrays of `token_id`s** (e.g. `[[108386,103924],[35946,101243]]`). Mixing token_ids and strings is rejected. This is a significant superset of OpenAI. |
| `max_tokens` | `[DOC]` | Supported. |
| `max_completion_tokens` | `[NOT DOC]` | Not in the allow-list. |
| `stream_options.include_usage` | `[DOC]` | Supported. |
| `parallel_tool_calls` | `[DOC]` | Documented in the Function Calling guide (`parallel_tool_calls=True`) as the way to get *all* tool calls back instead of just one. |
| `tool_choice` | `[DOC]` | `auto` (default), `none`, named object, `required` — **but `required` is explicitly not supported by Qwen series**; see §4.5. |
| `logprobs` / `top_logprobs` | `[NOT DOC]` | Not in the allow-list; the documented response examples always show `"logprobs": null`. |
| `logit_bias` | `[NOT DOC]` | Not in the allow-list. |
| `enable_search`, `search_options.forced_search` | `[DOC]` **vendor-only** | `extra_body={"enable_search": True}`; `search_options.forced_search` forces search. |
| `enable_thinking` | `[DOC]` **vendor-only** | Passed via `extra_body`. **Non-streaming calls must set it to `false`** — `parameter.enable_thinking must be set to false for non-streaming calls`. |
| `thinking_budget` | `[DOC]` **vendor-only** | Positive integer bounded by the model's max CoT length. |
| `reasoning_effort` | `[DOC]` | `"none"` disables thinking. Notably the docs describe the **default as `xhigh`** for tool-calling-with-thinking. |
| `preserve_thinking` | `[DOC]` **vendor-only** | Defaults to on; controls whether prior-turn thinking is kept. |
| `incremental_output` | `[DOC]` **vendor-only** | Must be `true` whenever `enable_thinking` is `true`; errors otherwise. |

### 4.2 Message/content parts

- **`role: "developer"` is not documented** `[NOT DOC]`; the compat page enumerates only
  `system`/`user`/`assistant`, and the Function Calling guide adds `tool` for tool results.
- `role: "tool"` is used in the function-calling flow `[DOC]`.
- Multimodal content parts: the error page documents that `content` arrays must contain strings or
  objects of the form `{"type":"text",...}`, `{"type":"image_url",...}` etc.; numbers, booleans, nested
  arrays or unknown `type` values trigger `InternalError.Algo.InvalidParameter` `[DOC]`.
- **`reasoning_content` on the request side is expected when continuing a thinking + tool-call
  conversation** `[DOC]`: "the thinking content, reply and tool calls of the previous assistant message
  must be added to the history together".

### 4.3 Response envelope

- **`system_fingerprint` is returned as an empty string `""`.** The compat page states outright:
  "模型运行时使用的配置版本，当前暂时不支持，返回为空字符串" (not currently supported; returns an empty
  string) `[DOC]`. Clients that treat `system_fingerprint` as a cache key or a non-empty identifier will
  break.
- **`finish_reason` documented value set is only three cases**: `null` while generating, `stop` on a stop
  condition, `length` on length. `tool_calls` is exercised in the tool-calling guide but is **not listed in
  the compat response table** `[DOC]`.
- `usage` documented as only `prompt_tokens`, `completion_tokens`, `total_tokens` `[DOC]`. The
  **`prompt_tokens_details.cached_tokens` field is not documented on the compat page**.
- `logprobs` is present in responses but documented as always `null` in the samples `[DOC]`.

### 4.4 Streaming

- `data: [DONE]` terminator: **present** `[DOC]`.
- **Usage arrives in a separate, final chunk with `"choices": []`** `[DOC]`. The documented stream shows:
  `{"id":"chatcmpl-...","choices":[{"delta":{"content":""},"finish_reason":"stop","index":0,"logprobs":null}],...,"usage":null}`
  followed by
  `{"id":"chatcmpl-...","choices":[],"created":...,"model":"qwen3.8-max","object":"chat.completion.chunk","system_fingerprint":null,"usage":{"completion_tokens":16,"prompt_tokens":22,"total_tokens":38}}`.
  This is a **different layout from DeepSeek**, which attaches usage to the last content chunk and
  emits no usage-only chunk. Clients written against one and pointed at the other will drop or
  double-count usage.
- `delta.content` is sent as an empty string on the opening chunk; `logprobs` is always emitted as `null`.
- Streaming chunks carry `"system_fingerprint": null` in some documented examples but `""` in others;
  both appear in the same page `[DOC]`.
- `delta.reasoning_content` is the documented thinking field `[DOC]`.

### 4.5 Tool calling

- Arguments are a JSON string in `function.arguments` `[DOC]`.
- **`tool_choice: "required"` is not supported by Qwen series** `[DOC]`. In non-thinking mode
  `required` "cannot guarantee" a tool call; **in thinking mode `required` and the object form are
  rejected outright** with
  `The tool_choice parameter does not support being set to required or object in thinking mode` `[DOC]`.
- **`[OBS]` Hard failure:** `tool_choice="required"` produced
  `400 {'code': 'invalid_parameter_error', 'param': None, 'message': 'tool_choice is one of the strings that should be ["none", "auto"]', 'type': 'invalid_request_error'}`
  ([pydantic-ai#1265](https://github.com/pydantic/pydantic-ai/issues/1265); also
  [nanobot#1927](https://github.com/HKUDS/nanobot/issues/1927), and a fix commit avoiding required tools in
  DashScope thinking in [qwen-code@c30de11](https://github.com/QwenLM/qwen-code/commit/c30de11fab222fce0f285cd4b0c55faea9eb9c08)).
- **`tools` cannot be combined with `stream=True`** in the documented compat surface ("tools 暂时无法与
  stream=True 同时使用") `[DOC]`.
- `parallel_tool_calls=True` is the documented mechanism for getting multiple tool calls in one response `[DOC]`.
- Qwen-Omni-Realtime does not support `tool_choice` or `parallel_tool_calls` `[DOC]`.

### 4.6 Errors

- **[DOC] The error envelope is OpenAI-shaped but the code vocabulary is not.** The Function Calling guide
  quotes `{'code': 'invalid_parameter_error', 'param': None, 'message': ..., 'type': 'invalid_request_error'}` —
  note `code` is a **string slug**, not a numeric string, and the payload is **flat** (no `error` wrapper)
  in that quoted form, unlike OpenAI's nested envelope.
- Documented error codes include non-OpenAI names: `InternalError.Algo.InvalidParameter`,
  `InvalidParameter.NotSupportEnableThinking`, `InternalError.Algo.InvalidParameter` for repeated identical
  tool calls in consecutive turns (a loop detector with no OpenAI equivalent), and `invalid_parameter_error`
  for "model service not activated".
- `429` variants are heavily namespaced: `Throttling.RateQuota`, `Throttling.AllocationQuota`,
  `Throttling.BurstRate`, `Throttling.Concurrency`, plus **billing-shaped 429s**: `CommodityNotPurchased`,
  `PrepaidBillOverdue`, `PostpaidBillOverdue`, `BudgetLimitExceeded` `[DOC]`. Clients that only retry on
  429 will retry on permanently-failing billing states.

---

## 5. Moonshot / Kimi

Sources: [Chat Completions API](https://platform.kimi.com/docs/api/chat) ·
[API 概述](https://platform.kimi.com/docs/api/overview) ·
[常见错误码](https://platform.kimi.com/docs/api/errors).

### 5.1 Request fields

Kimi publishes a full OpenAPI 3.1 spec on the Chat page. Supported: `model`, `messages`, `logprobs`,
`top_logprobs`, `prediction`, **`max_tokens` and `max_completion_tokens` (both)**, `response_format`,
`stop`, `stream`, `stream_options.include_usage`, `tools`, `prompt_cache_key`, `prompt_cache_options`,
`safety_identifier`, `tool_choice`, plus vendor `thinking` / `reasoning_effort` / `partial` `[DOC]`.

| Field | Status | Detail |
|---|---|---|
| `response_format` | `[DOC]` | **`text`, `json_object` and `json_schema`** — full Structured Output support. |
| `tool_choice` | `[DOC]` | `auto`, `none`, `required`, and named object. |
| `max_tokens` / `max_completion_tokens` | `[DOC]` | Both documented. |
| `thinking` | `[DOC]` **vendor-only** | `thinking.type`: `"enabled"` \| `"disabled"`; `thinking.keep`: `null` \| `"all"` (Preserved Thinking). For `kimi-k2.7-code`, `thinking` is always `enabled` and `keep` is fixed to `"all"`; **passing any other value errors**. Must be passed via `extra_body`. |
| `partial` | `[DOC]` **vendor-only** | Written **on the assistant message**, not top-level: `{"role":"assistant","content":"```python\n","partial":true}`. The overview page calls this out explicitly because it is easy to send as a top-level parameter by mistake. |
| `prompt_cache_key`, `prompt_cache_options.mode/ttl` | `[DOC]` **vendor-only** | Cache control. Docs note Chat Completions does **not** echo back the applied mode/ttl (unlike their Responses API). |
| `safety_identifier`, `prediction`, `logprobs`, `top_logprobs` | `[DOC]` | Supported. |
| `n`, `seed`, `logit_bias`, `presence_penalty`, `frequency_penalty` | `[NOT DOC]` | **Zero occurrences of `parallel_tool_calls`, `logit_bias`, `presence_penalty`, `frequency_penalty`, `seed` in the Chat Completions OpenAPI document.** |
| `parallel_tool_calls` | `[NOT DOC]` | Not in the spec. |

### 5.2 Message/content parts

- Roles: `system`, `user`, `assistant`, `tool` `[DOC]`. **`developer` is not documented** `[NOT DOC]`.
- Content parts: `text`, `image_url`, **`video_url`** (non-OpenAI) `[DOC]`. `image_url`/`video_url` accept
  **either an object `{"url": ...}` or a bare string** — a documented leniency OpenAI does not have.
  Supported reference forms: base64 data URLs and **`ms://<file_id>`** `[DOC]`.
- A Kimi-specific **`KimiK3DynamicToolMessage`**: a message with `role: "system"` that carries a `tools`
  array and **no `content`**, declaring tools available from that point in the conversation onward `[DOC]`.
  This has no OpenAI equivalent and will fail schema validation in strict OpenAI clients.

### 5.3 Response envelope

- `id`, `object`, `created`, `model`, `choices` — present `[DOC]`.
- **`system_fingerprint` does not appear anywhere in the Chat Completions OpenAPI document** `[NOT DOC]`.
- **`finish_reason` enum is `stop` | `length` | `tool_calls` | `null`** `[DOC]`. No
  `content_filter` in the chat schema.
- **`usage` contains a non-standard top-level `cached_tokens`** alongside
  `prompt_tokens`/`completion_tokens`/`total_tokens`, plus
  `prompt_tokens_details.cached_tokens` **and `prompt_tokens_details.cache_write_tokens`** `[DOC]`.
  The documented sample reads:
  `"usage":{"prompt_tokens":19,"completion_tokens":13,"total_tokens":32,"cached_tokens":12,"prompt_tokens_details":{"cached_tokens":12,"cache_write_tokens":0}}`.
  `cache_write_tokens` is not an OpenAI field.

### 5.4 Streaming

- `data: [DONE]` terminator: **present** `[DOC]`.
- Usage: with `stream_options.include_usage=true`, usage appears **in the last chunk before `[DONE]`** `[DOC]`.
  The docs also state that the full cache read/write breakdown appears **only** in the final chunk's `usage`.
- `delta.role` appears only in the first chunk; the closing chunk has an **empty `delta` object `{}` with
  `finish_reason: "stop"`** `[DOC]`.
- **`delta.reasoning_content`** is the thinking field `[DOC]`.

### 5.5 Tool calling

- Tool-call id, `type`, `function.name`, `function.arguments` (JSON string) `[DOC]`.
- `tools[].function.strict` is supported `[DOC]`.
- Streaming `delta.tool_calls[]` entries carry `id`, `type`, `function.name` and `function.arguments` in the
  published chunk schema; **the spec does not state that `id` is limited to the first fragment** `[DOC]`.
  No `index` field is documented on the streaming tool-call delta in the excerpt reviewed `[NOT DOC]`.

### 5.6 Errors

- Envelope is `{"error": {"type": ..., "message": ...}}`, with `code` **optional** and `message` the only
  required member `[DOC]`.
- Error `type` values documented: `content_filter`, `invalid_request_error`,
  `invalid_authentication_error`, `incorrect_api_key_error` (non-OpenAI), `permission_denied_error` `[DOC]`.
- Extra response headers carry a request id and a **`reqsigv1_<opaque-token>` response-signature header** —
  a Kimi-specific scheme `[DOC]`.

---

## 6. Zhipu GLM / 智谱

Sources: [对话补全 OpenAPI](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8) ·
[错误码](https://docs.bigmodel.cn/cn/api/api-code) ·
[Thinking mode](https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode).

### 6.1 Request fields

The published request schema is short enough to enumerate exhaustively: **`model`, `messages`, `stream`,
`thinking`, `reasoning_effort`, `do_sample`, `temperature`, `top_p`, `max_tokens`, `tool_stream`, `tools`,
`tool_choice`, `stop`, `response_format`, `request_id`, `user_id`** `[DOC]`.

Everything else is undocumented. Notable absences: **`n`, `seed`, `logit_bias`, `presence_penalty`,
`frequency_penalty`, `logprobs`, `top_logprobs`, `parallel_tool_calls`, `stream_options`,
`max_completion_tokens`, `user`** `[NOT DOC]`.

| Field | Status | Detail |
|---|---|---|
| `do_sample` | `[DOC]` **vendor-only** | Default `true`. When `false`, **`temperature` and `top_p` are ignored** and the model is greedy. No OpenAI equivalent. |
| `temperature` | `[DOC]` | **Range `[0.0, 1.0]`** — OpenAI allows up to 2. |
| `top_p` | `[DOC]` | **Range `[0.01, 1.0]`** — OpenAI allows 0. |
| `max_tokens` | `[DOC]` | Range 1–131072 depending on model. |
| `thinking` | `[DOC]` **vendor-only** | `ChatThinking` = `{type: enabled\|disabled, clear_thinking: bool}`. **`clear_thinking` defaults to `true`** and strips prior-turn `reasoning_content` from context; set it to `false` and pass history back verbatim to get Preserved Thinking — the docs warn that missing, truncated, rewritten or reordered history "will degrade or fail to take effect". |
| `reasoning_effort` | `[DOC]` | `max` (default), `xhigh`, `high`, `medium`, `low`, `minimal`, `none`. Documented **silent remaps**: for GLM-5.2, `none`/`minimal` abandon thinking, `low`/`medium` map to `high`, `xhigh` maps to `max`. GLM-5.3 and GLM-5.3-FLASH accept only `low`/`high`/`max`. |
| `stop` | `[DOC]` | **Array of strings only (`maxItems: 4`)** — a bare string is not in the schema. |
| `response_format` | `[DOC]` | **`text` and `json_object` only — no `json_schema`.** |
| `tool_stream` | `[DOC]` **vendor-only** | Boolean, default `false`; enables streaming function calls. Only on certain GLM series. |
| `request_id` | `[DOC]` **vendor-only** | Client-supplied request id, 6–64 chars. Replaces OpenAI's absence of such a field (OpenAI has `user`; Zhipu has both `request_id` and `user_id`). |
| `user_id` | `[DOC]` | 6–128 chars. This is the analogue of OpenAI's `user`. |

### 6.2 Message/content parts

- Roles: **`system`, `user`, `assistant`, `tool`** `[DOC]`. **`developer` is not documented** `[NOT DOC]`.
- Multimodal content parts (vision request branch): `image_url`, **`video_url`**, **`file`**, **`input_audio`** `[DOC]`.
- `tool_calls[].type` documented as supporting **`web_search`, `retrieval`, `function`, and `mcp`** `[DOC]`.
  `web_search`, `retrieval` and `mcp` are non-OpenAI tool types, including on the *response* side.
- `reasoning_content` is present on assistant messages and in streaming deltas `[DOC]`.

### 6.3 Response envelope

- `id`, `created`, `model`, `choices`, `usage` are documented. **`system_fingerprint` is not documented** `[NOT DOC]`.
- **`finish_reason` value set diverges substantially.** Enum documented as
  `stop`, `length`, `tool_calls`, **`sensitive`**, **`network_error`** `[DOC]`, with the prose description
  adding a sixth: **`model_context_window_exceeded`** `[DOC]`. `sensitive`, `network_error` and
  `model_context_window_exceeded` do not exist in OpenAI.
- Extra top-level response fields with no OpenAI equivalent: `web_search`, `content_filter`, `video_result` `[DOC]`.
- `usage` is a documented subset: `prompt_tokens`, `completion_tokens`, `total_tokens`,
  `prompt_tokens_details.cached_tokens`. **`completion_tokens_details.reasoning_tokens` is not documented** `[NOT DOC]`.
- **[DOC] Streaming failure signalling is moved into the body**: the error-code page states that when an SSE
  call fails mid-inference, the API does **not** return the business error code; instead the failure reason
  is returned **in `finish_reason`**. This is a documented mid-stream error model that differs from OpenAI's
  error-event approach.

### 6.4 Streaming

- `data: [DONE]` terminator: **explicitly documented** ("流式输出结束时会返回 `data: [DONE]` 消息") `[DOC]`.
- `finish_reason` appears only on the final chunk; the streaming enum is `stop`/`length`/`tool_calls`/`sensitive`/`network_error` `[DOC]`.
- `stream_options` / `include_usage` are **not documented** `[NOT DOC]` — the streaming `usage` object is
  documented as an inline property of the chunk instead.
- `delta.reasoning_content` is documented `[DOC]`.

### 6.5 Tool calling

- **`tool_choice` is the single sharpest divergence: the enum contains only `auto`** — "默认`auto`且仅支持`auto`"
  (defaults to `auto` and **only** `auto` is supported) `[DOC]`.
- **`[OBS]` Confirmed in the wild**: Z.AI (the Zhipu backend on OpenRouter) rejects **every** value except
  `auto` with HTTP 400 `{"error":{"message":"Tool choice must be auto","code":400,"metadata":{"provider_name":"Z.AI"}}}`.
  Both `"required"` and the named-function object form failed; `"auto"` succeeded and the tool was still
  called 5/5 times ([hindsight#4246](https://github.com/vectorize-io/hindsight/issues/4246)).
- `tools` supports up to 128 functions; `strict` appears under tool definition schemas but the exact
  JSON-Schema subset is not published on the Chat page `[NOT DOC]`.
- Tool-call arguments are JSON strings `[DOC]`.

### 6.6 Errors

- **The envelope is non-OpenAI.** Documented shape:
  `{"error":{"code":"1001","message":"Header 中未收到 Authentication 参数，无法进行身份验证"}}` `[DOC]`.
  There is **no `type` member**, and **`code` is a numeric string** from a vendor-specific table
  (e.g. `1000` auth failure, `1213` missing field, `1214` illegal field, `1261` prompt too long,
  `1301` content moderation, `1308` quota, `1313` fair-use throttle) `[DOC]`.
- **HTTP status is decoupled from the business code**: every response carries an outer HTTP status *and* an
  inner business code, and the docs say the business code is the more specific one `[DOC]`.
- Rate-limit signalling includes a vendor header `x-ratelimit-scope: global` on 429 responses (documented in
  the OpenAPI description for the managed-agent endpoints on the same host) `[DOC]`.

---

## 7. MiniMax

Source: [OpenAI SDK 接入](https://platform.minimaxi.com/docs/api-reference/text-openai-api).

### 7.1 Request fields

MiniMax documents a **short extra-parameter allow-list** for `MiniMax-M3` `[DOC]`:
`thinking`, `stream_options.include_usage`, `max_tokens`, `max_completion_tokens`, `temperature`,
`top_p`, `tools`, `reasoning_split`, `service_tier`.

| Field | Status | Detail |
|---|---|---|
| `thinking` | `[DOC]` **vendor-only** | `type` takes **`disabled` or `adaptive`** — note there is **no `enabled` value**; omitting `thinking` defaults to on, and `adaptive` simply means "explicitly keep it on". **For M2.x models thinking cannot be disabled at all** — passing `disabled` is accepted but ignored. |
| `reasoning_split` | `[DOC]` **vendor-only** | `true` splits thinking into **`reasoning_content` and `reasoning_details`**. `false` (default) leaves thinking **inline in `content` wrapped in `<think>...</think>` tags**. This is the most invasive default in this survey: with the OpenAI-compatible endpoint and no extra parameters, a MiniMax reasoning reply's `content` contains raw `<think>` markup. |
| `reasoning_details` | `[DOC]` | Structured thinking array; sample access is `message.reasoning_details[0]['text']`. |
| `service_tier` | `[DOC]` | Values **`standard` and `priority`** — not OpenAI's `auto`/`default`/`flex`. `priority` costs 1.5× and is a billing-affecting admission tier. |
| `temperature` | `[DOC]` | Range **[0, 2]**; "out of range returns an error". Recommended 1.0. |
| `top_p` | `[DOC]` | `[0,1]`; default 0.95 on M3, 0.9 on M2.x. |
| `max_tokens` / `max_completion_tokens` | `[DOC]` | Both documented; `max_completion_tokens` recommended for new integrations. |
| `presence_penalty`, `frequency_penalty`, `logit_bias`, "etc." | `[DOC]` **silently ignored** | Stated verbatim in a `<Warning>` block: "部分 OpenAI 参数（如 `presence_penalty`、`frequency_penalty`、`logit_bias` 等）会被忽略". Accepted, no error, no effect. |
| `n` | `[DOC]` | **"`n` 参数仅支持值为 1"** — only the value 1 is accepted. |
| `function_call` | `[DOC]` | **Deprecated**, use `tools`. |
| `logprobs`, `seed`, `tool_choice`, `parallel_tool_calls`, `response_format` | `[NOT DOC]` | Not in the published allow-list. |

### 7.2 Message/content parts

- **[DOC] `MiniMax-M3` accepts image and video content blocks** via `image_url` and `video_url`. `detail`
  takes **`low`, `default`, `high`** — `default` is a non-OpenAI value (OpenAI uses `auto`). An extra
  **`max_long_side_pixel`** controls the longest edge `[DOC]`.
- Video parameters: `fps` default 1, range 0.2–5. Limits: video URL/base64 ≤ 50 MB, image ≤ 10 MB, request
  body ≤ 64 MB. Larger video must go through the Files API and be referenced as **`mm_file://{file_id}`** —
  a MiniMax-specific URI scheme `[DOC]`.
- **Audio input is explicitly not supported** on the OpenAI-compatible path for M3 `[DOC]`.
- **Crucial multi-turn requirement** `[DOC]`: "In multi-turn Function Call conversations, the complete model
  return (the assistant message) must be added to the conversation history to preserve chain-of-thought
  continuity." For **native** OpenAI-format calls the `content` of M3/M2.x contains `<think>` tags and
  **must be preserved in full**; with `reasoning_split=True` the thinking goes to `reasoning_details` and
  must likewise be preserved.

### 7.3 Response envelope and streaming

- **[NOT DOC]** for most of the envelope. The OpenAI-compat page documents `choices[0].message.content`,
  `message.reasoning_details`, and the aliasing of local variables (`reasoning_details`, `content_text`) in
  its streaming example. It does **not** publish a response schema, a `finish_reason` value set, a
  `system_fingerprint` guarantee, or a `usage` field list `[NOT DOC]`.
- `stream_options.include_usage: true` returns token usage in-stream `[DOC]`.
- **[OBS]** MiniMax directs users to file issues at
  [github.com/MiniMax-AI/MiniMax-M2/issues](https://github.com/MiniMax-AI/MiniMax-M2/issues) for OpenAI-compat problems, but the
  published page does not enumerate SSE framing details `[NOT DOC]`.

### 7.4 Tool calling

- `tools` is the documented mechanism; `function_call` is deprecated `[DOC]`.
- Arguments are JSON strings `[DOC]` (the docs instruct `json.loads(...['function']['arguments'])`).
- **[NOT DOC]** `tool_choice`, `parallel_tool_calls`, tool-call id format, and whether a tool call can
  appear without an id.

### 7.5 Errors

- **[DOC] `temperature` outside [0, 2] returns an error**; `n ≠ 1` is not accepted. Everything else in the
  published warning list is *ignored*, not rejected.
- **[NOT DOC]** The page does not publish the error envelope or rate-limit headers.

---

## 8. ByteDance Doubao / Volcengine Ark (ModelArk)

Sources: [兼容 OpenAI SDK](https://www.volcengine.com/docs/82379/1330626) ·
[对话(Chat) API](https://www.volcengine.com/docs/82379/1298454) ·
[Chat API (ModelArk)](https://docs.byteplus.com/en/docs/ModelArk/1494384) ·
[API 兼容性升级公告](https://docs.volcengine.com/docs/82379/2678892) ·
[Base URL and authentication](https://docs.byteplus.com/en/docs/ModelArk/1298459) ·
[Error codes](https://docs.byteplus.com/en/docs/ModelArk/1299023) ·
[Best practices for backward compatibility](https://docs.byteplus.com/en/docs/ModelArk/2683680).

Ark is the most *complete* implementation in this survey and simultaneously the most instructive,
because it publishes an explicit list of **behaviour it used to reject and has now relaxed** — i.e. a
catalogue of divergences that were live in production.

### 8.1 The compatibility-upgrade notice is a divergence catalogue `[DOC]`

From [API 兼容性升级公告](https://docs.volcengine.com/docs/82379/2678892) (published 2026-09-09), each row is a
*pre*-upgrade divergence:

| Upgrade item | Before | After |
|---|---|---|
| `logprobs` / `top_logprobs` | **"API does not support passing `logprobs`, `top_logprobs`"** | supported for DeepSeek V4 GA series and GLM-5.2 |
| `role=developer` | **"when the content's `role=developer`, an error is returned"** | now accepted, no error |
| DeepSeek `stop` array limit | **max 4; configuring more than 4 returns an error** | raised to 16 |
| Unsupported modality input | **hard error, request rejected** | GLM-5.2 / DeepSeek V4 GA no longer error on image/video/audio they don't support |
| Unparseable `encrypted_content` / `signature` | **hard error** | tolerated |
| `content` being `null` or empty | **hard error** | tolerated in some cases |
| **`tool_call_id` null or empty string** | **hard error** | tolerated in some cases |
| `stop` / `stop_sequence` > 4 | error | 16 for DeepSeek |

The `tool_call_id` row is the documented answer to "can a tool call without an id occur?" — historically
Ark **rejected** a request whose `tool_call_id` was null/empty rather than tolerating it.

### 8.2 Request fields

Supported `[DOC]`: `model`, `messages`, `thinking`, `reasoning_effort`, `max_tokens`,
`max_completion_tokens`, `frequency_penalty`, `presence_penalty`, `logit_bias`, `logprobs`,
`top_logprobs`, `parallel_tool_calls`, `response_format`, `service_tier`, `stop`, `stream_options`,
`tool_choice`, `tools`, `stream`.

| Field | Status | Detail |
|---|---|---|
| `thinking` | `[DOC]` **vendor-only** | `type`: `enabled` \| `disabled` \| **`auto`**. `auto` is non-OpenAI ("the model determines whether reasoning is needed"). Must be passed via `extra_body` with the OpenAI SDK. |
| `reasoning_effort` | `[DOC]` | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. |
| **`max_tokens` vs `max_completion_tokens`** | `[DOC]` | **"Cannot be set while `max_tokens` parameter is set"** — strictly mutually exclusive. Semantics also differ: for most models `max_tokens` bounds the **answer only** and excludes chain-of-thought; `max_completion_tokens` bounds answer **+ CoT**. |
| `frequency_penalty` / `presence_penalty` | `[DOC]` | Range `[-2.0, 2.0]`, **but "Unsupported models: Seed 1.8 and later models"** — a moving support boundary across model versions. |
| `logit_bias` | `[DOC]` | Supported, `[-100, 100]`, **but "Models with deep reasoning capabilities do not support this parameter."** |
| `logprobs` / `top_logprobs` | `[DOC]` | `top_logprobs` range `[0, 20]`; again **not supported by deep-reasoning models**. |
| `parallel_tool_calls` | `[DOC]` | Supported; `false` is only honoured on Seed 1.6+, while `true` applies to all models (an asymmetric support matrix). |
| `stop` | `[DOC]` | Default up to 4; DeepSeek V4 models up to **16**. |
| `service_tier` | `[DOC]` | **`fast`, `auto`, `default`, `flex`** — a different vocabulary from OpenAI's `auto`/`default`/`flex`, with `auto` meaning "prefer TPM guarantee package". |
| `response_format` | `[DOC]` | **`text`, `json_object`, `json_schema`** — full support; `json_schema` requires `name` + `schema` and takes `strict` (default `false`) and `description`. Ark marks the whole capability as beta. |

### 8.3 Message/content parts

- Roles: `system`, `user`, `assistant`, `tool`; **`developer` is accepted only after the 2026-09-09 upgrade; before that it returned an error** `[DOC]`.
- **Request-side `reasoning_content` on assistant messages is supported** `[DOC]`, for Seed 1.8+,
  `deepseek-v4-pro`, `deepseek-v4-flash`, `deepseek-v3.2` — you pass prior CoT back in the `messages` array.
- Content parts `[DOC]`: `text`; `image_url` with `url` and/or `file_id`; `video_url` with `url`/`file_id`
  plus a non-OpenAI **`fps`** (default 1.0, range [0.2, 5]); `input_audio` with `data`/`file_id`/`url` plus
  a required **`format`** (mime types `.mp3`/`.wav`/`.aac`/`.m4a`, and embedded-audio `.pcm`/`.ac3`/`.alac`);
  `file` with `file_id`/`file_data`/`file_url` + `filename`.
- `image_url.detail` values are **`low`, `high`, `xhigh`** — `xhigh` and the absence of `auto` are non-OpenAI `[DOC]`.
- **`image_url.image_pixel_limit`** with `min_pixels`/`max_pixels` is a vendor-only field that **takes
  priority over `detail`** `[DOC]`. Pixel bounds differ by model generation (3136–4014080 pre-Seed-1.8,
  1764–9031680 for Seed 1.8/Dola Seed 2.0); out-of-range raises an error.

### 8.4 Response envelope

- `finish_reason` documented set: `stop`, `length`, `content_filter`, `tool_calls` `[DOC]`. `length` is
  documented as covering three distinct causes (max_tokens, max_completion_tokens, context_window) — clients
  cannot distinguish them from the value alone.
- `usage` is a superset with non-OpenAI members `[DOC]`: `prompt_tokens`, `completion_tokens`,
  `total_tokens`, `prompt_tokens_details.{cached_tokens, audio_tokens, audio_cached_tokens}`,
  `completion_tokens_details.reasoning_tokens`. **`audio_cached_tokens` has no OpenAI counterpart.**
- `choices[].logprobs.content.{bytes,logprob,token,top_logprobs}` is documented in OpenAI's shape `[DOC]`.
- **[NOT DOC] `system_fingerprint` does not appear in the ModelArk Chat API response reference** — Ark
  does not publish that field, so clients must not depend on it.

### 8.5 Streaming

- **`stream_options.include_usage: true` emits a separate final chunk with `"choices": []` carrying the
  whole request's usage, before `data: [DONE]`** `[DOC]`.
- **Ark adds a second, non-OpenAI streaming knob: `stream_options.chunk_include_usage`** — when `true`,
  **every** chunk carries the *cumulative* usage up to that chunk `[DOC]`. This field does not exist in OpenAI.
- `data: [DONE]` terminator: documented `[DOC]`.
- `delta.reasoning_content` is the thinking field `[DOC]`.

### 8.6 Tool calling

- `tool_choice` supports `none`/`auto`/`required`/named object, **but only on Seed 1.6 and later models** `[DOC]`.
- `tools[].function.strict` is supported `[DOC]`.
- `parallel_tool_calls` default `true` `[DOC]`.
- The upgrade notice implies that **historically a null/empty `tool_call_id` was rejected**, i.e. Ark is
  strict about tool-result correlation; the relaxation is recent and partial `[DOC]`.

### 8.7 Errors

- Envelope: the public error table is organised as `Type | Error code | Error message | Meaning`, with codes
  such as `InvalidParameter`, `InvalidParameter.UnsupportedParameter`,
  `InvalidArgumentError.UnknownRole` ("The value of role in the message body is not supported, such as
  `user_`"), `MissingRole`, `InvalidParameter.TaskTypeMismatch`, and the 429 family
  `RateLimitExceeded.EndpointRPMExceeded` / `.EndpointTPMExceeded` / `.EndpointFlexTPMExceeded` `[DOC]`.
  The published examples do not show an OpenAI-style `{"error":{...}}` wrapper for the Chat API `[NOT DOC]`.
- **SSE-stage errors are a different channel entirely** `[DOC]`: errors after the stream is established are
  sent as `data.type = "session.error"` events **without an HTTP status code**, with
  `error.type` ∈ `{model_overloaded_error, model_rate_limited_error, model_request_failed_error,
  billing_error, unknown_error}`. The docs give an explicit client rule: "Use `error.type` as the only
  parameter for programmatic classification", and note `error.message` is display-only.
- Ark publishes a written **backward-compatibility policy** advising permissive parsing, tolerance of
  unknown enum values, and reliance on HTTP status + error codes rather than message wording `[DOC]` — an
  acknowledgement that the surface evolves.

---

## 9. SiliconFlow 硅基流动

Source: [创建对话请求（OpenAI）](https://docs.siliconflow.com/cn/api-reference/chat-completions/chat-completions).

SiliconFlow publishes a complete OpenAPI 3.0 document. Its request schema is unusually small.

### 9.1 Request fields

Documented request properties `[DOC]`: `model`, `messages`, `stream`, `max_tokens`, `enable_thinking`,
`thinking_budget`, `min_p`, `stop`, `temperature`, `top_p`, `top_k`, `frequency_penalty`,
`response_format`, `tools`.

**Absent from the OpenAPI document** `[NOT DOC]`: `tool_choice`, `parallel_tool_calls`, `logprobs`,
`top_logprobs`, `logit_bias`, **`presence_penalty`**, `seed`, `stream_options`, `max_completion_tokens`,
`system_fingerprint`, `developer`.

| Field | Status | Detail |
|---|---|---|
| `messages` | `[DOC]` | **"Required array length: 1 - 10 elements"** — a **10-message cap** documented in the rendered API reference, which is far below OpenAI's practical limit. |
| `enable_thinking` | `[DOC]` **vendor-only** | Default **`true`**. Per-model: Qwen3 series, `tencent/Hunyuan-A13B-Instruct`, `zai-org/GLM-5V-Turbo`, `GLM-4.6V`, `GLM-4.5V`, DeepSeek-V3.1/V3.2. **To use function calling with `deepseek-ai/DeepSeek-V3.1` you must set `enable_thinking: false`** — a documented hard coupling between thinking and tools. |
| `thinking_budget` | `[DOC]` **vendor-only** | Default 4096; range 128–32768. |
| `min_p` | `[DOC]` **vendor-only** | `[0,1]`, "only applies to Qwen3". |
| `top_k` | `[DOC]` **vendor-only** | Standard in Chinese stacks, absent from OpenAI. |
| `frequency_penalty` | `[DOC]` | Present. **`presence_penalty` is not documented at all** — an asymmetric omission. |
| `stop` | `[DOC]` | **Up to 4 sequences.** Docs state the returned text will **not** contain the stop sequence (OpenAI-like). |
| `response_format` | `[DOC]` | Documented as an object; the sub-schema is not enumerated in the excerpt reviewed, so `json_object` vs `json_schema` is `[NOT DOC]`. |
| `n` | `[DOC]` (rendered reference) | "Number of generations to return", default 1 — but **the OpenAPI component schema does not enumerate `n`**, so the two surfaces disagree. |
| `tools[].function.strict` | `[DOC]` | Supported; docs explicitly warn that **"only a subset of JSON Schema is supported when `strict` is `true`"** without publishing the subset. |

### 9.2 Response envelope

- `id`, `choices`, `usage`, `created`, `model`, `object` (`chat.completion`) `[DOC]`.
- **`system_fingerprint` is not in the response schema** `[NOT DOC]`.
- **`usage` is a strict subset of OpenAI's**: only `prompt_tokens`, `completion_tokens`, `total_tokens` `[DOC]`.
  **`prompt_tokens_details.cached_tokens` and `completion_tokens_details.reasoning_tokens` are absent** —
  clients that read cached-token discounts will read nothing.
- **`finish_reason` enum is `stop`, `eos`, `length`, `tool_calls`** `[DOC]`. **`eos` is not an OpenAI value**
  and has no defined meaning in OpenAI's taxonomy.
- `choices[].message.reasoning_content` is documented (DeepSeek-R1 series), with a note that prior-turn
  reasoning is **not** appended to context in the next round `[DOC]`.

### 9.3 Errors

**This is the least OpenAI-compatible error surface found in this survey** `[DOC]`:

| HTTP | Schema |
|---|---|
| 400 | `{"code": <integer>, "message": <string>, "data": <string>}` — **flat, no `error` wrapper** |
| 401 | **a bare JSON string** (e.g. `"Invalid token"`), not an object |
| 404 | **a bare JSON string** (e.g. `"404 page not found"`) |
| 429 | `{"message": <string>, "data": <string>}` — **no code, no `error` wrapper** |
| 503 | `{"code": <integer>, "message": <string>, "data": <string>}` |
| 504 | **a bare JSON string** |

A client that does `response.json()["error"]["message"]` will raise `TypeError` on SiliconFlow's 401/404/504
because the body is a JSON **string**, not an object. This is a documented, not observed, divergence.

---

## 10. Tencent Hunyuan

Source: [混元 OpenAI 兼容接口相关调用示例](https://cloud.tencent.com/document/product/1729/111007).
Note the page carries a banner that the legacy Hunyuan console is being migrated to TokenHub and that the
older `hunyuan.ai.tencentcloudapi.com` native interface is **pre-sunset (planned 2026-12-21)** `[DOC]`.

### 10.1 The `stop` inversion — the single most dangerous divergence in this survey `[DOC]`

Hunyuan documents, with before/after output examples, that `stop` semantics are **reversed**:

> "调用 OpenAI 的接口时，如果您指定了 `stop` 参数, 模型会停止在匹配到 `stop` 的内容**之前**。在调用混元接口时，会停止在匹配到 `stop` 的内容**之后**。"
>
> Example raw output: `我是一个 AI 助手可以帮助您在不同方面做出更好的决策…`
> `stop` = `助手`
> **OpenAI** → `我是一个 AI`   **Hunyuan** → `我是一个 AI 助手`

Tencent adds that it "may change this behaviour in future to align with OpenAI" — so a client that
compensates today will break tomorrow. Any code that strips a trailing sentinel, or that relies on the stop
sequence being excluded, silently produces different text.

### 10.2 Request fields

| Field | Status | Detail |
|---|---|---|
| `messages` | `[DOC]` | **Maximum length 40.** Roles: `system`, `user`, `assistant`, `tool`. **`developer` is not listed** `[NOT DOC]`. Content longer than the model input limit is **truncated from the front, keeping the tail** — a silent destructive behaviour with no OpenAI equivalent. |
| `max_tokens` | `[DOC]` | Default **4096**. |
| `max_completion_tokens` | `[NOT DOC]` | Not listed. |
| `seed` | `[DOC]` | **Non-zero positive integer, maximum 10000.** OpenAI permits any integer and places no such cap (and OpenAI's `seed` may be 0). |
| `stop` | `[DOC]` | `string[]`. Semantics inverted — see above. |
| `temperature` | `[DOC]` | `[0.0, 2.0]`. |
| `top_p` | `[DOC]` | Default documented as **0** (an odd default for a probability threshold). |
| `tool_choice` | `[DOC]` | **Values are `none`, `auto`, `custom`.** `custom` is a non-OpenAI value meaning "force the model to call a specific tool". **`required` is not listed**, and the named-function object form is not described as such. Only effective on `hunyuan-turbos` and `hunyuan-functioncall`. |
| `stream_options.include_usage` | `[DOC]` | Supported; usage arrives in the **last** data chunk. |
| `presence_penalty`, `frequency_penalty`, `logit_bias`, `logprobs`, `response_format`, `n`, `parallel_tool_calls` | `[NOT DOC]` | Not in the published OpenAI-compatible parameter table. |
| Vendor-only | `[DOC]` | `citation`, `enable_enhancement` (search switch, default flipped to **off** on 2025-04-20), `enable_multimedia`, `enable_recommended_questions`, `force_search_enhancement`, `search_info`. |

### 10.3 Response and streaming

- **[DOC]** `stream_options.include_usage=true` returns `usage` in the last chunk. No separate usage-only
  chunk is described `[NOT DOC]`.
- **[DOC]** `enable_recommended_questions` adds a non-OpenAI **`recommended_questions`** field (up to 3
  entries) to the final streaming packet.
- **[NOT DOC]** `finish_reason` value set, `system_fingerprint`, `usage` sub-fields, and the tool-call delta
  shape are not published on this page.

### 10.4 Embeddings caveat (same page, same envelope)

`[DOC]` `/v1/embeddings` supports **only `input` and `model`**; `model` is fixed to `hunyuan-embedding` and
`dimensions` is fixed to 1024. Not a Chat Completions divergence, but it shows the same compatibility page
treats unsupported OpenAI parameters as absent rather than errored.

---

## 11. Baidu ERNIE / Qianfan

Source: [千帆文本生成 API 参考](https://cloud.baidu.com/doc/qianfan-api/s/3m7of64lb) ·
[OpenAI SDK 兼容介绍](https://cloud.baidu.com/doc/qianfan/s/Hmh4suq26).

Qianfan's OpenAI-compatible surface is served under a **`/v2`** path (not `/v1`). Its parameter table is
rich but contains several vendor-only fields and silently-different defaults.

| Field | Status | Detail |
|---|---|---|
| `max_tokens` vs `max_completion_tokens` | `[DOC]` | **Both documented with different semantics**: `max_tokens` limits **only the final answer**, excluding chain-of-thought; `max_completion_tokens` limits **answer + CoT**. **If both are set, `max_completion_tokens` wins** — compare Ark, where the two are *mutually exclusive and error*. |
| `penalty_score` | `[DOC]` **vendor-only** | Default **1.0**, range **[1.0, 2.0]**. This is a *reward-like* scale where 1.0 is neutral — semantically and numerically unrelated to OpenAI's `presence_penalty`/`frequency_penalty`, which are centered on 0 in `[-2, 2]`. A client that maps penalties onto it naively will produce very wrong output. Unsupported on DeepSeek-V3, DeepSeek-Reasoner, R1-Distill, QwQ-32B, ERNIE X1 Turbo, and several ERNIE 4.5 models. |
| `repetition_penalty` | `[DOC]` **vendor-only** | Standard in Chinese OSS stacks; no OpenAI equivalent. Support varies by model. |
| `presence_penalty`, `frequency_penalty` | `[DOC]` | Both listed, with model-dependent support and ranges ("refer to the model default-parameters page"). |
| `seed` | `[DOC]` | Range **(0, 2147483647)** and it **"will be randomly generated by the model"** when unset — i.e. the server always has a seed. Unsupported on ERNIE X1 Turbo, Qwen2.5, Qianfan-Agent-Intent. |
| `stop` | `[DOC]` | **Max 4 elements; each element ≤ 20 characters** (a length constraint OpenAI does not impose). Not supported on ERNIE X1 Turbo. |
| `stream_options.include_usage` | `[DOC]` | When `true`, the **final chunk** carries usage for the whole request. |
| `stream_options.chunk_include_usage` | `[DOC]` **vendor-only** | When `true`, **every chunk** carries cumulative usage. **This is the same non-OpenAI extension Ark has**, suggesting a shared lineage. |
| `tool_choice` | `[DOC]` | `none`, `auto`, `required`, and the named-function object form. |
| `parallel_tool_calls` | `[DOC]` | Listed (support subject to the model). |
| `web_search` | `[DOC]` **vendor-only** | Object with `enable` and **`search_mode`** ∈ {`auto`, `required`} — note `required` here means "force a web search", overloading OpenAI's `tool_choice` vocabulary for a different purpose. ERNIE series does not support `search_mode`. |
| `enable_thinking`, `thinking_budget` | `[DOC]` **vendor-only** | Thinking switch and CoT length cap ("when the thinking tokens exceed `thinking_budget`, reasoning is truncated and the final answer begins immediately"). |
| `reasoning_content` | `[DOC]` | Returned on responses and accepted on assistant messages. |
| `logprobs`, `top_logprobs`, `logit_bias` | `[NOT DOC]` | Not in the published parameter list. |
| `developer` role | `[NOT DOC]` | Not documented. |
| `system_fingerprint` | `[NOT DOC]` | Not documented. |

---

## 12. iFlytek Spark 讯飞星火

Source: [星火认知大模型 HTTP 接口文档](https://www.xfyun.cn/doc/spark/HTTP%E8%B0%83%E7%94%A8%E6%96%87%E6%A1%A3.html).

Spark's HTTP endpoint is OpenAI-shaped but the parameter table carries several hard divergences.

### 12.1 Auth and endpoint `[DOC]`

- `base_url` for the OpenAI SDK: `https://spark-api-open.xf-yun.com/v1/`; full path
  `/v1/chat/completions`.
- **The credential is the console "APIPassword", not an API key, and the correct APIPassword differs per
  model version.** Sent as `Authorization: Bearer <APIPassword>`. This means a single key cannot address all
  models, and the token is fundamentally a per-service secret rather than an account key — a real problem
  for multi-model gateways.

### 12.2 Request fields

| Field | Status | Detail |
|---|---|---|
| `model` | `[DOC]` | Values are **legacy version codes**: `4.0Ultra`, `generalv3.5` (=Max), `max-32k`, `generalv3` (=Pro), `pro-128k`, `lite` — not marketing model names. |
| `user` | `[DOC]` | Supported (OpenAI has this too). |
| `messages.role` | `[DOC]` | `user`, `assistant`, `system`, `tool`. **`developer` is not supported** `[NOT DOC]`. |
| `top_k` | `[DOC]` **vendor-only** | **Range [1, 6], default 4** — an unusually narrow range (most stacks allow 0–100+). |
| `presence_penalty` | `[DOC]` | **Range [0, 2], default 1.2.** Divergent on both axes: OpenAI's range is `[-2, 2]` and its default is 0. A client sending the OpenAI default of 0 will get *less* penalty than the server default of 1.2. |
| `frequency_penalty` | `[DOC]` | **Range [0, 1], default 0.02.** Again asymmetric: OpenAI allows negatives to *encourage* repetition; Spark clamps at 0. |
| `temperature` | `[DOC]` | `[0, 2]`, default 1.0. Docs recommend 1.2 for math/reasoning/code. |
| `top_p` | `[DOC]` | `(0, 1]`, default 1. |
| `max_tokens` | `[DOC]` | Per-version caps and defaults: Ultra `[1,32768]` default 32768; Max-32K `[1,32768]` default 4096; Max `[1,8192]` default 4096; Pro `[1,8192]` default 4096; Pro-128K default 4096; Lite `[1,4096]` default 4096. |
| `response_format` | `[DOC]` | **`text` and `json_object` only — no `json_schema`.** Requires the same "instruct the model to produce JSON" prompt-side nudge as DeepSeek. |
| `tools` | `[DOC]` | Supports both a **built-in `web_search` tool type** (`{type: "web_search", web_search: {enable, show_ref_label, search_mode}}`) and `function`. **`tools.web_search` is only on Ultra/Max/Pro; `tools.function` only on Max/Ultra.** |
| `tools.function.name` | `[DOC]` | **Length ≤ 32** (OpenAI allows 64). |
| `tool_choice` | `[DOC]` | `auto`, `none`, `required`, and the named-function object form — the full OpenAI set. |
| `tool_calls_switch` | `[DOC]` **vendor-only** | Default **false**. When `false`, tool calls are returned **"in json format"**; setting it to `true` makes `tool_calls` return as an array. **This means the default response shape for a tool call is not the OpenAI array shape.** |
| `logprobs`, `logit_bias`, `seed`, `n`, `stop`, `parallel_tool_calls`, `stream_options` | `[NOT DOC]` | Not in the published parameter table. |
| Vendor-only extras | `[DOC]` | `search_prompt` is returned when deep search mode is active. |

### 12.3 Response and errors

- `[DOC]` `usage` is only `prompt_tokens`, `completion_tokens`, `total_tokens` — **no details objects**.
- `[DOC]` `show_ref_label` makes the server emit **search results as a separate packet before** the model
  reply — an out-of-band mid-stream message type not in OpenAI's vocabulary.
- `[NOT DOC]` `finish_reason` value set, `system_fingerprint`, error envelope and rate-limit headers are not
  published on the HTTP page.

---

## 13. StepFun 阶跃星辰

Source: [Chat Completions API](https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create).

### 13.1 Request fields

| Field | Status | Detail |
|---|---|---|
| `modalities` | `[DOC]` | `["text","audio"]`; required for end-to-end audio models. `stepaudio-2.5-chat` errors if `audio` is requested. |
| `max_tokens` | `[DOC]` | **Default `INF` (no limit, model decides)** — OpenAI requires an explicit value or uses a model default. |
| `n` | `[DOC]` | **"默认值为 1，最大不限，建议不超过 5"** — maximum is *unbounded*; OpenAI caps `n` at 128. A client that validates `n ≤ 128` locally and a server that accepts anything will disagree about what is legal. |
| `temperature` | `[DOC]` | Default **0.5**. |
| `top_p` | `[DOC]` | Default **0.9**. |
| `stop` | `[DOC]` | `string` or `string[]`; on match the model stops immediately. OpenAI-like. |
| `frequency_penalty` | `[DOC]` | `[-2.0, 2.0]`, default 0. **`presence_penalty` is not documented** `[NOT DOC]`. |
| `response_format` | `[DOC]` | **`text`, `json_object`, `json_schema`** — full support; `json_schema` takes `name`, `schema`, and a **`strict`** boolean (default false). |
| `reasoning_format` | `[DOC]` **vendor-only** | **`general` (default) → the reasoning text is returned in a `reasoning` field; `deepseek-style` → it is returned in `reasoning_content`.** This is a per-request switch that changes the *response* field name. |
| `reasoning_effort` | `[DOC]` | `low`/`medium`/`high` for three-tier models; `step-3.5-flash-2603` accepts only `low`/`high`. |
| `tool_choice`, `parallel_tool_calls`, `logprobs`, `logit_bias`, `seed`, `presence_penalty`, `stream_options`, `max_completion_tokens`, `developer` | `[NOT DOC]` | **None of these appear in the Chat Completions reference.** |
| Vendor-only `audio` | `[DOC]` | Output audio config: `format` required (`pcm`/`wav`); **`wav` only when `stream=false`; streaming supports only `pcm` at 24 kHz mono 16-bit.** |

### 13.2 Streaming — two sharp divergences `[DOC]`

The documented SSE sample shows both:

```
data: {...,"choices":[{"index":0,"delta":{"role":"","content":"您"},"finish_reason":""}],"usage":{...84}}
data: {...,"choices":[{"index":0,"delta":{"role":"","content":"好"},"finish_reason":""}],"usage":{...85}}
data: {...,"choices":[{"index":0,"delta":{"role":"","content":""},"finish_reason":"stop"}],"usage":{...233}}
data: [DONE]
```

1. **`finish_reason` is the empty string `""` on non-final chunks, not `null`.** OpenAI uses `null`. Any
   client doing `if chunk.choices[0].finish_reason is None: continue` will treat *every* chunk as final on
   StepFun.
2. **`usage` is present on *every* chunk**, carrying a **cumulative** count — with no `stream_options`
   opt-in at all. Clients that sum `usage.completion_tokens` across chunks to compute cost will
   massively over-count; clients that read only the last chunk happen to be correct.

Additionally, `delta.role` is the **empty string `""`** rather than `"assistant"` or `null` `[DOC]`.

### 13.3 Other documented details

- `data: [DONE]` terminator: **present** `[DOC]`.
- `finish_reason` documented set: `stop`, `length`, `tool_calls` `[DOC]`.
- `usage` documented sub-fields: `prompt_tokens`, `completion_tokens`, `total_tokens`,
  `prompt_tokens_details.cached_tokens`, `completion_tokens_details.reasoning_tokens` `[DOC]` — this is the
  most OpenAI-faithful usage envelope in this survey.
- Both `reasoning` and `reasoning_content` are returned **together and with identical content** on
  StepFun reasoning models — the docs say `reasoning_content` is the "DeepSeek-compatible field name" `[DOC]`.
- A separate **Step Plan channel** (`https://api.stepfun.com/step_plan/v1`) changes the contract for one
  model: `step-router-v1` returns HTTP 400 `request_params_invalid` for any other model name;
  `max_tokens` is capped at 250K; images/documents in `messages` return `unsupported_content_type`; the
  `web_search` tool returns `unsupported_content_type` `[DOC]`. **The same request body is legal or illegal
  depending on which base URL the client was configured with.**
- Content parts: `text`, `image_url` (with `detail: low`/`high` — no `auto`), `video_url` (MP4, < 128 MB,
  ≤ 5 min suggested), **`input_audio`** (base64; **mp3 and wav only**) `[DOC]`.
- `[NOT DOC]` `system_fingerprint` and the error envelope are not published on this page.

---

## 14. Cross-vendor quick matrix

Legend: **Y** supported/documented · **N** documented as unsupported/rejected · **–** not documented ·
**≠** supported but with divergent semantics.

| | DeepSeek | Qwen | Kimi | Zhipu | MiniMax | Ark | SiliconFlow | Hunyuan | Qianfan | Spark | StepFun |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `/v1` required | **no** | yes+`/compatible-mode` | yes | **no (`/v4`)** | yes | **no (`/v3`)** | yes | yes | **no (`/v2`)** | yes | yes |
| `developer` role | **N** | – | – | – | – | **Y (recently)** | – | – | – | – | – |
| `response_format: json_schema` | **N** | – | Y | **N** | – | Y | – | – | – | **N** | Y |
| `stream_options.include_usage` | Y (last content chunk) | Y (separate chunk) | Y | – | Y | Y (separate chunk) | – | Y | Y | – | – (`usage` always, every chunk) |
| separate usage-only chunk (`choices: []`) | **N** | **Y** | – | – | – | **Y** | – | – | – | – | – |
| `tool_choice: required` | Y (not in thinking) | **N** | Y | **N (only `auto`)** | – | Y (Seed 1.6+) | – | **N (`custom` instead)** | Y | Y | – |
| `parallel_tool_calls` | – | Y | – | – | – | Y | – | – | Y | – | – |
| `max_completion_tokens` | – | – | Y | – | Y | Y (mutually exclusive w/ `max_tokens`) | – | – | Y (wins over `max_tokens`) | – | – |
| `logprobs` | Y | – | Y | – | – | Y (not on reasoning models) | – | – | – | – | – |
| `logit_bias` | – | – | – | – | ignored | Y (not on reasoning models) | – | – | – | – | – |
| `seed` | – | Y | – | – | – | – | – | Y (≤10000) | Y (0,2^31) | – | – |
| `presence_penalty` | ≠ (silently ignored in thinking) | Y (limited models) | – | – | **ignored** | Y (not Seed 1.8+) | – | – | Y | ≠ (default 1.2) | – |
| `frequency_penalty` | ≠ (silently ignored in thinking) | – | – | – | **ignored** | Y (not Seed 1.8+) | Y | – | Y | ≠ (default 0.02) | Y |
| `system_fingerprint` returned | Y (real fp) | **`""` empty string** | – | – | – | – | – | – | – | – | – |
| non-OpenAI `finish_reason`s | `insufficient_system_resource`, `aborted` | – | – | `sensitive`, `network_error`, `model_context_window_exceeded` | – | – | **`eos`** | – | – | – | – |
| `usage` cached tokens | Y (`prompt_tokens_details` + extra top-level) | – | Y + top-level `cached_tokens` + `cache_write_tokens` | Y | – | Y + `audio_cached_tokens` | **N** | – | – | **N** | Y |
| `usage.reasoning_tokens` | Y | – | – | – | – | Y | – | – | – | – | Y |
| OpenAI-shaped error envelope | – | **flat** | Y (partial) | **N (`code`+`message` only)** | – | – | **N (flat / bare strings)** | – | – | – | – |
| `stop` semantics | OpenAI-like | OpenAI-like + token_ids | OpenAI-like | array-only | – | OpenAI-like | OpenAI-like (≤4) | **INVERTED (after match)** | ≤4, ≤20 chars | – | OpenAI-like |

---

## 15. Top 10 differences that actually break clients (ranked)

Ranked by *probability of silent corruption or hard failure in a naive OpenAI-shaped client*, weighted by
how documented vs. observed the behaviour is.

1. **Hunyuan's `stop` semantics are inverted.** `[DOC]` The model stops **after** the matched string, so the
   sentinel is *included* in the output, where OpenAI excludes it. Silent data corruption, and Tencent says
   it may change the behaviour later — so compensating code is a time bomb.
   ([source](https://cloud.tencent.com/document/product/1729/111007))

2. **`tool_choice` is not portable at all.** `[DOC]`+`[OBS]` Zhipu rejects everything except `"auto"`
   (HTTP 400 `Tool choice must be auto`); Qwen rejects `"required"` with
   `tool_choice is one of the strings that should be ["none","auto"]` and rejects `required`/object form
   outright in thinking mode; Hunyuan replaces the enum with `custom`; DeepSeek 400s on `required`/named in
   thinking mode. Agent frameworks that force a first tool call fail on four of the vendors here.
   ([Zhipu](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8),
   [hindsight#4246](https://github.com/vectorize-io/hindsight/issues/4246),
   [pydantic-ai#1265](https://github.com/pydantic/pydantic-ai/issues/1265),
   [Hunyuan](https://cloud.tencent.com/document/product/1729/111007))

3. **Streaming usage-vs-finish-reason contract differs four ways.** `[DOC]` DeepSeek attaches usage to the
   last *content* chunk and emits no usage-only chunk; Qwen and Ark emit a separate final chunk with
   `"choices": []`; StepFun emits cumulative usage on **every** chunk and uses `finish_reason: ""` instead of
   `null` on non-final chunks; Kimi puts usage in the last chunk before `[DONE]`. A usage-accounting layer
   written for one vendor over- or under-counts on the others, and a `finish_reason is None` check
   mis-terminates every StepFun stream.
   ([DeepSeek](https://api-docs.deepseek.com/api/create-chat-completion),
   [Alibaba](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope),
   [StepFun](https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create))

4. **`role: "developer"` is rejected by DeepSeek.** `[DOC]`+`[OBS]` Modern OpenAI-style clients (Codex-style
   agents) send `developer` for the system prompt. DeepSeek's schema enumerates only
   `system`/`user`/`assistant`/`tool`, and the upstream error is an unhelpful serde message
   (`unknown variant 'developer'`). Ark admits it *used to* error on `developer` and only recently relaxed.
   ([opencode-go-cliproxyapi#5](https://github.com/massiveits/opencode-go-cliproxyapi/issues/5),
   [hebo-gateway#251](https://github.com/8monkey-ai/hebo-gateway/issues/251),
   [litellm#24664](https://github.com/BerriAI/litellm/issues/24664),
   [Ark upgrade notice](https://docs.volcengine.com/docs/82379/2678892))

5. **SiliconFlow's error envelope is not an object.** `[DOC]` For HTTP 401, 404 and 504 the response body is a
   **bare JSON string**; 400/429/503 are flat `{code,message,data}` objects with **no `error` wrapper**.
   Every `error["message"]` accessor throws, and the failure path is exactly when you need diagnostics.
   ([source](https://docs.siliconflow.com/cn/api-reference/chat-completions/chat-completions))

6. **`response_format: json_schema` is unavailable on DeepSeek, Zhipu and Spark.** `[DOC]` Structured Output
   only degrades to "valid JSON, no schema" (DeepSeek/Zhipu `json_object`) or is absent. Clients that rely on
   schema-constrained decoding for correctness get valid-but-wrong-shape JSON instead of an error.
   ([DeepSeek JSON Output](https://api-docs.deepseek.com/guides/json_mode),
   [Zhipu](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8),
   [Spark](https://www.xfyun.cn/doc/spark/HTTP%E8%B0%83%E7%94%A8%E6%96%87%E6%A1%A3.html))

7. **Silently ignored parameters destroy request intent.** `[DOC]` MiniMax ignores `presence_penalty`,
   `frequency_penalty`, `logit_bias` "etc."; DeepSeek ignores `temperature`/`presence_penalty`/
   `frequency_penalty` in thinking mode and clamps `top_p` to ≥ 0.95; Zhipu's `do_sample: false` silently
   discards `temperature` and `top_p`. No error is ever raised, so callers cannot tell their tuning was
   dropped. ([MiniMax](https://platform.minimaxi.com/docs/api-reference/text-openai-api),
   [DeepSeek Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode),
   [Zhipu](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8))

8. **`finish_reason` value sets leak vendor-internal states**, and clients switch on them. `[DOC]`
   DeepSeek emits `insufficient_system_resource` and `aborted`; Zhipu emits `sensitive`, `network_error` and
   `model_context_window_exceeded` (and, uniquely, signals **mid-stream SSE failures via `finish_reason`
   rather than an error event**); SiliconFlow emits `eos`. Any `match finish_reason { stop|length|tool_calls|content_filter => ... }`
   without a default arm panics or falls through.
   ([DeepSeek](https://api-docs.deepseek.com/api/create-chat-completion),
   [Zhipu](https://docs.bigmodel.cn/cn/api/api-code),
   [SiliconFlow](https://docs.siliconflow.com/cn/api-reference/chat-completions/chat-completions))

9. **`system_fingerprint` and cached-token accounting are unreliable.** `[DOC]` Alibaba returns
   `system_fingerprint: ""` (an empty string, not null and not a fingerprint) and does not document
   `prompt_tokens_details`; SiliconFlow omits `system_fingerprint` and returns a **three-field `usage`** with
   no cached-token detail at all; Kimi adds a non-standard **top-level `cached_tokens`** plus
   `cache_write_tokens`. Cache-discount logic and any code keying on the fingerprint silently produce wrong
   numbers ([Alibaba](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope),
   [SiliconFlow](https://docs.siliconflow.com/cn/api-reference/chat-completions/chat-completions),
   [Kimi](https://platform.kimi.com/docs/api/chat))

10. **Thinking/CoT round-tripping is vendor-specific and often mandatory in only one direction.**
    `[DOC]`+`[OBS]` DeepSeek returns `reasoning_content` and then **requires it back** on assistant messages
    that carry `tool_calls` (HTTP 400 otherwise); Qwen tells you to replay thinking + content + tool_calls
    together; Zhipu's `clear_thinking` (default `true`) **strips** prior CoT unless you set it to `false` and
    replay it byte-identically; MiniMax embeds thinking in `content` as `<think>` tags unless
    `reasoning_split` is set; StepFun renames the field via `reasoning_format`. A conversation state machine
    that treats the assistant message as `{role, content, tool_calls}` breaks on essentially all of them.
    ([fennara-godot-ai#151](https://github.com/fennaraOfficial/fennara-godot-ai/issues/151),
    [DeepSeek](https://api-docs.deepseek.com/guides/thinking_mode),
    [MiniMax](https://platform.minimaxi.com/docs/api-reference/text-openai-api),
    [Zhipu](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8),
    [StepFun](https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create))

**Honourable mention (fails loudly, so ranked below the silent ones):** Ark's
`max_tokens` and `max_completion_tokens` are **mutually exclusive and error together**, while Qianfan
documents that `max_completion_tokens` simply **wins** when both are set. A client that sends both for
"compatibility" errors on Ark and silently changes meaning on Qianfan.

---

## 16. Explicit gaps — what I could not establish

Stated so the dossier is not read as more complete than it is:

- **DeepSeek error envelope and rate-limit headers: not documented.** DeepSeek publishes only a status-code
  table. The `{"error":{"message","type"}}` shape quoted in §3.6 is community-observed.
- **MiniMax response schema: not documented.** No published `finish_reason` set, `system_fingerprint`
  guarantee, SSE framing, tool-call delta shape, error envelope or rate-limit headers.
- **Hunyuan response envelope: not documented** beyond `usage` placement and `recommended_questions`.
- **Spark `finish_reason` set, `system_fingerprint`, error envelope: not documented.**
- **StepFun `system_fingerprint` and error envelope: not documented.**
- **Baidu Qianfan `finish_reason` set, `system_fingerprint` and error envelope: not documented** on the pages
  I could retrieve; Qianfan's response-field table was only partially extractable.
- **Volcengine Ark's CN-site docs are JavaScript-rendered** and could not be read directly; the equivalent
  BytePlus ModelArk (international) documentation was used and is cited as such. The CN and intl surfaces
  share the `/api/v3` contract, but I did not independently verify every CN-only field.
- **`n`, `seed`, `logit_bias`, `presence_penalty`, `frequency_penalty` support for Zhipu, SiliconFlow, Kimi
  and StepFun is `[NOT DOC]`, not proven-rejected.** Only MiniMax and DeepSeek explicitly document
  ignore-vs-error behaviour.
- **Rate-limit response headers** are essentially undocumented across the board. Only Zhipu shows a vendor
  header (`x-ratelimit-scope: global`) anywhere in its published OpenAPI, and only for managed-agent
  endpoints; DeepSeek documents concurrency limits and `user_id` isolation but no headers.
- **`strict` JSON-Schema subsets** are published in detail only by DeepSeek. Ark, Kimi, SiliconFlow and
  StepFun document that `strict`/`json_schema` exist and warn that only a subset is supported, without
  enumerating the subset.
- **Community evidence is deliberately thin.** The GitHub issues cited were selected because they name the
  exact vendor, HTTP status and error string; I did not treat any blog post as a source. Several searches
  hit secondary sources I chose not to cite.
