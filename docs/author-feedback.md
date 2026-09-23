# 给作者的问题反馈（通俗版）

这份东西是给这个库的作者看的，尽量少讲术语，重点讲**"用户会看到什么症状"**。
技术细节和逐字段核对在 [protocol-conformance.md](protocol-conformance.md)，
官方 schema 标准在 [protocols/](../protocols/README.md)。

---

## 一句话总结

**这个库的设计是站得住的**：统一的 IR、三个适配器、四个跨族桥，分层很清楚，
解码侧（把上游/客户端的东西读懂）主干基本完整。问题几乎全部集中在两个地方：

1. **编码器输出的默认值策略**——不该填的自己填了，该留空的留了值；
2. **"静默"**——做不到的时候不报错、不告警，直接换个意思继续走。

也就是说，**问题不在"没读到"，在"读到了但说错了"和"读不到但不吭声"**。
这类问题跑 schema 校验是查不出来的，但它比字段缺失危险得多。

---

## 一、会让链路直接断的（建议最优先修）

### 1. 流式里会发出两个 `message_start`

- **症状**：Chat 入口 → Anthropic 上游时，如果上游第一个 chunk 带了 `delta.role`，
  解码器会发出两个 `StreamStart`，Anthropic 编码器又老老实实发了两次
  `message_start`。客户端收到重复的"开始"事件，严格的 SDK 直接报错，宽松的
  也会把消息拼成两条。
- **位置**：[openai_chat.go:253](openai_chat.go#L253) 的 `!d.started` 分支和
  [openai_chat.go:272](openai_chat.go#L272) 的 role 分支各发一次。
- **建议**：去掉其中一个，或让编码器去重。

### 2. 流结束时内容块没有关闭

- **症状**：Chat 上游这条链路上，`Close()` 只发了结束事件，没有把还开着的
  text / reasoning / tool 块关掉。Anthropic 客户端会一直等
  `content_block_stop`，表现为**流卡住或报"流异常中断"**。
- **位置**：[anthropic_messages.go:492](anthropic_messages.go#L492)。
  （Responses 上游那条路径是会发 `*-End` 的，所以这是路径不一致。）
- **建议**：`Close()` 里把活跃块 flush 掉，和 Responses 路径对齐。

### 3. `response.content_part.*` 事件的字段名写错了

- **症状**：官方这个事件里内容块的键叫 `part`，我们写成了 `content_part`。
  于是：对外的 Responses 客户端拿不到内容块；**我们自己的解码器也在找
  `content_part`**（[openai_responses.go:1507](openai_responses.go#L1507)），
  面对真实上游时这个分支永远是死的。属于"自家人不认自家格式"。
- **位置**：[openai_responses.go:1385](openai_responses.go#L1385)。
- **建议**：改成 `part`。

### 4. 自创了两个官方不存在的流事件类型

- **症状**：`event: raw`（[anthropic_messages.go:485](anthropic_messages.go#L485)）
  和 `redacted_thinking_delta`（[anthropic_messages.go:441](anthropic_messages.go#L441)）。
  用强类型判别联合的官方 SDK 遇到不认识的类型会直接抛错。
- **建议**：或者删掉，或者改成官方类型 + 元数据挂载。

### 5. 每个 `output[]` 项都带一个空的 `"usage": {}`

- **症状**：Responses 的响应里，message / reasoning / function_call 每一项都会多出
  一个官方没有的 `usage` 字段，而且是个空对象。严格校验的下游会判它非法。
  根因是结构体用了非指针 struct 加 `omitempty`——**Go 里 struct 的 `omitempty`
  是不生效的**，这个坑在 `Response.usage` 上也一样（会发出 `"usage": {}`）。
- **位置**：[openai_responses.go:1355](openai_responses.go#L1355)。
- **建议**：改成指针类型，或者去掉这个字段。

### 6. 请求方向有几处会被上游直接拒（400）

| 情况 | 位置 |
| --- | --- |
| Responses 的工具缺 `parameters`（schema 里是必填） | [openai_responses.go:1264](openai_responses.go#L1264) |
| Anthropic 的工具 schema 为空时，`input_schema` 被省略（必填） | [anthropic_messages.go:1295](anthropic_messages.go#L1295) |
| prompt 为空时发出 `"messages": null`（应该是数组） | [openai_chat.go:1093](openai_chat.go#L1093) |
| `response_format.json_schema` 里发出 `"schema": null` | [openai_chat.go:975](openai_chat.go#L975) |

---

## 二、不报错，但结果是错的（我认为这一档最危险）

这一档的共同点是：**没有任何报错，客户端也收得到响应，但意思变了。**

### 1. 报错被翻译成"正常说完"  ★最严重

`FinishError` / `FinishUnknown` / `FinishOther` 这类结束原因，在 Chat 方向被统一
写成 `"stop"`（[openai_chat.go:1040](openai_chat.go#L1040)），在 Anthropic 方向被
写成 `"end_turn"`（[anthropic_messages.go:1164](anthropic_messages.go#L1164)）。

**症状**：上游超时、内容被审核拦截、模型内部报错——客户端收到的都是
"模型正常说完了"。上层的 agent 循环会把它当成一次成功的收尾继续往下走，
**错误被彻底吞掉**。这类 bug 出问题时最难查。

### 2. 同一个响应，开流和不流结论不一样

内容审核拦截时，非流式编码成 `status: "completed"`（[openai_responses.go:1017](openai_responses.go#L1017)），
流式却编码成 `response.incomplete` + `content_filter`（[openai_responses.go:2073](openai_responses.go#L2073)）。
同一件事两种说法，调用方按非流式的结论判断就会漏掉拦截。

### 3. 没限制输出长度，却被塞了一个 4096 上限

请求里没给 `max_tokens`，解码时被填成 4096（[types.go:328](types.go#L328)），
再编码出去就变成"我要求最多输出 4096"。

**症状**：用户什么都没设，回答却在 4096 token 处被砍断。这不是丢字段，
是**凭空改变了请求的语义**。

### 4. reasoning effort 的档位被丢掉

Chat 方向：`low` / `medium` / `high` 解码时被压成一个 bool
（[openai_chat.go:581](openai_chat.go#L581)），编码时永远输出 `"medium"`
（[openai_chat.go:589](openai_chat.go#L589)）。IR 里 `LLMRequest.ReasoningEffort`
这个字段从头到尾没人用。

**症状**：用户想省钱写 `low`、想要质量写 `high`，拿到的都是同一个东西。
反向的 budget → effort 也会输出一个官方枚举里没有的 `"xhigh"`
（[openai_responses.go:421](openai_responses.go#L421)），而且 8192 / 4096 这个分界
是拍出来的，没有依据。

### 5. `created` 时间戳永远是 0

[openai_chat.go:500](openai_chat.go#L500) 的 `currentTimestamp()` 是个占位实现，
直接返回 0，但每个流式 chunk 都在用它。

**症状**：客户端拿到的所有 chunk 时间戳都是 1970 年。

### 6. `cache_control` 的默认值反了

应该是"用户要求才加"，实际是"用户没反对就加"——`Cache == nil` 时反而会注入
缓存断点（[anthropic_messages.go:917](anthropic_messages.go#L917)）。

**症状**：本来没打算用 prompt cache 的请求被打上缓存标记，写入按 1.25 倍计费。
**这个默认值是 opt-out 的，建议改成 opt-in。**

### 7. 强制工具调用被悄悄降级

开了 thinking 时，`required` 和"指定调用某个工具"会被改成 `auto`
（[anthropic_messages.go:112](anthropic_messages.go#L112)）。

这个降级本身是**必需的**（Anthropic 不允许 thinking + 强制工具），
但**用户不知道**：他明确要求"必须调用这个工具"，实际变成"随便聊"。
建议至少在响应里带一条 warning。

### 8. 工具参数解析失败时会降级成字符串

[openai_chat.go:802](openai_chat.go#L802)：参数 JSON 解析不了就原样保留成字符串。
转到 Anthropic 时 `tool_use.input` 就变成一个 JSON 字符串而不是对象，上游行为不可预期。

### 9. Anthropic 的 thinking token 数丢了

`Usage.ReasoningTokens` 这个字段存在，两个 OpenAI 适配器都在填，
唯独 [anthropic_messages.go:1178](anthropic_messages.go#L1178) 的
`decodeAnthropicUsage` 没填，`output_tokens_details.thinking_tokens` 被丢掉。
看起来是**纯漏写**。

### 10. `stop_sequences` 在两条路径上行为不一致

同样一个 IR：直接调适配器会**报错**"不支持"
（[openai_responses.go:65](openai_responses.go#L65)），走 Anthropic → Responses 的桥
却**静默丢掉**（[bridge_anthropic_to_responses.go:28](bridge_anthropic_to_responses.go#L28)）。
同一个输入两条路径两种结果，这本身就是 bug。建议二选一：要么都报错，要么都转。

### 11. `top_k` 明明支持，桥里却没接

原生 Anthropic 编码器有 `TopK` 字段（[anthropic_messages.go:1204](anthropic_messages.go#L1204)），
但两个"目标是 Anthropic"的桥从来不填它。能力白丢。

---

## 三、我们"替用户编"了数据（会改变模型看到的东西）

这一类比缺字段严重，因为它是**主动写入**的，而且改变的是模型的实际输入：

| 编了什么 | 位置 | 影响 |
| --- | --- | --- |
| 没给输出上限就填 4096 | [types.go:328](types.go#L328) | 输出被砍断 |
| 默认加 `cache_control` | [anthropic_messages.go:917](anthropic_messages.go#L917) | 计费变化 |
| json_schema 名字默认成 `"response_format"` | [openai_responses.go:550](openai_responses.go#L550) | 协议里本来没有名字这个概念 |
| 图片 `detail` 默认成 `"auto"` | [bridge_anthropic_to_responses.go:119](bridge_anthropic_to_responses.go#L119) | 影响识图成本和质量 |
| 工具参数为空时填 `"{}"` | [openai_chat.go:814](openai_chat.go#L814) | 上游可能把它当成"传了空参数"而不是"没传" |
| reasoning item 的 id 用 sha256 现造 | [bridge_anthropic_to_responses.go:168](bridge_anthropic_to_responses.go#L168) | 幂等/去重会失效 |
| 不支持的文件替换成一段 `[Proxy warning: ...]` 文本 | [anthropic_messages.go:774](anthropic_messages.go#L774) | **模型会读到这段提示文字**，可能影响回答 |
| 不支持的工具说明塞进 system prompt | [anthropic_messages.go:1047](anthropic_messages.go#L1047) | 同上，而且三条路径只有一条这么做 |

---

## 四、"能力有，但没接上"的三处

不是没实现，是**已经有的东西没用**：

1. **Anthropic 的 20 种 server tool 全被丢掉**
   （[anthropic_messages.go:1028](anthropic_messages.go#L1028) 只认 `type: ""` 和 `"custom"`）。
   但 IR 里 `ToolProviderDefined` + `Config` 槽位**已经存在**，Responses 适配器就在用
   （[openai_responses.go:686](openai_responses.go#L686)）。这是最值得先补的一块能力。
2. **`top_k`**（见上）。
3. **`ReasoningTokens`**（见上）。

另外 **Chat ↔ Responses 互转整个不存在**：两者同属 OpenAI 家族，查表直接返回
false。这是个功能缺口，不是做不到——IR 里字段都是齐的。

---

## 五、想确认一下：下面这些是有意的吗？

行为都可能是合理的，但**代码里没有注释、也不产出 warning**，
调用方无法区分"故意降级"和"写错了"：

- Claude Code system prompt 剥离（[anthropic_messages.go:13](anthropic_messages.go#L13)）
- 强制工具降级为 auto
- thinking budget 小于 1024 或大于等于 `max_tokens` 时，**整个 thinking 配置被丢掉**
  （[anthropic_messages.go:997](anthropic_messages.go#L997)）
- 无签名的工具续写场景下抑制 thinking（[bridge_openai_to_anthropic.go:13](bridge_openai_to_anthropic.go#L13)）
- signature delta 过滤（不往 Chat 方向传）
- `reasoning_content` 只读不写（请求方向读非官方字段，响应方向刻意不输出）

目前只有 Chat 上游那条 usage 延迟逻辑带了注释，其余都没有。
建议：至少加注释，条件允许的话补 `Warning`——IR 里已经有 `Warning` 类型了。

---

## 六、可以先不管的

这些是纯信息损失，不影响互通，也不用急着修：

- 响应侧：`service_tier`、`system_fingerprint`、`annotations`、Responses 的 item `id`、
  `logprobs` 明细、audio / prediction 的 token 明细
- 请求侧：`n`、`presence_penalty`、`frequency_penalty`、`seed`、`logit_bias`、
  `prediction`、`modalities`、`audio`、`store`、`user`
- 未实现的 provider 工具输出（`web_search_call` / `file_search_call` / `computer_call`）

顺带一提：**响应侧的 missing required 不用太紧张**。官方 SDK 大多把响应解到结构体，
缺字段就是零值，程序照跑。只有严格校验的网关、录制回放工具和强类型客户端才会炸。
所以第一、二档修完之后，这一档可以排后面。

---

## 建议的修复顺序

1. **第一档 + 第二档**（约 20 处）。改动都不大，但决定了这个桥接层能不能被信任。
2. **"能力有但没接上"的三处**，尤其 Anthropic server tool —— 这是最容易看出价值的补齐。
3. **给所有静默丢弃补 warning**，把"诚实性"这条补上。
4. 第三档字段和 Chat ↔ Responses 桥，作为独立排期。

---

## 怎么复现

```bash
# 一致性测试（沙箱里默认的 GOCACHE 是只读的，所以要指定）
GOCACHE=$PWD/.scratch/gocache go test -run 'TestOfficial|TestEncoded|TestAnthropicOfficial' -v .

# 全量
GOCACHE=$PWD/.scratch/gocache go test ./...
```

测试里**故意把当前的偏差钉住了**：一旦某处被修好，测试会失败并提示
"去更新 protocols/ 和 protocol-conformance.md"。所以测试通过**不代表**编码器
合规，那份失败清单本身就是待办列表。

---

## 最后一句

逐字段核对容易给人一种"问题很多"的错觉。实际上：

- **解码能力是完整的**，主干语义都能读懂；
- **真正的窟窿集中在编码器的默认值和静默降级上**；
- 这些问题**schema 一个都查不出来**，但恰恰是最容易在线上出事的。

按"后果"而不是按"和 schema 差多少"来排优先级，需要动的代码量其实不大。
