# Protocol-Compatibility Dossier: vLLM vs SGLang OpenAI / Anthropic / Responses Servers

**Scope.** How the two dominant open-source inference servers diverge from the official OpenAI and
Anthropic HTTP APIs, and how mature their Anthropic Messages and Responses support is. Focused on what
breaks a **Codex**-style (Responses API) or **Claude-Code**-style (Anthropic Messages) client.

**Method and evidence labels.** Every claim below is tagged:

- **[documented]** — stated in the vendor's own docs; the doc URL is cited.
- **[source-derived]** — read out of the pinned source revision cited; quote + file path given.
- **[issue/PR]** — established by a GitHub issue or pull request; number and date cited.
- **[unknown]** — could not be determined; not guessed.

**Revision pinned.** All source quotes are from `main` as fetched on the date of this research:
`raw.githubusercontent.com/vllm-project/vllm/main/...` and
`raw.githubusercontent.com/sgl-project/sglang/main/...`. Because there is no release tag on either
fetch, treat source-derived claims as **"main at time of writing"**, not as any released version.
Source-derived line numbers are given so a reader can re-verify against a specific commit.

**A note on dates.** The cited artifact dates run from 2024 to **2026-09**, so several "current
behaviour" claims describe a codebase considerably newer than the v0.11/v0.5-era releases most
deployments run. Where a behaviour arrived in a datable PR, the merge date is given — **check the date
before assuming your deployed version behaves this way.**

---

## 0. Executive summary

Five divergences dominate everything else:

1. **vLLM renamed the reasoning field from `reasoning_content` to `reasoning`.** This is the single
   highest-impact change for any OpenAI-compatible client. It is explicitly documented as a silent
   failure mode.
2. **vLLM silently ignores unknown request fields** (`extra="allow"`) — including the *removed*
   `guided_json`/`guided_regex`/`guided_choice`/`guided_grammar` family, which now produces
   **unconstrained output with only a log line**. SGLang instead silently *drops* unknown fields
   (Pydantic default `extra="ignore"`).
3. **SGLang reports reasoning tokens at `usage.reasoning_tokens`, not
   `usage.completion_tokens_details.reasoning_tokens`.** Any client billing or accounting on the
   official path reads zero.
4. **SGLang's `/v1/chat/completions` error envelope is flat** (`{"object":"error","message":...}`), not
   the OpenAI-nested `{"error":{...}}`. Requests are parsed from the *top level*, so a client doing
   `body["error"]["message"]` gets a `KeyError`/`TypeError` instead of the server's message.
5. **Both servers' Responses APIs are partially implemented, and they diverge in opposite directions.**
   vLLM documents `/v1/responses` but **silently ignores `store`** unless an env var is set. SGLang
   implements a broader SSE event set but **documents none of it** and gates statefulness behind a
   flag with an explicit error.

Plus: **neither server emits `system_fingerprint` in the OpenAI format**, vLLM's is a vLLM-specific
string; **SGLang emits none at all**. And **vLLM's Anthropic endpoint has no `thinking` request
field**, so `{"type":"enabled","budget_tokens":N}` from Claude Code is silently discarded.

---

# PART A — OpenAI Chat Completions compatibility (`/v1/chat/completions`)

## A.1 vLLM — request-field disposition

Primary evidence: [OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/)
([raw markdown](https://raw.githubusercontent.com/vllm-project/vllm/main/docs/serving/online_serving/openai_compatible_server.md))
and [`vllm/entrypoints/openai/chat_completion/protocol.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/openai/chat_completion/protocol.py).

### A.1.1 The extra-field policy is the root cause of most silent failures

**[source-derived]** vLLM's request base class explicitly permits unknown fields:

```python
class OpenAIBaseModel(BaseModel):
    # OpenAI API does allow extra fields
    model_config = ConfigDict(extra="allow")
```
— [`vllm/entrypoints/serve/engine/protocol.py:37-39`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/serve/engine/protocol.py)

A wrap-validator then **logs** unrecognised keys and returns the model unchanged — it never raises:

```python
extra_keys = data.keys() - field_names
if extra_keys:
    removed = extra_keys & _REMOVED_GUIDED_FIELDS
    if removed:
        logger.warning_once(
            "Request contains the removed guided-decoding field(s) "
            "%s, which are ignored; output will NOT be constrained. ...")
    logger.debug(
        "The following fields were present in the request but ignored: %s", extra_keys)
```
— [`vllm/entrypoints/serve/engine/protocol.py:60-74`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/serve/engine/protocol.py)

**Consequence:** a client that sends an unsupported or misspelled parameter gets `HTTP 200` and
unconstrained output. The only signal is a server-side log line at `debug` (or a one-shot `warning`
for the four removed guided-decoding fields). There is no way for the client to detect the drop.

### A.1.2 Field-by-field disposition

| Request field | vLLM status | Evidence |
|---|---|---|
| `messages` | **Honored** (required) | `protocol.py:216` |
| `model` | Optional, `None` default; not validated against the served model | `protocol.py:217` |
| `n` | **Honored** → `SamplingParams.n` | `protocol.py:228`, `:711` |
| `best_of` | **Silently ignored** — not a field on `ChatCompletionRequest`; exists only on the separate batch request model | `protocol.py:1094` (`BatchChatCompletionRequest`) |
| `seed` | **Honored**, range-checked `int64` | `protocol.py:231`, `:720` |
| `presence_penalty` | **Honored** | `protocol.py:229`, `:712` |
| `frequency_penalty` | **Honored** | `protocol.py:218`, `:713` |
| `repetition_penalty` | **Honored** (vLLM extra, not OpenAI) | `protocol.py:269`, `:714` |
| `logit_bias` | **Honored** → `SamplingParams.logit_bias` | `protocol.py:219`, `:741` |
| `logprobs` / `top_logprobs` | **Honored**; only wired when `logprobs=True` and `logprob_token_ids` unset | `protocol.py:220-221`, `:723-727` |
| `stop` | **Honored** | `protocol.py:232`, `:721` |
| `response_format` = `text` | **Honored** (no constraint) | `generate/base/protocol.py:138-139` |
| `response_format` = `json_object` | **Honored** → `StructuredOutputsParams(json_object=True)` | `generate/base/protocol.py:142-143` |
| `response_format` = `json_schema` | **Honored** → `{"json": <schema>}`; validator **errors** if `json_schema` missing | `protocol.py:766-777` |
| `response_format` = `structural_tag` | **Honored** (vLLM extension) | `generate/base/protocol.py:156-158` |
| `tools` | **Honored** (schema injected into prompt) | `protocol.py:237` |
| `tool_choice: "auto"` | **Honored only if `--enable-auto-tool-choice` *and* a `--tool-call-parser` are set** — see A.1.5 | `chat_completion/serving.py:1004-1006`, `:1030-1035` |
| `tool_choice: "required"` / named | **Honored even without** `--enable-auto-tool-choice` | `chat_completion/serving.py:1011-1020` |
| `tool_choice: "none"` | Honored | `serving.py:1024-1027` |
| `parallel_tool_calls` | **Honored, but only `false` has an effect** — truncates the result to the first tool call | [`serve/utils/tool_calls_utils.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/serve/utils/tool_calls_utils.py) |
| `stream_options.include_usage` | **Honored** | `serve/utils/api_utils.py:289-301` |
| `stream_options.continuous_usage_stats` | **Honored** (vLLM extra; requires `include_usage`) | `generate/base/protocol.py:238-240`, `api_utils.py:296-298` |
| `max_completion_tokens` | **Honored**, takes precedence over `max_tokens` | `protocol.py:605-610` |
| `max_tokens` | Honored but **marked deprecated** in the OpenAPI schema | `protocol.py:222-226` |
| `bad_words` | **Honored** (vLLM extra) | `protocol.py:301`, `:742` |
| `min_p` / `top_k` | **Honored** (vLLM extras) | `protocol.py:267-268`, `:718-719` |
| `min_tokens` | **Honored** (vLLM extra) | `protocol.py:275`, `:732` |
| `ignore_eos` | **Honored** (vLLM extra) | `protocol.py:274`, `:730` |
| `guided_json` / `guided_regex` / `guided_choice` / `guided_grammar` | **REMOVED and silently ignored** → output is *not* constrained | `_REMOVED_GUIDED_FIELDS` warning, `serve/engine/protocol.py:62-70`; docs say "→ `{"structured_outputs": ...}`, or `StructuredOutputsParams(...)`" — [Structured Outputs](https://docs.vllm.ai/en/latest/features/structured_outputs/) |
| `guided_decoding_backend` | **REMOVED**; docs say literally *"Remove this field from your request"* | [Structured Outputs](https://docs.vllm.ai/en/latest/features/structured_outputs/) |
| `user` | **Documented as ignored**: *"Note: `user` parameter is ignored."* | [OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/); `protocol.py:262-263` `# NOTE this will be ignored by vLLM` |
| `use_beam_search`, `length_penalty` | Honored, but only via the beam-search path | `protocol.py:266`, `:271`, `:633-650` |
| any unknown field | **Silently ignored** (`extra="allow"`) | `serve/engine/protocol.py:39` |

**[documented]** Structured-output backends: `xgrammar`, `guidance` (which links to
`guidance-ai/llguidance`), `outlines`, and `lm-format-enforcer`; selected with
`--structured-outputs-config.backend`, default `auto`. Regex dialect differs per backend
("`xgrammar`, `guidance`, and `outlines` use Rust-style regex, while `lm-format-enforcer` uses Python's
`re` module") — [Structured Outputs](https://docs.vllm.ai/en/latest/features/structured_outputs/).

**[documented]** Sampling defaults can be **overridden by the model repo's `generation_config.json`**:
*"By default, the server applies `generation_config.json` from the Hugging Face model repository if it
exists. This means the default values of certain sampling parameters can be overridden by those
recommended by the model creator. To disable this behavior, please pass `--generation-config vllm`."*
— [OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/).
**This means an omitted `temperature` does not necessarily mean OpenAI's default of 1.0.** The in-code
defaults are `{repetition_penalty: 1.0, temperature: 1.0, top_p: 1.0, top_k: 0, min_p: 0.0}`
(`protocol.py:625-631`).

### A.1.3 Reasoning models

**[documented] The field is `reasoning`, not `reasoning_content`.** From the docs, verbatim:

> !!! warning
>     `reasoning` used to be called `reasoning_content`. To migrate, directly replace
>     `reasoning_content` with `reasoning`.
>     It is important that you also update your client code. Otherwise, your client code could silently
>     read an empty `reasoning_content`, even when `reasoning` is populated.

— [Reasoning Outputs](https://docs.vllm.ai/en/latest/features/reasoning_outputs/)
([raw](https://raw.githubusercontent.com/vllm-project/vllm/main/docs/features/reasoning_outputs.md), lines 7-9)

**[source-derived]** On the *request* side vLLM accepts the legacy name and normalises it:

```python
reasoning_content = msg.pop("reasoning_content", None)
if reasoning_content is not None and msg.get("reasoning") is None:
    msg["reasoning"] = reasoning_content
```
— `chat_completion/protocol.py:542-544`. So **inbound `reasoning_content` is tolerated; outbound is
`reasoning` only** — an asymmetric rename, which is exactly what makes the failure silent in one
direction.

**[source-derived]** Full `--reasoning-parser` value list
([`vllm/reasoning/__init__.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/reasoning/__init__.py),
lines 22-159) — note the **underscore** naming convention:

`deepseek_r1`, `deepseek_v3`, `deepseek_v4`, `deepseek_v41`, `poolside_v1`, `cohere_command3`,
`cohere_command4`, `ernie45`, `gemma4`, `glm45`, `glm47`, `ling3`, `openai_gptoss`, `granite`, `holo2`,
`hunyuan_a13b`, `hy_v3`, `hy_v4`, `kimi_k2`, `kimi_k3`, `k2_horizon`, `mimo`, `minimax_m2`,
`minimax_m2_append_think`, `minimax_m3`, `mistral`, `nemotron_v3`, `olmo3`, `muse_glimmer`, `qwen3`,
`seed_oss`, `step3`, `step3p5`, `inkling`.

**[source-derived]** Request-side reasoning controls — all **vLLM-specific**, none in the OpenAI spec:

- `reasoning_effort` with a **superset** Literal: `none | minimal | low | medium | high | xhigh | max`.
  The field description itself says *"'max' is specific to the DeepSeek V4 series and is not part of the
  standard OpenAI API specification."* (`protocol.py:245-257`).
- `thinking_token_budget` (`protocol.py:258`).
- `include_reasoning: bool = True` — request-side suppression of reasoning in output
  (`protocol.py:259`; used at `serving.py:357`, `:690`, `:982`).
- `chat_template_kwargs` passthrough. **[documented]** `reasoning_effort` auto-injects
  `enable_thinking`: `low|medium|high → true`, `none → false`, unset → not injected
  ([Reasoning Outputs](https://docs.vllm.ai/en/latest/features/reasoning_outputs/)).

### A.1.4 Response envelope fidelity (vLLM)

| Field | Status | Evidence |
|---|---|---|
| `id` | **Emitted**, format `chatcmpl-<uuid>` (or `chatcmpl-<client X-Request-Id>`) | `serving.py:282-284` |
| `object` | **Emitted**: `chat.completion` / `chat.completion.chunk` | `protocol.py:128`; `serving.py:464` |
| `created` | **Emitted** (`int(time.time())`) | `serving.py:929` |
| `model` | **Emitted** | `serving.py:789` |
| `system_fingerprint` | **Emitted since PR #40537, but NOT in OpenAI's format** — see below | [issue/PR] |
| `choices[].logprobs` | **Emitted** when `logprobs=True`; `ChatCompletionLogProbs` shape | `protocol.py:82-97` |

**[issue/PR] `system_fingerprint` was added by [PR #40537](https://github.com/vllm-project/vllm/pull/40537),
merged 2026-04-27.** The default `--fingerprint-mode` is `full`, producing values like
`vllm-0.11.0-H100-tp8-bf16-a3b21f94`. The CLI help enumerates the modes verbatim:

```
- ``full`` (default): ``vllm-<version>[-<parallelism>]-<hash8>``.
- ``hash``: ``vllm-<version>-<hash8>``. Parallelism stripped.
- ``custom``: emits the literal string from ``--fingerprint-value``.
- ``none``: the field is omitted (serialized as ``null``).
```
— `vllm/entrypoints/launchers/cli_args.py:198-207`. **This is *not* an OpenAI `fp_…` fingerprint**;
code that pattern-matches OpenAI's format will not recognise it. In streaming it is stamped **only on
terminal chunks** (those carrying `finish_reason`) and on the final usage chunk
(`chat_completion/serving.py:791-800`, `:869`).

**[source-derived] `finish_reason` values.** The engine enum is
([`vllm/v1/engine/__init__.py:48-66`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/v1/engine/__init__.py)):

```python
class FinishReason(enum.IntEnum):
    STOP = 0
    LENGTH = 1
    ABORT = 2
    ERROR = 3
    REPETITION = 4
```

So the strings a client can see are **`stop`, `length`, `abort`, `repetition`**, plus:

- **`tool_calls`** — synthesised by the serving layer when a tool was actually invoked
  (`serving.py:1073-1094`).
- **`content_filter` is NOT IN THE ENUM.** vLLM **can never emit `content_filter`.** A client that
  branches on it has dead code.
- **`error`** is *not* returned as a finish_reason — it is converted into an exception and surfaced as
  an error response (`serving.py:205-212`, `:753`).
- **`streaming_complete`** appears only in request-logger output, not on the wire
  (`serving.py:903`).

**`usage` details (vLLM):** `UsageInfo` = `{prompt_tokens, total_tokens, completion_tokens,
prompt_tokens_details, completion_tokens_details}` (`serve/engine/protocol.py:135-140`).

- **`prompt_tokens_details` — including `cached_tokens` — is OFF BY DEFAULT.** `_make_prompt_tokens_details`
  returns `None` unless `enable_prompt_tokens_details` is set (`chat_completion/serving.py:90-109`), and
  the CLI default is `False`: `enable_prompt_tokens_details: bool = False` /
  `"""If set to True, enable prompt_tokens_details in usage."""` (`cli_args.py:139-140`). Clients must
  pass `--enable-prompt-tokens-details` server-side to see `cached_tokens`.
- `completion_tokens_details.reasoning_tokens` is emitted **only when a `--reasoning-parser` is
  configured** — `self._include_reasoning_tokens_details = bool(reasoning_parser)` (`serving.py:160`).
- vLLM adds **non-OpenAI** fields inside `prompt_tokens_details`: `created_cache_tokens` and
  `multimodal_tokens` (`serve/engine/protocol.py:121-128`).

### A.1.5 Tool calling (vLLM)

**[source-derived]** 50 registered `--tool-call-parser` names
([`vllm/tool_parsers/__init__.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/tool_parsers/__init__.py)):

`apertus`, `cohere_command3`, `cohere_command4`, `deepseek_v3`, `deepseek_v31`, `deepseek_v32`,
`deepseek_v4`, `deepseek_v41`, `dots`, `ernie45`, `functiongemma`, `gemma4`, `gigachat3`, `glm45`,
`glm47`, `granite`, `granite4`, `hermes`, `hunyuan_a13b`, `hy_v3`, `hy_v4`, `inkling`, `internlm`,
`jamba`, `k2_horizon`, `kimi_k2`, `kimi_k3`, `lfm2`, `ling3`, `llama3_json`, `llama4_json`,
`llama4_pythonic`, `longcat`, `mimo`, `minicpm5`, `minimax_m2`, `minimax_m3`, `mistral`,
`muse_glimmer`, `olmo3`, `openai`, `phi4_mini_json`, `poolside_v1`, `pythonic`, `qwen3_coder`,
`qwen3_xml`, `seed_oss`, `step3`, `step3p5`, `xlam`.

**[source-derived] What happens when tools are sent but no parser is configured — no error.** This is
the critical branch:

```python
if (not self.enable_auto_tools or not tool_parser_cls) and (
    not is_named_tool_choice and not is_required_tool_choice
):
    message = self._create_chat_message(
        role=role, reasoning=reasoning, content=content
    )
```
— `chat_completion/serving.py:1004-1009`

For `tool_choice: "auto"` (or defaulted-to-auto) with no `--enable-auto-tool-choice` or no
`--tool-call-parser`, the model's tool syntax is **left in `content` as raw text and no `tool_calls`
array is produced**. The request succeeds. The `tool_choice: "required"` / named-tool paths *do* parse
regardless (`:1011-1020`).

**[documented]** vLLM's own Claude Code guide makes the requirement explicit: *"you'll need to enable
tool calling explicitly with `--enable-auto-tool-choice` and the right `--tool-call-parser`"* —
[vLLM Claude Code](https://docs.vllm.ai/en/latest/serving/integrations/claude_code/).

**[source-derived] Tool-call IDs are generated**, format `chatcmpl-tool-<uuid>` by default, with a
Kimi-K2-specific `functions.<name>:<idx>` form:
```python
def make_tool_call_id(id_type: str = "random", func_name=None, idx=None):
    if id_type == "kimi_k2":
        return f"functions.{func_name}:{idx}"
    else:
        return f"chatcmpl-tool-{random_uuid()}"
```
— `vllm/entrypoints/chat_utils.py:2258-2263`.

**[documented] `parallel_tool_calls` semantics** (worth quoting because it differs in spirit from
OpenAI's guarantee): *"Setting the `parallel_tool_calls` parameter to `false` ensures vLLM only returns
zero or one tool call per request. Setting it to `true` (the default) allows returning more than one
tool call per request. There is no guarantee more than one tool call will be returned if this is set to
`true`, as that behavior is model dependent…"* — [OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/).

**Validity of `arguments` as JSON:** **[unknown]** — no source or doc statement found asserting a
guarantee. Parsers are per-model and templates vary; treat as not-guaranteed.

### A.1.6 Streaming (vLLM)

**[source-derived]**
- **`data: [DONE]` is always emitted**, including after an error:
  ```python
  except Exception as e:
      logger.exception("Error in chat completion stream generator.")
      data = self.create_streaming_error_response(e)
      yield f"data: {data}\n\n"
  # Send the final done message after all response.n are finished
  yield "data: [DONE]\n\n"
  ```
  — `chat_completion/serving.py:908-915`.
- **Final usage chunk** exists when `stream_options.include_usage=true` **or**
  `--enable-force-include-usage`; it carries `choices=[]` and a `system_fingerprint`:
  `final_usage_chunk = ChatCompletionStreamResponse(..., choices=[], model=model_name, usage=final_usage,
  system_fingerprint=self.system_fingerprint, ...)` — `serving.py:862-875`.
- `should_include_usage` returns `(True, True)` unconditionally when `--enable-force-include-usage` is
  set, otherwise derives both flags from `stream_options` — `serve/utils/api_utils.py:289-301`.
- **Errors mid-stream are signalled as an SSE data frame containing the error envelope, followed by
  `[DONE]`.** Because the HTTP `200` and `Content-Type: text/event-stream` headers are already flushed,
  a client that only inspects the HTTP status will see a *successful* response with an error body. This
  is the standard failure mode for streaming OpenAI clients against vLLM.

Other ordering issues (`usage` chunk placement, duplicate `role` deltas, missing `finish_reason` on the
last content chunk): **[unknown]** — not asserted by docs, and I did not run a live server.

### A.1.7 Server-level (vLLM)

**[documented] `--api-key` does not protect everything.** The docs carry a warning block: the key *"only
authenticates requests to endpoints under the `/v1`, `/v2`, and `/inference` path prefixes. Other
endpoints on the same HTTP server are **not** authenticated — most notably `/invocations`…"* —
[OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/).

**[source-derived]** The middleware implements exactly that:
```python
GUARDED_PREFIX = ("/v1", "/v2", "/inference", "/cohere")
```
— [`vllm/entrypoints/serve/middleware/authenticate.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/serve/middleware/authenticate.py)

**Two divergences fall out of this:**

1. **The 401 body is not the OpenAI error envelope.** It is `{"error": "Unauthorized"}`, a *string*, not
   an object: `response = JSONResponse(content={"error": "Unauthorized"}, status_code=401)`
   (`authenticate.py`).
2. **`/tokenize` and `/detokenize` are mounted at the root, not under `/v1`**, and therefore are
   **not authenticated even when `--api-key` is set**. The router registers bare paths
   (`@router.post("/tokenize")`, `@router.post("/detokenize")` —
   [`vllm/entrypoints/serve/tokenize/api_router.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/serve/tokenize/api_router.py))
   and `attach_router` does a plain `app.include_router(router)` with no prefix.

**[source-derived] `/v1` prefix is required** for chat: the router declares the full path
`@router.post("/v1/chat/completions", ...)` with no separate bare alias
([`chat_completion/api_router.py:41-42`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/openai/chat_completion/api_router.py)).
There is no `/chat/completions`.

**[source-derived] Error envelope shape.** vLLM *does* nest correctly:
```python
class ErrorInfo(OpenAIBaseModel):
    message: str
    type: str
    param: str | None = None
    code: int

class ErrorResponse(OpenAIBaseModel):
    error: ErrorInfo
```
— `serve/engine/protocol.py:79-87`. But note **`code` is typed `int` and is set to the HTTP status
value** (`code=status_code.value`, `serve/exception_handling/error_response.py`), whereas OpenAI's
`code` is a string identifier (`"invalid_api_key"`) or `null`. `type` values produced include
`BadRequestError`, `UnprocessableEntityError`, `NotFoundError`, `InternalServerError`,
`NotImplementedError`, and the HTTP status phrase for `GracefulHTTPError` (same file).

**Extra endpoints:** `/tokenize`, `/detokenize`, optional `/tokenizer_info` (gated on
`--enable-tokenizer-info-endpoint`), `/health`, `/metrics`, `/ping`, `/load`, `/v1/models`,
`/v1/chat/completions/batch`, `/invocations` (SageMaker). Health/metrics live in
`vllm/entrypoints/serve/instrumentator/`.

**Max request size:** **[unknown]** — no limit constant found in the CLI args or middleware.

**[source-derived] Chat-template behaviour:** `--chat-template` accepts a file path or an inline
string; `load_chat_template(args.chat_template)` is resolved once at startup
(`vllm/entrypoints/generate/api_router.py`). A **per-request** `chat_template` is only honored when
`--trust-request-chat-template` is set (`trust_request_chat_template=args.trust_request_chat_template`
passed into `OpenAIServingChat`). Related knobs, with defaults: `add_generation_prompt: bool = True`,
`continue_final_message: bool = False`, `add_special_tokens: bool = False` (`protocol.py:312-339`) — the
`add_special_tokens` docstring notes *"the chat template takes care of adding the special tokens so this
should be set to false (as is the default)"*.

**[documented] Multimodal:** vision and audio are supported; **`image_url.detail` is not** — *"Note:
`image_url.detail` parameter is not supported."* — [OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/).

---

## A.2 SGLang — request-field disposition

Primary evidence: [OpenAI APIs — Completions](https://docs.sglang.io/docs/basic_usage/openai_api_completions)
and [`python/sglang/srt/entrypoints/openai/protocol.py`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/entrypoints/openai/protocol.py).

### A.2.1 Extra-field policy: silently *dropped*, not logged

**[source-derived]** `ChatCompletionRequest` is a plain `BaseModel` with **no `model_config`**. The only
`extra="allow"` in the entire 2,206-line protocol file belongs to `TokenizeRequest`:

```
$ grep -n "model_config" sgl_protocol.py
1479:    model_config = ConfigDict(extra="allow")     # -> class TokenizeRequest
```

Pydantic v2's default is `extra="ignore"`. So **SGLang silently discards unknown request fields with no
log line at all** — strictly quieter than vLLM, which at least logs at `debug`. This is material for
`guided_json`-style legacy params and for genuine typos.

### A.2.2 Field-by-field disposition

| Request field | SGLang status | Evidence |
|---|---|---|
| `model` | **Required field, with a default** (`DEFAULT_MODEL_NAME`); docs state the field is not validated against the loaded model | `protocol.py:850`; [Anthropic-Compatible API](https://docs.sglang.io/docs/basic_usage/anthropic_api) states *"SGLang does not validate the request `model` field"* |
| `n` | **Honored** → `sampling_params["n"]` | `protocol.py:871`, `:1143` |
| `best_of` | **Silently dropped** — present only on `CompletionRequest`, not `ChatCompletionRequest` | `protocol.py:337` (completions) vs `:849` (chat) |
| `seed` | **Honored** → `sampling_params["sampling_seed"]` | `protocol.py:1149` |
| `presence_penalty` | **Honored** | `protocol.py:1138` |
| `frequency_penalty` | **Honored** | `protocol.py:1139` |
| `repetition_penalty` | **Honored** (falls back to `generation_config` then `1.0`) | `protocol.py:1140` |
| `logit_bias` | **Honored** — and explicitly documented with a `-100..100` range | `protocol.py:1147`; [OpenAI APIs — Completions](https://docs.sglang.io/docs/basic_usage/openai_api_completions) |
| `logprobs` / `top_logprobs` | **Honored** (present in the request model) | `protocol.py:858-859` |
| `stop` | **Honored** | `protocol.py:1132` |
| `response_format` = `text` | Honored (no constraint) | `protocol.py:1153-1168` |
| `response_format` = `json_object` | **Honored** → `json_schema = '{"type": "object"}'` | `protocol.py:1163-1164` |
| `response_format` = `json_schema` | **Honored** → converted to a schema string | `protocol.py:1153-1162` |
| `response_format` = `structural_tag` | **Honored** (XGrammar structural tags) | `protocol.py:1165-1168` |
| `regex` / `ebnf` | **Honored** (SGLang extras) | `protocol.py:1141-1142` |
| `tools` / `tool_choice` | **Honored**; `tool_choice` defaults to `"auto"` when tools present, else `"none"` | `protocol.py:994-1001` |
| `tool_choice: "required"` / named **+** `response_format`/`regex`/`ebnf` | **Errors** — *"cannot be combined … the tool-call constraint and the output constraint cannot both be honored."* | `protocol.py:1178-1187` |
| `parallel_tool_calls` | **Honored** | `protocol.py:1182`, `:1344-1350` |
| `stream_options.include_usage` | **Honored** | `protocol.py:217-220` |
| `stream_options.continuous_usage_stats` | **Honored** (SGLang extra) | `protocol.py:220` |
| `max_completion_tokens` | **Honored**, preferred over `max_tokens` | `protocol.py:1130` |
| `bad_words` | **Silently dropped** — absent from the schema | no occurrence in `protocol.py` |
| `min_p` / `top_k` / `min_tokens` | **Honored** | `protocol.py:1131`, `:1136-1137` |
| `ignore_eos` | **Honored** | `protocol.py:1145` |
| `stop_token_ids`, `stop_regex`, `no_stop_trim` | **Honored** (SGLang extras) | `protocol.py:1133-1134`, `:1144` |
| `guided_json` / `guided_regex` / `guided_choice` / `guided_grammar` | **Silently dropped** — no occurrence anywhere in `protocol.py` | grep over `sgl_protocol.py` returns nothing |
| `chat_template_kwargs` | **Honored** (documented as *the* extension mechanism) | `protocol.py:1122-1126`; [OpenAI APIs — Completions](https://docs.sglang.io/docs/basic_usage/openai_api_completions) |
| `separate_reasoning` / `stream_reasoning` | **Honored**, `separate_reasoning` defaults `True` | `protocol.py:882-883` |
| `logit_bias` reaches the engine as a real sampling param (unlike the removed vLLM family) | **Honored** | `protocol.py:1147` |

**[documented] Structured-output backends differ from vLLM's.** SGLang offers
**XGrammar (default)**, **Outlines** (`--grammar-backend outlines`), and **Llguidance**
(`--grammar-backend llguidance`, linking to `guidance-ai/llguidance`). Constraints are `json_schema`,
`regex`, or `ebnf`, and *"Only one constraint parameter (`json_schema`, `regex`, or `ebnf`) can be
specified for a request."* — [Structured Outputs](https://docs.sglang.io/docs/advanced_features/structured_outputs).

**Cross-server divergence worth flagging:** vLLM offers `lm-format-enforcer` and spells the guidance
backend `guidance`; SGLang has neither, and spells its equivalent `llguidance`. A `--guided-decoding-backend`
style config is **not portable** between them.

### A.2.3 Reasoning models

**[documented + source-derived] The output field is `reasoning_content`** — the OPPOSITE of vLLM's
current `reasoning`. Documented at [Reasoning Parser](https://docs.sglang.io/docs/advanced_features/separate_reasoning):
*"`reasoning_content`: The content of the CoT."*, with sample code reading
`response_non_stream.choices[0].message.reasoning_content` and
`chunk.choices[0].delta.reasoning_content`.

**[source-derived]** Confirmed in the schema: `ChatMessage.reasoning_content` (`protocol.py:1211`) and
`DeltaMessage.reasoning_content` (`protocol.py:1270`).

**[source-derived]** SGLang is deliberate about always serialising it, for OpenAI-SDK compatibility — a
comment in the fast SSE builder explains why:

```python
class StreamDelta(msgspec.Struct, omit_defaults=True):
    """Delta content for streaming responses.

    OpenAI Python SDK's ChoiceDelta does not declare reasoning_content; it is
    surfaced via pydantic `extra`. With omit_defaults=True, defaulting to
    None would drop the key entirely from the SSE payload, making
    `data.reasoning_content` raise AttributeError on the client. Keep it
    required (no default) so it is always serialized as null or a string.
    """
    reasoning_content: Optional[str]
```
— [`python/sglang/srt/entrypoints/openai/sse_utils.py`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/entrypoints/openai/sse_utils.py)

**[source-derived]** Full `--reasoning-parser` list
([`parser/reasoning_parser_names.py`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/parser/reasoning_parser_names.py))
— note **hyphens**, where vLLM uses underscores:

`apertus2509`, `deepseek-r1`, `deepseek-v3`, `deepseek-v4`, `deepseek-v41`, `dots`, `glm45`, `ling3`,
`hunyuan`, `gpt-oss`, `k2_horizon`, `kimi`, `kimi_k2`, `kimi_k3`, `mimo`, `muse`, `poolside_v1`, `qwen3`,
`qwen3-thinking`, `minimax`, `minimax-append-think`, `minimax-m3`, `nanbeige`, `step3`, `step3p5`,
`mistral`, `nemotron_3`, `granite_thinking_parser`, `interns1`, `gemma4`, `inkling`, `cohere_command4`,
`gigachat35`.

**Naming is not portable.** `--reasoning-parser deepseek_r1` (vLLM) vs `deepseek-r1` (SGLang);
`openai_gptoss` (vLLM) vs `gpt-oss` (SGLang); `qwen3-thinking` exists only in SGLang. A deployment
script copied between the two will fail at startup.

**[documented]** Request-side reasoning toggles are model-specific and go through
`chat_template_kwargs`: `enable_thinking` (DeepSeek-R1 distills, Qwen3), `thinking` (DeepSeek-V3.1,
Holo2). `--default-chat-template-kwargs '{"enable_thinking": false}'` sets a server-wide default, with
precedence *request → server default → template default* (except `reasoning_effort`, which *"follows a
separate normalization path"*) — [OpenAI APIs — Completions](https://docs.sglang.io/docs/basic_usage/openai_api_completions).

### A.2.4 Response envelope fidelity (SGLang)

```python
class ChatCompletionResponse(BaseModel):
    id: str
    object: str = "chat.completion"
    created: int = Field(default_factory=lambda: int(time.time()))
    model: str
    choices: List[ChatCompletionResponseChoice]
    usage: UsageInfo
    metadata: Optional[Dict[str, Any]] = None
    sglext: Optional[SglExt] = None
```
— `openai/protocol.py:1244-1253`

| Field | Status |
|---|---|
| `id`, `object`, `created`, `model` | **Emitted** |
| `system_fingerprint` | **ABSENT ENTIRELY** — `grep -rn system_fingerprint` over `protocol.py`, `serving_chat.py`, `http_server.py` returns **no matches**. SGLang never emits it. |
| `choices[].logprobs` | **Emitted** (`Optional[Union[LogProbs, ChoiceLogprobs]]`) |
| **Non-OpenAI extras** | `choices[].matched_stop`, top-level `metadata`, top-level `sglext`, and `choices[].meta_info` (when `return_meta_info`) |

**[source-derived] `finish_reason` Literal** (`protocol.py:1219-1224`, mirrored for streaming at
`:1293-1298`):

```python
finish_reason: Optional[
    Literal["stop", "length", "tool_calls", "content_filter", "function_call", "abort"]
] = None
```

**SGLang declares `content_filter` and `function_call`, which vLLM does not** — and declares neither
`repetition` nor `error`, which vLLM's enum contains. The practical mapping happens in
`serving_chat.py`: a natural stop with tool calls present is rewritten to `tool_calls`
(`:2067-2069`), and `abort` carries an HTTP status and message used to build an error
(`:2016-2029`).

**`usage` details — the single biggest billing divergence.** SGLang's usage model is:

```python
class UsageInfo(BaseModel):
    prompt_tokens: int = 0
    total_tokens: int = 0
    completion_tokens: Optional[int] = 0
    # Used to return cached tokens info when --enable-cache-report is set
    prompt_tokens_details: Optional[PromptTokensDetails] = None
    reasoning_tokens: Optional[int] = 0
```
— `openai/protocol.py:208-215`

- **`reasoning_tokens` sits at the TOP LEVEL of `usage`.** OpenAI — and vLLM — put it at
  `usage.completion_tokens_details.reasoning_tokens`. A client reading the official path gets `None`/`0`
  from SGLang. **The source comment confirms the non-standard placement is deliberate.**
- **`prompt_tokens_details.cached_tokens` requires `--enable-cache-report`** — stated verbatim in the
  source comment above, and the field defaults to `0` (`protocol.py:188`).
- `prompt_tokens_details` adds **non-OpenAI** `image_tokens`, `audio_tokens`, `video_tokens`
  (`protocol.py:188-207`).
- `CachedTokensDetails` (an SGLang extension) further breaks caching down by `device` / `host` /
  `storage` + `storage_backend` (`protocol.py:168-186`).

**Both servers share one divergence here: `cached_tokens` is off by default** — vLLM needs
`--enable-prompt-tokens-details`, SGLang needs `--enable-cache-report`. Neither flag name matches the
other.

### A.2.5 Tool calling (SGLang)

**[source-derived]** 41 registered `--tool-call-parser` names
([`function_call/parser_names.py`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/function_call/parser_names.py)):

`apertus2509`, `cohere_command4`, `deepseekv3`, `deepseekv31`, `deepseekv32`, `deepseekv4`,
`deepseekv41`, `dots`, `glm`, `glm45`, `glm47`, `gpt-oss`, `k2_horizon`, `kimi_k2`, `kimi_k3`, `lfm2`,
`ling3`, `llama3`, `mimo`, `minicpm5`, `mistral`, `muse`, `poolside_v1`, `pythonic`, `qwen`, `qwen25`,
`qwen3_coder`, `spark25`, `step3`, `step3p5`, `minimax-m2`, `minimax-m3`, `nanbeige`, `trinity`,
`interns1`, `hermes`, `hunyuan`, `gigachat3`, `gigachat35`, `gemma4`, `inkling`.

**Note the naming divergence from vLLM for the same models:** `hermes` (same), but `llama3` (SGLang) vs
`llama3_json` (vLLM), `deepseekv3` vs `deepseek_v3`, `qwen25` vs (no vLLM equivalent), `minimax-m2` vs
`minimax_m2`. §A.2.3's portability warning applies equally here.

**[source-derived] Without `--tool-call-parser`, tool calls are not parsed and arrive as raw text.**
The parse path is guarded:

```python
# Try model-specific parser when output is in native format.
if self.tool_call_parser:
    parser = FunctionCallParser(
        tools, self.tool_call_parser, tokenizer=self.tokenizer_manager.tokenizer)
```
— `serving_chat.py:2559-2562`

When the guard is false the text is passed through untouched. **[documented]** The SGLang Anthropic page
states the user-visible consequence for Claude Code: *"Without a tool-call parser, tool schemas are
still accepted but the model's tool calls come back as raw text, and Claude Code cannot execute
them."* — [Anthropic-Compatible API](https://docs.sglang.io/docs/basic_usage/anthropic_api).

**So both servers fail the same silent way**: `tools` accepted, HTTP 200, no `tool_calls`, tool syntax in
`content`. Neither errors.

### A.2.6 Streaming (SGLang)

**[source-derived]**
- **`data: [DONE]` is emitted** at the end of the stream — `serving_chat.py:2222` and
  `http_server.py:916` both `yield "data: [DONE]\n\n"`.
- The SSE frame shape is `data: {json}\n\n` (`sse_utils.py`), with `object: "chat.completion.chunk"`.
- `reasoning_content` is **always** present in each delta (null or string) by explicit design
  (`sse_utils.py` docstring quoted in §A.2.3). **vLLM's `reasoning` is not guaranteed present in the
  same way** — so a client written against SGLang will not crash on vLLM, but will read `None`.
- `continuous_usage_stats` drives per-chunk usage (`serving_chat.py:878`, `:908`, `:955`, `:983`).

### A.2.7 Server-level (SGLang)

**[source-derived] Error envelope: FLAT for chat completions.** This is a high-severity divergence. From
the two exception handlers in
[`http_server.py`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/entrypoints/http_server.py):

```python
error = ErrorResponse(
    object="error",
    message=exc.detail,
    type=str(exc.status_code),
    code=exc.status_code,
)
return ORJSONResponse(content=error.model_dump(), status_code=exc.status_code)
```
— `http_server.py:588-594`, where `ErrorResponse` is (`protocol.py:99-104`):

```python
class ErrorResponse(BaseModel):
    object: str = "error"
    message: str
    type: str
    param: Optional[str] = None
    code: int
```

**The error fields are at the top level of the JSON body, not nested under `error`.** Compare vLLM and
OpenAI, which both nest. A client doing `resp.json()["error"]["message"]` raises `KeyError`. Note also
`type=str(exc.status_code)` — the `type` field carries a **numeric string like `"400"`**, where OpenAI
uses `"invalid_request_error"`.

The handler source explicitly documents the inconsistency, and shows that **the Responses endpoint gets
the OpenAI-nested shape while chat completions does not**:

```python
    if request.url.path.startswith("/v1/responses"):
        # adapt specially, for v1/responses API only (notice the error key is different)
        nested_error = {
            "message": message,
            "type": HTTPStatus.BAD_REQUEST.phrase,
            "param": None,
            "code": HTTPStatus.BAD_REQUEST.value,
        }
        return ORJSONResponse(status_code=400, content={"error": nested_error})
```
— `http_server.py:622-630`. The docstring above the handler names the three shapes: *"For `/v1/messages`,
emit Anthropic-style envelope… For `/v1/responses`, keep OpenAI-style. Otherwise use the legacy
`ErrorResponse` shape."* (`:602-605`).

**[source-derived] Validation errors return `400`, not FastAPI's default `422`** — the handler is
`@app.exception_handler(RequestValidationError)` returning `status_code=400` (`http_server.py:597-641`).
OpenAI also uses 400, so this is a *convergence*, not a divergence — but it means `422` never appears.

**[source-derived] `/v1` prefix is required for chat completions.** The route is declared as
`@app.post("/v1/chat/completions", ...)` (`http_server.py:1718`); there is no bare
`/chat/completions`. The **native** `/generate` endpoint *is* bare (`:882`).

**[source-derived] Route inventory** (from the decorator scan of `http_server.py`), which shows how much
non-OpenAI surface is mounted at the root:

`/`, `/health`, `/health_generate`, `/ping`, `/ready`, `/get_model_info`, `/server_info`,
`/get_server_info`, `/model_info`, `/v1/models`, `/v1/models/{model:path}`, `/v1/chat/completions`,
`/v1/completions`, `/v1/embeddings`, `/v1/classify`, `/v1/score`, `/v1/audio/transcriptions`,
`/v1/messages`, `/v1/messages/count_tokens`, `/v1/responses`, `/v1/responses/{response_id}`,
`/v1/responses/{response_id}/cancel`, **`/tokenize` and `/v1/tokenize`**, **`/detokenize` and
`/v1/detokenize`**, `/generate`, `/separate_reasoning`, `/parse_function_call`, `/abort_request`,
`/pause_generation`, `/continue_generation`, `/flush_cache`, `/update_weights_from_*`,
`/init_weights_update_group`, `/destroy_weights_update_group`, `/api/chat`, `/api/generate`, `/api/tags`,
`/api/show` (Ollama-compat), `/invocations`, `/vertex_generate`.

**Note `update_weights_from_*` is reachable on the inference port** — a materially larger attack surface
than vLLM's, though gated by auth when `--api-key` is set.

**[source-derived] Auth.** `--api-key` and `--admin-api-key`, applied by
[`sglang/srt/utils/auth.py`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/utils/auth.py)
via a per-endpoint `AuthLevel` (`NORMAL` / `ADMIN_OPTIONAL` / `ADMIN_FORCE`) resolved by route matching.
`http_server.py:2564` records the compatibility contract: *"api_key only: behavior matches legacy (all
endpoints require api_key)"*. So unlike vLLM, **`--api-key` alone covers all endpoints** in single
tokenizer mode (`:2559-2574`).

**However, the 401 body is the same non-standard shape as vLLM's** — a bare string, not a nested error
object:
```python
response = ORJSONResponse(
    content={
        "error": (
            "Unauthorized"
            if decision.error_status_code == 401
            else "Forbidden"
        )
    },
    status_code=decision.error_status_code,
)
```
— [`sglang/srt/utils/auth.py:192-201`](https://raw.githubusercontent.com/sgl-project/sglang/main/python/sglang/srt/utils/auth.py).
**Both servers diverge identically here**, so an OpenAI client that parses `body["error"]["message"]`
fails to read the auth error on either — even though each server nests correctly for ordinary 4xx
errors on `/v1/chat/completions` (SGLang) or everywhere (vLLM).

**[documented] Multimodal content parts supported:** `image_url`, `video_url`, `audio_url`,
`input_audio`, plus text. **[source-derived]** confirmed by the part classes
`ChatCompletionMessageContentImagePart`, `…VideoPart`, `…AudioURLPart`, `…AudioInlinePart`
(`protocol.py:600-628`). Notably SGLang also accepts **`thinking` / `reasoning` content parts in
request messages** (`ChatCompletionMessageContentThinkingPart`, `protocol.py:557-558`, type Literal
`["thinking", "reasoning"]`) — i.e. reasoning can be replayed inbound. **vLLM renames inbound
`reasoning_content` → `reasoning`; SGLang has no such part concept.** See also
[OpenAI APIs — Vision](https://docs.sglang.io/docs/basic_usage/openai_api_vision).

### A.2.8 SGLang native `/generate` vs `/v1/chat/completions`

**[documented]** SGLang itself frames the native API as the low-level one — see
[SGLang Native APIs](https://docs.sglang.io/docs/basic_usage/native_api). **[source-derived]** The
OpenAI layer is a thin translation layer: `ChatCompletionRequest.to_sampling_params()` emits a dict
whose keys are the *native* sampling-parameter names (`max_new_tokens`, `min_new_tokens`, `sampling_seed`,
`json_schema`, `structural_tag`, `regex`, `ebnf`, `no_stop_trim`, …) — `protocol.py:1101-1205`.

Key resulting deviations of the OpenAI surface under the native one:

| Aspect | Native `/generate` | `/v1/chat/completions` |
|---|---|---|
| Prompt | `text` / `input_ids` | `messages` + chat template |
| Sampling | nested `sampling_params` object | flattened into the request body |
| Seed param name | `sampling_seed` | `seed` |
| Max tokens param name | `max_new_tokens` | `max_tokens` / `max_completion_tokens` |
| Metadata | `meta_info` (token counts, `finish_reason{type,matched}`) | reshaped into `usage` + `finish_reason` + `matched_stop` |
| Escape hatch | — | `extra_body` passthrough, `custom_params`, `sgl_ext` / `sglext` |

**Practical leak:** `finish_reason` arrives from the engine as a **dict** (`meta_info.finish_reason`
with `type` and `matched`), and the OpenAI layer projects `type` onto `finish_reason` and `matched` onto
the **non-OpenAI** `matched_stop` choice field (`serving_chat.py:2396-2399`, `:2063-2077`). The
`abort` path additionally forwards an HTTP `status_code` and message (`:2016-2029`).

---

# PART B — Anthropic Messages support

## B.1 vLLM serves `/v1/messages` — yes, unconditionally

**[source-derived]** vLLM has a full Anthropic entrypoint at `vllm/entrypoints/anthropic/`
(`protocol.py`, `serving.py`, `api_router.py`). It is registered for **every** generate-capable server
with **no enabling flag**:

```python
from vllm.entrypoints.anthropic.api_router import (
    attach_router as register_anthropic_api_router,
)
register_anthropic_api_router(app)
```
— `vllm/entrypoints/generate/api_router.py`, inside `register_generate_api_routers`

and the serving object is constructed whenever `"generate" in supported_tasks`
(`... else None`), in the same function.

**[issue/PR]** Added by [PR #22627 "Support Anthropic API /v1/messages Endpoint"](https://github.com/vllm-project/vllm/pull/22627),
**merged 2025-10-22**, closing [issue #21313](https://github.com/vllm-project/vllm/issues/21313)
(2025-07-21). The issue is itself the origin story for the third-party proxy ecosystem — see §B.3.

**Routes:** `POST /v1/messages` and `POST /v1/messages/count_tokens`
([`anthropic/api_router.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/anthropic/api_router.py)).
There is **no `/v1/complete`**.

**[documented] No dedicated Anthropic API documentation page exists.** A traversal of the docs tree for
`anthropic` returns only `docs/assets/deployment/claude-code-example.png` and
`docs/serving/integrations/claude_code.md`. The integration page asserts the capability: *"vLLM
implements the Anthropic Messages API, which is the same API that Claude Code uses…"* —
[vLLM Claude Code](https://docs.vllm.ai/en/latest/serving/integrations/claude_code/). **So the
Anthropic surface is real but effectively undocumented field-by-field; the source is the spec.**

### B.1.1 Request fields

**[source-derived]** `AnthropicMessagesRequest` is a plain `BaseModel` — **not** `OpenAIBaseModel`. There
is no `model_config`, so Pydantic's default `extra="ignore"` applies: **unknown Anthropic fields are
silently discarded.**

Declared fields (`anthropic/protocol.py`):

| Field | Notes |
|---|---|
| `model: str` | **Required**; `validate_model` rejects empty |
| `messages: list[AnthropicMessage]` | `role` Literal is `["user", "assistant", "system"]` — `"system"` is a **non-spec extension** |
| `max_tokens: int` | **Required**; `validate_max_tokens` rejects `<= 0` |
| `metadata: dict \| None` | accepted |
| `output_config: AnthropicOutputConfig \| None` | `effort` Literal `low/medium/high/xhigh/max`; `format` (json_schema) — **Claude-4.x shape** |
| `stop_sequences` | capped at `VLLM_MAX_STOP_STRINGS` |
| `stream`, `temperature`, `top_p`, `top_k` | standard |
| `system: str \| list[AnthropicContentBlock] \| None` | both official shapes |
| `tool_choice: AnthropicToolChoice \| None` | `type` Literal `auto/any/tool/none` + `disable_parallel_tool_use`; validator requires `name` when `type == "tool"` |
| `tools: list[AnthropicTool] \| None` | `name`, `description`, `input_schema` (**required**), `strict`, `defer_loading` |
| vLLM extras | `cache_salt`, `kv_transfer_params`, `ec_transfer_params`, `vllm_xargs`, `chat_template_kwargs` |

#### The critical gap: **there is no `thinking` request field**

**`thinking` does not appear anywhere in `AnthropicMessagesRequest`.** Claude Code sends
`thinking: {"type": "enabled", "budget_tokens": N}`; against vLLM this is **silently ignored**
(`extra="ignore"`), and the request still succeeds. vLLM's substituted controls are
`output_config.effort` (mapped to `reasoning_effort` at `anthropic/serving.py:516`) and the
vLLM-specific `chat_template_kwargs`.

Other officially-specified Anthropic fields that vLLM does **not** declare — `container`,
`mcp_servers`, `service_tier`, `anthropic_beta` — are likewise silently discarded.

**Content block types** (`AnthropicContentBlock.type` Literal): `text`, `image`, `tool_use`,
`tool_result`, `tool_reference`, `thinking`, `redacted_thinking`. **[unknown]** whether a request
carrying a block type *outside* this set (e.g. `document`, `search_result`) is rejected or silently
dropped — `redacted_thinking` inbound blocks are handled at `anthropic/serving.py:362-363`.

### B.1.2 Response and streaming

**[source-derived]**
```python
class AnthropicUsage(BaseModel):
    input_tokens: int
    output_tokens: int
    cache_creation_input_tokens: int | None = None
    cache_read_input_tokens: int | None = None
```
— `anthropic/protocol.py:27-32`. This matches the official usage shape.

**Response `id` is not spec-shaped:**
```python
def model_post_init(self, __context):
    if not self.id:
        self.id = f"msg_{int(time.time() * 1000)}"
```
— `anthropic/protocol.py:214-216`. **The ID is a millisecond timestamp with no random component**, so
two concurrent requests in the same millisecond collide. Anthropic's real IDs are high-entropy. The
`msg_` prefix is correct, so prefix-checking clients pass, but ID uniqueness is not guaranteed.

**`stop_reason` Literal — 4 values only:**
```python
stop_reason: (Literal["end_turn", "max_tokens", "stop_sequence", "tool_use"] | None) = None
```
(`anthropic/protocol.py:208` for the streaming delta and `:243` for the response). **Missing vs the official enum: `pause_turn`, `refusal`, and
`model_context_window_exceeded`.** The mapping is (`anthropic/serving.py:137-140`, `:635-645`):
`stop → end_turn`, `length → max_tokens`, `tool_calls → tool_use`, and a **string** stop reason becomes
`stop_sequence`.

**Streaming events.** The declared Literal is
`message_start | message_delta | message_stop | content_block_start | content_block_delta | content_block_stop | ping | error`
(`protocol.py:172-181`). **[source-derived]** but scanning the emission sites in `serving.py` shows
**`ping` is declared and never sent** — every other type is emitted at least once
(`serving.py:743-1020`). Since `ping` is optional in the spec this is harmless, but the declared/emitted
asymmetry means an OpenAPI-generated client will wait for a `ping` that never arrives.

**Delta types:** `text_delta`, `input_json_delta`, `thinking_delta`, `signature_delta`
(`protocol.py:138-146`). **`citations_delta` is missing.**

**SSE framing is correct and includes the `event:` line:**
```python
def wrap_data_with_event(data: str, event: str):
    return f"event: {event}\ndata: {data}\n\n"
```
— `anthropic/serving.py:94-95`. This matters: the Anthropic SDK keys off the `event:` field.

**⚠️ Thinking signatures are fabricated.** When a thinking block is emitted, vLLM generates
`signature=uuid.uuid4().hex` (`anthropic/serving.py:653`). Anthropic's `signature` is a cryptographic
attestation. A client that round-trips the signature back (Claude Code does, on multi-turn) is sending a
value that no verifier — including vLLM itself, and including the real Anthropic API if a session is
ever migrated — can validate. **The signature is not meaningful; treat it as opaque-and-fake.**

**`count_tokens`** is implemented: `AnthropicCountTokensRequest` / `AnthropicCountTokensResponse`
(`input_tokens`, optional `context_management.original_input_tokens`).

**Error envelope is Anthropic-shaped** — this one is done correctly:
```python
class AnthropicErrorResponse(BaseModel):
    type: Literal["error"] = "error"
    error: AnthropicError
```
— `protocol.py:18-22`, applied by `translate_error_response` in `api_router.py`.

**What vLLM's Anthropic layer reuses from the OpenAI stack:** `AnthropicServingMessages` subclasses
`OpenAIServingChat` (`serving.py:98`) and is constructed with the **same**
`--enable-auto-tool-choice`, `--tool-call-parser`, and `--reasoning-parser` settings
(`generate/api_router.py`). **Consequence: `--reasoning-parser` and `--tool-call-parser` are mandatory
for a useful Anthropic endpoint, exactly as for the OpenAI path** — and the failure mode when they are
missing is the same silent raw-text one (§A.1.5). **[issue/PR]** The PR body confirms the design intent:
*"Compatibale with all existed tool call parser in OpenAI API"* —
[PR #22627](https://github.com/vllm-project/vllm/pull/22627).

**Known open bug:** [PR #35557 "Fix Anthropic API base64 image handling in Messages endpoint"](https://github.com/vllm-project/vllm/pull/35557)
indicates base64 image handling in this endpoint has been defective.

**`/v1/messages` and `--api-key`:** the path starts with `/v1`, so it **is** inside `GUARDED_PREFIX` and
**is** authenticated (`serve/middleware/authenticate.py`).

---

## B.2 SGLang serves `/v1/messages` — yes, and it is documented

**[documented]** SGLang documents the endpoint at
[Anthropic-Compatible API](https://docs.sglang.io/docs/basic_usage/anthropic_api):

> SGLang ships an Anthropic-compatible `/v1/messages` endpoint so any client built for the Anthropic
> Messages API — including the Anthropic SDKs and agentic CLIs such as Claude Code — can talk to a
> self-hosted SGLang server without changes.
> …
> The endpoint is registered automatically on every SGLang server; no extra flag is required to enable
> it. It reuses the same model, chat template, and reasoning / tool-call parsers as the
> OpenAI-compatible endpoint, and supports both non-streaming and streaming responses, tool use, and a
> `count_tokens` route.

**[source-derived]** Confirmed by the route list: `/v1/messages` and `/v1/messages/count_tokens`
(`http_server.py`).

**[issue/PR]** Introduced by [PR #18630 "[FEAT] Add Anthropic compatible API endpoint"](https://github.com/sgl-project/sglang/pull/18630),
**merged 2026-02-21**, described as *"A translation layer (`AnthropicServing`) that delegates to
`OpenAIServingChat` internally."* Thinking support was added by
[PR #19334](https://github.com/sgl-project/sglang/pull/19334) (2026-02-25, closed unmerged) and
[PR #22135](https://github.com/sgl-project/sglang/pull/22135) (2026-04-05), plus
[PR #21902](https://github.com/sgl-project/sglang/pull/21902) (2026-04-02).

### B.2.1 Request fields — SGLang is materially closer to the current Anthropic spec

**[source-derived]** `AnthropicMessagesRequest` (`anthropic/protocol.py:360-379`), a discriminated-union
design that the module docstring says *"Mirrors the shape of the official Anthropic Python SDK"*:

| Field | Notes |
|---|---|
| `model`, `messages`, `max_tokens`, `metadata`, `stop_sequences`, `stream`, `system`, `temperature`, `top_k`, `top_p`, `tools`, `tool_choice` | standard; `max_tokens` required and `> 0` |
| **`thinking: AnthropicThinkingParam`** | `type` Literal **`["enabled", "disabled", "adaptive"]`** |
| **`output_config: AnthropicOutputConfig`** | `effort` (`minimal…max`) and **`task_budget`** |
| **`betas: list[str]`** | *"Claude 4.7 fields. The Anthropic SDK / Claude Code attach these even when targeting non-Anthropic backends, so the schema must accept them."* |

**This is the decisive advantage over vLLM: SGLang actually accepts `thinking`.** Its semantics are
honestly documented in the source rather than overclaimed:

```python
    The serving layer treats ``adaptive`` identically to ``enabled``
    because the local OpenAI-compatible backend has no auto-throttle
    equivalent. ``budget_tokens`` is accepted on ``enabled`` for SDK
    compatibility but the backend has no hard-cap knob to honor it; the
    serving layer logs a WARNING so operators see that the requested
    budget is not enforced. ``display="omitted"`` is accepted but
    similarly cannot suppress reasoning mid-stream and is logged.
```
— `anthropic/protocol.py:265-273`

Validation mirrors the SDK's discriminated variants: `enabled` **requires** `budget_tokens >= 1024`;
`disabled` **forbids** `budget_tokens` (`:283-299`). `output_config.effort` maps to OpenAI
`reasoning_effort` with **`xhigh` → `max`** because the OpenAI Literal lacks `xhigh` (`:333-343`).

**Content block types are broader than vLLM's:** `text`, `image`, `tool_use`, `tool_result`,
`tool_reference`, **`search_result`**, `thinking`, `redacted_thinking` (`protocol.py:55-110`).
Non-spec extras include `ToolReferenceBlock` (documented as *"sglang extension: references a
deferred-loaded tool by name"*) and `SearchResultBlock`.

**Tool definitions cover Anthropic's server-tool families:** `AnthropicCustomTool`, plus
`AnthropicWebSearchTool` (`^web_search_\d{8}$`), `AnthropicComputerTool` (`^computer_\d{8}$`),
`AnthropicBashTool` (`^bash_\d{8}$`), `AnthropicTextEditorTool` (`^text_editor_\d{8}$`)
(`protocol.py:130-200`). **[source-derived]** however, built-in server tools are **not executed** —
there is an explicit skip with a log: `"Skipping built-in Anthropic server tool %r (type=%r): …"`
(`anthropic/serving.py:667`). So accepting the schema does not mean honoring its behaviour.

**`tool_choice`** includes `none` in addition to the official `auto`/`any`/`tool`, plus
`disable_parallel_tool_use` (`protocol.py:251-253`).

### B.2.2 Response and streaming

**Response `id` is properly random:** `id: str = Field(default_factory=lambda: f"msg_{uuid.uuid4().hex}")`
(`protocol.py:503`). Same for the streaming path (`anthropic/serving.py:858`, `:1325`). **This is a real
correctness advantage over vLLM's timestamp ID.**

**`stop_reason` is the same 4-value Literal** (`end_turn`, `max_tokens`, `stop_sequence`, `tool_use`) —
so **SGLang shares vLLM's missing `pause_turn` / `refusal` / `model_context_window_exceeded`.** The
mapping is honest about the lossy edges:

```python
# values in ``AnthropicMessagesResponse.stop_reason``'s Literal are valid
# on the wire; ``content_filter`` and ``abort`` have no perfect mapping
# so they fall through to the ``end_turn`` default with a WARNING at the
...
STOP_REASON_MAP = {
    "stop": "end_turn",
    "length": "max_tokens",
    "tool_calls": "tool_use",
}
```
— `anthropic/serving.py:64-72`, applied at `:1075` and `:1316`. **Note the consequence: an `abort` or
`content_filter` becomes a normal-looking `end_turn`.** A client cannot distinguish a clean finish from
an aborted generation via `stop_reason`.

**Usage:** `AnthropicUsage` mirrors the official shape, with `input_tokens`/`output_tokens` typed
Optional for a well-reasoned compatibility purpose:

```python
    ``input_tokens``/``output_tokens`` are ``Optional`` because Anthropic's
    streaming ``message_delta`` event omits ``input_tokens`` (the spec
    requires it only on ``message_start``). Non-streaming responses set both.
```
— `protocol.py:31-37`. The implementation also **clamps rather than trusts** a `cached_tokens >
prompt_tokens` inconsistency, logging *"Cached tokens (%d) exceed prompt tokens (%d); clamping …"*
(`anthropic/serving.py:98-101`) — a defensive detail vLLM lacks.

**Streaming events** — same set: `message_start`, `content_block_start`, `content_block_delta`,
`content_block_stop`, `message_delta`, `message_stop`, `ping`, `error` (`protocol.py:483-499`).
**`ping` is again declared but never emitted** (`grep -n "PingEvent"` finds only the class definition).
Delta types: `text_delta`, `input_json_delta`, `thinking_delta`, `signature_delta` — **no
`citations_delta`, same gap as vLLM.** Framing is correct:
`return f"event: {event_type}\ndata: {data}\n\n"` (`anthropic/serving.py:158`).

**SGLang handles thinking signatures more safely than vLLM.** It emits `signature_delta` **only when a
real signature was captured**, deliberately:
```python
    # Only emit signature_delta when a real signature is available.
    # Anthropic's spec treats absence as "unsigned thinking"; an
    # empty-string signature would fail downstream verifiers.
    if content_block_type == "thinking" and captured_thinking_signature:
```
— `anthropic/serving.py:889-898`. **vLLM invents a UUID; SGLang omits rather than fabricates.** For a
verifier, absence is well-defined and fabrication is not.

**Also note:** SGLang **drops prior-turn thinking history** on the request side
(`"Dropping prior-turn thinking history (%d blocks): %s"`, `anthropic/serving.py:406`). This is not
official behaviour, but it is at least logged.

**Error envelope:** Anthropic-shaped and *scrubbed*. `_anthropic_error_response` is used for validation
errors on this path, and the module notes *"5xx is always generic — never echo upstream `str(e)`
payloads, which may contain stack frames, file paths, or PII"* (`http_server.py:602-612`;
`anthropic/serving.py` `_scrub_error_message`). **This is stronger than vLLM's Anthropic error path,
which passes `response.error.type` straight through.**

### B.2.3 The prefix-cache trap (both servers, Claude Code specific)

**[documented]** Claude Code prepends a per-request attribution block
`x-anthropic-billing-header: cc_version=<ver>.<per-request-hash>; …; cch=<hash>;` to the **start** of the
system prompt. Because the first differing token is at position ~0, radix/prefix caching is defeated and
the whole history is re-prefilled every turn. SGLang's documented fix is
`CLAUDE_CODE_ATTRIBUTION_HEADER=0` — [Anthropic-Compatible API](https://docs.sglang.io/docs/basic_usage/anthropic_api).

**[issue/PR]** [PR #21064 "[Anthropic API] Strip billing header to fix prefix caching"](https://github.com/sgl-project/sglang/pull/21064)
(2026-03-21) attacks this **server-side**, stripping the block before it reaches the model, with measured
impact:

| Metric | Before | After |
|---|---|---|
| Cached tokens per request | ~4,800 | ~20,800+ |
| New tokens to prefill per turn | ~82,000 | ~285 |

**[documented]** vLLM addresses the same problem but frames it as *already fixed*: *"This is addressed
automatically in vLLM versions > 0.17.1 but for older versions `CLAUDE_CODE_ATTRIBUTION_HEADER: 0`
should be added"* — [vLLM Claude Code](https://docs.vllm.ai/en/latest/serving/integrations/claude_code/).

**Practical upshot:** on either server, a Claude-Code client that does **not** set
`CLAUDE_CODE_ATTRIBUTION_HEADER=0` against an older build pays a full re-prefill per turn. The SGLang
docs also warn that `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` **does not** cover this — *"The
attribution header is a separate code path"*.

### B.2.4 Claude Code configuration notes (SGLang doc, applies to both)

**[documented]** From [Anthropic-Compatible API](https://docs.sglang.io/docs/basic_usage/anthropic_api),
all of which is server-agnostic:

- `ANTHROPIC_BASE_URL` is the **server root without `/v1`** — the Anthropic SDK appends `/v1/messages`
  itself. (Contrast the OpenAI SDK, where the base URL *does* include `/v1`.) **This is the single most
  common misconfiguration.**
- `ANTHROPIC_AUTH_TOKEN` must be a non-empty string; any value works on a server launched without
  `--api-key`.
- The `[1m]` model-name suffix is a **client-side hint** that enables Claude Code's 1M-context beta.
  *"SGLang does not validate the request `model` field, so Claude Code can send any name."*
- `API_TIMEOUT_MS` should be raised — reasoning + long context routinely exceed the default.
- *"A reasoning model may emit a `thinking` block before the `text` block — pick the text block rather
  than assuming `content[0]`."*
- **vLLM-specific counterpart:** model names containing `/` cannot be used with Claude Code, so
  `--served-model-name` is required — *"You cannot use model names with `/` in them, such as
  `openai/gpt-oss-120b` directly from Huggingface"* — [vLLM Claude Code](https://docs.vllm.ai/en/latest/serving/integrations/claude_code/).

---

## B.3 Third-party proxies and what they paper over

**[issue/PR]** The proxy ecosystem is the *reason* native support exists. vLLM
[issue #21313](https://github.com/vllm-project/vllm/issues/21313) (2025-07-21) reads: *"There are routers
that implement the endpoint by wrapping the OpenAI api server from vLLM, like
https://github.com/musistudio/claude-code-router and https://github.com/1rgs/claude-code-proxy."*

| Project | Status | What it does / papers over |
|---|---|---|
| [musistudio/claude-code-router](https://github.com/musistudio/claude-code-router) | Active, ~37.4k★, TypeScript, last push 2026-09-20. Described as *"One local control plane for every AI agent"* | Anthropic↔OpenAI translation + routing/fusion. Widest adoption; has outgrown its original narrow purpose. |
| [1rgs/claude-code-proxy](https://github.com/1rgs/claude-code-proxy) | Active, ~3.7k★, Python, last push 2026-06-23. *"Run Claude Code on OpenAI models"* | Minimal Anthropic→OpenAI proxy. |
| [luohy15/y-router](https://github.com/luohy15/y-router) | **ARCHIVED**, ~384★, TypeScript, last push 2026-01-11 | Cloudflare Worker translating Anthropic→OpenAI for OpenRouter. **No longer maintained — do not build on it.** |
| [dynamicheart/claude-code-toolkit](https://github.com/dynamicheart/claude-code-toolkit) | Niche, 0★, Shell, last push 2026-04-15 | Explicitly targets *"self-hosted LLM services (vLLM/SGLang) — debug tool_use"*. Useful as a diagnostic harness, not a production proxy. |
| **LiteLLM** | Active, documented | The most rigorous option, and the only one with an explicit compatibility contract. |

### What LiteLLM papers over — and where it stops

LiteLLM's [Native `/v1/messages` and `/v1/responses` Passthrough](https://docs.litellm.ai/docs/anthropic_unified/native_passthrough)
page is the clearest published statement of the translation losses, and should be read as a **checklist
of exactly what the proxies hide**:

> When a deployment's provider has no native Anthropic Messages support, LiteLLM translates each
> `/v1/messages` request into the provider's own API: `openai/` deployments go through the OpenAI
> Responses API and everything else goes through `/v1/chat/completions`. That translation only keeps
> what the target API can express: **`cache_control` blocks are dropped, `thinking` is mapped to the
> provider's own reasoning parameter, and other Anthropic-only request details are approximated or
> lost.**

Three concrete papered-over issues:

1. **`cache_control` `ttl` breaks strict backends.** *"Strict implementations of the Messages API
   reject Anthropic-only `cache_control` extensions such as `ttl` with `cache_control.ttl: 1h is not
   supported`, and clients like Claude Code send `{"type": "ephemeral", "ttl": "1h"}` on every prompt
   block whenever 1h prompt caching is on."* LiteLLM's default is to **reduce every `cache_control` to
   `{"type": "ephemeral"}`** — i.e. the 1h cache TTL silently becomes the default 5m. Opaque caching
   semantics loss.
2. **`previous_response_id` against stateless backends fails.** *"OpenAI-compatible servers that do not
   store responses reject it, typically with a 400… Against such a backend either send the full
   conversation history in `input` on every turn, or leave the opt-in off and use the bridged path…"*
   **This is precisely the vLLM `store`-silently-ignored problem (§C.1) and the SGLang
   `--enable-response-store` requirement (§C.2), surfaced as a client-visible 400.**
3. **Native passthrough is opt-in and recent.** `/v1/messages` passthrough *"Available from v1.92.0"*,
   `/v1/responses` *"Available from v1.102.0"*; both require listing the endpoint in
   `model_info.supported_endpoints`. **Without the opt-in you silently get the lossy translation path.**

**Known LiteLLM bug in exactly this area:** [issue #29518](https://github.com/BerriAI/litellm/issues/29518)
— *"/v1/messages adapter drops reasoning_content → thinking blocks for OpenAI-compatible
chat-completions backends (streaming)"*. This is the reasoning-field problem of §0(1) resurfacing in the
proxy layer, and it is unfixed as of the cited report.

**Summary of the proxy trade-off:** a proxy lets a Claude-Code client talk to a server with **no**
native Anthropic endpoint, at the cost of (a) dropped `cache_control` semantics, (b) approximated
thinking, (c) a bridge through `/v1/chat/completions` that *inherits every Part A divergence above* —
including the silent `tools`-without-parser raw-text failure and the usage-field mismatch. Since **both
vLLM and SGLang now serve `/v1/messages` natively**, a proxy is only justified for routing,
multi-backend load-balancing, or auth/spend control — **not** for protocol translation.

---

# PART C — Responses API support

## C.1 vLLM `/v1/responses`

**Status: implemented and documented, but NOT marked experimental in the docs — and it silently ignores
`store`.**

**[documented]** `/v1/responses` is listed as a first-class supported API alongside `/v1/responses/{response_id}`
and `/v1/responses/{response_id}/cancel` in
[OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/).
The doc states *"Our Responses API is compatible with OpenAI's Responses API; you can use the official
OpenAI Python client to interact with it."*

**On "experimental": I found NO experimental/beta marker.** A keyword scan of the compatible-server doc,
the reasoning doc, and the tool-calling doc for `experimental|beta|Experimental` returns nothing.
**So the honest answer is: vLLM does not label it experimental in these docs — it labels it
"compatible".** The word "experimental" is often attributed to this endpoint; I could not substantiate
it. **[unknown]** whether some older version or another docs page carried that label.

**[issue/PR]** The closest thing to a maturity caveat is
[issue #32850 "[RFC]: Clarify policy for Open Responses API extensions in vLLM"](https://github.com/vllm-project/vllm/issues/32850)
(2026-01-22, closed stale 2026-06-02), which states *"vLLM recently added support for the
OpenAI-compatible `/v1/responses` API"* and that vLLM *"is already extending the Responses API beyond
the vanilla spec."* It enumerated a Chat-Completions/Responses feature gap. **Caveat: the issue is dated
2026-01, and the current source has since closed part of that gap** — `presence_penalty`,
`frequency_penalty`, `repetition_penalty`, `seed`, `stop`, `ignore_eos` are now present in
`ResponsesRequest`.

**[documented + issue/PR]** Codex is supported as a first-class target:
[vLLM Codex](https://docs.vllm.ai/en/latest/serving/integrations/codex/) configures
`wire_api = "responses"` and notes *"When using the `responses` API, ensure your vLLM version supports
the OpenAI Responses API."* The doc's own example uses
`--reasoning-parser qwen3 --enable-auto-tool-choice --tool-call-parser qwen3_coder`.

### C.1.1 Request fields

**[source-derived]** `ResponsesRequest` (`openai/responses/protocol.py`, class begins line 4264):

`background`, `include`, `input`, `instructions`, `max_output_tokens`, `max_tool_calls`, `metadata`,
`model`, `logit_bias`, `parallel_tool_calls`, `previous_response_id`, `prompt`, `reasoning`,
`include_reasoning`, `service_tier` (`auto|default|flex|scale|priority`), `store`, `stream`,
`temperature`, `text`, `tool_choice`, `tools`, `top_logprobs`, `top_p`, `top_k`, `truncation`
(`auto|disabled`), `user`, `skip_special_tokens`, `include_stop_str_in_output`, `presence_penalty`,
`frequency_penalty`, `prompt_cache_key`, `watermarking`.

**vLLM-specific extensions**, from the `responses-extra-params` docs region
(`protocol.py:216-314`, which is what the docs page inlines via mkdocs snippet include): `request_id`
(`resp_<uuid>`), `session_id`, `media_io_kwargs`, `mm_processor_kwargs`, `priority`, `cache_salt`,
`enable_response_messages`, `previous_input_messages`, `structured_outputs`, `repetition_penalty`,
`seed`, `stop`, `ignore_eos`, `vllm_xargs`, `kv_transfer_params`, `ec_transfer_params`,
`chat_template_kwargs`. Plus, on the response object, `input_messages` / `output_messages` when
`enable_response_messages=true` (`protocol.py:723-739`).

**`tools` families:** `mcp` and `code_interpreter` and `web_search` have dedicated event emitters
(§C.1.2), so they are at least partially wired; `file_search` emits no events and is **[unknown]**.
`logprobs` are explicitly rejected for gpt-oss: `message="logprobs are not supported with gpt-oss models"`
(`responses/serving.py:253`).

### C.1.2 The `store` problem — a silent, memory-leaking no-op

**[source-derived] This is the most consequential vLLM Responses divergence.** `store` defaults to
`true` in the request model, but is **silently ignored unless an environment variable is set**:

```python
# If False (default), the "store" option is (silently) ignored and the
...
self.enable_store = envs.VLLM_ENABLE_RESPONSES_API_STORE
```
— `openai/responses/serving.py:155-160`

The code then deliberately downgrades the request rather than erroring:
```python
if request.store and not self.enable_store:
    # Disable the store option.
    ...
    # we assume most users do not intend to actually store the response
    # (i.e., their request's `store=True` just because it's the default
```
— `responses/serving.py:337-342`

**Consequences:**
1. A client that sends `store: true` (which is OpenAI's default) and later calls
   `GET /v1/responses/{id}` or sets `previous_response_id` **gets behaviour that depends on an env var
   it cannot see**. `store=true` is acknowledged and discarded.
2. The only hard error is the *combination* `store && !enable_store && background`
   (`responses/serving.py:257-261`). `previous_response_id` + `previous_input_messages` together is also
   rejected (`:270-274`).
3. **The in-memory store is admittedly unsound.** The source carries three explicit
   `HACK(woosuk)` / `FIXME` blocks — *"If `enable_store=True`, this may cause a memory leak since we
   never remove responses from the store"* (`:175-178`), and equivalents for `msg_store` (`:181-184`)
   and `event_store` (`:186-189`). **Enabling the store trades correctness for an unbounded leak.**

**This is the single most likely Responses divergence to break a Codex-style client:** Codex sends
`store` semantics and reuses conversation state. Against vLLM the statefulness is silently absent.

### C.1.3 SSE event coverage

**[source-derived]** Event-type strings in
[`responses/streaming_events.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/openai/responses/streaming_events.py),
plus the lifecycle events constructed in
[`responses/serving.py`](https://raw.githubusercontent.com/vllm-project/vllm/main/vllm/entrypoints/openai/responses/serving.py):

**Emitted — lifecycle:** `response.created` (`serving.py:1344`), `response.in_progress` (`:1351`),
`response.completed` (`:1395`).

**Emitted — content:** `response.output_item.added`, `response.output_item.done`,
`response.content_part.added`, `response.content_part.done`, `response.output_text.delta`,
`response.output_text.done`.

**Emitted — reasoning:** `response.reasoning_part.added`, `response.reasoning_part.done`,
`response.reasoning_text.delta`, `response.reasoning_text.done`.

**Emitted — tools:** `response.function_call_arguments.delta`, `response.function_call_arguments.done`,
`response.mcp_call.in_progress`, `response.mcp_call.completed`, `response.mcp_call_arguments.delta`,
`response.mcp_call_arguments.done`, `response.web_search_call.in_progress`,
`response.web_search_call.searching`, `response.web_search_call.completed`,
`response.code_interpreter_call.in_progress`, `response.code_interpreter_call.interpreting`,
`response.code_interpreter_call.completed`, `response.code_interpreter_call_code.delta`,
`response.code_interpreter_call_code.done`.

**MISSING vs the official OpenAI event set** (verified absent by keyword scan):

- `response.queued`
- **`response.failed`**
- **`response.incomplete`**
- **`response.reasoning_summary_text.delta` / `.done`**
- **`response.reasoning_summary_part.added` / `.done`**
- `response.refusal.delta` / `response.refusal.done`
- `response.output_text.annotation.added`

**NON-STANDARD events vLLM emits:** `response.reasoning_part.added` and `response.reasoning_part.done`.
OpenAI's equivalent family is `response.reasoning_summary_part.*`. **A client that switches on the
official reasoning-summary event names will see reasoning events it does not recognise and will miss the
summary entirely — it only gets `response.reasoning_text.*`.**

**Practical impact of the missing `response.failed` / `response.incomplete`:** an OpenAI-SDK client that
relies on those terminal events to close out a stream will never receive them from vLLM; it must fall
back to `response.completed` or to the transport closing. Combined with `store` being off, there is no
reliable way to distinguish "completed" from "failed" other than by parsing accumulated content.

**`usage` on the terminal event:** **[source-derived]** the terminal `response.completed` carries the
full response object; vLLM's `UsageInfo` gating from §A.1.4 applies — `input_tokens_details.cached_tokens`
requires `--enable-prompt-tokens-details`, and `output_tokens_details.reasoning_tokens` requires a
`--reasoning-parser`.

**Related history:** [issue #23222 "[Feature][Responses API] Stream Function Call"](https://github.com/vllm-project/vllm/issues/23222)
(2025-08-20, closed stale 2026-04-03) documented that function calls were not streamed at all. The
current `streaming_events.py` does emit `response.function_call_arguments.delta/done`, so **this gap has
since been closed** — a good illustration of why the datable PRs and issue dates matter when judging
main.

## C.2 SGLang `/v1/responses`

**Status: implemented, broader SSE coverage than vLLM, and COMPLETELY UNDOCUMENTED.**

**[documented — by absence]** `https://docs.sglang.io/llms.txt` is SGLang's own documentation index, and
a grep for `respons` returns **no entry**. There is **no Responses API page in the SGLang docs.**

**[source-derived]** Nevertheless the endpoint is real and substantial: `serving_responses.py` is
**115,518 bytes**, with `responses_adapters.py` alongside. Routes are declared unconditionally in
`http_server.py`: `@app.post("/v1/responses")` (`:1904`), `@app.get("/v1/responses/{response_id}")`
(`:1923`), `@app.post("/v1/responses/{response_id}/cancel")` (`:1931`).

**[source-derived] Endpoint availability is best-effort.** Construction is wrapped in `try/except`;
on failure the state object is never attached and the route handler will fail at attribute access:
```python
    except Exception as e:
        # Optional endpoint; a load failure (e.g. the gpt-oss harmony vocab
        # download) must not look like a fatal error. One-line WARNING, full
        # traceback at DEBUG.
        logger.warning(
            f"OpenAI Responses API (/v1/responses) disabled: "
            f"OpenAIServingResponses init failed ({type(e).__name__}: {e})"
        )
```
— `http_server.py:370-379`. **So `/v1/responses` can silently not exist on an otherwise healthy server,
signalled only by one WARNING line at startup.** A client sees a 500, not a 404.

**[issue/PR]** Introduced by [PR #8837 "Support v1/responses and use harmony in serving_chat"](https://github.com/sgl-project/sglang/pull/8837)
(merged 2025-08-06), then corrected toward spec by
[PR #9624 "Update `v1/responses` to be more OpenAI-compatible"](https://github.com/sgl-project/sglang/pull/9624)
(merged 2025-10-05), with router/state work in #10487, #10581, #11926, #12153, #12386.

### C.2.1 Request fields and statefulness

**[source-derived]** `ResponsesRequest` (`openai/protocol.py:1622-1667`): `background`, `include`
(with six Literal values), `input`, `instructions`, `max_output_tokens`, `max_tool_calls`, `metadata`,
`model` (*"Made optional to match vLLM"* — an explicit cross-implementation compatibility note),
`parallel_tool_calls`, `previous_response_id`, `reasoning`, `service_tier`, `store`, `stream`,
`temperature`, `text`, `tool_choice`, `tools`, `top_logprobs`, `top_p`, `truncation`, `user`, plus
`chat_template_kwargs` and `request_id` (`resp_<uuid>`).

**Statefulness requires a flag — and fails LOUDLY, unlike vLLM:**
```python
def _response_store_disabled_error(self, param: str) -> ORJSONResponse:
    "Response store is disabled. Stateful Responses require "
    "--enable-response-store on a standalone server; response storage "
...
if not self.enable_response_store and request.previous_response_id is not None:
    return self._response_store_disabled_error("previous_response_id")
if not self.enable_response_store and request.background:
    return self._response_store_disabled_error("background")
if request.background and not request.store:
    ... "background=true requires store=true."
```
— `openai/serving_responses.py:250-300`

**This is the correct design and is the mirror image of vLLM's behaviour:** vLLM silently ignores
`store`; SGLang returns an explicit error naming the flag. For a Codex-style client, **a loud 400 is far
easier to diagnose than a silent state loss.**

**[source-derived]** `enable_prompt_tokens_details=True` is **hardcoded** for the Responses path
(`http_server.py:366`), so `input_tokens_details.cached_tokens` **is** populated here — unlike the
SGLang chat path, which needs `--enable-cache-report`.

### C.2.2 SSE event coverage — SGLang emits MORE of the official set than vLLM

**[source-derived]** Event strings in `serving_responses.py`. Emitted:

**Lifecycle:** `response.created`, `response.in_progress`, `response.completed`,
**`response.failed`**, **`response.incomplete`**.

**Content:** `response.output_item.added` / `.done`, `response.content_part.added` / `.done`,
`response.output_text.delta` / `.done`.

**Reasoning:** **`response.reasoning_summary_part.added` / `.done`**,
**`response.reasoning_summary_text.delta` / `.done`**, `response.reasoning_text.delta` / `.done`.

**Tools:** `response.function_call_arguments.delta` / `.done`,
**`response.custom_tool_call_input.delta` / `.done`**, `response.code_interpreter_call_code.delta` / `.done`.

**Against vLLM this is a strict improvement on the official event set:** SGLang has
`response.failed`, `response.incomplete`, and the whole `response.reasoning_summary_*` family, all of
which vLLM lacks. SGLang does **not** appear to emit vLLM's non-standard `response.reasoning_part.*`.

**Net:** if a Codex-style client needs faithful terminal-event semantics, **SGLang's Responses
implementation is closer to the OpenAI spec than vLLM's** — despite being entirely undocumented. The
trade is documentation and stability signalling (one WARNING line can disable the endpoint) against
event fidelity.

---

# Ranked divergences most likely to break a Codex / Claude-Code-style client

Ranked by *(probability the client hits it)* × *(severity when it does)* × *(silence of the failure)*.

### 1. vLLM renamed the reasoning field to `reasoning`; SGLang still uses `reasoning_content`
**Impact: total loss of chain-of-thought display, silently.** vLLM's own doc warns the client *"could
silently read an empty `reasoning_content`, even when `reasoning` is populated"*
([Reasoning Outputs](https://docs.vllm.ai/en/latest/features/reasoning_outputs/)). A client written for
one server breaks on the other with **no error, no missing field, just empty reasoning** — the worst
possible failure signature. Compounded by LiteLLM [issue #29518](https://github.com/BerriAI/litellm/issues/29518),
which drops `reasoning_content`→`thinking` blocks in the proxy layer for streaming. **Affects both
Claude Code (thinking blocks) and Codex (reasoning items).**

### 2. Tools accepted but never parsed when `--tool-call-parser` / `--enable-auto-tool-choice` is missing
**Impact: the agent silently stops being an agent.** Both servers return `HTTP 200` with the model's tool
syntax left in `content` and **no `tool_calls` array** (vLLM `chat_completion/serving.py:1004-1009`;
SGLang `serving_chat.py:2559-2562`). vLLM adds a further trap: even *with* a tool parser,
`tool_choice: "auto"` requires `--enable-auto-tool-choice`, while `required`/named work without it
(`serving.py:1011-1035`) — so behaviour differs by `tool_choice` value in a way no client would predict.
Claude Code's documented symptom is *"tool calls come back as raw text, and Claude Code cannot execute
them"* ([SGLang](https://docs.sglang.io/docs/basic_usage/anthropic_api)). **A misconfigured server looks
healthy and produces plausible prose instead of failures.**

### 3. vLLM silently ignores `store` on `/v1/responses`
**Impact: Codex loses all conversation state with no error.** `store` defaults to `true` and is discarded
unless `VLLM_ENABLE_RESPONSES_API_STORE` is set (`responses/serving.py:155-160`, `:337-342`). The store
is also a documented unbounded memory leak when enabled (three `FIXME` blocks, `:175-189`). Codex is
*the* Responses client and relies on this. **SGLang's equivalent failure is a loud 400 naming the
flag — which is why this ranks above it.**

### 4. SGLang reports reasoning tokens at `usage.reasoning_tokens`, not `completion_tokens_details.reasoning_tokens`
**Impact: silent accounting drift.** `protocol.py:208-215` places `reasoning_tokens` at the **top level**
of `usage`, where OpenAI and vLLM place it under `completion_tokens_details`. Any client that budgets,
truncates, or bills on the official path reads `None`/`0`. **Same class of silent-wrong-number failure as
#1, and it compounds with it for Codex's reasoning-effort accounting.**

### 5. vLLM silently ignores unknown fields, including the removed `guided_json` family
**Impact: unconstrained output where the client demanded a schema.** `extra="allow"`
(`serve/engine/protocol.py:39`) means a request carrying `guided_json` — removed from the schema — gets
`HTTP 200`, a `warning_once` log on the server, and **free-form text instead of JSON**
(`_REMOVED_GUIDED_FIELDS`, `:60-74`). A client then fails at `json.loads()`, far from the cause.
SGLang drops unknowns at Pydantic's default rather than logging at all, so a typo'd parameter is
*completely* invisible there.

### 6. vLLM's Anthropic endpoint has no `thinking` request field
**Impact: Claude Code's thinking budget is discarded, silently.** `thinking` appears nowhere in
`AnthropicMessagesRequest` (`anthropic/protocol.py`), and the model is a plain `BaseModel`
(`extra="ignore"`). Claude Code sends `{"type":"enabled","budget_tokens":N}`; vLLM accepts the request,
ignores the budget, and exposes only the vLLM-specific `output_config.effort` / `chat_template_kwargs`.
**SGLang accepts `thinking` (`enabled`/`disabled`/`adaptive`) and validates `budget_tokens >= 1024`** —
making it the more faithful Claude-Code target.

### 7. `cached_tokens` is off by default on both servers, under different flag names
**Impact: cache-hit telemetry reads zero, masking the prefix-cache problem.** vLLM needs
`--enable-prompt-tokens-details` (`cli_args.py:139-140`); SGLang needs `--enable-cache-report`
(`protocol.py:211` comment). Combined with #8, a Claude-Code operator sees 0% cache hit rate and has no
way to tell a cold cache from an unpopulated field.

### 8. Claude Code's per-request attribution header defeats prefix caching
**Impact: a ~300× prefill regression on multi-turn sessions.** Documented by SGLang with measured
numbers (cached tokens ~4,800 → ~20,800+; new prefill ~82,000 → ~285 tokens/turn) and fixed server-side
in [PR #21064](https://github.com/sgl-project/sglang/pull/21064). vLLM claims it is handled
*"automatically in vLLM versions > 0.17.1"* ([vLLM Claude Code](https://docs.vllm.ai/en/latest/serving/integrations/claude_code/)).
**Requires `CLAUDE_CODE_ATTRIBUTION_HEADER=0` on older builds, and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`
does NOT cover it.** Manifests as unexplained latency, not an error — hence hard to attribute.

### 9. SGLang's flat error envelope for `/v1/chat/completions`
**Impact: client crashes while reporting an error.** SGLang returns
`{"object":"error","message":…,"type":"400","code":400}` at the **top level**
(`http_server.py:588-594`, `protocol.py:99-104`), where OpenAI and vLLM nest under `error`. A client
doing `body["error"]["message"]` raises while trying to display the server's message. SGLang's own
source calls it *"the legacy `ErrorResponse` shape"* and grants the OpenAI shape only to
`/v1/responses` (`http_server.py:602-630`). **Also note `type` is a numeric string like `"400"`, not
`"invalid_request_error"`.**

### 10. vLLM's `/v1/responses` is missing `response.failed` and `response.incomplete`
**Impact: a Responses client cannot distinguish success from failure.** Verified absent from
`streaming_events.py`/`serving.py`; SGLang emits both. vLLM also renames the reasoning-summary family to
its own `response.reasoning_part.*`. A Codex-style client that closes its stream on `response.failed`
waits forever and must fall back to transport close — and with #3 in play, there is no state to
reconcile against.

---

## Appendix — quick reference

**Endpoint parity**

| Capability | vLLM | SGLang |
|---|---|---|
| `/v1/chat/completions` | ✅ `/v1` required | ✅ `/v1` required |
| `/v1/completions` | ✅ | ✅ |
| `/v1/messages` + `count_tokens` | ✅ unconditional, **undocumented** | ✅ unconditional, **documented** |
| `/v1/messages` `thinking` field | ❌ **absent** | ✅ `enabled`/`disabled`/`adaptive` |
| `/v1/responses` + GET + cancel | ✅ **documented** | ✅ **undocumented** |
| Responses `store` | ⚠️ silently ignored | ✅ requires `--enable-response-store`, else 400 |
| Native non-OpenAI endpoint | `/inference`, `/invocations`, `/cohere` | `/generate`, `/separate_reasoning`, `/parse_function_call`, Ollama `/api/*` |
| `/tokenize`,`/detokenize` | root only, **unauthenticated** | both root **and** `/v1`, authenticated |
| `system_fingerprint` | ✅ since [PR #40537](https://github.com/vllm-project/vllm/pull/40537), `vllm-…` format | ❌ never emitted |
| `finish_reason: content_filter` | ❌ not in enum | ✅ declared in Literal |
| Reasoning field | `reasoning` | `reasoning_content` |
| Reasoning token usage path | `completion_tokens_details.reasoning_tokens` | **`usage.reasoning_tokens`** |
| `cached_tokens` gate | `--enable-prompt-tokens-details` | `--enable-cache-report` |
| Unknown request fields | `extra="allow"` — accepted, logged at debug | Pydantic default — dropped, not logged |
| Error envelope (`/chat/completions`) | nested `{"error":{…}}`, `code` is an **int** | **flat** `{"object":"error",…}`, `type` is a numeric string |
| 401 body | `{"error": "Unauthorized"}` (string, not an object) | **same** `{"error": "Unauthorized"}` / `"Forbidden"` string shape |
| Chat template | `--chat-template`; per-request needs `--trust-request-chat-template` | `--chat-template`, `--default-chat-template-kwargs` |
| Grammar backends | xgrammar, guidance(llguidance), outlines, lm-format-enforcer | xgrammar (default), outlines, llguidance |
| `--reasoning-parser` naming | `deepseek_r1`, `openai_gptoss`, `glm45` (**underscores**) | `deepseek-r1`, `gpt-oss`, `glm45` (**hyphens**); extra `qwen3-thinking` |
| `--tool-call-parser` naming | `llama3_json`, `deepseek_v3`, `minimax_m2`, `hermes` | `llama3`, `deepseekv3`, `minimax-m2`, `hermes` |
| Claude Code attribution fix | client-side (`>0.17.1` auto) | server-side ([PR #21064](https://github.com/sgl-project/sglang/pull/21064)) |

**Parser-name portability warning.** Neither `--reasoning-parser` nor `--tool-call-parser` values are
interchangeable between servers for the same model (`deepseek_r1` vs `deepseek-r1`; `llama3_json` vs
`llama3`; `openai_gptoss` vs `gpt-oss`). A launch script copied between them fails at startup.

**Version/date caveats.** The datable artifacts cited here span 2024-12 to 2026-09. Key dates:
vLLM Anthropic [PR #22627](https://github.com/vllm-project/vllm/pull/22627) 2025-10-22;
vLLM `system_fingerprint` [PR #40537](https://github.com/vllm-project/vllm/pull/40537) 2026-04-27;
vLLM Responses RFC [issue #32850](https://github.com/vllm-project/vllm/issues/32850) 2026-01-22;
SGLang Anthropic [PR #18630](https://github.com/sgl-project/sglang/pull/18630) 2026-02-21;
SGLang thinking [PR #19334](https://github.com/sgl-project/sglang/pull/19334) 2026-02-25 and
[PR #22135](https://github.com/sgl-project/sglang/pull/22135) 2026-04-05; SGLang billing-header strip
[PR #21064](https://github.com/sgl-project/sglang/pull/21064) 2026-03-21; y-router archived with last
push 2026-01-11. **A deployment on an older release may not exhibit several behaviours described above —
check the version before acting.**
