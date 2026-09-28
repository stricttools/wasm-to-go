#!/usr/bin/env bash
# libm-reference.sh: checks libc-gen/test_math/expected.txt against wasmtime
# with NaN canonicalization (-W nan-canonicalization=y), which implements the
# WebAssembly deterministic profile. Every call of the math test
# (Test_math_expected) is made with wasmtime --invoke on
# libc-gen/test_math/mathtest.wasm, the module the test's translation was
# made from, and wasmtime's results must be identical to expected.txt.
#
# Usage: scripts/libm-reference.sh
#
# Environment:
#   WASMTIME  the wasmtime binary (default: wasmtime on PATH)
#   LOGDIR    where the compiled module and wasmtime's results go (default:
#             experiments/libm-reference, which git ignores)
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
wasmtime=${WASMTIME:-$(command -v wasmtime)} || {
	echo "libm-reference.sh: no wasmtime on PATH; set WASMTIME" >&2
	exit 2
}
logdir=${LOGDIR:-$root/experiments/libm-reference}
mkdir -p "$logdir"
expected=libc-gen/test_math/expected.txt

"$wasmtime" compile -W nan-canonicalization=y libc-gen/test_math/mathtest.wasm -o "$logdir/mathtest.cwasm" || exit 1

call() { # call <export> <operand bits>...: prints the result's bits
	local export=$1 r
	shift
	local args=()
	for a in "$@"; do args+=("$((a))"); done
	r=$("$wasmtime" run --allow-precompiled -W nan-canonicalization=y \
		--invoke "$export" "$logdir/mathtest.cwasm" "${args[@]}" 2>&1 | grep -v '^warning')
	printf '0x%x' "$r"
}

invoke() { # invoke <lines of expected.txt>: the same lines, from wasmtime
	local line name ops res r
	while read -r line; do
		name=${line%% *}
		ops=${line#"$name"}
		ops=${ops%% -> *}
		res=${line#* -> }
		# shellcheck disable=SC2086 # the operands are words
		r="$name$ops -> $(call "${name}_" $ops)"
		if [ "$res" != "${res#* }" ]; then # a second result
			r="$r $(call "${name}_2" $ops)"
		fi
		echo "$r"
	done <"$1"
}

split -n l/4 -d "$expected" "$logdir/part."
for p in "$logdir"/part.0[0-3]; do
	invoke "$p" >"$p.out" &
done
wait
cat "$logdir"/part.0[0-3].out >"$logdir/wasmtime.txt"
if cmp -s "$logdir/wasmtime.txt" "$expected"; then
	echo "expected.txt is identical to wasmtime's results ($(wc -l <"$logdir/wasmtime.txt") calls)"
else
	diff "$logdir/wasmtime.txt" "$expected" | head -40
	echo "expected.txt differs from wasmtime's results ($logdir/wasmtime.txt)" >&2
	exit 1
fi
