#!/usr/bin/env bash
# Builds qjs.wasm, the reference module of Test_quickjs: quickjs-ng v0.17.0
# (MIT, LICENSE) with qb.c, the embedding it exports (qb_new, qb_eval,
# qb_call, qb_out, qb_malloc, qb_free), for wasm32-wasip1 with wasi-sdk 34,
# then optimized with binaryen 133's wasm-opt as cgofree's tree-sitter
# recipe does.
#
# Usage: build.sh QUICKJS_NG_DIR WASI_SDK_DIR WASM_OPT
#
#   QUICKJS_NG_DIR  a checkout of github.com/quickjs-ng/quickjs at v0.17.0
#   WASI_SDK_DIR    wasi-sdk 34 (its bin/clang is used)
#   WASM_OPT        binaryen 133's wasm-opt
set -euo pipefail
if [ $# -ne 3 ]; then
	sed -n '2,13p' "$0" >&2
	exit 2
fi
qjs=$(cd "$1" && pwd) clang=$2/bin/clang wasmopt=$3
here=$(cd "$(dirname "$0")" && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

flags=(--target=wasm32-wasip1 -O2 -g0 -std=gnu11 -DNDEBUG -D_GNU_SOURCE -funsigned-char
	-D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_SIGNAL -w
	-mmutable-globals -mmultivalue -mnontrapping-fptoint -msign-ext
	-mreference-types -mbulk-memory -mextended-const -I"$qjs")
objs=()
for f in "$qjs/quickjs.c" "$qjs/libregexp.c" "$qjs/libunicode.c" "$qjs/dtoa.c" "$here/qb.c"; do
	o=$out/$(basename "$f" .c).o
	"$clang" "${flags[@]}" -c -o "$o" "$f"
	objs+=("$o")
done
"$clang" --target=wasm32-wasip1 -O2 -o "$out/qjs.wasm" "${objs[@]}" \
	-mexec-model=reactor -Wl,--stack-first -Wl,-z,stack-size=2097152 \
	-lwasi-emulated-process-clocks -lwasi-emulated-signal
"$wasmopt" "$out/qjs.wasm" -o "$here/qjs.wasm" --low-memory-unused --converge -O4 \
	--enable-mutable-globals --enable-multivalue --enable-nontrapping-float-to-int --enable-sign-ext \
	--enable-reference-types --enable-bulk-memory --enable-extended-const --strip --strip-producers
