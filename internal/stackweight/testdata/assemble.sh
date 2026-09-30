#!/bin/sh
# Assembles the pass's test modules (wabt's wat2wasm).
set -e
cd "$(dirname "$0")"
for f in *.wat; do wat2wasm --enable-tail-call --enable-exceptions "$f" -o "${f%.wat}.wasm"; done
