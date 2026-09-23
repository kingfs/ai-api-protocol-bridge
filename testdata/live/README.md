# Captured live traffic

Real bytes captured from a live multi-protocol gateway
(`https://ai-api-gateway.app.baizhi.cloud`) serving `deepseek-flash`, kept as
golden fixtures for [`live_calibration_test.go`](../../live_calibration_test.go).

The gateway is **not** this library. Its chat endpoint is close to a pure
passthrough that forwards the model's own stream, and its Anthropic endpoint is
its own cross-family conversion. A fixture therefore says nothing about how our
code behaves — it says what the protocols look like on the wire, which is the
one thing the vendored schemas cannot tell us. The schemas say what is allowed;
these say what a working implementation does.

## What is here

| File | Protocol | What it is |
| --- | --- | --- |
| `openai-chat.request.json` | Chat Completions | The request that produced the chat fixtures: one user turn, a `get_weather` tool, `stream: true`. |
| `openai-chat.response.json` | Chat Completions | A non-streaming completion. |
| `openai-chat.stream.sse` | Chat Completions | A streamed tool call, with `reasoning_content` and `logprobs` on every chunk. |
| `openai-responses.request.json` | Responses | The request that produced the Responses stream. |
| `openai-responses.stream.sse` | Responses | A streamed tool call, with named SSE events. |
| `anthropic-messages.request.json` | Anthropic Messages | A plain non-streaming request. |
| `anthropic-messages.response.json` | Anthropic Messages | Its response. |
| `anthropic-messages.stream.sse` | Anthropic Messages | A short streamed answer. |
| `anthropic-messages-tool.stream.sse` | Anthropic Messages | A streamed tool call: a `thinking` block followed by a `tool_use` block. |

## What they established

- **The Responses content-part event carries `part`, not `content_part`.** The
  event *name* is `response.content_part.added`; the member inside the payload
  is `part`. The pre-fix encoder emitted `content_part` as the member, which
  made the event unreadable to every client and left our own decoder's handling
  of it dead.
- **`response.function_call_arguments.done` carries `arguments`,** and is
  followed by `response.output_item.done`. The pre-fix encoder omitted the
  arguments and never emitted the item-done event.
- **`output` is nested inside `response`** on `response.completed` and
  `response.in_progress` — it is not a sibling.
- **Output items carry no `usage` member** in a stream, so an `"usage": {}` on
  an item is our invention.
- **The Anthropic stream is strictly sequential.** Content blocks are opened and
  closed one at a time, and `message_stop` comes only after the last close.
- **`choices[].logprobs` is on every chat chunk,** and reasoning arrives
  interleaved with the tool call as `reasoning_content`.

## Reproducing a capture

The request bodies in this directory are exactly what was sent; the response and
stream files are the raw response bodies. Nothing was edited, and no credential
appears in them.
