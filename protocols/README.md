# Protocol schemas

This directory vendors the **official protocol schema** for every protocol family
this library implements, reduced to the parts that are actually in scope, in a
machine readable form.

It exists so that "is our parser correct?" has an answer that is not a matter of
opinion: the checked in JSON is extracted from an upstream specification, a test
loads it, and the adapters are compared against it (see
[`schema_conformance_test.go`](../schema_conformance_test.go)).

These files are a **reference standard, not part of the runtime API**. Nothing in
the library imports them; they are consumed by tests and by the audit report in
[`docs/protocol-conformance.md`](../docs/protocol-conformance.md).

## Contents

```text
protocols/
  README.md
  openai/
    chat-completions.schema.json   # POST /v1/chat/completions request/response/stream chunk
    chat-completions.examples.json # official x-oaiMeta request/response examples
    responses.schema.json          # POST /v1/responses request/response/stream event union
    responses.examples.json        # official x-oaiMeta request/response examples
  anthropic/
    messages.schema.json           # POST /v1/messages request/response/stream event union
    messages.examples.json         # official Create a Message request + 200 response
    streaming.examples.json        # official SSE transcripts
    stream-events.json             # documented event names
  tools/
    extract_openai_schema.py
    extract_anthropic_schema.py
    fetch_anthropic_docs.sh
```

## Bundle format

Every `*.schema.json` uses the same envelope:

```jsonc
{
  "id": "openai/chat-completions",
  "protocol": "openai_chat",          // matches the Protocol constant in types.go
  "endpoint": "POST /v1/chat/completions",
  "dialect": "…",                     // what the reader must know to interpret it
  "documents": {                      // the interface faces, each a $ref or inline schema
    "request":        { "$ref": "#/$defs/CreateChatCompletionRequest" },
    "response":       { "$ref": "#/$defs/CreateChatCompletionResponse" },
    "stream_response":{ "$ref": "#/$defs/CreateChatCompletionStreamResponse" }
  },
  "source": {                         // provenance: where this came from, and the digest
    "kind": "openapi",
    "version": "2.3.0",
    "url": "https://raw.githubusercontent.com/openai/openai-openapi/manual_spec/openapi.yaml",
    "file": "openapi.documented-2026-06-02.yml",
    "sha256": "…"
  },
  "components_in_scope": ["…"],       // sorted $defs keys
  "$defs": { "…": { } }               // self-contained component table
}
```

`$defs` is closed: every `$ref` inside a bundle resolves within that same bundle,
so a consumer never needs the upstream YAML. `openai/*` bundles use the
OpenAPI 3.0 flavour of JSON Schema (`nullable: true`, `allOf` composition);
`anthropic/messages.schema.json` uses a plain JSON Schema subset (`anyOf` unions,
`enum` for literals, `required` arrays).

## Provenance

| Bundle | Upstream | Kind | Retrieved |
| --- | --- | --- | --- |
| `openai/chat-completions.schema.json` | `openai/openai-openapi@manual_spec`, `info.version` 2.3.0, digest `6a6c681b…67cad9` | official OpenAPI 3.0 document | snapshot dated 2026-06-02 |
| `openai/responses.schema.json` | same document | official OpenAPI 3.0 document | snapshot dated 2026-06-02 |
| `openai/*.examples.json` | the `x-oaiMeta.examples` blocks of the same document's path items | official examples | snapshot dated 2026-06-02 |
| `anthropic/messages.schema.json` | `https://platform.claude.com/docs/en/api/messages.md` | official API reference, Markdown form | 2026-09-23 |
| `anthropic/streaming.examples.json` | `https://platform.claude.com/docs/en/build-with-claude/streaming.md` | official streaming guide, Markdown form | 2026-09-23 |

The OpenAI snapshot is the same document that
`llm-tracelab/docs/protocol-reference/upstream/openai/` pins; the raw digests are
recorded in each bundle's `source.sha256` so a refresh can be diffed.

Anthropic does not publish an OpenAPI document for Messages. The HTML API
reference page is a client rendered shell with no schema content, so the Markdown
form served from the same URL is the machine readable source. Its digest is
recorded in `anthropic/messages.schema.json` under `source.sha256`.

## Regenerating

```bash
# OpenAI (no network; reads the pinned snapshot)
python3 protocols/tools/extract_openai_schema.py \
    --spec /path/to/openapi.documented-2026-06-02.yml \
    --paths /path/to/openai-path-items \
    --out protocols/openai

# Anthropic (fetches the official Markdown, then extracts)
protocols/tools/fetch_anthropic_docs.sh /tmp/anthropic-docs
python3 protocols/tools/extract_anthropic_schema.py \
    --messages /tmp/anthropic-docs/messages-api.md \
    --streaming /tmp/anthropic-docs/streaming.md \
    --out protocols/anthropic
```

Both extractors are deterministic: the same input produces byte identical output.

## Known limits of this standard

- Coverage is the two interface faces this library implements. OpenAI
  `/v1/embeddings`, `/v1/models`, realtime, assistants and the Anthropic
  `/v1/messages/count_tokens` face are out of scope and absent on purpose.
- The Anthropic bundle is derived from prose documentation, so `required` there
  means "the reference does not mark this member optional". Response objects list
  every member, and a member such as `usage.service_tier` is documented as
  present even though older responses omit it. The schema records what the docs
  say; it is not a claim that the API rejects a payload missing the field.
- Both bundles describe the *upstream* protocols. They say nothing about
  OpenAI-compatible vendors that only implement a subset.
