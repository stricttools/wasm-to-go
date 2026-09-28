#!/usr/bin/env bash
# libm-reference.sh: checks libc-gen/test_math/expected.txt against wasmtime
# with NaN canonicalization (-W nan-canonicalization=y), which implements the
# WebAssembly deterministic profile. Every call of the math test
# (Test_math_expected) is made with wasmtime --invoke on
# libc-gen/test_math/mathtest.wasm, the module the test's translation was
# made from, and wasmtime's results must be identical to expected.txt.
#
# The module imports fma, a Go host function in the translation (IEEE 754
# defines its result exactly, and a NaN result is the canonical NaN). Here
# wasmtime links it to musl's fma (libc-gen/c/libm/fma.c, compiled to
# WebAssembly with libc-gen's clang, as it was before it became a host
# function), with two corrections: a NaN result is the canonical NaN (musl
# returns a NaN third operand unchanged, which the deterministic profile does
# not), and a finite nonzero product plus a zero is the product, rounded once
# (musl adds the zero, which turns a negative product that rounds to -0 into
# +0; IEEE 754 gives the exact result's sign, the product's).
#
# Usage: scripts/libm-reference.sh
#
# Environment:
#   WASMTIME  the wasmtime binary (default: wasmtime on PATH)
#   WASI_SDK  wasi-sdk's bin directory (default: libc-gen/tools/wasi-sdk/bin,
#             which libc-gen/tools.sh installs)
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

wasi_sdk=${WASI_SDK:-$root/libc-gen/tools/wasi-sdk/bin}
cat >"$logdir/fma.c" <<'EOF'
#define fma musl_fma
#include "libm/fma.c"
#include "libm/scalbn.c"
#undef fma

// Bit operations, which clang cannot fold away as it could a NaN test.
__attribute__((export_name("fma"))) double fma_canon(double x, double y, double z) {
	unsigned long long r = __builtin_bit_cast(unsigned long long, musl_fma(x, y, z));
	if (z == 0 && x != 0 && y != 0 && __builtin_isfinite(x) && __builtin_isfinite(y))
		r = __builtin_bit_cast(unsigned long long, x * y);
	if ((r & 0x7fffffffffffffff) > 0x7ff0000000000000)
		r = 0x7ff8000000000000;
	return __builtin_bit_cast(double, r);
}
EOF
"$wasi_sdk/clang" --target=wasm32 -ffreestanding -nostdlib -std=c23 -O2 -w \
	-include features.h -I libc-gen/c -I libc-gen/c/libm -Wl,--no-entry \
	-o "$logdir/fma.wasm" "$logdir/fma.c" || exit 1
"$wasmtime" compile -W nan-canonicalization=y libc-gen/test_math/mathtest.wasm -o "$logdir/mathtest.cwasm" || exit 1
"$wasmtime" compile -W nan-canonicalization=y "$logdir/fma.wasm" -o "$logdir/fma.cwasm" || exit 1

call() { # call <export> <operand bits>...: prints the result's bits
	local export=$1 r
	shift
	local args=()
	for a in "$@"; do args+=("$((a))"); done
	r=$("$wasmtime" run --allow-precompiled -W nan-canonicalization=y \
		--preload env="$logdir/fma.cwasm" --invoke "$export" "$logdir/mathtest.cwasm" "${args[@]}" 2>&1 | grep -v '^warning')
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
