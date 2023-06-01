#!/usr/bin/env bash
set -euo pipefail

cd -P -- "$(dirname -- "$0")"

ROOT=../../../../
BINARYEN="$ROOT/tools/binaryen-version_112/bin"
WASI_SDK="$ROOT/tools/wasi-sdk-20.0/bin"

"$WASI_SDK/clang" --target=wasm32-wasi -flto -g0 -O2 \
	-o speedtest1.wasm main.c \
	-I"$ROOT/sqlite3" \
	-mmutable-globals \
	-mbulk-memory -mreference-types \
	-mnontrapping-fptoint -msign-ext \
	-Wl,--stack-first \
	-Wl,--import-undefined

"$BINARYEN/wasm-opt" -g -O2 speedtest1.wasm -o speedtest1.tmp \
	--enable-multivalue --enable-mutable-globals \
	--enable-bulk-memory --enable-reference-types \
	--enable-nontrapping-float-to-int --enable-sign-ext
mv speedtest1.tmp speedtest1.wasm