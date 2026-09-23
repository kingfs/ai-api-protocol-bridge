#!/bin/bash
u="$1"
d=/tmp/corpus/or
f=$(echo "$u" | sed 's|https://openrouter.ai/docs/||; s|/|__|g')
code=$(curl -sS -m 40 -o "$d/$f" -w "%{http_code}" "$u")
echo "$code $(stat -c%s "$d/$f" 2>/dev/null) $f"
