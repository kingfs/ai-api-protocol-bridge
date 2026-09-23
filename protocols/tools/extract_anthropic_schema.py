#!/usr/bin/env python3
"""Extract a machine readable protocol schema for Anthropic Messages.

Anthropic does not publish an OpenAPI document for the Messages API. The
authoritative machine readable artifact is the official API reference, which is
served both as HTML and as Markdown from ``platform.claude.com``. The Markdown
form is a regular nested list: every named type is a ``### <Human Readable
Name>`` section under ``## Domain types``, every member is a list item whose
signature is backticked, and nested list items describe object members or union
branches. Type references use the de-spaced name (``### Text Block Param`` is
referenced as ``TextBlockParam``).

This script parses that list into a normalized, self-contained JSON Schema
bundle covering:

  * ``POST /v1/messages`` request (``MessageCreateParams``)
  * non-streaming response (``Message``)
  * streaming response events (``RawMessageStreamEvent``)

Usage:
    python3 protocols/tools/extract_anthropic_schema.py \
        --messages /path/to/anthropic-messages-api.md \
        --streaming /path/to/anthropic-streaming.md \
        --out protocols/anthropic
"""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import sys

ITEM = re.compile(r"^(?P<indent>\s*)-\s+`(?P<label>[^`]+)`\s*$")
HEADING = re.compile(r"^(?P<level>#{2,4})\s+(?P<title>.+?)\s*$")
ATTRIBUTE = re.compile(
    r"^(?P<key>minimum|maximum|minLength|maxLength|default|format|pattern|const|maxItems|minItems):\s*(?P<value>.+)$"
)
IDENTIFIER = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
PRIMITIVES = {"string", "number", "integer", "boolean", "object", "null"}


class Node:
    __slots__ = ("label", "name", "expr", "doc", "attrs", "children", "line")

    def __init__(self, label: str, line: int):
        self.label = label
        self.line = line
        self.doc: list[str] = []
        self.attrs: dict[str, str] = {}
        self.children: list[Node] = []
        self.name: str | None = None
        self.expr: str = label
        if ":" in label:
            head, _, tail = label.partition(":")
            if IDENTIFIER.fullmatch(head.strip()):
                self.name = head.strip()
                self.expr = tail.strip()


def parse_list(lines: list[str], start: int, end: int) -> list[Node]:
    """Parse a Markdown nested list covering lines[start:end]."""
    roots: list[Node] = []
    stack: list[tuple[int, Node]] = []
    in_fence = False
    current: Node | None = None

    index = start
    while index < end:
        line = lines[index]
        if line.lstrip().startswith("```"):
            in_fence = not in_fence
            index += 1
            continue
        if in_fence:
            index += 1
            continue

        match = ITEM.match(line)
        if match:
            indent = len(match.group("indent"))
            node = Node(match.group("label").strip(), index + 1)
            while stack and stack[-1][0] >= indent:
                stack.pop()
            if stack:
                stack[-1][1].children.append(node)
            else:
                roots.append(node)
            stack.append((indent, node))
            current = node
            index += 1
            continue

        stripped = line.strip()
        if current is not None and stripped:
            attribute = ATTRIBUTE.match(stripped)
            if attribute:
                current.attrs[attribute.group("key")] = attribute.group("value").strip()
            else:
                current.doc.append(stripped)
        index += 1
    return roots


def headings(lines: list[str]) -> list[tuple[str, str, int]]:
    found: list[tuple[str, str, int]] = []
    in_fence = False
    for index, line in enumerate(lines):
        if line.lstrip().startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        match = HEADING.match(line)
        if match:
            found.append((match.group("level"), match.group("title"), index))
    found.append(("#", "__end__", len(lines)))
    return found


def section_span(found: list[tuple[str, str, int]], title: str) -> tuple[int, int]:
    for position, (level, name, index) in enumerate(found[:-1]):
        if name != title:
            continue
        for next_level, _, next_index in found[position + 1 :]:
            if len(next_level) <= len(level):
                return index + 1, next_index
        return index + 1, len(lines)
    raise SystemExit(f"section not found: {title}")


def strip_optional(expr: str) -> tuple[str, bool]:
    expr = expr.strip()
    if expr.startswith("optional "):
        return expr[len("optional ") :].strip(), True
    return expr, False


def split_union(expr: str) -> list[str]:
    """Split 'A or B or C' on top level ' or ', ignoring quoted segments."""
    parts: list[str] = []
    depth = 0
    in_quote = False
    buffer = ""
    index = 0
    while index < len(expr):
        char = expr[index]
        if char == '"':
            in_quote = not in_quote
            buffer += char
            index += 1
            continue
        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
        if not in_quote and depth == 0 and expr.startswith(" or ", index):
            parts.append(buffer.strip())
            buffer = ""
            index += 4
            continue
        buffer += char
        index += 1
    if buffer.strip():
        parts.append(buffer.strip())
    return parts


def drop_assignment(expr: str) -> str:
    return re.sub(r"^[A-Za-z_][A-Za-z0-9_]*\s*=\s*", "", expr.strip()).strip()


def normalize(schema):
    """Collapse redundant anyOf nesting produced by the recursive parse."""
    if isinstance(schema, list):
        return [normalize(item) for item in schema]
    if not isinstance(schema, dict):
        return schema
    schema = {key: normalize(value) for key, value in schema.items()}
    branches = schema.get("anyOf")
    if not isinstance(branches, list):
        return schema
    others = {key: value for key, value in schema.items() if key != "anyOf"}

    flat: list = []
    for branch in branches:
        if isinstance(branch, dict) and set(branch.keys()) == {"anyOf"}:
            flat.extend(branch["anyOf"])
        else:
            flat.append(branch)
    seen = []
    has_null = False
    for branch in flat:
        if branch == {"type": "null"}:
            has_null = True
            continue
        if branch not in seen:
            seen.append(branch)
    if has_null:
        seen.append({"type": "null"})
    if len(seen) == 1 and isinstance(seen[0], dict):
        return {**seen[0], **others}
    return {"anyOf": seen, **others}


class Builder:
    def __init__(self, types: dict[str, Node]):
        self.types = types
        self.defs: dict[str, object] = {}
        self.pending: list[str] = []
        self.building: set[str] = set()

    def reference(self, name: str) -> dict:
        if name not in self.defs and name not in self.building:
            self.pending.append(name)
        return {"$ref": f"#/$defs/{name}"}

    def is_named_type(self, name: str) -> bool:
        return name in self.types

    def schema_for(self, node: Node) -> dict:
        expr = drop_assignment(node.expr)
        expr, optional = strip_optional(expr)

        named_children = [child for child in node.children if child.name is not None]
        bare_children = [child for child in node.children if child.name is None]

        arms = split_union(expr)
        nullable = "null" in arms
        arms = [arm for arm in arms if arm != "null"]
        if bare_children and any(re.fullmatch(r"\d+ more", arm) for arm in arms):
            # The reference truncates long unions with "or N more"; the nested
            # list is the complete enumeration, so prefer it.
            inner = {"anyOf": [self.schema_for(child) for child in bare_children]}
        else:
            inner = self._arms_schema(node, arms, named_children, bare_children)
        if nullable:
            inner = {"anyOf": [inner, {"type": "null"}]}
        schema = normalize(inner)
        doc = " ".join(node.doc).strip()
        if doc:
            schema["description"] = doc
        self._apply_attrs(schema, node)
        return schema

    def _arms_schema(
        self, node: Node, arms: list[str], named_children: list[Node], bare_children: list[Node]
    ) -> dict:
        if len(arms) > 1:
            literals = [arm for arm in arms if arm.startswith('"') and arm.endswith('"')]
            if len(literals) == len(arms):
                return {"type": "string", "enum": [literal.strip('"') for literal in literals]}
            return {"anyOf": [self._arm_schema(node, arm) for arm in arms]}
        return self._expr_schema(node, arms[0], named_children, bare_children)

    def _apply_attrs(self, schema: dict, node: Node) -> None:
        for key, value in node.attrs.items():
            if key in ("minLength", "maxLength", "minItems", "maxItems"):
                try:
                    schema[key] = int(value)
                except ValueError:
                    continue
            elif key in ("minimum", "maximum"):
                try:
                    schema[key] = int(value)
                except ValueError:
                    try:
                        schema[key] = float(value)
                    except ValueError:
                        continue
            elif key == "default":
                schema["default"] = value
            elif key == "format":
                schema["format"] = value
            elif key == "pattern":
                schema["pattern"] = value
            elif key == "const":
                schema["const"] = value

    def _item_schema(self, node: Node, item_expr: str) -> dict:
        if self.is_named_type(item_expr):
            return self.reference(item_expr)
        if item_expr.endswith(" object") and self.is_named_type(item_expr[: -len(" object")]):
            return self.reference(item_expr[: -len(" object")])
        inline = [child for child in node.children if child.name is None]
        if inline:
            if len(inline) == 1:
                return self.schema_for(inline[0])
            return {"anyOf": [self.schema_for(child) for child in inline]}
        return self._expr_schema(node, item_expr, [], [])

    def _arm_schema(self, parent: Node, arm: str) -> dict:
        for child in parent.children:
            if child.name is not None:
                continue
            if drop_assignment(child.expr) == arm:
                return self.schema_for(child)
        return self._expr_schema(parent, arm, [], [])

    def _expr_schema(
        self, node: Node, expr: str, named_children: list[Node], bare_children: list[Node]
    ) -> dict:
        expr = strip_optional(expr)[0]
        if expr.startswith('"') and expr.endswith('"'):
            return {"type": "string", "enum": [expr.strip('"')]}
        array_match = re.fullmatch(r"array of (.+)", expr)
        if array_match:
            return {"type": "array", "items": self._item_schema(node, array_match.group(1).strip())}
        if expr.endswith(" object"):
            base = expr[: -len(" object")].strip()
            if base and self.is_named_type(base):
                return self.reference(base)
            if named_children:
                return self.object_schema(named_children)
            return {"type": "object"}
        if expr in PRIMITIVES:
            return {"type": expr}
        if expr.startswith("map[") or expr == "map[unknown]":
            return {"type": "object"}
        if self.is_named_type(expr):
            return self.reference(expr)
        if named_children:
            return self.object_schema(named_children)
        if bare_children:
            if len(bare_children) == 1:
                return self.schema_for(bare_children[0])
            return {"anyOf": [self.schema_for(child) for child in bare_children]}
        return {"type": "string", "x-unparsed-expr": expr}

    def object_schema(self, children: list[Node]) -> dict:
        properties: dict[str, object] = {}
        required: list[str] = []
        for child in children:
            if child.name is None:
                continue
            properties[child.name] = self.schema_for(child)
            _, optional = strip_optional(child.expr)
            if not optional:
                required.append(child.name)
        schema: dict = {"type": "object", "properties": properties}
        if required:
            schema["required"] = required
        return schema

    def define(self, name: str, schema: dict) -> None:
        self.defs[name] = schema

    def build_named(self, name: str) -> None:
        if name in self.defs or name in self.building:
            return
        node = self.types[name]
        self.building.add(name)
        schema = self.schema_for_root(node, name)
        self.building.discard(name)
        self.defs[name] = schema

    def schema_for_root(self, node: Node, name: str) -> dict:
        """Build the definition of a named type.

        A domain type is documented as ``- `Name object` `` and its members are
        the nested list items. Resolving that expression through the normal path
        would emit a self reference, so expand it inline.
        """
        expr = strip_optional(drop_assignment(node.expr))[0]
        if expr.endswith(" object"):
            expr = expr[: -len(" object")].strip()
        if expr == name:
            members = [child for child in node.children if child.name]
            if members:
                schema = self.object_schema(members)
                doc = " ".join(node.doc).strip()
                if doc:
                    schema["description"] = doc
                self._apply_attrs(schema, node)
                return schema
        return self.schema_for(node)

    def drain(self) -> None:
        while self.pending:
            self.build_named(self.pending.pop())


def collect_types(lines: list[str], found: list[tuple[str, str, int]]) -> dict[str, Node]:
    start, end = section_span(found, "Domain types")
    types: dict[str, Node] = {}
    for position, (level, title, index) in enumerate(found[:-1]):
        if level != "###" or not (start <= index < end):
            continue
        for next_level, _, next_index in found[position + 1 :]:
            if len(next_level) <= len(level):
                section_end = next_index
                break
        else:
            section_end = end
        body = parse_list(lines, index + 1, section_end)
        if len(body) != 1:
            continue
        node = body[0]
        expr = drop_assignment(node.expr)
        if node.name is not None:
            continue
        if expr == "object" or expr.endswith(" object") or node.children:
            key = title.replace(" ", "")
            if key in types:
                raise SystemExit(f"duplicate domain type: {key}")
            types[key] = node
    return types


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


CURL_BODY = re.compile(r"-d\s*'(?P<body>\{.*?\})'\s*$", re.S)
FENCE = re.compile(r"^\s*```(?P<lang>[A-Za-z0-9_+-]*)[^\n]*$")


def fenced_blocks(text: str) -> list[tuple[str, str]]:
    blocks: list[tuple[str, str]] = []
    language = None
    buffer: list[str] = []
    for line in text.split("\n"):
        match = FENCE.match(line)
        if match and language is None:
            language = match.group("lang")
            buffer = []
            continue
        if match and language is not None:
            blocks.append((language, "\n".join(buffer)))
            language = None
            buffer = []
            continue
        if language is not None:
            buffer.append(line)
    return blocks


def parse_sse(raw: str) -> list[dict]:
    events: list[dict] = []
    for chunk in re.split(r"\n\s*\n", raw.strip()):
        event_name = None
        data_lines: list[str] = []
        for line in chunk.split("\n"):
            if line.startswith("event:"):
                event_name = line[len("event:") :].strip()
            elif line.startswith("data:"):
                data_lines.append(line[len("data:") :].strip())
        if event_name or data_lines:
            events.append({"event": event_name or "", "data": "\n".join(data_lines)})
    return events


def extract_examples(lines: list[str], found: list[tuple[str, str, int]]) -> list[dict]:
    create_start, create_end = section_span(found, "Create a Message")
    scoped = [entry for entry in found if create_start <= entry[2] < create_end]
    scoped.append(("#", "__end__", create_end))
    start, end = section_span(scoped, "Example")
    text = "\n".join(lines[start:end])

    request = None
    for language, body in fenced_blocks(text):
        if language not in ("bash", "sh", "shell", "curl"):
            continue
        match = CURL_BODY.search(body.strip())
        if match:
            # Shell single-quote escaping (`'\''`) is not valid JSON.
            payload = match.group("body").replace("'\\''", "'")
            try:
                request = json.loads(payload)
            except json.JSONDecodeError:
                request = None
            if request is not None:
                break

    response = None
    response_marker = text.find("Response (200)")
    if response_marker != -1:
        for language, body in fenced_blocks(text[response_marker:]):
            if language == "json":
                try:
                    response = json.loads(body)
                except json.JSONDecodeError:
                    response = None
                break
    if request is None and response is None:
        return []
    return [{"title": "Create a Message", "request": request, "response": response}]


def extract_streaming_examples(text: str) -> list[dict]:
    transcripts = []
    for index, (language, body) in enumerate(fenced_blocks(text)):
        if language != "sse":
            continue
        events = parse_sse(body)
        # The guide abbreviates some identifiers (for example `msg_01G...`), so a
        # few transcripts are illustrative rather than literal. Flag them so a
        # consumer can select only transcripts whose payloads are valid JSON.
        complete = True
        for event in events:
            try:
                json.loads(event["data"])
            except json.JSONDecodeError:
                complete = False
        transcripts.append(
            {
                "title": f"official streaming transcript {index + 1}",
                "raw": body.strip() + "\n",
                "complete": complete,
                "events": events,
            }
        )
    return transcripts


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--messages", required=True, type=pathlib.Path)
    parser.add_argument("--streaming", type=pathlib.Path)
    parser.add_argument("--out", required=True, type=pathlib.Path)
    args = parser.parse_args()

    raw = args.messages.read_bytes()
    lines = raw.decode("utf-8").split("\n")
    found = headings(lines)
    types = collect_types(lines, found)

    create_start, create_end = section_span(found, "Create a Message")
    body_start, body_end = section_span(
        [entry for entry in found if create_start <= entry[2] < create_end] + [("#", "__end__", create_end)],
        "Body parameters",
    )
    request_params = parse_list(lines, body_start, body_end)

    builder = Builder(types)
    request_schema = builder.object_schema([node for node in request_params if node.name])
    builder.define("MessageCreateParams", request_schema)
    builder.drain()

    for root in ("Message", "RawMessageStreamEvent"):
        if root not in types:
            raise SystemExit(f"missing domain type: {root}")
        builder.define(root, builder.schema_for_root(types[root], root))
        builder.drain()

    unresolved = []
    for name, definition in builder.defs.items():
        for ref in re.findall(r'"\$ref":\s*"#/\$defs/([^"]+)"', json.dumps(definition)):
            if ref not in builder.defs:
                unresolved.append((name, ref))
    if unresolved:
        raise SystemExit(f"unresolved refs: {unresolved[:10]}")

    bundle = {
        "id": "anthropic/messages",
        "protocol": "anthropic_messages",
        "title": "Anthropic Messages",
        "endpoint": "POST /v1/messages",
        "dialect": "json-schema subset: anyOf unions, enum for literals, $ref into #/$defs",
        "documents": {
            "request": {"$ref": "#/$defs/MessageCreateParams"},
            "response": {"$ref": "#/$defs/Message"},
            "stream_event": {"$ref": "#/$defs/RawMessageStreamEvent"},
        },
        "source": {
            "kind": "official-docs-markdown",
            "urls": [
                "https://platform.claude.com/docs/en/api/messages.md",
                "https://platform.claude.com/docs/en/build-with-claude/streaming.md",
            ],
            "files": {
                "messages": args.messages.name,
                "streaming": args.streaming.name if args.streaming else None,
            },
            "sha256": {
                "messages": sha256_bytes(raw),
                "streaming": sha256_bytes(args.streaming.read_bytes()) if args.streaming else None,
            },
        },
        "$defs": dict(sorted(builder.defs.items())),
    }
    bundle["components_in_scope"] = sorted(bundle["$defs"].keys())

    args.out.mkdir(parents=True, exist_ok=True)
    target = args.out / "messages.schema.json"
    target.write_text(json.dumps(bundle, indent=2) + "\n", encoding="utf-8")

    def stream_event_literals(prefix: str) -> list[str]:
        values: set[str] = set()
        for candidate, definition in bundle["$defs"].items():
            if not candidate.startswith(prefix):
                continue
            properties = (definition or {}).get("properties", {})
            literal = properties.get("type", {}).get("enum")
            if literal:
                values.add(literal[0])
        return sorted(values)

    event_bundle = {
        "id": "anthropic/messages-stream-events",
        "source": bundle["source"],
        "events": stream_event_literals("Raw"),
    }
    (args.out / "stream-events.json").write_text(
        json.dumps(event_bundle, indent=2) + "\n", encoding="utf-8"
    )

    print(f"wrote {target} ({len(bundle['$defs'])} defs)")
    print(f"wrote {args.out / 'stream-events.json'} ({len(event_bundle['events'])} events)")

    examples = {
        "id": "anthropic/messages",
        "source": bundle["source"],
        "examples": extract_examples(lines, found),
    }
    (args.out / "messages.examples.json").write_text(
        json.dumps(examples, indent=2) + "\n", encoding="utf-8"
    )
    print(f"wrote {args.out / 'messages.examples.json'} ({len(examples['examples'])} examples)")

    if args.streaming is not None:
        streaming_text = args.streaming.read_text(encoding="utf-8")
        streaming = {
            "id": "anthropic/messages-streaming",
            "source": {
                "kind": "official-docs-markdown",
                "urls": ["https://platform.claude.com/docs/en/build-with-claude/streaming.md"],
                "sha256": {"streaming": sha256_bytes(args.streaming.read_bytes())},
            },
            "transcripts": extract_streaming_examples(streaming_text),
        }
        (args.out / "streaming.examples.json").write_text(
            json.dumps(streaming, indent=2) + "\n", encoding="utf-8"
        )
        print(
            f"wrote {args.out / 'streaming.examples.json'} ({len(streaming['transcripts'])} transcripts)"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
