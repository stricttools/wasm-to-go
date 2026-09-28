#!/usr/bin/env bash
# determinism-reference.sh: checks testdata/determinism/expected.txt against
# wasmtime with NaN canonicalization (-W nan-canonicalization=y), which
# implements the WebAssembly deterministic profile. Every call of the
# determinism test (Test_determinism_expected) is made with wasmtime
# --invoke on testdata/determinism/determinism.wasm, and wasmtime's results
# must be identical to expected.txt, trap messages included.
#
# Usage: scripts/determinism-reference.sh
#
# Environment:
#   WASMTIME  the wasmtime binary (default: wasmtime on PATH)
#   LOGDIR    where the compiled module and wasmtime's results go (default:
#             experiments/determinism-reference, which git ignores)
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
wasmtime=${WASMTIME:-$(command -v wasmtime)} || {
	echo "determinism-reference.sh: no wasmtime on PATH; set WASMTIME" >&2
	exit 2
}
logdir=${LOGDIR:-$root/experiments/determinism-reference}
mkdir -p "$logdir"

"$wasmtime" compile -W nan-canonicalization=y testdata/determinism/determinism.wasm -o "$logdir/determinism.cwasm" || exit 1
go run ./testdata/determinism/listcases >"$logdir/cases.txt" || exit 1

invoke() { # invoke <cases file>: one "name a b -> result" line per case
	local name export ha hb a b r
	while read -r name export ha hb a b; do
		r=$("$wasmtime" run --allow-precompiled -W nan-canonicalization=y \
			--invoke "$export" "$logdir/determinism.cwasm" "$a" "$b" 2>&1 | grep -v '^warning')
		case $r in
		-* | [0-9]*) r=$(printf '0x%x' "$r") ;;
		*) r="trap: $(echo "$r" | grep -o 'wasm trap: .*' | sed 's/^wasm trap: //')" ;;
		esac
		echo "$name $ha $hb -> $r"
	done <"$1"
}

split -n l/4 -d "$logdir/cases.txt" "$logdir/part."
for p in "$logdir"/part.0[0-3]; do
	invoke "$p" >"$p.out" &
done
wait
cat "$logdir"/part.0[0-3].out >"$logdir/wasmtime.txt"
if cmp -s "$logdir/wasmtime.txt" testdata/determinism/expected.txt; then
	echo "expected.txt is identical to wasmtime's results ($(wc -l <"$logdir/wasmtime.txt") calls)"
else
	diff "$logdir/wasmtime.txt" testdata/determinism/expected.txt | head -40
	echo "expected.txt differs from wasmtime's results ($logdir/wasmtime.txt)" >&2
	exit 1
fi
