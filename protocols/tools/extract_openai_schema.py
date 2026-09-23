#!/usr/bin/env python3
"""Extract a self-contained protocol schema subset for the OpenAI family.

Input is the official OpenAI OpenAPI document (``openapi.documented-*.yml``).
The document is large (it covers the whole OpenAI API surface), so this script
resolves only the components reachable from the two interface faces this library
implements:

  * ``POST /chat/completions`` -- request, response, stream chunk
  * ``POST /responses``        -- request, response, stream event union

The result is written as one JSON bundle per interface face. Every ``$ref`` in a
bundle points at ``#/$defs/<ComponentName>`` inside that same bundle, so the file
can be consumed by a loader that never sees the upstream YAML.

Usage:
    python3 protocols/tools/extract_openai_schema.py \
        --spec /path/to/openapi.documented-2026-06-02.yml \
        --paths /path/to/openai-path-items \
        --out protocols/openai
"""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import sys
from collections import deque

import yaml

COMPONENT_PREFIX = "#/components/schemas/"

# Interface faces: output file -> (protocol id, root component names).
FACES = {
    "chat-completions": {
        "protocol": "openai_chat",
        "title": "OpenAI Chat Completions",
        "endpoint": "POST /v1/chat/completions",
        "documents": {
            "request": "CreateChatCompletionRequest",
            "response": "CreateChatCompletionResponse",
            "stream_response": "CreateChatCompletionStreamResponse",
        },
    },
    "responses": {
        "protocol": "openai_responses",
        "title": "OpenAI Responses",
        "endpoint": "POST /v1/responses",
        "documents": {
            "request": "CreateResponse",
            "response": "Response",
            "stream_event": "ResponseStreamEvent",
        },
    },
}

# Path item files whose x-oaiMeta.examples carry official request/response JSON.
EXAMPLE_FILES = {
    "chat-completions": "chat-completions-path-2026-06-02.json",
    "responses": "responses-path-2026-06-02.json",
}


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


class Extractor:
    """Rewrites component refs into local ``$defs`` refs and collects the closure."""

    def __init__(self, components: dict):
        self.components = components
        self.used: set[str] = set()
        self.queue: deque[str] = deque()
        self.defs: dict[str, object] = {}

    def rewrite(self, node):
        if isinstance(node, list):
            return [self.rewrite(item) for item in node]
        if not isinstance(node, dict):
            return node

        ref = node.get("$ref")
        if isinstance(ref, str) and ref.startswith(COMPONENT_PREFIX):
            name = ref[len(COMPONENT_PREFIX) :]
            self._require(name)
            siblings = {k: self.rewrite(v) for k, v in node.items() if k != "$ref"}
            local = {"$ref": f"#/$defs/{name}"}
            if not siblings:
                return local
            # OpenAPI 3.0 ignores siblings of $ref. Keep them as an allOf branch
            # so that description/nullable annotations survive the conversion.
            return {"allOf": [local], **siblings}
        return {key: self.rewrite(value) for key, value in node.items()}

    def _require(self, name: str) -> None:
        if name in self.used:
            return
        if name not in self.components:
            raise SystemExit(f"unknown component referenced: {name}")
        self.used.add(name)
        self.queue.append(name)

    def drain(self) -> None:
        while self.queue:
            name = self.queue.popleft()
            self.defs[name] = self.rewrite(self.components[name])


def extract_face(face_id: str, config: dict, components: dict) -> dict:
    extractor = Extractor(components)
    documents = {}
    for slot, root in config["documents"].items():
        if root not in components:
            raise SystemExit(f"{face_id}: missing root component {root}")
        extractor._require(root)
        extractor.drain()
        documents[slot] = {"$ref": f"#/$defs/{root}"}
    extractor.drain()
    return {
        "id": f"openai/{face_id}",
        "protocol": config["protocol"],
        "title": config["title"],
        "endpoint": config["endpoint"],
        "dialect": "openapi-3.0 (JSON Schema subset; `nullable` is OAS 3.0 style)",
        "documents": documents,
        "$defs": dict(sorted(extractor.defs.items())),
    }


CURL_BODY = re.compile(r"-d\s*'(?P<body>\{.*\})'\s*$", re.S)


def curl_json(curl: str):
    match = CURL_BODY.search(curl.strip())
    if not match:
        return None
    try:
        return json.loads(match.group("body"))
    except json.JSONDecodeError:
        return None


def extract_examples(path: pathlib.Path) -> list[dict]:
    document = json.loads(path.read_text(encoding="utf-8"))
    examples = document.get("post", {}).get("x-oaiMeta", {}).get("examples", [])
    result = []
    for example in examples:
        request = curl_json(example.get("request", {}).get("curl", ""))
        response = None
        raw_response = example.get("response")
        if isinstance(raw_response, str):
            try:
                response = json.loads(raw_response)
            except json.JSONDecodeError:
                response = None
        if request is None:
            continue
        result.append({"title": example.get("title", ""), "request": request, "response": response})
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--spec", required=True, type=pathlib.Path)
    parser.add_argument(
        "--paths",
        type=pathlib.Path,
        help="directory holding the extracted *-path-*.json files (for official examples)",
    )
    parser.add_argument("--out", required=True, type=pathlib.Path)
    args = parser.parse_args()

    spec_bytes = args.spec.read_bytes()
    spec_digest = hashlib.sha256(spec_bytes).hexdigest()
    document = yaml.safe_load(spec_bytes)
    components = document["components"]["schemas"]

    args.out.mkdir(parents=True, exist_ok=True)
    written = []

    for face_id, config in FACES.items():
        bundle = extract_face(face_id, config, components)
        bundle["source"] = {
            "kind": "openapi",
            "title": document["info"]["title"],
            "version": document["info"]["version"],
            "url": "https://raw.githubusercontent.com/openai/openai-openapi/manual_spec/openapi.yaml",
            "file": args.spec.name,
            "sha256": spec_digest,
        }
        bundle["components_in_scope"] = sorted(bundle["$defs"].keys())
        target = args.out / f"{face_id}.schema.json"
        target.write_text(json.dumps(bundle, indent=2, sort_keys=False) + "\n", encoding="utf-8")
        written.append((target, len(bundle["$defs"])))

        if args.paths is not None:
            example_file = args.paths / EXAMPLE_FILES[face_id]
            if example_file.is_file():
                examples = extract_examples(example_file)
                example_bundle = {
                    "id": f"openai/{face_id}",
                    "source": {
                        "kind": "openapi-x-oaiMeta-examples",
                        "file": example_file.name,
                        "sha256": sha256_file(example_file),
                    },
                    "examples": examples,
                }
                example_target = args.out / f"{face_id}.examples.json"
                example_target.write_text(
                    json.dumps(example_bundle, indent=2) + "\n", encoding="utf-8"
                )
                written.append((example_target, len(examples)))

    for path, count in written:
        print(f"wrote {path} ({count} entries)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
