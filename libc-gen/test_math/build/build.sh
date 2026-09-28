#!/usr/bin/env bash
# Builds main.c with libc-gen's C library, and translates it to ../mathtest.go
# (and the host functions libc-gen provides to ../libc.go).
set -euo pipefail

cd -P -- "$(dirname -- "$0")"

ROOT=../../../
LIBC="$ROOT/libc-gen/c/"
BINARYEN="$ROOT/libc-gen/tools/binaryen/bin/"
WASI_SDK="$ROOT/libc-gen/tools/wasi-sdk/bin/"

trap 'rm -f mathtest mathtest.wasm' EXIT

"$WASI_SDK/clang" --target=wasm32 -ffreestanding -nostdlib -std=c23 -g0 -Oz \
	-Wall -Wextra -Wno-unused-parameter -Wno-unused-function \
	-o mathtest main.c "$LIBC/libc.c" -I"$LIBC" \
	-mexec-model=reactor \
	-mmutable-globals -mmultivalue \
	-mnontrapping-fptoint -msign-ext \
	-mreference-types -mbulk-memory \
	-mextended-const -mtail-call \
	-mwide-arithmetic \
	-Wl,--no-entry \
	-Wl,--stack-first \
	-Wl,--import-undefined

"$BINARYEN/wasm-opt" -g mathtest -o mathtest.wasm \
	--gufa-optimizing --generate-global-effects \
	--low-memory-unused --converge -O4 \
	--enable-mutable-globals --enable-multivalue \
	--enable-nontrapping-float-to-int --enable-sign-ext \
	--enable-reference-types --enable-bulk-memory \
	--enable-extended-const --enable-tail-call \
	--enable-wide-arithmetic \
	--strip --strip-producers

cp mathtest.wasm ../mathtest.wasm
go run "$ROOT/libc-gen" -pkg mathtest -wasm mathtest.wasm -o ../libc.go
go run "$ROOT" -pkg mathtest -provided ../libc.go -o ../mathtest.go mathtest.wasm
