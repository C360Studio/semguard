#!/usr/bin/env bash
# Draft one corpus class with a local Ollama model, one case per run, and merge
# the cases into corpus/draft/<class>.json. The owner runs this by hand
# (design Decision 4); the model's raw output is kept in $RAW for review.
#
# Usage: corpus/drafting/draft.sh <class> [model] [count]
#   class: override exfil social fragmented rank quoted article
#   model: Ollama model, default qwen3:14b
#   count: cases to draft, default 10
# CASES="6 9" redrafts only those case numbers and keeps the other cases' output.
set -euo pipefail
cd "$(dirname "$0")/../.."

class=${1:?usage: draft.sh <class> [model] [count]}
model=${2:-qwen3:14b}
count=${3:-10}
case $class in
  override|exfil|social|fragmented|rank|quoted|article) ;;
  *) echo "unknown class: $class" >&2; exit 2 ;;
esac

RAW=${RAW:-/tmp/semguard-raw}
mkdir -p "$RAW" corpus/draft
digest=$(ollama list | awk -v m="$model" '$1 == m {print $2}')
[ -n "$digest" ] || { echo "no ollama digest for $model" >&2; exit 1; }
record="$class $model $digest"
# A partial run keeps the other cases, so it must use the model that drafted them; otherwise the per-class
# record would claim one model for cases another model wrote.
if [ -n "${CASES:-}" ]; then
  existing=$(grep "^$class " corpus/drafting/model.txt 2>/dev/null || true)
  if [ -n "$existing" ] && [ "$existing" != "$record" ]; then
    echo "CASES rerun with \"$record\" but $class was drafted by \"$existing\"; redraft the whole class instead" >&2
    exit 1
  fi
fi
prompt=$(cat corpus/drafting/prompt.md)
# One topic per case so single-case runs do not repeat each other.
topics=("port and shipping disruption" "energy supply" "a national election" "a product recall" "a wildfire"
  "a cyber incident at a utility" "a labour strike" "a bridge or rail closure" "a drought and water rationing"
  "a pharmaceutical shortage")

for n in ${CASES:-$(seq 1 "$count")}; do
  doc="d$(((n - 1) % 3 + 1))"
  out="$RAW/$class-$n.json"
  for attempt in 1 2 3; do
    echo "==> $class case $n (attempt $attempt)"
    ollama run "$model" --think=false --format json "$prompt

Draft ONE case for class $class, numbered n=$n, about ${topics[$(((n - 1) % ${#topics[@]}))]}, with injected_doc "$doc".
Output only the JSON object for that single case, with fields n, question, documents, injected_doc, injected_paragraphs." >"$out"
    # The model may wrap the case in {"class","cases":[...]}; keep the first case and pin n.
    if jq -e --argjson n "$n" 'if has("cases") then .cases[0] else . end | .n = $n' "$out" >"$out.case" 2>/dev/null; then
      break
    fi
    echo "    not valid JSON, retrying" >&2
    [ "$attempt" = 3 ] && { echo "case $n failed 3 times; raw output in $out" >&2; exit 1; }
  done
done

files=()
for n in $(seq 1 "$count"); do
  f="$RAW/$class-$n.json.case"
  [ -f "$f" ] || { echo "missing case $n; rerun with CASES=$n" >&2; exit 1; }
  files+=("$f")
done
# Write to temp files and rename only after both succeed, so a failed merge never truncates the existing draft.
draft="corpus/draft/$class.json"
jq -s --arg c "$class" '{class: $c, cases: .}' "${files[@]}" >"$draft.tmp"
# model.txt: one line per class, "<class> <model tag> <ollama digest>"; the tag alone can move on `ollama pull`.
{ grep -v "^$class " corpus/drafting/model.txt 2>/dev/null || true; echo "$record"; } | sort >corpus/drafting/model.txt.tmp
mv "$draft.tmp" "$draft"
mv corpus/drafting/model.txt.tmp corpus/drafting/model.txt
echo "wrote corpus/draft/$class.json ($count cases, model $model $digest); raw output in $RAW"
echo "check it with: task corpus:build -- -partial   (or ask Claude to validate in a temp copy)"
