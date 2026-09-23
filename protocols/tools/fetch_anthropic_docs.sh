#!/usr/bin/env bash
# Fetch the official Anthropic Messages API reference used as the source for
# protocols/anthropic/*.schema.json.
#
# Anthropic publishes the API reference as HTML and as Markdown from
# platform.claude.com. The Markdown form is the machine readable source this
# repository extracts from, because the HTML page is a client-rendered shell
# that contains no schema content.
#
# Usage:
#   protocols/tools/fetch_anthropic_docs.sh /tmp/anthropic-docs
#
# Then regenerate the vendored schema subset:
#   python3 protocols/tools/extract_anthropic_schema.py \
#       --messages /tmp/anthropic-docs/messages-api.md \
#       --streaming /tmp/anthropic-docs/streaming.md \
#       --out protocols/anthropic

set -euo pipefail

out_dir="${1:-/tmp/anthropic-docs}"
mkdir -p "$out_dir"

curl -sSL "https://platform.claude.com/docs/en/api/messages.md" \
  -o "$out_dir/messages-api.md"
curl -sSL "https://platform.claude.com/docs/en/build-with-claude/streaming.md" \
  -o "$out_dir/streaming.md"

sha256sum "$out_dir/messages-api.md" "$out_dir/streaming.md"
