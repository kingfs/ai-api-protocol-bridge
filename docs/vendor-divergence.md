# 厂商实现与权威标准的偏差：一份面向兼容层的调研

本文回答三个问题：

1. 国内厂商实现的 `chat/completions`、`responses`、`messages`，与 OpenAI / Anthropic
   的权威标准**具体差在哪里**？
2. 这些差异里，哪些会**断链**、哪些只是**静默地改变语义**？
3. 一个协议桥接层要怎么做，才能把可用性做到最大？

本文是本仓库既有审计的**下一层**。既有文档回答的是"我们自己的解析器是否符合官方
schema"（[protocol-conformance.md](protocol-conformance.md)）和"跨协议转换会丢什么"
（同文件 §4）。`protocols/README.md` 结尾已经写明它的边界：

> Both bundles describe the *upstream* protocols. They say nothing about
> OpenAI-compatible vendors that only implement a subset.

本文补的就是这一段。

**证据分级。** 全文对每条结论标注来源等级，因为"官方兼容"这个词在两种意义上被滥用：

| 标记 | 含义 | 可信度 |
| --- | --- | --- |
| 【文档】 | 厂商官方文档明确写出的行为 | 高，但会过期 |
| 【源码】 | 直接读该实现的协议定义/事件处理代码 | 高，且能看出文档没说的事 |
| 【实况】 | GitHub issue / 捕获的真实流量 | 高，但是单点 |
| 【推定】 | 由前几项推导，未直接验证 | 需实测确认 |

---

## 0. 结论先行

### 0.1 三个"权威"其实不是一个东西

| 家族 | 权威物 | 它到底是什么 | 由谁驱动 |
| --- | --- | --- | --- |
| OpenAI Chat Completions | OpenAPI 文档 + 官方 SDK | **事实标准**，最广适配 | 几乎所有客户端 |
| OpenAI Responses | 官方 OpenAPI（无独立规范文本；另有中立的 [Open Responses](https://www.openresponses.org/specification) 规范，见 §8.1） | **单一客户端驱动**：Codex | Codex CLI / IDE / Desktop |
| Anthropic Messages | API Reference（无 OpenAPI）+ streaming 指南 | **单一客户端驱动**：Claude Code | Claude Code |

这决定了偏差的**方向性**：

- Chat Completions 的偏差是"**各家自己长出来的**"——每家在官方字段之外加了
  `thinking`、`enable_thinking`、`reasoning_content`，互不相同。
- Responses / Messages 的偏差是"**跟着一个客户端追**"。厂商不是为了"符合
  OpenAI/Anthropic"而实现，而是为了"**Codex / Claude Code 能跑起来**"而实现。
  典型证据：DeepSeek 的 Responses 文档开篇就写 *"To meet the demand for Codex,
  our API now supports the Responses API format"*【文档】；
  智谱的 Claude Code 文档默认做**服务端模型映射**，客户端看到的仍是 Claude 模型名【文档】。

  推论很重要：**这两个协议的实际兼容基线，是客户端的实现，而不是 schema。**
  判断"能不能用"，要去读 Codex / Claude Code 的源码，而不是读 OpenAPI。

### 0.2 一句话概括偏差分布

> **Chat Completions 的问题是"多"（各家都加料，字段和枚举漂移）；**
> **Responses 的问题是"少"（每家实现一个子集，且子集边界互不相同）；**
> **Messages 的问题是"假装一样"（路径、鉴权、模型名、思考开关都在协议之外协商）。**

### 0.3 最反直觉的一条

偏差里**最危险的不是"不支持"，而是"支持了但没生效"**。
DeepSeek 的 Responses 文档把这一点直接写在正文里【文档】：

> Unsupported parameters are **silently ignored** and do not cause errors, so
> existing Responses API clients can connect without modification.

`store`、`previous_response_id`、`include`、`metadata`、`truncation`、
`parallel_tool_calls`、`prompt_cache_key` 全部落在这个"静默忽略"里。
链路是通的，客户端不会报错，但**请求的语义已经变了**。
这类偏差没有任何 schema 校验能查出来，和第 5 节要讨论的"诚实性"直接相关。

### 0.4 三条最容易被漏掉的横向差异

上面三条结论是按"协议家族"看的。调研过程中还浮现出三条**横切所有家族**、
且**几乎不会被字段对比发现**的差异，它们对兼容层的设计影响最大（前两条详见 §2.3）：

1. **思维链的方向性**：官方 Chat 协议里 assistant 消息就是 `{role, content, tool_calls}`；
   但在思考模式下，**历史里的 `reasoning_content` 必须被原样回传**——
   DeepSeek 缺了直接 400，智谱 GLM 的 `clear_thinking` 默认 `true` 会主动剥掉它，
   MiniMax 把思维链内联在 `content` 的 `<think>` 标签里。
   **任何"规范化对话历史"的兼容层都会在这里出事**，而且症状是"模型变笨了"，
   不是报错。这条直接决定了 §6.2 能力画像必须新增 `reasoning.roundtrip` 维度。

2. **流内报错有四种互不相同的模型**：官方是独立 error 事件；智谱把它写进
   `finish_reason`；火山方舟用 `data.type="session.error"` 另开通道且**没有 HTTP 状态码**；
   OpenRouter 伪装成一个带 `finish_reason:"error"` 的正常 data chunk。
   **这类偏差的特点是 HTTP 状态码完全正常**，任何只看状态码的健康检查都发现不了，
   而漏解的后果是"上游中途失败"被当成"正常结束"——**L1 级静默失真**。

3. **客户端自己会违反官方 schema，于是"照文档实现"反而成了缺陷。**
   Claude Code ≥2.1.154 会往 `messages[]` 里塞 **`role:"system"`** 的条目，
   而 Anthropic 官方 reference 白纸黑字写着 *"there is no `system` role for input
   messages in the Messages API"*（§3.2.1）；官方端点容忍它，**按文档严格校验的
   兼容端点则 400**。同理 Codex 每轮都发官方 schema 里没有的 `client_metadata`，
   并把 HTTP 当无状态、恒发 `store:false`（§3.1）。
   **这条推翻了一个默认假设：权威 schema ≠ 实际互操作契约。**
   真正的契约是**权威服务的实现行为**（它比 schema 宽）**加上官方客户端的实际行为**
   （它会越界）。所以校验器不能以 schema 为唯一依据——
   **对已知客户端实际发送的越界形态要显式放行**（§6.7）。

这三条共同的教训是：**协议兼容的难点正在从"字段/枚举"下移到"消息与流的骨架"，
再下移到"客户端实现 vs 文档"的落差。**
字段级对比工具（包括本仓库既有的 conformance 审计）看不见它们。

---

## 1. 偏差分类学（D1–D8）

逐字段罗列没用，因为每家的清单都不一样。先把偏差按**成因**分成八类，后面所有清单
都挂在这八类上。这个分类本身就是兼容策略的设计依据——不同类要不同的应对。

### D1 覆盖缺口（Coverage gap）
官方有、厂商没有。三种子形态，后果完全不同：

| 子类 | 行为 | 例子 | 后果 |
| --- | --- | --- | --- |
| D1a | **静默忽略** | DeepSeek Responses 的 `store`/`include`/`metadata`【文档】 | 最危险：链路通、语义变 |
| D1b | **显式拒绝** | Kimi `kimi-k2.7-code` 在 thinking 关闭时返回 400 `invalid thinking: only type=enabled is allowed for this model`【文档】 | 好定位 |
| D1c | **接受但无效** | DeepSeek Anthropic 的 `top_k` 标注 "Ignored"【文档】 | 最难发现 |

**D1b 其实是最健康的形态**——它至少会说话。

### D2 语义重映射（Semantic remap）
同名字段，不同含义、范围或优先级。这类偏差不报错，但结果偏离意图：

- `temperature`：Anthropic 官方文档的范围是 `[0.0, 1.0]`，阿里百炼 Anthropic
  兼容端点是 `[0, 2)`，文档自己加了警告"该范围与 Anthropic 官方的 [0.0, 1.0] 不同，
  **从 Anthropic 迁移时请确认该参数取值**"【文档】。
  **注意这里还有一个更麻烦的层次**：官方当前 schema 已把 `temperature` 标记为
  deprecated，新模型只接受 `1.0`，其他值直接 400（见 §3.5）。
  所以这不是简单的"两边范围不同"，而是**三方语义**：
  旧官方模型、新官方模型、兼容厂商，三者对同一个值的接受度都不同。
- `top_p`：DeepSeek 只在 thinking 模式生效，且**下界被抬到 0.95**，非 thinking
  模式固定为 1.0、传入值被忽略【文档】。
- `max_tokens` 是否包含思维链 token：**同一个端点内随模型变化**。阿里百炼的
  Anthropic 兼容端点：`qwen3.8-*`/`deepseek-v4-*` 下 `max_tokens` 是"回复+思维链"之和；
  `glm-5.2` 传了 `thinking.budget_tokens` 时又只算回复部分【文档】。
- `reasoning_effort` 的枚举（见 §2.1 表）。
- `stop_sequences`：命中后阿里百炼的 `stop_reason` **仍是 `end_turn`**，
  且不回填命中的序列【文档】——与 Anthropic 官方语义直接冲突。

### D3 命名漂移（Naming drift）
同一概念，不同线上字段名。这是最容易让客户端"读到空值却不知道"的一类：

- 思维链输出：`reasoning_content`（DeepSeek 及多数国产约定）vs **`reasoning`**
  （vLLM 新版已改名）vs Anthropic 的 `thinking` content block。
  vLLM 文档对此有一句非常关键的警告【文档】：

  > `reasoning` used to be called `reasoning_content`. To migrate, directly
  > replace `reasoning_content` with `reasoning`. It is important that you also
  > update your client code. Otherwise, your client code could **silently read an
  > empty `reasoning_content`**, even when `reasoning` is populated.

- 思考开关：`thinking`（DeepSeek Chat 顶层）【文档】、`enable_thinking`
  （vLLM `chat_template_kwargs`）【文档】、`reasoning.effort`
  （Responses / 阿里百炼）【文档】、`output_config.effort`（阿里百炼
  Anthropic 兼容端点，且文档说明 `thinking.budget_tokens` **即将废弃**）【文档】。
- token 上限：`max_tokens` vs `max_completion_tokens`。

### D4 信封与流式管线（Envelope & stream plumbing）
协议"形状"本身不同。这一类的特征是**一错就断链或挂起**：

| 差异点 | 官方 | 偏差实例 |
| --- | --- | --- |
| 流终止 | Chat: `data: [DONE]`；Responses: `response.completed` 终结事件、**无** `[DONE]` | DeepSeek Responses 明确"there is no `data: [DONE]` message"【文档】，与官方一致；Chat 有 `[DONE]`【文档】 |
| usage 位置 | Chat + `include_usage`：**额外的、`choices` 为空数组的独立 chunk** | DeepSeek：**不发独立 chunk**，"statistics ride on the last content chunk"【文档】 |
| 内容块顺序 | Anthropic：严格顺序开闭，`content_block_stop` 先于下一个 `start` | 需实测；vLLM/SGLang 源码里是单块状态机，形态正确【源码】 |
| 早期事件里的 `model` | 官方在 `response.created` 就带 model | 阿里百炼样例里 `response.created` 的 `model` 是 `""`，到 `response.completed` 才是 `qwen3.8-max`【文档】 |

### D5 状态与副作用（State & side effects）
"无状态还是有状态"是国内厂商之间**最大的分水岭**，而它不由字段列表体现：

| 能力 | DeepSeek Responses | 阿里百炼 Responses |
| --- | --- | --- |
| `previous_response_id` | **不支持**（无状态 API）【文档】 | **支持**，响应 id 有效期 7 天【文档】 |
| `store` | 不支持，响应恒为 `store: false`【文档】 | — |
| 缓存 | 自动管理，`prompt_cache_key` 不支持【文档】 | 显式/隐式缓存并存【文档】 |

Anthropic 侧的同类问题：`cache_control` 被忽略【文档】。这不是"少个字段"，
而是**成本和延迟特性整体改变**——DeepSeek issue #1269 把 Claude Code 场景下
"跨层推理能力显著低于同规格模型"归因于此【实况】。

### D6 部署与模板决定的契约（开源服务特有）
**这是 vLLM / SGLang 独有的偏差维度，也是最容易被忽略的。**
同一个 URL、同一个协议，契约取决于**启动参数和模型的 chat template**：

- 工具调用不是默认开的：必须 `--enable-auto-tool-choice --tool-call-parser <parser>`，
  且 parser **按模型选**（`hermes` / `llama3_json` / `deepseek_v3` / `qwen3_coder` /
  `glm45` / `kimi_k2` / `openai` …）【文档】。
- 思维链不是默认开的：需要 `--reasoning-parser`，否则思维链混进 `content`。
- 思考开关经由 `chat_template_kwargs` 传给模板；**模板不声明该 kwarg 时被静默过滤**
  （vLLM 文档：*"For models whose templates don't declare `enable_thinking` … the
  injected kwarg is harmlessly filtered out"*）【文档】。
- 严格工具 schema 由 `--tool-strict-level` 与 `VLLM_ENFORCE_STRICT_TOOL_CALLING`
  两个服务端开关决定；文档明确说"**Most OpenAI-compatible clients … never set
  `strict`**"，所以默认情况下参数可能不受语法约束【文档】。

结论：**对开源服务，"OpenAI 兼容"不是一个可以静态判定的属性。**
必须运行时探测，见 §5.5。

### D7 扩展字段（Vendor-only extensions）
厂商在官方字段之外自加的字段。双向都有害：发错了会 400，读了会污染统一 IR。

已确认存在的：`thinking`（DeepSeek Chat）、`chat_template_kwargs` / `vllm_xargs` /
`cache_salt` / `kv_transfer_params` / `ec_transfer_params`（vLLM）、
`include_reasoning`（vLLM Responses）、`output_config`（阿里百炼 Anthropic）、
`recommended` 之外的 OpenRouter `provider`/`reasoning`/`cost`（见 §3.5）。

注意 vLLM 的 Anthropic 协议源码里把这类字段显式标注为
`# vLLM-specific fields that are not in Anthropic spec`【源码】——这是**正确**的做法，
值得作为规范：扩展必须可识别、可去除。

### D8 模型身份与能力协商（Model identity）
**协议之外还有一层约定，schema 里完全没有，但它决定了能不能用。**

- **模型名映射**：DeepSeek 把 `claude-opus*` 映射到 `deepseek-v4-pro`、
  `claude-sonnet*`/`claude-haiku*` 映射到 `deepseek-flash`；传不认识的模型名
  会**自动落到 `deepseek-flash`**，不报错【文档】。
- **上下文窗口写在模型名里**：`[1m]` 后缀是事实标准，DeepSeek（`deepseek-flash[1m]`）、
  Kimi（`kimi-k3[1m]`）、智谱（`glm-5.2[1m]`）、阿里（`qwen3.7-plus[1m]`）都在用【文档】。
- **能力档位靠客户端环境变量协商**：`ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL`、
  `CLAUDE_CODE_SUBAGENT_MODEL`、`CLAUDE_CODE_EFFORT_LEVEL`、
  `CLAUDE_CODE_AUTO_COMPACT_WINDOW`【文档】。没配全的话，
  **对应场景静默失败**（Kimi 文档原文："Configuring only some of the variables makes
  the corresponding scenarios fail silently"）【文档】。
- **模型名不能带 `/`**（vLLM + Claude Code 的限制）【文档】。

也就是说：客户端发的 `model` 字段是一个**逻辑角色**，不是模型 ID。兼容层如果
把它当字符串透传，就会踩到映射表。

---

## 2. Chat Completions：偏差矩阵

权威侧基线直接取自本仓库 vendored 的
[`protocols/openai/chat-completions.schema.json`](../protocols/openai/chat-completions.schema.json)：

| 字段 | 官方权威值 |
| --- | --- |
| `reasoning_effort` | `enum: ["low","medium","high"]`，默认 `medium`，nullable |
| `stop` | `string \| array`，`maxItems: 4` |
| `tool_choice` | `["none","auto","required"]` + named |
| `stream_options.include_usage` | **额外一个 chunk**，其 `choices` 恒为空数组 |
| `choices[].finish_reason` | `["stop","length","tool_calls","content_filter","function_call"]` |
| 请求消息 role | `system` / `developer` / `user` / `assistant` / `tool` / `function` |

### 2.1 已核实的偏差（DeepSeek）

DeepSeek 的 Chat Completions 有一份非常详细的 API reference，逐条比对如下【文档】：

| 项 | 官方 | DeepSeek | 类别 |
| --- | --- | --- | --- |
| `reasoning_effort` | low / medium / high | **none / low / high / max**（无 `medium`，多 `none`、`max`） | D2 |
| `stop` 上限 | 4 | **16** | D2 |
| `include_usage` 的 usage chunk | 独立 chunk，`choices: []` | **无独立 chunk**，usage 附着在 `[DONE]` 前最后一个内容 chunk 上 | D4 |
| `finish_reason` | 5 个值 | 多出 **`insufficient_system_resource`**、**`aborted`** | D1/D2 |
| `response_format` | text / json_object / **json_schema** | 仅 text / json_object，**无 json_schema** | D1 |
| `developer` role | 支持 | 未文档化（文档只有 system/user/assistant/tool） | D1 |
| `max_tokens` 默认值 | 无默认（由模型决定） | **随模式变化**：非思考 8K / 思考 64K / `reasoning_effort=max` 时 128K | D2 |
| `temperature` | 0–2 | 0–2，但**思考模式下无效** | D2 |
| `top_p` | 0–1 | 仅思考模式生效且 clamp 到 **0.95–1.0**，非思考模式固定 1.0 | D2 |
| `thinking`（顶层开关） | 不存在 | 存在，`{"type":"enabled"\|"disabled"}` | D7 |
| `logprobs` | 有 | 有，但 token 概率用 **`-9999.0`** 作为"极不可能"哨兵值 | D2 |
| usage | `prompt_tokens_details.cached_tokens` | 额外有 **`prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`** | D7 |
| `json_object` 前置条件 | 需要提示词里要求 JSON | 需要提示词里要求 JSON，否则可能**生成到 max_tokens 的空白流** | D2 |
| `reasoning_effort` 的**静默重映射** | 无（官方枚举） | 传入 `minimal→low`、**`medium→high`**、`xhigh→high`、`ultra→max`【文档】 | D2 |
| `developer` role | 支持 | **硬 400**：`unknown variant 'developer', expected one of 'system','user','assistant','tool','latest_reminder'`【实况】。注意错误里泄露的那个 `latest_reminder` 角色，官方根本没有 | D1b |
| **思考模式下的 CoT 回传** | 官方无此要求 | **工具调用之后必须把 `reasoning_content` 传回**，否则 400 `The "reasoning_content" in the thinking mode must be passed back to the API`【实况】 | L0 |
| `tools[].strict` | 直接支持 | **需要把 base_url 换成 `/beta`** 才生效【文档】 | D8 |
| 限流模型 | RPM/TPM | **按并发数**限流，不是按请求/ token 数；`user_id` 可做隔离【文档】 | D2 |
| HTTP 状态码 | 400/401/403/404/429/500 | 额外使用 **402（余额不足）** 与 **422（参数非法）**【文档】 | D2 |
| 流式保活 | 无 | **发 SSE 注释 `: keep-alive`**；非流式会返回连续空行；推理 10 分钟未开始则切断连接【文档】 | D4 |
| `choices[].logprobs` | 标准结构 | 额外带一个**非标准的 `reasoning_content` 数组**【文档】 | D7 |

> `-9999.0` 这类哨兵值值得单独注意：它长得像合法数字，任何做数值统计/阈值判断的
> 客户端都会把它当成真实 logprob 吃进去。

### 2.2 厂商矩阵

**接入路径本身就不统一**（`/v1` 是少数派）【文档】：

| 厂商 | base URL | 说明 |
| --- | --- | --- |
| OpenAI 官方 | `https://api.openai.com/v1` | 权威 |
| DeepSeek | `https://api.deepseek.com` | 完整路径是 `/chat/completions`，**文档给出的 base_url 无 `/v1`**；另有 `/beta` |
| 火山方舟 / 豆包 | `https://ark.cn-beijing.volces.com/api/v3` | **v3** |
| 阿里百炼 | `https://dashscope.aliyuncs.com/compatible-mode/v1` | 新形态按工作空间/地域分主机；**Key 与地域绑定，错地域 401** |
| 智谱 GLM | `https://open.bigmodel.cn/api/paas/v4/chat/completions` | **v4** |
| Kimi / Moonshot | `https://api.moonshot.ai/v1` | — |
| MiniMax / SiliconFlow / StepFun / 混元 / 星火 | `/v1` | — |

**字段级偏差**（全部来自各厂商官方 API reference；其中标 ★ 的两条已由本文独立复核并附原文）：

| 项 | 官方 | 已核实的厂商行为 |
| --- | --- | --- |
| ★ `stop` 的**语义** | 停止且**不包含**匹配串 | **腾讯混元：停止在匹配内容之<u>后</u>**。原文："调用 OpenAI 接口时…停止在匹配到 stop 的内容之前。在调用混元接口时，会停止在匹配到 stop 的内容之后。" 官方示例：原文 `我是一个 AI 助手…`、`stop=助手` → OpenAI 得 `我是一个 AI`，混元得 `我是一个 AI 助手`。**同一个字段名，输出内容相反**，且腾讯声明"未来可能改为与 OpenAI 一致"——**补偿代码是定时炸弹** |
| `stop` 上限/形态 | 4 个字符串 | DeepSeek **16**；GLM **4 且仅接受数组**（不接受裸字符串）；混元 ≤4；千帆 **≤4 且每个 ≤20 字符**；Qwen **扩展**为可传 token_id 数组 |
| ★ `tool_choice: "required"` | 支持 | **Qwen 不支持**（400 `tool_choice is one of the strings that should be ["none","auto"]`）；思考模式下 required/object 直接 400 |
| `tool_choice` 枚举 | none/auto/required/named | 智谱 GLM **只有 `auto`**（400 `Tool choice must be auto`）；腾讯混元是 `none`/`auto`/`custom`（**`custom` 非官方，且没有 `required`**）；Ark 的 required 仅 Seed 1.6+ |
| `messages` 形态约束 | 无交替约束 | **Qwen：仅 `messages[0]` 可为 system；user/assistant 必须交替；最后一条必须是 user**——违反即错。混元 **≤40 条**；SiliconFlow **1–10 条** |
| `developer` role | 支持 | DeepSeek **硬 400**（见 §2.1）；Ark **2026-09-09 之前也报错**，之后才接受 |
| `tools` + `stream` | 可同时使用 | **Qwen 兼容面：`tools` 暂时无法与 `stream=True` 同时使用**（文档原文） |
| `response_format` | text/json_object/json_schema | DeepSeek **无 json_schema**；GLM **无 json_schema**；星火**无 json_schema**；Kimi / Ark / StepFun **支持 json_schema** |
| `max_tokens` vs `max_completion_tokens` | 二者互斥（发两个会错） | **Ark：互斥且报错**；**千帆：`max_completion_tokens` 静默获胜**。**"两个都发以求兼容"的客户端在 Ark 上报错、在千帆上被改语义** |
| usage 位置 | 独立的 `choices: []` chunk | **阿里百炼 / Ark：规范**（发独立空 choices chunk）；**DeepSeek：附着在最后一个内容 chunk，且明确不发独立 usage chunk**；**StepFun：每个 chunk 都带 usage（累计值），且无需任何 opt-in** |
| usage 字段 | `prompt_tokens_details.cached_tokens` | Kimi 另有**顶层 `cached_tokens`** 与 `cache_write_tokens`；DeepSeek 另有 `prompt_cache_hit_tokens`/`prompt_cache_miss_tokens`；Ark 有 `audio_cached_tokens`（官方无对应）；**SiliconFlow 的 usage 只有 3 个字段，完全没有 cached tokens** |
| 非官方 stream 开关 | 无 | Ark 与千帆都有 **`stream_options.chunk_include_usage`**：让**每个 chunk 都携带累计 usage**（与 `include_usage` 并存） |
| 思维链字段名 | （Chat 无官方字段） | MiniMax 用 `reasoning_details`；**StepFun 用 `reasoning_format` 参数在 `reasoning` 与 `reasoning_content` 之间切换响应字段名** |
| 思维链的**默认位置** | — | **MiniMax `reasoning_split` 默认 false，思考内容直接内联在 `content` 里，包在 `<think>…</think>` 标签中**——不做任何额外参数时，兼容路径返回的 `content` 就是带标签的原文 |
| `finish_reason` | 5 个值，其余为 `null` | DeepSeek 多出 `insufficient_system_resource`/`aborted`；GLM 多出 `sensitive`/`network_error`/`model_context_window_exceeded`；SiliconFlow 多出 **`eos`**；**StepFun 非最终 chunk 用空字符串 `""` 而非 `null`**（`is None` 判断会提前终止）；Ark 的 `length` 把三种原因合并成一个值 |
| `system_fingerprint` | 存在 | 阿里百炼返回**空串 `""`**（文档明说不支持）；Kimi/SiliconFlow/Ark **完全没有该字段** |
| 采样参数覆盖面 | n/seed/logit_bias/penalties/logprobs | GLM 请求 schema 极短，`n`/`seed`/penalties/logprobs **全无**，且 `temperature` 上限 **1.0**、`top_p` 下限 **0.01**；MiniMax 文档写明 `presence_penalty`/`frequency_penalty`/`logit_bias` 等**"会被忽略"**（静默）；SiliconFlow 连 `presence_penalty` 都没文档化 |
| 采样参数的**默认值** | penalties 默认 0 | **星火 `presence_penalty` 范围 [0,2] 默认 1.2、`frequency_penalty` 范围 [0,1] 默认 0.02**——两个轴都不同，且不接受负值；**千帆另有 `penalty_score` 默认 1.0 范围 [1.0,2.0]**，是"1.0 为中性"的奖励式刻度，与官方以 0 为中心的惩罚**数值与语义都不对应**（朴素映射会得到错误输出） |
| 静默丢弃 | — | **智谱 `do_sample: false` 时 `temperature`/`top_p` 被忽略**；DeepSeek 思考模式下 temperature/penalties 静默无效；MiniMax 同上 |
| `n` | 1..128 | MiniMax **只支持 1**；Qwen **1–4 且仅 qwen-plus，带 tools 时强制为 1**；StepFun **未设上限**（"最大不限"） |
| 工具调用的**默认响应形态** | `tool_calls` 数组 | **星火 `tool_calls_switch` 默认 `false`，此时工具调用以 JSON 文本返回而非 `tool_calls` 数组**——默认就不兼容 |
| 非官方工具类型 | 只有 `function` | GLM 额外支持 `web_search`/`retrieval`/`mcp`（**响应侧也出现**）；星火有内置 `web_search`；千帆 `web_search.search_mode` 复用 `required` 一词表示"强制联网" |
| 错误信封 | `{"error":{"message","type","code"}}` | 智谱 GLM 是 `{"error":{"code":"1214","message":...}}`（**`code` 是数字字符串，无 `type`**）；**Qwen 是扁平结构**（无 `error` 外壳）；**SiliconFlow 最糟：401/404/504 的响应体是裸 JSON 字符串**，400/429/503 是扁平对象——`resp.json()["error"]["message"]` 会抛 `TypeError`，**恰好在你最需要诊断信息的时候** |
| 思考模式与强制工具 | 互斥（官方亦如此） | Qwen、DeepSeek 在思考模式下 `tool_choice` 的 `required`/object 形式都会 400 |

> **"思考 + 强制工具调用互斥"是一条贯穿所有厂商的结构性约束。**
> Anthropic 官方也有同样限制，本仓库原审计 §3.6 记录了"thinking 开启时把
> `required`/指定工具**静默降级为 `auto`**"的处理。现在的证据表明：
> 这个降级不是 Anthropic 的特色，而是**整个行业的共性**，
> 但各家的表达方式不同——官方静默降级、Qwen/DeepSeek 直接 400。
> 兼容层因此必须**按上游选择策略**：能降级就降级并告警，会 400 就先降级再发。

### 2.3 两条最容易被低估的横向差异

**(1) 思维链的往返是"单向强制"的。**

流式返回 `reasoning_content` 只是故事的一半；**回传**才是坑：

| 厂商 | 回传要求 |
| --- | --- |
| DeepSeek | **强制**：带 `tool_calls` 的 assistant 消息若缺少原 `reasoning_content`，下一轮 400 `The "reasoning_content" in the thinking mode must be passed back to the API` |
| Qwen | 要求把上一轮的 **thinking + 正文 + tool_calls 一起**加入历史 |
| 智谱 GLM | `thinking.clear_thinking` **默认 `true`，会主动剥掉历史里的 `reasoning_content`**；要保留必须设 `false` **并逐字节原样、按序回放**，否则"会退化或失效" |
| MiniMax | 思考默认内联在 `content` 的 `<think>` 标签里，**必须原样整体回放**才能续上思维链 |

**结论：`{role, content, tool_calls}` 三元组作为对话状态是不够的。**
任何把 assistant 消息规范化成"只留 content + tool_calls"的兼容层，
都会在 DeepSeek 上直接 400、在 GLM/MiniMax 上静默丢失思维链。
这直接支持 §6.2 能力档案里的 `reasoning_roundtrip` 维度，
也是本仓库 `RawFinishReason` 之外**同等重要的一个"必须保真字段"**。

**(2) 流中途报错有三种互不相同的模型。**

| 模型 | 表现形式 | 代表 |
| --- | --- | --- |
| 官方式 | 单独的 error 事件 / 非 200 终止 | OpenAI |
| **写进 `finish_reason`** | SSE 中途失败**不返回业务错误码**，失败原因出现在 `finish_reason` 里 | 智谱 GLM（文档明确） |
| **换一个事件通道** | 流建立之后，错误以 `data.type="session.error"` 发送，**没有 HTTP 状态码**，只能按 `error.type` 分类 | 火山方舟 Ark |
| **伪装成正常 chunk** | 普通 data chunk 里带 `finish_reason:"error"` 和一个 `error` 对象 | OpenRouter（§8） |

**四种模型意味着"流内错误处理"不能只写一种。**
兼容层必须在解码侧统一成一个内部错误事件，否则上游换一家，
错误就会被当成正常结束——**这是 L1 级静默失真，也是最难在测试中发现的一类。**

厂商字段级完整枚举（含 11 家 × 各自声明/缺口/社区实测证据，100+ 条来源）
见 [chinese-vendor-chat-completions-divergences.md](chinese-vendor-chat-completions-divergences.md)。

### 2.4 最强的旁证：厂商自己发布的"历史偏差清单"

前面所有结论都可以被质疑为"你可能理解错了文档"。但有一份证据无法这样解释——
**火山方舟（豆包）公开发布了一份《API 兼容性升级公告》，逐条列出自己过去"不兼容"的行为**
（2026-09-09）【文档】：

| 升级项 | 升级前 | 升级后 |
| --- | --- | --- |
| `logprobs` / `top_logprobs` | **"API 不支持传入"** | 支持 |
| `role=developer` | **"role=developer 时会报错"** | 接受 |
| DeepSeek `stop` 数组 | **最多 4 个，超过 4 个报错** | 放宽到 16 |
| 不支持的模态输入 | **直接报错拒绝** | 不再报错 |
| 无法解析的 `encrypted_content` / `signature` | **直接报错** | 容忍 |
| `content` 为 null 或空 | **直接报错** | 部分容忍 |
| **`tool_call_id` 为 null 或空串** | **直接报错** | 部分容忍 |

这份清单的价值有三层：

1. **它证明 D1 类偏差不是分析者的臆测**，而是厂商自己承认的产品状态。
2. **它证明偏差是"移动的靶子"**——同一家厂商会在版本间改行为，
   所以"今天测过"不等于"明天还对"。这正是 §6.5 要求探针按模型版本失效的依据。
3. **它示范了一种值得推荐的工程姿态**：*把不兼容当成需要公告的变更来管理*。
   本报告 §6.6 建议的"把静默变成一等公民"，本质上是把这种做法内建到兼容层里，
   而不是等厂商发公告。

顺带一个反向洞察：**`tool_call_id` 为 null/空串曾经是硬错误**。
也就是说，"工具调用可以没有 id"这个问题，Ark 的历史答案是**拒绝**而不是容忍——
那些为容忍缺失 id 而写的兼容代码，在 Ark 上从来不是必需的，
而在别家上可能仍然是必需的。

---

## 3. Responses 与 Messages：先看客户端要什么

这两个协议的偏差分析必须从客户端出发，否则会把大量无害差异误判为风险。

### 3.1 Codex 对 Responses 的硬性要求（源码级）

Codex 是开源的。它的 SSE 分发在 `codex-rs/codex-api/src/sse/responses.rs`
的 `process_responses_event()`【源码】。**关键是要区分"被消费"和"被具名丢弃"**——
该函数有一段**显式列出、只打 trace 日志、不做任何事**的 unhandled 分支
（`responses.rs#L531` 起）：

```rust
"codex.response.metadata"
| "response.content_part.added"
| "response.content_part.done"
| "response.custom_tool_call_input.done"
| "response.function_call_arguments.delta"
| "response.function_call_arguments.done"
| "response.in_progress"
| "response.metadata"
| "response.output_text.done"
| "response.reasoning_summary_part.done"
| "responsesapi.websocket_timing" => { trace!("unhandled responses event: {}", event.kind); }
kind if kind.ends_with(".delta") => { trace!("unhandled responses event: {kind}"); }
_ => { debug!("unhandled responses event: {:?}", ...); }
```

> **⚠️ 一处必须纠正的常见误读。** 只按"事件名出现在这个文件里"来统计，
> 会把上面这些**空操作**也算成"被支持"，得到一张 25 项的假事件表。
> 本文早期版本就犯了这个错（把 `function_call_arguments.delta/done` 列为已处理）。
> **正确的方法是看它落在哪个 match 臂**。下表按此重列。

**真正被消费的事件**（会产生语义）：

| 事件 | 作用 | 硬性要求 |
| --- | --- | --- |
| `response.output_item.done` | **唯一**能产生 assistant 文本、reasoning、`function_call`、`custom_tool_call` 条目的路径 | **必需** |
| `response.completed` | 结束这一轮 | **必需**，且 `response.id` 是**必填字段（无 serde default）** |
| `response.output_text.delta` | 实时文本 | 可选（增量体验） |
| `response.output_item.added` | 实时文本所需 | 可选 |
| `response.reasoning_summary_text.delta` | 思考摘要 | **必须同时带 `delta` 和 `summary_index`**，缺一个就静默丢弃 |
| `response.reasoning_text.delta` | 思考正文 | 需要 `content_index` |
| `response.custom_tool_call_input.delta` | freeform 工具输入 | 需要 `delta` + (`item_id` 或 `call_id`) |
| `response.failed` / `response.incomplete` | 失败终态 | `incomplete` **总是**被转成错误 |
| `response.created` | 记录 response id | 可选 |

由此推出几条**直接改变实现**的结论：

1. **工具调用只能从 `response.output_item.done` 拿到。**
   `function_call_arguments.delta/done` **是空操作**。也就是说：
   **一个只流式发送参数增量、而 `output_item.done` 里不带组装好的
   `{"type":"function_call","name","call_id","arguments"}` 的上游，
   在 Codex 眼里永远没有工具调用。** 这是 §5 L0 里最隐蔽的一条——
   流是通的、没有报错、模型"说了话"，但工具永远不会被执行。
2. **`response.completed` 缺失是致命错误**，不是"流自然结束"：
   Codex 抛 `ApiError::Stream("stream closed before response.completed")`，
   按 `stream_max_retries`（默认 5）重试，然后用户可见
   `stream disconnected before completion: stream closed before response.completed`。
   **上游若以 `data: [DONE]` 收尾而不发终态事件，就落在这里。**
3. **裸 `error` 事件会被忽略**（落进 default 臂）。错误必须走
   `response.failed`（带 `response.error.{code,message}`）或 HTTP 非 2xx。
   **把错误写成裸 `error` 事件 = 让 Codex 等到重试耗尽。**
4. **Codex 同时接受 `reasoning_text.*` 和 `reasoning_summary_*`**，
   这解释了为什么 vLLM 能跑通（vLLM 只发 `reasoning_text.*`，
   不发任何 `reasoning_summary_*`）【源码】。
5. **`sequence_number` 和 `obfuscation` 在 Codex 里没有任何读取方**，可以安全省略。
6. `response.custom_tool_call_input.*` 是 Codex 私有通道，对应 `apply_patch`
   这类 freeform 工具。DeepSeek 明确为它做了兼容
   （`{"type":"custom","name":"apply_patch"}`，其他 custom 名字返回 400）【文档】。
7. **模型能力是客户端配置声明的，不是协议声明的**：Codex 通过
   `~/.codex/models.json` 声明 `supported_reasoning_levels`、`apply_patch_tool_type`、
   `web_search_tool_type`、`context_window` 等【文档】。
   也就是说**"这个模型支持什么"由客户端本地目录决定**，上游无法通过协议表达。

**Codex 实际每轮都发的请求体**（来自 `core/src/client.rs` 与
`codex-api/src/common.rs`）【源码】：

| 字段 | 值 | 是否恒发 |
| --- | --- | --- |
| `input` | `Vec<ResponseItem>`，**永远是数组** | 是 |
| `store` | **`false`** | **恒发** |
| `stream` | `true` | 恒发 |
| `tool_choice` | 字面量 `"auto"` | 恒发 |
| `include` | **`["reasoning.encrypted_content"]`** | 恒发 |
| `prompt_cache_key` | 由 responses metadata 派生 | 恒发 |
| **`client_metadata`** | **非 OpenAI 字段**：`x-codex-installation-id`、`session_id`、`thread_id`、`x-codex-window-id`、`turn_id`…… | **恒发** |
| `instructions` | system prompt | **为空时省略** |
| `tools` | 数组 | **为空时整个省略** |
| `reasoning` / `parallel_tool_calls` | — | 恒发 |
| `temperature`/`top_p`/`max_output_tokens`/`truncation`/`metadata`/`conversation`/`previous_response_id` | — | **一律不发** |

这张表有两条**反直觉但极其重要**的推论：

- **`client_metadata` 是恒发的非标准字段。** 一个"只拒绝了未知字段、其余都实现了"
  的上游，会因为这一个字段而在**每一个请求**上失败。
  **容忍未知 key 比拒绝更安全**——这是 §6.3 里"保守交集"之外的另一条硬要求。
- **Codex 在 HTTP 上不使用 `previous_response_id`，也不使用 `store`。**
  它每轮把完整 `input` 数组重发一遍，是**完全无状态**的（`previous_response_id`
  只在 WebSocket 续传路径上构造，而那条路径需要 provider 声明
  `supports_websockets`，普通自定义 provider 不会开）。
  **因此"上游不支持 `previous_response_id` / 静默忽略 `store`"对 Codex 无害**——
  这一点本文早期版本判断反了（见 §4.4 的更正）。

**Codex 最小可行 Responses 子集**（据此清单可自检一个上游是否可用）：

1. `POST {base_url}/responses`，JSON，`Accept: text/event-stream`。
2. 接受并**忽略**：`include`（任意值）、`store:false`、`prompt_cache_key`、
   **`client_metadata`**、`tool_choice:"auto"`、`parallel_tool_calls`、
   `reasoning`、`service_tier`。**忽略未知 key 严格优于报错。**
3. `input` **永远是数组**；至少处理 `message`（user/assistant/system/developer）、
   `function_call`、`function_call_output`（**带与不带 `id` 都要收**，但 `call_id` 必须在）、
   `reasoning`（常常没有 `id`）、`custom_tool_call`、`custom_tool_call_output`；
   `agent_message` / `additional_tools` 要**容忍而不是 400**。
4. `tools` 必须接受 `function`、`custom`、`web_search`、**`namespace`**（或把它摊平）。
   **拒绝 `namespace` 会静默杀死 MCP**——Codex 把每个 MCP server 的工具包在
   非标准的 `{"type":"namespace",…}` 里，只有 OpenAI/Azure 会展开它；
   其他后端原样透传或拒绝，模型就只看到一个不可调用的 `mcp__<server>` 工具【实况】。
5. `stream:true` 必须产生真 SSE（不是 JSON 体）。
6. **对每一个输出条目发 `response.output_item.done`，且条目必须完整**——
   尤其是 `function_call` 要带 `name`、`call_id`、`arguments`（**JSON 字符串**）。
7. **恰好一次** `response.completed`，在最后，**必须含 `response.id`**；
   能带 `usage.input_tokens`/`output_tokens`/`total_tokens` 更好。
8. **绝不在没有 `completed`/`failed`/`incomplete` 之一的情况下结束流。**
9. 错误用 `response.failed` + `response.error.{code,message}`。

**Codex 完全不读的东西**（可以省）：`sequence_number`、`obfuscation`、
`response.in_progress`、`response.content_part.*`、`response.output_text.done`、
`response.function_call_arguments.delta/done`、`response.reasoning_summary_text.done`、
`response.created`、`metadata`、`conversation`、`previous_response_id`、`store`、
`truncation`、`max_output_tokens`、`temperature`、`top_p`。

> **两条即使九条全过也会中招的陷阱**：(a) **只有参数增量不够**——
> `output_item.done` 必须携带组装好的调用；(b) **`reasoning_summary_text.delta`
> 必须同时带 `delta` 和 `summary_index`** 才会被渲染。

**还有一个对所有流式代理都适用的教训**：真实上游的事件顺序是
`response.created` → `response.in_progress` → `response.output_item.added`
**在任何一个 token 之前**。一个"缓冲到有输出才开始写响应头"的代理会把
`output_item.added` 误当成"输出已开始"而过早提交 header，
于是上游随后的过载拒绝就无法再重试【实况】。

### 3.2 Claude Code 对 Messages 的硬性要求

**这一节比我原先预想的有权威来源。** Anthropic 自己发布了一份
**《Claude Code gateway compatibility guide》**（`code.claude.com/docs/en/llm-gateway-protocol`），
逐条说明 Claude Code 往 `ANTHROPIC_BASE_URL` 发什么、以及网关剥掉什么会出事【文档】。
下面以它为准，厂商侧证据作为补充。

#### 3.2.1 最关键的纠正：`messages[].role: "system"`

生态里长期把**两件不同的事**混为一谈，分开之后问题才可解：

| # | 现象 | 是否合法 Anthropic |
| --- | --- | --- |
| ① | 顶层 `system` 是**文本块数组**（Claude Code 用它挂 `cache_control`） | **合法** |
| ② | `messages[]` 里出现 **`role:"system"`** 的条目（Claude Code ≥2.1.154 用它追加会话/skill/hook 上下文） | **官方明确不合法** |

Anthropic 官方 API reference 原文：*"there is no `system` role for input messages
in the Messages API"*。也就是说，② 之所以能对官方端点工作，只是因为**一方 API
容忍了它**；任何按文档枚举校验 role 的兼容端点都会在解析阶段拒绝。

**那个流传很广的 400 其实是 ② 造成的，不是 ①**——错误文本
`unknown variant 'system', expected 'user' or 'assistant'` 说的是 **role 枚举**，
不是顶层 `system` 的类型。本文早期版本把这条误归因于"`system` 从字符串变成了数组"，
现予更正。

谁拒绝 ②、谁接受 ②【文档】+【实况】：

| 接受 `messages[].role:"system"` | 拒绝 | 未文档化 |
| --- | --- | --- |
| **阿里百炼**、**火山方舟 Ark** | **Kimi**（role 枚举仅 user/assistant）、**腾讯混元**（同上）、**DeepSeek**（400） | GLM、MiniMax、SiliconFlow、ModelScope |

DeepSeek 那条的根因还有一个更细的层次：据 issue 报告，它**只把 `system` 当字符串处理**，
其内部的 Anthropic→OpenAI 转换把 `system` 的文本块**塞进 `messages` 变成
`role:"system"` 条目**，然后被自己的校验拒掉——**是厂商网关内部的跨协议转换
制造了新 bug**，而两侧 schema 各自都没错。最终处理是 **closed stale / not planned**。

#### 3.2.2 其余硬性要求（全部来自官方 gateway 指南）

1. **`count_tokens` 是可选的。** 原文：*"Token-counting endpoints are the only
   optional ones: when they're absent, Claude Code falls back to a character-based
   estimate of context usage."*
   > **这纠正了一个常见误解**：`count_tokens` 不是硬性依赖。
   > 它缺失只会让 `/context` 显示近似值，**不会报错**。
   > 但反过来讲，**如果兼容层要宣称"支持 Claude Code"，就不该在这条路由上返回 5xx**
   > ——返回 404 让客户端降级才是正确姿态。
2. **两种鉴权头都必须接受**：`ANTHROPIC_AUTH_TOKEN` → `Authorization: Bearer`，
   `ANTHROPIC_API_KEY` → `x-api-key`。**你无法控制用户配哪个**，
   只读一个的实现会在默认配置下就失败。
3. **`anthropic-beta` 与 `anthropic-version` 必须原样转发**，
   且**不能对 beta 值做白名单**（集合随版本变化）。用 claude.ai 登录时，
   `anthropic-beta` 里带一个 OAuth 能力串，**被剥掉就是 401**。
4. **`?beta=true` 查询串**：推理请求发到 `/v1/messages?beta=true`——
   **匹配路径而不是完整 URL**。
5. **`GET /v1/models` 上的任何重定向都算失败**，且它只保留 id 里含
   `claude`/`anthropic` 的条目。
6. **`thinking: {"type":"adaptive"}` 是 Claude 4.6+ 的默认**，
   而且**对"不认识的模型名"（包括网关别名）也照发**。
   只认 `enabled`/`disabled` 的实现会在这里 400。
7. **能力是"header + body"成对的**，这是全文最有洞察力的一条：
   > *"A gateway that strips the header while passing the body, or forwards an
   > Anthropic-format body to an upstream with a different schema, produces hard
   > 400 errors; **only when both halves are absent together does the feature turn
   > off quietly**."*

   也就是说：**"只删 header"和"只删 body"都比"两个都删"更糟。**
   这给兼容层一条明确规则——要么成对透传，要么成对剥离，**不要单独处理一半**。
   官方给的策略是 *"Forward as open lists"*：**不要做白名单**。
8. **优雅降级是有限的，而且取决于错误措辞**：
   > 当上游拒绝 `thinking`、会话中途的 system 消息、或这类消息上的 `cache_control`
   > 标记时，Claude Code 会**重试并把该能力在本次会话内关掉**。
   > 但它**不会**对 `context_management` 和工具 schema 字段的拒绝做重试。
   > *"The retry logic matches on the upstream's error wording, so **forward error
   > response bodies unmodified**."*

   **推论很硬：兼容层重新包装上游错误报文，会破坏客户端的自愈路径。**
   这和 §6.6"把静默变成一等公民"并不矛盾——**告警要加，但错误原文必须保留**。

#### 3.2.3 流式：`ping` 是功能必需的，不是优化

官方指南里最可操作的一条【文档】：

> Claude Code **统计网关上转发的每一个字节**，包括 SSE `ping` 事件和注释行，
> 并在**默认 300 秒**无字节时中止流。上游的 ping 是长时间思考停顿期间**唯一的流量**……
> 对于完全不发 ping 的上游（例如 Bedrock 的二进制事件流），
> **翻译时要自己补发 `ping` 事件**。

对照厂商文档：**Kimi、火山方舟 Ark、腾讯混元三家都明确不发 `ping`**
（事件集合里根本没有这个类型）【文档】。阿里百炼的样例里有 `ping`。

**所以"给这三家做兼容层"和"做纯协议翻译"不是一回事**——
必须在静默间隙主动补字节，否则会在长思考时被客户端掐断。
这是 §4.7 那张代理能力表里、原生端点也不会替你做的事。

另外：**必须返回 `content-type: text/event-stream`，且不能缓冲**
（缓冲会让 Claude Code 卡住）。流失败后它会**退回非流式重试**；
若 200 但 body 不是 Claude message 形状，会以
`API returned an empty or malformed response (HTTP 200)` 结束该轮，
**并报告收到了多少个流事件**——这给了排查一个明确的抓手。

#### 3.2.4 与既有证据的衔接

前文（及原审计）已记录的两条厂商侧偏差仍然成立，且与官方指南一致：

- **`cache_control` 必须真的生效**，否则多轮 agent 每轮全量重算。DeepSeek 标注
  `cache_control` 为 "Ignored"【文档】。官方指南把后果写得更直白：
  **`cache_control` 被忽略时"每轮都按未缓存输入计费，且不报任何错"**。
- **`stop_reason` 不得自造枚举值**。腾讯混元用 **`sensitive`** 取代官方的 `refusal`，
  百炼和 Kimi 在 `stop_sequences` 命中时**仍返回 `end_turn`**、
  且**不回填 `stop_sequence`**（见 §3.3）。

**Claude Code 最小可行 Messages 子集**（共 26 条，完整清单见
[anthropic-messages-compat-dossier.md](research/anthropic-messages-compat-dossier.md)）。
其中**最容易漏、且一旦漏就断链**的是这五条：

1. **接受 `messages[].role:"system"`**（②，第一大断裂）；
2. **两种鉴权头都接受**；
3. **`thinking:{type:"adaptive"}` 不得 400**（含未知模型名）；
4. **静默间隙补 `ping`/字节**（300 秒上限）；
5. **错误报文原样转发**，不要重新包装（否则自愈失效）。


#### 3.2.5 直接证据：从 Claude Code 二进制里读出来的事实

Claude Code 虽然闭源，但它是**打包后的可执行文件**，相关字符串与内联源码片段
可以直接读出。以下均来自本机安装的
`@anthropic-ai/claude-code-linux-arm64`（`claude` 二进制）【源码】：

**1）归属头的确切构造**（内联 JS 片段，逐字）：

```js
u = r && i === "firstParty" && Yd() ? ` cc_prev_req=${r};` : "";
d = `x-anthropic-billing-header: cc_version=${n}; cc_entrypoint=${o};${s}${l}${c}${u}`;
```

并且存在 `cc_is_subagent=true;` 片段。这印证了 §4.3.1：
**这个头是内容的一部分**（会被拼进 system prompt），而不是 HTTP 头，
所以它必然进入前缀缓存的键。

**2）环境变量的开关逻辑**（内联 JS）：

```js
function RYn(e,t,r){ if(su(process.env.CLAUDE_CODE_ATTRIBUTION_HEADER)) return ""; ... }
```

设了 `CLAUDE_CODE_ATTRIBUTION_HEADER` 就直接返回空串——**这是唯一有效的关闭方式**。

**3）`count_tokens` 确实会被调用，且有降级路径**：

```text
POST  /v1/messages/count_tokens
count_tokens_unreachable
count_tokens is not supported on Bedrock upstreams
```

这说明两件事：
- Claude Code **会主动调用** `POST /v1/messages/count_tokens`；
- 它**预期**这个端点可能不可达，并为此准备了专门的降级状态
  （`count_tokens_unreachable`）。

因此 §3.2 里"缺 `count_tokens` 可容忍"这条**得到证实**，
但它不是"静默"的——客户端内部是有状态的。兼容层如果希望行为可预测，
**应当实现 `count_tokens`**（vLLM 和 SGLang 都实现了）。

**4）effort 档位比官方枚举宽**：字符串里同时存在
`low`/`medium`/`high`/`xhigh`，以及会话级的 `ultracode`，其描述为
*"Enable ultracode for the session: **xhigh effort** plus standing
dynamic-workflow orchestration"*。

所以完整的档位阶梯是 `low / medium / high / xhigh`，
外加 `ultracode` 这个"xhigh + 工作流编排"的会话模式。
这解释了 §3.2 的 GLM 映射表为什么要把 `xhigh`/`max`/`ultracode` 归为一档：
**它们是同一个客户端档位的不同表述。**
同时也再次确认：**任何对 `effort` 做官方枚举白名单校验的兼容层都会拒绝掉合法请求。**

### 3.3 各家 Anthropic 兼容端点的形状差异

**接入形态本身就不统一**，这是 D8 的具体表现【文档】：

| 厂商 | `base_url`（SDK 会自行追加 `/v1/messages`） | 鉴权 | 路径特征 | `count_tokens` |
| --- | --- | --- | --- | --- |
| Anthropic 官方 | `https://api.anthropic.com` | `x-api-key` | 权威 | 有 |
| DeepSeek | `https://api.deepseek.com/anthropic` | `x-api-key` | — | **未见文档** |
| Kimi / Moonshot | `https://api.moonshot.cn/anthropic` | **仅 `Authorization: Bearer`**（OpenAPI 里没有 `x-api-key` 方案） | — | 兼容规范里没有 |
| 智谱 GLM | `https://open.bigmodel.cn/api/anthropic` | `x-api-key` | — | 未知 |
| 阿里百炼（按量） | `https://{WorkspaceId}.{region}.maas.aliyuncs.com/apps/anthropic` | `x-api-key` 或 `Bearer` | 段是 **`apps/anthropic`**，不是 `anthropic` | **无，且 `/v1/models` 明确 404** |
| 阿里百炼（Coding Plan） | `https://coding.dashscope.aliyuncs.com/apps/anthropic` | 套餐专属 Key | Key 与 base_url 必须配套，否则 401 | — |
| 阿里百炼（Token Plan） | `https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic` | 套餐专属 Key | 同上 | — |
| 阿里百炼（旧版） | `https://dashscope.aliyuncs.com/api/v2/apps/claude-code-proxy` | — | 仅支持 qwen3-coder-plus，已废弃 | — |
| MiniMax | `https://api.minimax.io/anthropic`（国内 `.cn`） | SDK 默认 | — | **有** |
| 火山方舟 Ark | `https://ark.cn-beijing.volces.com/api/compatible` | API Key | 段是 **`api/compatible`** | **有** |
| 腾讯混元 | `https://api.hunyuan.cloud.tencent.com/anthropic` | **`x-api-key` 必需** | — | 未知 |
| SiliconFlow | `https://api.siliconflow.cn/` | `Authorization: Bearer` | 文档渲染为 `POST /messages`，`/v1` 前缀未确认 | 未见文档 |

四个可直接落地的观察：

1. **鉴权头有两种形态**（`x-api-key` / `Bearer`），兼容层应同时接受。
   官方 gateway 指南把这条升级成了硬要求：Claude Code 的两种凭据环境变量
   （`ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_API_KEY`）分别映射到这两个头，
   **你无法控制用户配哪个**（§3.2.2）。
2. **"计费套餐"渗进了协议配置**：同一个模型，按量计费和 Coding Plan 是**不同的
   主机名 + 不同的 Key**，混用直接 401。这属于 D8 的极端形式——
   能力边界不在协议里，在商务关系里。
3. **路径前缀没有统一规律**：`/anthropic`、`/apps/anthropic`、`/api/anthropic`、
   `/api/compatible` 四种并存。**不能靠字符串拼接猜端点**。
4. **`anthropic-version` 会被静默忽略**（DeepSeek 文档明确 Ignored），
   而 `anthropic-beta` 在 DeepSeek 是 "Ignored for `/messages`"、
   在混元是"不处理此头部"【文档】。但官方指南要求**原样转发 `anthropic-beta`**
   （§3.2.2 第 3 条）——**厂商忽略它，和兼容层有权剥掉它，是两件事**。

### 3.4 Anthropic 兼容端点的字段级差异（已核实部分）

阿里百炼的 Anthropic 兼容文档是这一侧最完整的对照物【文档】，
与 DeepSeek 的兼容表【文档】对比（**加粗行为本次新增的厂商**）：

| 项 | Anthropic 官方 | 阿里百炼 | DeepSeek |
| --- | --- | --- | --- |
| `temperature` 范围 | **已废弃**：新模型只接受 `1.0`，其他值 400 | `[0, 2)`（文档自带迁移警告） | `[0.0, 2.0]` |
| `top_p` 语义 | **已废弃**：新模型只接受 `>= 0.99` | 常规 | **仅 thinking 模式生效，下界 0.95** |
| `top_k` | **已废弃**：新模型任何值都 400 | 支持 | **Ignored** |
| `tool_choice` 值 | `auto`/`any`/`tool`/**`none`** | `auto`/`any`/`none`/`tool` | `none`/`auto`/`any`/`tool` |
| `disable_parallel_tool_use` | 支持 | — | **Ignored** |
| `thinking.budget_tokens` | 支持 | **即将废弃**，改用 `output_config.effort` | **被忽略**（`budget_tokens` is ignored） |
| `thinking` 默认开关 | 关 | **随模型变化**（qwen3.8/deepseek-v4/glm 默认开；kimi-k2.6/2.5 默认关；kimi-k2.7-code/kimi-k2-thinking/MiniMax-M2.5/2.1 只能开） | — |
| `stop_sequences` 命中 | `stop_reason=stop_sequence` + 回填序列 | **`stop_reason` 仍为 `end_turn`**，不回填 | Fully Supported |
| 结构化输出 | `output_config.format` | `output_config.format`，但**非严格模型会退化成普通 JSON 模式**，且提示词里必须含 "JSON" 字样，否则报 `'messages' must contain the word 'json' in some form` | — |
| `cache_control` | 支持 | 支持 | **Ignored** |
| `service_tier` / `container` / `mcp_servers` | 支持 | — | **Ignored** |
| content block `document` | 支持 | — | **Not Supported** |
| `redacted_thinking` 输入 | 支持 | — | **Not Supported** |
| `thinking` 块的 `signature` | 真实签名 | 样例中为空串【文档】 | — |
| usage 字段 | 9 项（含 `output_tokens_details.thinking_tokens` 等） | 4 项 | 4 项（`cache_*_input_tokens` 有，其余无） |

**其余厂商的尖锐差异**（本次新增，全为【文档】）：

| 厂商 | 差异 | 后果 |
| --- | --- | --- |
| **Kimi** | `tool_choice` **只有 `auto`/`any`/`none`，没有 `tool`**；`temperature`/`top_p` **整个不在请求 schema 里**；无 `top_k`；`stop_reason` 枚举**没有 `stop_sequence`**（命中返回 `end_turn`） | 用 `tool_choice:{type:"tool"}` 强制指定工具会 400；显式调温度的客户端请求会被丢弃 |
| **Kimi** | 提示词缓存改用**非标准的顶层 `cache_control`**，**`messages` 体内的 `cache_control` 会被忽略** | 位置放错就静默失去缓存 |
| **Kimi** | `usage` 对齐得很好（含 `cache_creation` 分解与 `thinking_tokens`），错误信封也一致 | 它是这一侧最规范的实现 |
| **MiniMax** | `service_tier` 取值是 **`standard`/`priority`**（priority 1.5 倍计费），**不是官方的 `auto`/`standard_only`** | 传官方值语义不明 |
| **MiniMax** | **`thinking:{"type":"adaptive"}` 表示"开启"**（M3）；**没有 `budget_tokens` 形式**；`top_k`/`stop_sequences`/`context_management`/`container` **均被忽略** | 与官方 `adaptive` 的语义**不同**（官方是"自动决定"，这里是"显式开启"）——**同名不同义，D2 的典型** |
| **SiliconFlow** | **`tool_choice` 只有 `Auto`/`Tool`/`None`，没有 `any`** | 与 Kimi 恰好互补：两家各缺一个值 |
| **SiliconFlow** | **流式以 `data: [DONE]` 结束** | **Anthropic 没有 `[DONE]` 哨兵**——纯 Anthropic 客户端会把它当成多出来的垃圾帧 |
| **火山方舟 Ark** | **`output_format` 是非标准字段名**（官方是 `output_config.format`）；`output_config.effort` 多出 `none`/`minimal`；`service_tier` 取值 **`default`/`flex`**；`stop_sequences` 上限 **4** | 结构化输出的字段名对不上，按官方写的请求拿不到结构化输出 |
| **火山方舟 Ark** | `messages[].role` **接受 `user`/`system`/`assistant`/`developer`**；`max_tokens` **可选**；支持 `document`、`signature_delta` 与 thinking 签名回放 | **是这一侧最接近官方、且明确容忍 `role:"system"` 的两家之一**（另一家是百炼） |
| **火山方舟 Ark** | 错误事件多出 `code` 与 `param` 成员 | 结构是超集，通常无害 |
| **腾讯混元** | **`stop_reason` 用 `sensitive` 取代官方的 `refusal`**；无 `pause_turn`/`model_context_window_exceeded`；**不发 `signature_delta`**（签名挂在 `content_block_start` 且固定为空） | **自造枚举值**——严格按官方枚举 switch 的客户端会落到 default 分支 |
| **腾讯混元** | `message_start.message.stop_reason` **固定为空**；`service_tier` **坐在 `usage` 里**；**不发 `ping`**；`anthropic-version` 在文档里根本不出现 | 见 §3.2.3：不发 ping = 长思考时会被 Claude Code 掐断 |
| **腾讯混元** | content 类型**仅 `text`/`thinking`/`tool_use`/`tool_result`**（无 image/document/cache_control） | 多模态与缓存能力全线缺失 |
| **智谱 GLM** | **根本没有发布字段级兼容矩阵**，只有一句"某些场景下仍存在差异，但不影响整体兼容性" | **它的偏差是"未测量"，不是"无"**——见下方说明 |

> **一个方法论要点，值得单列：厂商不发布兼容矩阵 ≠ 没有偏差。**
> GLM 与 ModelScope 属于这一类。本报告对它们的态度是标 **`UNMEASURED`**，
> 而不是像字段表那样填"支持"。这与 §6.2 能力画像的设计直接相关：
> **画像里"未知"必须是一个合法取值**，不能被默认成"支持"——
> 否则兼容层会把一个从未验证的假设当成事实，而这正是 §0.3 批判的那种静默。

> **`signature: ""` 是个隐蔽的坑。** 官方 schema 对 thinking 块的签名有明确规定：
> *"Used to verify that the block was generated by Claude when it is passed back to
> the API … Thinking blocks must be passed back unmodified and in their original
> order; **a modified block results in a 400**"*【文档】。空签名意味着签名在往返中
> 被抹掉，任何做 thinking 内容往返的兼容层都要把它列为**结构性风险**，
> 而不是当成普通文本。混元更进一步：它**不发 `signature_delta`**，
> 签名字段固定为空——**在这条链路上，"保真回传签名"这件事本身不成立**。

9 家厂商 × 30 余字段的完整对照矩阵（含 `UNMEASURED` 标记与逐条来源）
见 [anthropic-messages-compat-dossier.md](research/anthropic-messages-compat-dossier.md)。

### 3.5 反向偏差：权威标准自己也在动

这一节单独拎出来，因为它是**唯一一类"厂商比官方更宽松"的偏差**，而且它推翻了
"以官方为稳定锚点"的直觉。

当前 Anthropic 官方 schema 里【文档】：

- `temperature`：**Deprecated**。"Models released after Claude Opus 4.6 do not
  support setting temperature. A value of 1.0 will be accepted for backwards
  compatibility, **all other values will be rejected with a 400 error**."
- `top_p`：**Deprecated**，新模型只接受 `>= 0.99`。
- `top_k`：**Deprecated**，"any value will be rejected with a 400 error"。
- `thinking` 配置新增了 `type: "adaptive"` 这一官方形态，而厂商侧几乎都只实现了
  `enabled`/`disabled`。
- `budget_tokens` 有硬约束：`>= 1024` **且小于 `max_tokens`**。

后果是：

1. **"照官方发"未必安全。** 一个兼容层如果按 Anthropic 官方文档透传
   `temperature: 0.2`，对**新版官方模型**是 400，对国产兼容端点是正常生效。
   换句话说，**同一份请求在两个方向上的合法性是相反的**。
2. **兼容层必须把"官方"也当成一个需要画像的上游**，而不是永远正确的基准。
3. **`thinking.budget_tokens < max_tokens` 是一条容易被忽略的硬约束**：
   兼容层在做 `reasoning.effort → budget_tokens` 映射时，
   如果不同时抬高 `max_tokens`，就会造出官方必然拒绝的请求。
   本仓库原审计 §3.6 已经记录过对称的问题（budget 与 `max_tokens` 冲突时
   整个 thinking 配置被静默丢弃）。
4. 厂商侧反而成了"更守旧、更宽容"的一方——**这与"国产实现质量差"的直觉相反**，
   偏差的方向取决于**你在哪个时间点对齐**。

---

## 4. 开源推理服务：vLLM / SGLang

这两家的偏差性质与厂商不同——**它们不是"实现不全"，而是"契约由部署决定"（D6）**，
并且它们的 Anthropic / Responses 实现是**原生**的，可以直接读源码比对。

> **版本告警（很重要）。** 本节（以及 §4.3.1、§4.6、§4.7）的证据取自两家的
> `main` 分支源码与当前文档，相关改动的时间跨度从 2024-12 一直到 2026-09。
> **线上大多数部署跑的是更早的 release 标签**，所以这些"当前行为"未必适用于
> 你手上的那个 vLLM / SGLang 版本。**动手前先核对版本**，
> 逐条的 file:line 明细见 [vllm-sglang-protocol-divergences.md](vllm-sglang-protocol-divergences.md)。
> 另外：本节结论来自**读源码与文档**，没有真机运行，流式顺序类结论属于源码推导。

### 4.1 两家都原生提供三套协议

| 端点 | vLLM | SGLang | 证据 |
| --- | --- | --- | --- |
| `/v1/chat/completions` | ✅ | ✅ | — |
| `/v1/responses` | ✅ 有完整实现与事件测试 | 有（另有独立 adapters/serving 模块） | 【源码】 |
| `/v1/messages`（Anthropic） | ✅ `vllm/entrypoints/anthropic/` | ✅ `python/sglang/srt/entrypoints/anthropic/` | 【源码】 |

这本身就是个重要结论：**"官方最权威、开源只支持 Chat"这个前提已经不成立了。**
两家的 Anthropic 端点是按官方结构写的 pydantic 模型，不是第三方代理糊出来的。

### 4.2 vLLM 的 Anthropic 实现：字段级对照（源码）

`vllm/entrypoints/anthropic/protocol.py` 的 `AnthropicMessagesRequest` 字段【源码】：

```text
model, messages, max_tokens, metadata, output_config, stop_sequences,
stream, system, temperature, tool_choice, tools, top_k, top_p
+ vLLM 扩展: cache_salt, kv_transfer_params, ec_transfer_params,
             vllm_xargs, chat_template_kwargs
```

由此可读出若干**文档不会明说**的偏差：

| 项 | vLLM 实现 | 与官方的差异 |
| --- | --- | --- |
| 顶层 `thinking` | **该字段不存在** | Claude Code 若发 `thinking`，取决于 pydantic 的 extra 策略——该文件**未声明 `model_config`**，即默认 `extra="ignore"`，**静默丢弃**【源码】【推定】 |
| `tool_choice.type` | `auto`/`any`/`tool`/`none` | **与官方一致**（`none` 是官方就有的） |
| `stop_reason` | 只有 `end_turn`/`max_tokens`/`stop_sequence`/`tool_use` | 缺 `pause_turn`/`refusal`/`model_context_window_exceeded` |
| `usage` | 只有 4 个字段 | 缺 `cache_creation` 明细、`output_tokens_details`、`service_tier` 等 |
| content block 类型 | text/image/tool_use/tool_result/**tool_reference**/thinking/redacted_thinking | **无 `document`**、无 server tool 结果类 |
| `messages[].role` | `user`/`assistant`/**`system`** | 官方消息内**不允许** `system` 角色 |
| `tools[]` | 增加 `strict`、`defer_loading` | 非官方字段 |
| `stop_sequences` | 有 `max_length=envs.VLLM_MAX_STOP_STRINGS` 上限 | 官方无此上限 |
| 响应 | 增加 `kv_transfer_params`/`ec_transfer_params` | 非官方字段 |
| `count_tokens` | **实现**（`AnthropicCountTokensRequest`） | 与官方一致，但不是所有厂商都做 |

### 4.3 SGLang 的 Anthropic 实现：比 vLLM 更全的地方

`sglang/srt/entrypoints/anthropic/protocol.py`【源码】比 vLLM 多出：

- **`thinking` 请求参数是支持的**，并且实现了官方的判别联合语义：
  `enabled` 必须带 `budget_tokens`，**且 `budget_tokens >= 1024`**，
  `disabled` 不允许带 `budget_tokens`/`display`——校验规则与官方 SDK 对齐。
- **`output_config` 与 `betas: list[str]`**（对应 `anthropic-beta`）。
- **服务端工具**：`AnthropicWebSearchTool`（`web_search_\d{8}`）、
  `AnthropicComputerTool`、`AnthropicBashTool`、`AnthropicTextEditorTool`，
  以及 `SearchResultBlock`/`ToolReferenceBlock` 等内容块。
- `AnthropicCustomTool` 带 `defer_loading`。

**所以"开源服务比官方差多少"的答案要分维度**：在 Anthropic 协议的**覆盖面**
上 SGLang 已接近官方子集（`/v1/messages` 与 `/v1/messages/count_tokens` 都是官方路径，
`system` 接受字符串或 text block 数组【文档】），差距主要在：

1. **模型能力依赖 parser 配置**（D6）——协议对了，模型不一定会用工具；
2. **`usage` 明细缩水**（无 `cache_creation` 分解、无 `thinking_tokens` 明细）；
3. **`stop_reason` 枚举缩小**；
4. **服务端工具是"能声明"还是"真能执行"要分开看**；
5. **不校验 `model` 字段**：SGLang 文档明确"does not validate the request `model`
   field and serves whatever model was loaded at startup"【文档】。
   官方会对未知模型报错，这里**静默忽略**——又是一个 D1a。

### 4.3.1 一个被两个开源项目独立记录的真实故障：Claude Code 的归属头

这是本次调研里**证据最硬的一条 L2 偏差**，因为 vLLM 和 SGLang 的文档各自记录了它：

Claude Code 会在 system prompt 开头注入一段**每请求都会变化**的归属信息
【文档】：

```text
x-anthropic-billing-header: cc_version=<ver>.<per-request-hash>; cc_entrypoint=...; cch=<hash>;
```

SGLang 文档把后果讲得最清楚【文档】：

> The per-request hash is the **first token to differ between turns**, so the radix
> prefix cache can only reuse the short prefix before that hash and re-prefills the
> system prompt plus the entire conversation history on every turn.

也就是说：**协议完全正确，功能完全正常，但多轮对话每轮都在全量重算前缀。**
修复方式是客户端侧一个环境变量：

```bash
CLAUDE_CODE_ATTRIBUTION_HEADER=0
```

三个必须记住的细节：

1. 这是一个**只有在网关场景下才暴露**的问题——直连 Anthropic 时它被官方吸收掉了。
2. `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` **不能**解决它——SGLang 文档专门加了
   一条 note 澄清：那个变量只管自动更新/遥测/错误上报，
   "*The attribution header is a separate code path*"【文档】。
   而智谱的接入文档恰恰推荐了 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`——
   **两个来源的建议不等价，必须以 `ATTRIBUTION_HEADER` 为准。**
3. 两侧的修法不同，而且**修在哪一层本身就是设计分歧**：
   vLLM 文档说 > 0.17.1 自动处理，旧版要求客户端设 `CLAUDE_CODE_ATTRIBUTION_HEADER=0`；
   SGLang 既有服务端处理（PR #21064），文档里**仍然**把
   `CLAUDE_CODE_ATTRIBUTION_HEADER=0` 列为"Required"【文档】。
   **同一个问题、两种修法、且文档口径不一致**——这正说明它不是协议问题，
   而是网关责任边界问题。
4. 量级不小：关闭后有实测把缓存命中从约 4.8K 提升到 20.8K+ tokens，
   每轮 prefill 从约 82K 降到约 285 tokens【实况】。
   也就是说这是一个**能把成本差出一个数量级**的"协议外"偏差。

### 4.3.2 工具调用"被接受但不生效"的静默形态

SGLang 文档给出了一条非常具体的静默降级【文档】：

> Without it the `tools` field is **still accepted** but the model's tool calls come
> back as **text** instead of `tool_use` blocks, and Claude Code cannot execute them.

即：**没有配 `--tool-call-parser` 时，请求不会 400，`tools` 被照收，
但工具调用变成正文文本返回。** 这是 D6 + D1a 的复合形态，
也必须靠 §6.5 的探针来发现——因为从 HTTP 层看不出任何异常。

### 4.4 vLLM 的 Responses 实现：Codex 能不能用

从 `vllm/entrypoints/openai/responses/protocol.py` 与 `streaming_events.py`【源码】：

- 请求侧**字段面**相当完整：`include`（含 `reasoning.encrypted_content`、
  `message.output_text.logprobs`）、`parallel_tool_calls`、`previous_response_id`、
  `store`、`truncation`、`include_reasoning`（vLLM 扩展）。
  **但"有字段"不等于"有行为"**：
  - **`store` 默认被静默忽略**，除非设了环境变量
    `VLLM_ENABLE_RESPONSES_API_STORE`（`store=True` 被静默降级为 false，
    只有 `store` + `background` 组合才报错）【源码】。
  - `prompt_cache_key` **被接受但明确是死字段**——vLLM 文档原文：
    *"has not been implemented yet and vLLM will ignore it"*【文档】。
    这是又一个 D1a：字段在、协议对、**行为没有**。
  - `previous_response_id` 需要 `VLLM_ENABLE_RESPONSES_API_STORE=1`，
    否则 id 查不到【源码】。
  - 作为对照：**SGLang 需要 `--enable-response-store`，但缺失时会返回一个明确
    指出该 flag 名字的 400**【源码】。**同一个能力，两种错误哲学**：
    vLLM 静默、SGLang 显式。这条差异比字段本身更值得关注。

  > **⚠️ 更正：这条偏差对 Codex 其实无害。**
  > 本文早期版本说"vLLM 静默忽略 `store`，对 Codex 这种依赖多轮状态的客户端是致命的"。
  > **这是错的。** §3.1 的源码取证表明：Codex 在 HTTP 上**恒发 `store: false`**、
  > **从不使用 `previous_response_id`**，而是每轮把完整 `input` 数组重发一遍，
  > 完全无状态。所以 vLLM 的 `store` 行为**根本不进入 Codex 的路径**。
  > 受影响的只是**其他**依赖服务端状态的客户端（OpenCode 等 agent），
  > 以及希望用 `GET /v1/responses/{id}` 取回历史的使用方。
  > 这个错误的教训值得保留：**判断一条偏差的危害，必须先确认目标客户端是否走那条路径**，
  > 而不是从"字段名对得上"推出"客户端依赖它"。
- **vLLM 的请求基类是 `extra="allow"`**：未知字段（包括**已被移除**的
  `guided_json`/`guided_regex`/`guided_choice`/`guided_grammar`）会被接受、
  返回 200，**输出不受约束**，只在 debug 级别留一行日志【源码】。
  这是一个比"400"危险得多的形态：调用方以为拿到了结构化输出保证，
  实际拿到的是无约束生成。SGLang 用 pydantic 默认策略更安静地丢弃未知字段。
  > 与 §3.1 的 `client_metadata` 恰好构成一对**反向要求**：
  > **对上游**（vLLM）宽容未知字段是好的，因为 Codex 会恒发非标准字段；
  > **但宽容本身也让"用户以为设了约束"变成静默失效**。
  > 正确的分界是：**未知字段容忍，已知但未实现的字段必须告警**（§6.6）。
- 流式事件（源码中 Literal 声明的命名事件）共 24 个，**只发
  `reasoning_text.*` 和 `reasoning_part.*`，不发 `reasoning_summary_*`**。
  因为 Codex 两者都认（§3.1），所以**可用**，但**思维摘要不会像官方那样呈现**。
  另外 vLLM 的 responses 模块里 **`response.failed` 命中数为 0**——
  也就是说它**发不出 Codex 唯一认的错误终态**（§3.1 第 3 条），
  失败时 Codex 只能靠 `response.completed` 缺失走重试路径。
- vLLM 官方文档直接为 Codex 和 Claude Code 各写了一页接入指南【文档】，
  这本身就说明它能跑；但那两页里的 warning 才是真正的兼容代价：
  - Claude Code 会在 system prompt 里注入**每请求变化的 hash**，导致
    **前缀缓存失效、性能大幅下降**；vLLM > 0.17.1 才自动处理，旧版需要设置
    `CLAUDE_CODE_ATTRIBUTION_HEADER=0`【文档】。
  - Claude Code 侧**不能使用带 `/` 的模型名**（如 `openai/gpt-oss-120b`），
    必须配 `--served-model-name`【文档】。

### 4.5 开源侧独有的两个"偏差放大器"

1. **`chat_template_kwargs` 是个"协议外的传参通道"。**
   同一个 `enable_thinking`，在 Qwen3 上是"默认开、可关"，在 DeepSeek-V3.1 上是
   "默认关、要显式 `thinking=True`"，在 Gemma 4 上是"默认关、`enable_thinking=True`"
   【文档】。**这个差异是模型模板的差异，不是 API 的差异**，
   因此无法从端点推断。
2. **`reasoning_effort` 会被翻译成模板 kwarg**：vLLM 文档明确
   `low|medium|high → enable_thinking=true`、`none → enable_thinking=false`
   【文档】。也就是说**一个 OpenAI 的采样参数，在开源侧被复用成了思考开关**——
   这正是 D3 命名漂移的成因。

### 4.6 其余源码级事实（逐条可复核）

以下条目来自对两家 `main` 分支源码的直接阅读，明细与 file:line 见
[vllm-sglang-protocol-divergences.md](vllm-sglang-protocol-divergences.md)【源码】：

| 事实 | vLLM | SGLang | 对兼容层的含义 |
| --- | --- | --- | --- |
| 思维链字段名 | **`reasoning`**（接受入站 `reasoning_content` 并改写，但只输出 `reasoning`） | **`reasoning_content`**（有意为之） | **必须双读**；写死任一个都会静默丢 CoT |
| thinking 签名 | **用 `uuid4().hex` 伪造** | 不发 `signature_delta`，不伪造 | 伪造的签名在回传时无法通过真实验证；**兼容层不应把它当可信签名往返** |
| `system_fingerprint` | 发 `vllm-<ver>-<hash8>`，**不是官方 `fp_…` 格式** | **完全不发** | 不能按官方格式解析 |
| `cached_tokens` | 默认关闭，需 `--enable-prompt-tokens-details` | 默认关闭，需 `--enable-cache-report` | 缓存统计默认读不到，且 flag 名不同 |
| `finish_reason` | 枚举里**没有 `content_filter`**，永不发出；有 `abort`/`error`/`repetition` | 有 `abort`/`content_filter`，但在 Anthropic 路径上**被映射成 `end_turn`** | 映射表必须按服务分别维护 |
| 错误信封 | 嵌套正确，但 `error.code` 是 **int**；401 是 `{"error":"Unauthorized"}`（字符串） | `/v1/chat/completions` 是**扁平**的 `{"object":"error","message",…,"type":"400","code":400}`；401 同样是裸字符串 | `body["error"]["message"]` 在 SGLang 上会 **KeyError**——**在报错路径上二次崩溃** |
| Anthropic `id` | `msg_{epoch_ms}`，**毫秒内会碰撞** | `msg_{uuid4().hex}` | 不能当唯一键 |
| `ping` 事件 | 声明了但**从不发** | 声明了但**从不发** | **结合 §3.2.3，这不只是"少个心跳"**：Claude Code 在 300 秒无字节时**主动中止流**，而长思考停顿期间 ping 是唯一流量。**兼容层必须自己补字节**，不能靠上游 |
| Responses 事件覆盖 | 有 `response.reasoning_part.*`（非标准），**缺 `response.failed`/`response.incomplete`** | **缺文档**，但发了 `response.failed`/`response.incomplete`/`reasoning_summary_*` | **SGLang 的 Codex 兼容性在终态与摘要维度反而更好**——与"文档更全 = 实现更全"的直觉相反 |
| parser 名字 | `deepseek_r1` / `openai_gptoss` / `llama3_json` / `minimax_m2` | `deepseek-r1` / `gpt-oss` / `llama3` / `minimax-m2` | **启动参数不可跨服务复制**，图像/探针要按服务区分 |

**一条重要的银弹**：两家现在都**无需任何 flag 就原生提供 `/v1/messages`**
（vLLM PR #22627、SGLang PR #18630）。
也就是说，**为了"协议翻译"而自建代理的理由已经不存在了**；
自建兼容层的正当理由只剩下路由、鉴权、配额、成本与可观测性——
以及本文讨论的**跨厂商差异抹平**（这恰恰是原生实现不做的事）。

### 4.7 同类代理做了什么、没做什么

| 代理 | 能力 | 已知的"没抹平"处 |
| --- | --- | --- |
| **LiteLLM** | 唯一有书面兼容契约：`/v1/messages` 原生透传（≥1.92.0）、`/v1/responses`（≥1.102.0），均需 `model_info.supported_endpoints` 显式开启 | 未开启时会**经 `/v1/chat/completions` 桥接**，于是：**`cache_control` 的 `ttl` 被丢弃**（Claude Code 的 `ttl:"1h"` 静默变 5 分钟默认）、`thinking` 被近似、`previous_response_id` 原样转发（无状态后端直接 400）；bug #29518 会在流式下丢掉 `reasoning_content`→`thinking` 的映射 |
| **y-router** | — | **已归档**（最后提交 2026-01-11） |
| **claude-code-router / claude-code-proxy** | vLLM issue #21313 点名的两个原生实现之前的过渡方案 | 属于"在原生支持出现前"的产物 |

这张表给出一个判断：**代理层的价值不在于"能转协议"，而在于"能把差异显式化"。**
如果它只是把请求转过去，那它既不如原生端点，还会新增一层静默降级。

---

## 5. 差距到底有多大：按失效级别排序

把上面所有偏差按"会不会断链"重新排一次，这才是排期依据：

| 级别 | 后果 | 典型偏差 |
| --- | --- | --- |
| **L0 断链** | 400 / 解析失败 / 永久挂起 | 模型强制 thinking 但客户端关了（Kimi 400）；**思考模式 + `tool_choice: required`/object（Qwen/DeepSeek 400）**；**`messages[].role:"system"` 被拒（Kimi/混元/DeepSeek 400，§3.2.1）**；**顶层 `system` 的 block 数组被忽略/拒绝**；**`role:"developer"` 被拒（DeepSeek 400，Ark 升级前也 400）**；**带 `tool_calls` 的历史缺 `reasoning_content`（DeepSeek 400）**；**`tools` 与 `stream=true` 同时使用（Qwen 直接不支持）**；**错误信封没有 `error` 壳、或 body 是裸字符串（SGLang/Qwen/SiliconFlow）→ 在报错路径上二次抛错**；**只发参数增量、`output_item.done` 不带完整调用 → Codex 永远拿不到工具调用（§3.1）**；**`response.completed` 缺失 → Codex 重试 5 次后失败（§3.1）**；**静默超过 300 秒无字节 → Claude Code 主动中止（§3.2.3）**；上游不发终态事件；内容块不闭合；模型名带 `/`；base_url 与 Key 套餐/地域不匹配（401） |
| **L1 静默语义漂移** | 链路通，结果错 | `store`/`include`/`truncation` 被静默忽略；**`store` 在 vLLM 上被静默忽略（`VLLM_ENABLE_RESPONSES_API_STORE` 未设时）**；`temperature` 范围不同；`top_p` 被 clamp；`max_tokens` 含不含思维链；未知 `finish_reason` 被当成 `stop`；**`finish_reason:""` 被 `is None` 判断漏过（StepFun）**；**流内错误未被解码，失败被当成正常结束（四种模型，§2.3）**；**裸 `error` 事件被 Codex 忽略 → 被当成未完成（§3.1）**；**`stop` 语义反转（混元）**；**`message_shape` 违规（Qwen 交替约束）**；**`max_tokens` 与 `max_completion_tokens` 同发时被静默改语义（千帆）**；**`stop_reason` 自造值（混元 `sensitive`）或缺失（百炼/Kimi 命中 `stop_sequences` 仍回 `end_turn`）** |
| **L2 能力降级** | 能跑，但更慢/更贵/更笨 | `cache_control` 被忽略（**且不报错，每轮按未缓存计费**）；`budget_tokens` 被忽略；前缀缓存被每请求 hash 打散；工具调用没开 parser；**历史里的 CoT 被静默剥掉（GLM `clear_thinking` 默认 true）→ 模型变笨但无报错**；**`reasoning_content` 落到 `content` 的 `<think>` 标签里（MiniMax 默认）**；**读 `x-ratelimit-*` 做退避的上游（OpenRouter）永远读到空值** |
| **L3 信息丢失** | 不影响功能 | 缺 `service_tier`、`system_fingerprint`、`logprobs` 明细、annotations |

**关键判断：L0 的条目是有限的、可枚举的；L1/L2 是无限的、只能靠机制对付。**
所以兼容工程的投入应该按这个比例分配——而不是逐个厂商去修 L1。

值得单独指出：**本表 L1 里新增的几条，全部是"字段名对得上、语义不对"的类型**，
其中"流内错误不解码"和"CoT 被静默剥掉"在**任何字段级对比测试里都不会失败**。
它们是本文相对既有审计的主要增量。

---

## 6. 兼容策略：怎么把可用性做到最大

### 6.1 三个姿态要分开

一个兼容层同时扮演三个角色，混在一起做就会互相污染：

| 姿态 | 面向 | 正确做法 |
| --- | --- | --- |
| **Emitter** | 客户端（Codex / Claude Code / 第三方 SDK） | **严格**：只发官方 schema 允许的东西，把厂商怪癖藏在里面 |
| **Consumer** | 上游厂商端点 | **宽松**：接受字符串或数组、接受扩展字段、接受枚举外的值、容忍缺字段 |
| **Negotiator** | 上游能力 | **显式**：把"支持什么"数据化，不靠猜 |

这正好对应 Postel 定律，但**必须加一条**：宽松解析不等于静默吞掉——
不能翻译的必须变成**可见的 warning**（见 §6.6）。

### 6.2 用"能力画像"取代 "OpenAI-compatible" 布尔

"是否兼容 OpenAI"这个布尔值已经被证明无意义——DeepSeek 的 Responses 端点在
**协议层严格兼容、在能力层大面积静默忽略**。改成按 (上游, 端点, 模型) 三元组
描述能力：

```yaml
# capability profile 草案
upstream: deepseek
endpoint: responses
model: deepseek-flash
stateful:
  store: false              # 恒为 false
  previous_response_id: unsupported   # → 必须每轮自带完整历史
params:
  parallel_tool_calls: ignored        # 发出去也没用，别依赖
  include: ignored
  metadata: ignored
  truncation: unsupported             # 超窗直接 400
  reasoning.effort: [low, high, max]  # 注意没有 medium
  reasoning.summary: accepted_noop
enums:
  finish_reason_extra: [insufficient_system_resource, aborted]
tools:
  custom: [apply_patch]               # 只认这一个
stream:
  terminator: response.completed      # 无 [DONE]
  usage_location: completed_event
```

第二个画像示例，用于 Chat 侧，覆盖 §2.2/§2.3 里那些**会改变请求形状**的差异：

```yaml
upstream: qwen
endpoint: chat_completions
model: qwen3.8-max
message_shape:                # 不是"建议"，是硬约束
  system_only_first: true
  must_alternate: true
  must_end_with_user: true
length_params:
  family: max_tokens_only     # 无 max_completion_tokens
  both_sent: n/a
reasoning:
  field: reasoning_content
  roundtrip: required_with_tools   # 见 §2.3(1)
  supports_disable: true
  disable_required_when: [non_streaming]   # 非流式必须 enable_thinking=false
tool_choice:
  required: unsupported
  object: unsupported
  in_thinking_mode: forbidden
constraints:
  tools_with_stream: false    # 文档明确不能同时用
stream:
  usage_location: separate_empty_choices_chunk
  error_model: openai_event
errors:
  envelope: flat              # 无 error 外壳
  billing_as_429: true        # CommodityNotPurchased 等，盲目重试会死循环
```

这两份画像的要点：

1. **枚举要按上游覆写**，不能直接用官方枚举做白名单（§3.2 的 `ultracode` 就是反例）。
2. **区分 `unsupported` / `ignored` / `accepted_noop`**——三者的应对完全不同。
3. **`ignored` 必须能触发 warning**，否则等于把厂商的静默变成自己的静默。
4. **新增三个非参数字段：`message_shape`、`reasoning.roundtrip`、`stream.error_model`。**
   它们描述的不是"某个参数支不支持"，而是"消息和流的骨架长什么样"——
   恰恰是 §5 里 L0/L1 级失效的来源。**把它们放进画像，才能让"不可用"
   在配置阶段就暴露，而不是在线上第一次工具调用时才暴露。**

### 6.3 编码保守交集（对上游）

在能力画像未知时，默认只发交集。经验性清单：

- **不要默认发**：`developer` role、`response_format.json_schema`、
  `tools[].strict`、`parallel_tool_calls`、`store`、`previous_response_id`、
  `service_tier`、`metadata`、`n>1`、`seed`、`logit_bias`。
- **`max_tokens` 两种名字**：不要同时发。由画像决定发哪个；未知时优先
  `max_tokens`（Chat 侧覆盖面最广），Anthropic 侧必须发（它是必填）。
- **`stop`**：按最小值发（≤4），不要依赖 DeepSeek 的 16。
- **`reasoning_effort`**：交付前按画像**收窄到上游支持的值**；
  未知上游时不要发，或只发 `high`——它出现在所有已知枚举里
  （官方 low/medium/high、DeepSeek low/high/max、GLM low/high/max、
  阿里 low/medium/xhigh/max）。**`medium` 反而是最不安全的那一个。**
- **thinking 开关**：优先用上游的**原生**表达（Anthropic `thinking` /
  Responses `reasoning.effort`），Chat 侧的 `thinking`/`enable_thinking`/
  `chat_template_kwargs` 只在画像明确时使用。

### 6.4 解码宽松 + 规范化（对上游响应）

兼容层的价值很大一部分在"把各家的流规整成一条官方流"。必须做的规范化：

| 规范化 | 针对的偏差 |
| --- | --- |
| `reasoning_content` ↔ `reasoning` 双读双写 | D3（vLLM 改名；MiniMax 用 `reasoning_details`） |
| `finish_reason` 映射表 + **保留原始值** | D2（`insufficient_system_resource`、`aborted`） |
| `finish_reason: ""` 空串归一为 `null` | D2（StepFun 用空串代替 null） |
| `system_fingerprint: ""` 归一为省略 | D2（阿里百炼返回空串） |
| **`stop` 语义反转的内容后处理** | D2（腾讯混元停在匹配串**之后**）：出口侧需裁剪尾部 stop 串并告警——这是**唯一必须做内容层后处理**的偏差 |
| usage 定位：独立 chunk / 末内容 chunk / 每 chunk / `message_delta` 四处都读 | D4 |
| 流终止补全：`[DONE]` 与终态事件互转 | D4 |
| content block 顺序修复：补齐缺失的 `*_stop`，去重 `message_start` | D4 |
| logprob 哨兵值归一（`-9999.0` → null） | D2 |
| tool-call id 合成与去重（按 index 而非 id 归并） | 各家的 delta 形态差异 |
| **"思考 + 强制工具"冲突的策略化降级** | Qwen/DeepSeek 直接 400、官方静默降级：统一降级为 `auto` 并产生 warning |
| **思维链字段的双读与归一**（`reasoning_content` / `reasoning` / `reasoning_details`） | D3：vLLM 只发 `reasoning`、SGLang 只发 `reasoning_content`、MiniMax 默认内联在 `content` 的 `<think>` 里 |
| **思维链的保真回传**：assistant 消息上保留原始 thinking 块，不参与任何规范化 | L0：DeepSeek 缺了就 400；GLM `clear_thinking`/MiniMax 会静默丢 |
| **流内错误统一为单一内部事件**（error event / `finish_reason` / `session.error` / 伪正常 chunk 四合一） | L1：不解码就会把"上游中途失败"当成"正常结束" |
| 扁平和裸字符串错误信封装的**安全解包**（缺 `error` 壳、body 是字符串） | L0：`["error"]["message"]` 在 SGLang/Qwen/SiliconFlow 上会二次抛错 |
| **`tool_calls_switch` 类"默认形态就不对"的开关**（星火默认返回 JSON 文本） | D7：不能靠"字段名对得上"判断工具调用可用 |

倒数第四条（流内错误）值得单独强调：**它是唯一一类"HTTP 状态码完全正常、
但语义已经失败"的偏差**，任何只看状态码的健康检查都发现不了它。

最后一条（工具 id）在本仓库有直接前科：`c0dd933` 修的正是"OpenAI 只在第一个 chunk 给
id、后续只给 index"导致工具调用被拆成两个【实况】。**这是所有厂商共有的形态问题，
不是某一家的问题。**

### 6.5 探针与能力缓存（D6 的必然要求）

因为 vLLM / SGLang 的契约取决于启动参数，**能力不能静态声明**。建议：

- 首次遇到一个 (endpoint, model) 组合时跑一组**最小探针**：
  1. 发一个带 `tools` + `tool_choice: "auto"` 的极短请求，
     看是否真的返回 `tool_calls`（判断 `--enable-auto-tool-choice` + parser 是否都开着）；
  2. 再发一个 `tool_choice: "required"` 的，**单独判断**——
     因为 vLLM 上 `required`/named 不需要 auto 开关也可能可用（§4.6），
     只测 `auto` 会低估能力；
  3. 发一个 `reasoning_effort`/`thinking` 请求，看响应里出现的是
     `reasoning_content` 还是 `reasoning`（判断 D3 的实际形态）；
  4. 发一个 `stream + include_usage`，看 usage 落在哪个 chunk、
     以及该 chunk 的 `choices` 是否为空（判断 D4；**不要对 `choices` 形状做断言**）；
  5. 发一个**必然报错**的请求（如非法 `model` 或超长 `max_tokens`），
     检查错误信封是嵌套还是扁平（判 SGLang `/v1/chat/completions` 的 KeyError 风险）；
  6. 试一个 `system` 数组（Messages 侧），判断 D1。
- 探针结果缓存并**按模型版本失效**（上游会升级）。
- 探针必须便宜：`max_tokens: 1`、无工具调用真实执行。

### 6.6 把"静默"变成一等公民

这一条是全文最重要的工程建议，而且本仓库已经吃过亏——
[author-feedback.md](author-feedback.md) 的核心结论就是
"**问题不在'没读到'，在'读到了但说错了'和'读不到但不吭声'**"。

具体做法：

1. **每个 `ignored` / `unsupported` 的字段产生一条 warning**，带字段路径、
   上游标识、以及"语义可能已改变"的说明。
2. **禁止把未知 `finish_reason` 归一成 `stop` / `end_turn`**。
   未知值应当原样保留（或映射为显式的 `unknown`），因为
   "上游超时"和"模型正常说完"对 agent 循环是**相反**的信号。
3. **不要替用户编数据**（默认 4096、默认 `detail:auto`、默认开 `cache_control`）——
   这类"发明"会改变模型看到的输入。
4. warning 走**结构化通道**，不要拼进 system prompt
   （本仓库目前有把不支持的工具说明塞进 system prompt 的做法，
   代价是模型会读到这段文字）。

### 6.7 对本仓库的落点

现有 IR（`LLMRequest` / `LLMResponse` / `Part` / `Warning`）大体够用，
需要补的是"厂商维度"的表达能力：

| 需要表达的东西 | 现状 | 建议 |
| --- | --- | --- |
| 上游原始 `finish_reason` | 会被折叠成 `FinishReason` 枚举 | 在 `LLMResponse` 增 `RawFinishReason` 字段保留原文 |
| 厂商扩展字段 | 无槽位 | 增 `ProviderExtensions map[string]json.RawMessage`，解码时收集、编码时按画像决定是否回放 |
| usage 明细 | 部分字段 | 增 `Usage.CacheCreation`（1h/5m 分解）与 `Usage.ServiceTier`；`ReasoningTokens` 已在 IR，但 Anthropic 解码未填（原审计 §3.7 已记录） |
| 能力画像 | 无 | 新增一个纯数据包（如 `profiles/`），由调用方注入，库只消费 |
| `reasoning_content` / `reasoning` 双名 | 只读 `reasoning_content` | 双读；编码时按画像写入正确的名字 |
| **历史消息里的思维链保真** | 无槽位：assistant 消息被规范化为 `{role, content, tool_calls}` | **在 IR 的 assistant 消息上保留原始 thinking 块，并保证编码回上游时逐字节回放**（§2.3）。这是与 `RawFinishReason` 同等重要的"必须保真字段" |
| **流内错误** | 各协议各自处理，无统一事件 | **统一为一个内部错误事件**（§2.3 的四种模型），并保证"上游失败"永远不会被表达成"正常结束" |
| **错误信封的安全解包** | 假定嵌套 `{"error":{...}}` | 解包要容忍缺壳/扁平/裸字符串三种形态，且**在解包失败时仍能报出原始 HTTP 状态与 body 片段** |
| **对话骨架约束** | 无表达 | 画像里的 `message_shape`（Qwen 的交替/首条 system/末条 user）需要在**编码前校验并修正或报错**，而不是原样发出去等 400 |
| **`messages[].role:"system"`** | 按官方 schema 校验会被拒 | **必须显式放行**：官方文档说不存在这个 role，但 Claude Code ≥2.1.154 会发（§3.2.1）。**按文档实现的严格校验器恰好是错的那一方** |
| **静默间隙补字节** | 无机制 | 面向 Claude Code 时需要**主动发 `ping`**（§3.2.3）；Kimi/Ark/混元都不发，300 秒会被掐断 |
| **未知字段双策略** | 单一策略 | **未知字段容忍**（Codex 恒发 `client_metadata`，§3.1）与**已知未实现字段告警**（§6.6）要分开——前者必须放行，后者必须出声 |
| **错误报文原样转发** | 可能会重新包装 | **禁止重新包装上游错误文本**（§3.2.2 第 8 条）：Claude Code 按**错误措辞**匹配来决定关掉哪个能力，改写会破坏自愈 |
| **`UNMEASURED` 画像取值** | 无 | 画像必须允许"未知"（§3.4）：GLM / ModelScope 没有发布兼容矩阵，**默认成"支持"就是把假设当事实** |
| **CORS 暴露头** | 无 | OpenRouter 把 `X-Generation-Id`/`X-Provider-Name` 放进 `access-control-expose-headers`；若本仓库也做网关，浏览器端调用需要同样的处理 |

另外，原审计 §4.5 列出的
**"Chat ↔ Responses 同族桥缺失"** 在这个背景下价值更高了：
厂商侧 `responses` 的实现差异远大于 `chat/completions`，
**"把 Codex 的 Responses 请求降级成 Chat Completions 打到只支持 Chat 的上游"
才是可用性最大化的关键路径**，而 IR 里字段是齐的。

---

## 7. 待验证清单

下面这些在没有真实 key 的情况下只能标为【推定】，建议按此顺序实测补齐：

1. 阿里百炼 Anthropic 端点的 `thinking` 块 `signature` 是否真的为空、
   回传是否被接受（若被拒，thinking 往返在这条链路上不可用）。
2. vLLM Anthropic 端点对顶层 `thinking` 字段是"静默忽略"还是 400
   （取决于 pydantic extra 策略，源码未声明 `model_config`）。
3. Claude Code 当前版本实际发送的请求体（`system` 数组、`cache_control` 位置、
   `tool_choice`、是否调用 `count_tokens`）。
4. 各家 `finish_reason` 的**实际**取值集合（文档未必穷举；StepFun 的空串已经说明
   文档与实现可能都不完整）。**已补齐文档侧**（见 §2.2 的 `finish_reason` 行）：
   DeepSeek 2 个、GLM 3 个、SiliconFlow 1 个非官方值——但**实现是否会发出更多仍未验证**。
5. ~~MiniMax / 豆包（火山方舟）/ SiliconFlow / 星火的完整字段枚举~~
   —— **已完成**，见 [chinese-vendor-chat-completions-divergences.md](chinese-vendor-chat-completions-divergences.md)
   （§7/§8/§9/§12，另含 Kimi/GLM/千帆/StepFun/混元）。
   剩下的真实缺口是：**MiniMax / 混元 / 星火 / StepFun / 千帆 的响应信封与
   `finish_reason` 集合厂商根本没发布**——这不是"我们没查到"，是文档层面就不存在。
6. OpenRouter 注入字段（`provider`/`cost`/`is_byok`/`error_type`）对统一 IR 的污染面。
7. ~~vLLM/SGLang 在关闭工具解析时带 `tools` 的请求行为~~ —— **已确认**
   （§4.3.2、§4.6）：两家**都不报错**，而是把工具调用当正文文本返回。
   vLLM 另有一个陷阱：`tool_choice: "auto"` 需要 `--enable-auto-tool-choice`
   **加** parser，而 `required`/named 形式两者都不需要就能解析。
   **§6.5 的探针必须覆盖这两个分支，否则会把"仅 required 可用"误判为"工具不可用"。**
8. **腾讯混元的 `stop` 反转**在真实链路里是否还成立（文档明确，但需确认是否为
   遗留描述），以及裁剪尾部 stop 串时是否要处理"匹配串跨 chunk"的情况。
   **这是全文优先级最高的单点实测**——它是唯一需要做内容层后处理、
   且厂商声明"未来可能改"的偏差。
9. **思维链往返的实测**（§2.3）：DeepSeek 缺 `reasoning_content` 的 400 是否
   只发生在带 `tool_calls` 时；GLM `clear_thinking:false` 的"逐字节原样"到底有多严格；
   MiniMax `<think>` 内联形态在多轮里如何续接。**这是 L0 级问题，优先级仅次于第 8 条。**
10. **流内错误的四家实测**（§2.3）：智谱用 `finish_reason` 报错时具体填什么值；
    Ark `session.error` 是否真的不带 HTTP 状态码。**没有真实 key 时无法验证。**
11. **限流响应头**：全线未文档化。目前只有智谱在 OpenAPI 里出现过
    `x-ratelimit-scope: global`。若兼容层要做自适应退避，需要逐家抓包建立映射。
12. **火山方舟国内站文档是 JS 渲染的**，本次用的是 BytePlus 国际站等价文档
    （同为 `/api/v3` 契约），**国内站独有字段未独立核对**。
13. **`messages[].role:"system"` 的完整接受面**（§3.2.1）：文档确认的只有
    百炼与 Ark 接受、Kimi 与混元拒绝；**GLM/MiniMax/SiliconFlow 未文档化**。
    这是 L0 级问题，且是 Claude Code 场景的第一大断裂，值得优先实测。
14. **GLM 与 ModelScope 的字段级兼容性全部未测量**（§3.4）：两家都没有发布
    兼容矩阵。任何"GLM 支持 X"的说法目前都缺证据——**建议实测后再入画像**。
15. **Claude Code 在 `code.claude.com` 的错误文档有一节无法抓取**
    （"Streaming response ended before any complete data was received"，
    多次 TLS 失败）。**缺 `message_start`/`content_block_stop`/`message_delta.usage`
    的逐事件后果因此仍未知**——这是本文少数明确承认没查到的点。
16. **Codex 对 HTTP 200 + `application/json`（而非 SSE）的反应**是源码推断
    （会落到"stream closed before response.completed"），**未直接实测**。
17. **OpenRouter 对 `client_metadata` / `include` / `parallel_tool_calls`
    的转发策略未文档化**（§8.7）。对 Codex 走 OpenRouter 的场景，
    这决定了 `client_metadata` 是被透传、丢弃还是导致 400。

---

## 8. 聚合网关（OpenRouter 及同类）

聚合网关的偏差性质与其他三类都不同：**它是有意"规范化"的**，
因此偏差是**系统性的、跨全部上游的、并且会污染计费与成本归因**。
OpenRouter 的文档把这些差异写得很清楚，逐条如下【文档】。

### 8.1 它同时暴露三套协议（含一个第三方规范）

| 端点 | 形态 |
| --- | --- |
| `/api/v1/chat/completions` | OpenAI Chat 形态 |
| `/api/v1/responses` | **"OpenResponses"**，items-based，**仅无状态** |
| `/api/v1/messages` | Anthropic Messages 形态，文档称之为 **"Anthropic Skin"** |
| `/api/v1/messages/count_tokens` | **不存在（实测 404）** |

两个要点：

1. Responses 侧走的是 **OpenResponses**（openresponses.org）这一**独立规范**，
   不是 OpenAI 的私有 Responses。vLLM 的扩展策略 RFC（issue #32850）引用的
   也是同一个规范【文档】。也就是说 **Responses 这一层正在出现一个"中立规范"**，
   而 Codex 只认 OpenAI 的实际行为——两者不完全等同。
2. "Anthropic Skin" 的定位是"behaves exactly like the Anthropic API"，
   并且**自动处理模型映射、透传 thinking 块与原生工具调用**。

**但它缺了 `count_tokens` 这条路由，而且这是实测结论不是文档缺失**：
`POST /api/v1/messages/count_tokens` 返回 **404**，而同级的
`/api/v1/chat/completions` 等返回 401（说明该网关是"先路由后鉴权"，
404 因此具有判定意义）。对照：`/api/v1/messages` 本身返回 401【实况】。

这条差异的重要性取决于客户端，需要分开看：

- 对**Claude Code**：`count_tokens` 是**可选**的（§3.2.2 第 1 条），
  缺失只会让 `/context` 显示近似值，**不会断链**。所以这个 404 并不可怕。
- 对**Anthropic 官方 SDK / Agent SDK**：它们会主动调用它，
  于是拿到 404；社区报告里表现为
  `400 No endpoints available that support Anthropic's context management features`【实况】。

**给兼容层的直接启示**：既然官方已把 `count_tokens` 定义为可选，
**兼容层在这条路由上返回 404 是正确姿态**（让客户端降级），
而返回 500 或伪造一个数字都会更糟。这是一个"少做反而更对"的少见案例。


### 8.2 与官方规范**明确相悖**的七处（前四处由文档自己承认）

| # | 差异 | 原文要点 |
| --- | --- | --- |
| 1 | **usage chunk 的 `choices` 非空** | "When streaming, usage is returned exactly once in the final chunk before the `[DONE]` message. **Unlike OpenAI's spec, this chunk contains a non-empty choices array**: a choice with a content-free delta that repeats the finish_reason of the stream." |
| 2 | **流内错误用 `finish_reason: "error"`** | 出错时发一个普通 data chunk，内含 `error` 对象、`delta.content` 为空、`finish_reason: "error"`；`error` 不是 OpenAI 定义的 finish_reason 值 |
| 3 | **响应里注入 `provider`** | chunk 与响应都带 `provider`（如 `"provider":"Anthropic"`），非官方字段——**但只在错误样例里出现，正常响应的类型定义里没有它** |
| 4 | **错误码被压平** | Responses 侧"many distinct internal types collapse to `server_error`"，精确原因被挪到**顶层非标准字段 `error_type`**（在 `error` 对象之外） |
| 5 | **同一个网关内 `error.code` 的类型不一致** | Chat 侧是**数字**（`502`），Responses 侧是**字符串**（`"invalid_prompt"`）——官方两边都是字符串 |
| 6 | **`x-ratelimit-*` 只在平台限流的 429 上出现** | 文档原文：*"Successful inference responses do not include `X-RateLimit-*` headers."* 官方是每个成功响应都带。要查配额只能调 `GET /api/v1/key` |
| 7 | **限流类错误被转成成功** | `context_length_exceeded` 等被转成 `finish_reason: "length"` 的**成功响应**，而不是错误 |

第 1 条尤其值得注意，因为它和 **DeepSeek 的行为"意外一致"**：
两边都把 usage 放在 `choices` 非空的 chunk 上，而**官方规范要求 `choices: []`**。
所以正确的兼容实现是**不要在 usage chunk 上做 `choices` 形状断言**，
只认"最后一个带 `usage` 的 chunk"。

第 3 条需要更正一个常见说法：**`provider` 不是可靠的响应字段**——
它出现在文档的**错误样例**里，但既不在文档化的响应类型定义中，
也不在 OpenAPI 的 `ChatResult`/`ChatChoice`/`ChatStreamChunk` schema 里。
实际暴露路由信息的是**一个未文档化的响应头 `X-Provider-Name`**
（但它出现在实测的 CORS expose 列表里）【实况】。
**依赖 body 里的 `provider` 会时有时无。**

第 6 条对兼容层有直接影响：**上游健康度探测不能依赖速率头**。
OpenAI 生态里"读 `x-ratelimit-remaining` 做自适应退避"是常规做法，
在这个网关上会永远读到空值，而且**不会报错**——又一个静默降级。


### 8.3 无条件注入的字段

- `usage` 里注入 `cost`、`is_byok`、`cost_details`；文档说明
  "OpenRouter always returns detailed usage information"，
  即**客户端有没有请求 usage 都会拿到**，这使"是否请求了 usage"这个判断失效。
- `system_fingerprint` 只在"provider supports it"时出现——**缺失是正常态**。
- `X-Generation-Id` 响应头 + `/api/v1/generation?id=` 事后查询端点，
  用于补取 token 与 cost。

### 8.4 模型身份被网关改写（D8 的加重版）

- `model` 支持 `:provider` / `:nitro` / `:floor` / `:online` / `:free` 等后缀。
  但要分清**目录变体**（`:free`/`:batch`，真的在 `/api/v1/models` 里）
  与**路由变体**（`:nitro`/`:floor`/`:exacto`，**永远不在目录里**，
  却对任何模型 id 合法）。实测 454 个模型条目里只有 `batch`(71) 和 `free`(21)，
  **没有任何一个路由变体**——文档对此有明确说明，但**"查目录来校验模型名"的
  客户端会误判**。
- 响应里的 `model` 是**路由后的模型**，未必等于请求的 slug。
  任何用响应 `model` 做校验、缓存键或记账的逻辑都会错。
  要查"我当初请求的是什么"，得读 `openrouter_metadata.requested`。
- **`provider` 这个词在不同网关里意思不同，而且都是请求体字段**：
  OpenRouter 的 `provider` 是**路由偏好对象**（`order`/`only`/`sort`/`zdr`…），
  Portkey 的 `provider` 是**字符串**（`"openai"`/`"anthropic"`）。
  **把一个网关的请求体发给另一个，不会报错，但语义完全变了。**
  这是 D3 命名漂移在"网关之间"的形态——比厂商之间更难发现，
  因为双方文档都没有提到对方。
- **字段会被静默退休**：`transforms` 已经**从 OpenAPI 和参数文档中彻底消失**
  （功能改由 `plugins:[{id:"context-compression"}]` 承担），
  `route` 也降级为 `provider.sort.partition` 的兼容别名。
  老客户端发 `transforms` 会怎样？**文档没说**——这本身就是一种静默。


### 8.5 比官方**更宽松**的地方（反向偏差，再次出现）

文档明确：

> **Requests through OpenRouter are not subject to this enforcement**:
> history edits that would 400 against the Anthropic API directly succeed
> through OpenRouter.

也就是对 thinking 块的历史编辑，**官方会 400，OpenRouter 会放行**；
需要审计或丢弃不匹配的 thinking 块要显式开启 opt-in。
这与 §3.5 是同一类现象：**"以官方为严格基准"这个假设在聚合网关上也不成立。**

### 8.6 对兼容层的直接要求

1. **usage 采集**：不要依赖 `choices: []`；改为"扫描最后一个含 `usage` 的 chunk"，
   并同时兼容"独立 chunk / 末内容 chunk / 每 chunk"三种形态（§6.4）。
2. **流内错误**：必须把 `finish_reason: "error"` 与 chunk 内的 `error` 对象
   识别为**失败**，而不是未知结束原因——否则会退化成"模型正常说完"（L1）。
3. **`provider` / `cost` / `is_byok` / `error_type` 等注入字段**必须归入
   `ProviderExtensions` 槽位（§6.7），不能进入统一 IR 的正文语义。
4. **入库的 `model` 字段**要区分"请求的 slug"与"响应的实际模型"。
5. 需要一个**debug 开关**：OpenRouter 提供 `debug` 选项可回显发给上游的真实请求体。
   这正是 §6.5 探针机制想要的能力，值得在自研兼容层里对应实现。

### 8.7 同类网关：LiteLLM / Portkey / Cloudflare 的对照

同类网关的共性偏差可用一句话概括：**它们会重写 usage 与错误信封、
补全缺失字段、并把 provider 路由信息注入响应**。
因此它们既是"兼容性的放大器"（能替你抹平上游差异），
也是"语义的二次加工者"（你收到的已不是上游的原始响应）。

但四家的**默认姿态差异极大**，这张表比"它们都是网关"有用得多：

| 行为 | OpenRouter | LiteLLM | Portkey | Cloudflare AI GW |
| --- | --- | --- | --- | --- |
| **不支持参数怎么办** | **静默忽略**（无严格模式；`require_parameters` 只影响路由） | **默认抛异常**，`drop_params:true` 才丢弃 | 适配器路径**静默丢弃** Anthropic 专有参数 | 未文档化 |
| **流式 usage chunk** | **恒发，且 `choices` 非空** | 需 `include_usage`，`choices:[]` | **OpenAI 侧默认帮你打开 `include_usage`** | 未文档化 |
| **成功响应带 `x-ratelimit-*`** | **否**（仅平台 429） | 是（标准化；上游没发则为 `None`） | 未文档化 | 未文档化 |
| **错误 `code` 类型** | **数字**（Responses 侧却是字符串） | **字符串** | 仅状态码 | v4 信封 `{result,success,errors}` |
| **响应 `model`** | 实际回答的模型 | **重写成客户端别名** | `@provider/model` | 原样 |
| **`/v1/messages` 默认** | 翻译 | **翻译**，需 opt-in 才原生透传 | **翻译**（原生 provider 除外） | 两种面：`/ai/v1/messages`（统一）+ `/anthropic/v1/messages`（原生） |
| **`count_tokens`** | **404** | 有（限部分 provider） | 有 | 未文档化 |

几条对设计有决定性影响的细节：

1. **LiteLLM 的 `drop_params` 默认值是 OpenRouter 的反面**（抛异常 vs 静默忽略）。
   **同一个请求打到两个网关，一个成功一个失败**——这再次说明
   "兼容性"不是请求的属性，而是"请求 × 上游（或网关）"的属性。
2. **LiteLLM 的 `/v1/messages` 默认是翻译器，不是透传**。
   要原生透传必须显式开 `model_info.supported_endpoints: ["/v1/messages"]`；
   即使开了，**默认还会把 `cache_control` 削成 `{"type":"ephemeral"}`**
   （因为 Claude Code 发的 `ttl:"1h"` 会被严格实现拒绝），
   要保留 `ttl` 得再设 `model_info.cache_control_ttl: true`。
   **两个开关层层递进，每个默认值都在损失信息。**
3. **Portkey 有至少三套互不兼容的错误信封**：按状态码的 OpenAI 式、
   `{"success":false,"data":{...}}`（权限失败 AB03）、
   以及 JSON-RPC（MCP 工具被护栏拦截）。
   还引入了 **`246`——一个"HTTP 成功但代表策略失败"的状态码**。
   **只检查非 2xx 的客户端会漏掉 `246`。**
4. **Portkey 默认会剥掉 provider 特有字段**（`x-portkey-strict-open-ai-compliance`
   默认合规=true），而**要看原生 thinking 块必须把它设为 `false`**——
   也就是说"思考能力"在这个网关上默认是关的，且开关在 header 里。
5. **Cloudflare 有两个面**，容易搞混：`gateway.ai.cloudflare.com/.../{provider}/...`
   是**provider 原生路径**（模型名不带前缀），
   而 `api.cloudflare.com/.../ai/v1/{chat/completions,responses,messages}`
   是**统一协议面**（模型名是 `{provider}/{model}`）。
   **鉴权头也不同**（`cf-aig-authorization` vs `Authorization: Bearer`），
   且 REST API 需要 Workers AI 权限，只有 AI Gateway 权限的 token 会 401。
6. **四家里只有 OpenRouter 把"偏离官方"写成了明确的文档条目**
   （见 §8.2 的"Unlike OpenAI's spec"）。其余三家的偏差要靠读 schema 和
   社区报告反推——**文档质量本身也是一种差异**。

把这类网关放在兼容层**下游**，等于放弃对上游真实行为的观测能力；
放在**上游**，则会让能力画像失真。这一取舍需要在架构里明确表态。


---

## 9. 复现与证据索引

本仓库内可直接复核的证据：

```bash
# 官方三协议的权威基线（机器可读）
protocols/openai/chat-completions.schema.json
protocols/openai/responses.schema.json
protocols/anthropic/messages.schema.json

# 我们自己的一致性审计与跨协议转换分析
docs/protocol-conformance.md

# 真实流量捕获（deepseek-flash 经多协议网关）
testdata/live/

# 配套深度档案一：vLLM / SGLang 逐字段源码级核对（1518 行，52 个来源，带 file:line）
docs/vllm-sglang-protocol-divergences.md

# 配套深度档案二：11 家国内厂商 Chat Completions 字段级核对（1050 行，100+ 个来源）
docs/chinese-vendor-chat-completions-divergences.md

# 配套深度档案三：Codex（openai/codex main @ 0a2eb46）源码级 Requirements + 端点探测表
docs/research/codex-responses-compat-dossier.md

# 配套深度档案四：9 家厂商 Anthropic Messages 保真度 + Claude Code 官方 gateway 指南逐条摘录
docs/research/anthropic-messages-compat-dossier.md

# 配套深度档案五：OpenRouter / LiteLLM / Portkey / Cloudflare 四家网关逐条核对
docs/research/gateway-compat-dossier.md
```

**四份配套档案的证据分级各自独立**，正文引用时已标注：
`【文档】`=官方文档页 · `【源码】`=仓库源码（带 file:line）· `【实况】`=真实流量或
issue 记录 · `【推定】`=从上述证据推断、**未经直接验证**。
档案内部另用 `[DOC]/[OBS]/[NOT DOC]` 与 `[D]`/`[I]`/`[U]` 标记，
含义与上表对应。凡标【推定】的结论，正文都已写明它依赖哪个未验证前提。

外部一手来源（本文引用过的）：

*协议文档*
- DeepSeek [Using the Anthropic API](https://api-docs.deepseek.com/guides/anthropic_api)（含逐字段兼容表）
- DeepSeek [Using the Responses API](https://api-docs.deepseek.com/guides/responses_api)（含"静默忽略"声明与事件列表）
- DeepSeek [Create Chat Completion](https://api-docs.deepseek.com/api/create-chat-completion)
- DeepSeek [Integrate with Codex](https://api-docs.deepseek.com/quick_start/agent_integrations/codex)（含 `models.json` 能力声明样例）
- 阿里百炼 [Anthropic 兼容 API](https://help.aliyun.com/zh/model-studio/anthropic-api-messages)
- 阿里百炼 [兼容 OpenAI Responses API](https://help.aliyun.com/zh/model-studio/compatibility-with-openai-responses-api)
- 阿里百炼 [Claude Code 接入](https://help.aliyun.com/zh/model-studio/claude-code)
- Kimi [Use Kimi in Claude Code](https://platform.kimi.ai/docs/guide/claude-code-kimi)
- 智谱 [Claude Code 接入](https://docs.bigmodel.cn/cn/guide/develop/claude)
- 腾讯混元 [对话接口](https://cloud.tencent.com/document/product/1729/111007)（`stop` 语义反转的原文出处）
- 阿里百炼 [Function Calling](https://help.aliyun.com/zh/model-studio/qwen-function-calling)（`tool_choice: required` 与思考模式冲突）
- OpenRouter [API Reference](https://openrouter.ai/docs/api-reference/overview)（另有全量纯文本 `https://openrouter.ai/docs/llms-full.txt`）
- SGLang [Anthropic-Compatible API](https://docs.sglang.io/docs/basic_usage/anthropic_api)
- vLLM [Codex 接入](https://docs.vllm.ai/en/latest/serving/integrations/codex/)
- vLLM [Claude Code 接入](https://docs.vllm.ai/en/latest/serving/integrations/claude_code/)
- vLLM [Tool Calling](https://docs.vllm.ai/en/latest/features/tool_calling/)
- vLLM [Reasoning Outputs](https://docs.vllm.ai/en/latest/features/reasoning_outputs/)

*源码（最能说明文档没写的事）*
- Codex SSE 事件处理：[`codex-rs/codex-api/src/sse/responses.rs`](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/sse/responses.rs)
- vLLM Anthropic 协议：[`vllm/entrypoints/anthropic/protocol.py`](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/anthropic/protocol.py)
- vLLM Anthropic 服务：[`vllm/entrypoints/anthropic/serving.py`](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/anthropic/serving.py)
- vLLM Responses 协议：[`vllm/entrypoints/openai/responses/protocol.py`](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/openai/responses/protocol.py)
- vLLM Responses 事件集：[`vllm/entrypoints/openai/responses/streaming_events.py`](https://github.com/vllm-project/vllm/blob/main/vllm/entrypoints/openai/responses/streaming_events.py)
- SGLang Anthropic 协议：[`python/sglang/srt/entrypoints/anthropic/protocol.py`](https://github.com/sgl-project/sglang/blob/main/python/sglang/srt/entrypoints/anthropic/protocol.py)
- vLLM Responses 扩展策略 RFC：[issue #32850](https://github.com/vllm-project/vllm/issues/32850)

*真实故障（客户端演进打破"已兼容"端点）*
- DeepSeek-V3 [#1369](https://github.com/deepseek-ai/DeepSeek-V3/issues/1369)：Claude Code 改用 `system` block 数组 → 400
- DeepSeek-V3 [#967](https://github.com/deepseek-ai/DeepSeek-V3/issues/967)：并行工具结果配对失败
- DeepSeek-V3 [#1269](https://github.com/deepseek-ai/DeepSeek-V3/issues/1269)：`cache_control` / `budget_tokens` 被忽略导致能力下降
