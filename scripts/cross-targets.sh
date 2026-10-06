#!/usr/bin/env bash
# cross-targets.sh: runs the floating-point spec tests and the determinism
# tests (Test_determinism_expected, with the regression tests of NaN results,
# conversions, and constants, and Test_determinism_libm, libc-gen's C math
# functions) on every Go target: amd64 natively (at GOAMD64=v1, and at v3,
# where the CPU has fused multiply-add), amd64 with GODEBUG=cpu.sse41=off,
# amd64 with GODEBUG=cpu.fma=off (math.FMA, which libc-gen's fma calls, then
# computes in software), 386 natively, every other Linux GOARCH under
# qemu-user, wasip1/wasm under wasmtime, and js/wasm under Node.js.
#
# Usage: scripts/cross-targets.sh [-all] [target...]
#
#   -all      run the whole spec suite, not only its floating-point part
#   target    labels to run (default: all of them), from:
#             amd64 amd64-v3 amd64-nosse41 amd64-nofma 386 arm arm64 loong64
#             mips mipsle mips64 mips64le ppc64 ppc64le riscv64 s390x wasip1 js
#
# Environment:
#   QEMU_DIR  directory holding qemu-<arch>-static (or qemu-<arch>) binaries;
#             when unset, they are looked up on PATH
#   WASMTIME  the wasmtime binary for wasip1 (default: wasmtime on PATH)
#   GOFLAGS_P the go -p value (default 2: cross builds are memory-hungry)
#   LOGDIR    where each target's go test output goes (default:
#             experiments/cross-targets, which git ignores)
#
# Every target is built with the same pinned settings (GOAMD64=v1, GO386=sse2,
# GOARM=7, GOMIPS=hardfloat, GOMIPS64=hardfloat, GOPPC64=power8,
# GORISCV64=rva20u64, GOARM64=v8.0, CGO_ENABLED=0) and without any flag that
# disables fused multiply-add: the translated code must not fuse on its own.
# The script exits nonzero when any target fails, and prints one line per
# target.
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

all=
if [ "${1:-}" = -all ]; then
	all=1
	shift
fi
targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
	targets=(amd64 amd64-v3 amd64-nosse41 amd64-nofma 386 arm arm64 loong64 mips mipsle mips64 mips64le ppc64 ppc64le riscv64 s390x wasip1 js)
fi

export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= GODEBUG=
export GOAMD64=v1 GO386=sse2 GOARM=7 GOARM64=v8.0 GOMIPS=hardfloat GOMIPS64=hardfloat GOPPC64=power8 GORISCV64=rva20u64
p=${GOFLAGS_P:-2}

if [ -n "$all" ]; then
	pkgs=(./internal/spectest/...)
else
	pkgs=()
	for d in conversions const f32 f32_bitwise f32_cmp f64 f64_bitwise f64_cmp \
		float_exprs float_literals float_memory float_misc memory64/float_memory64 \
		simd/simd_conversions simd/simd_f32x4 simd/simd_f32x4_arith simd/simd_f32x4_cmp \
		simd/simd_f32x4_pmin_pmax simd/simd_f32x4_rounding simd/simd_f64x2 simd/simd_f64x2_arith \
		simd/simd_f64x2_cmp simd/simd_f64x2_pmin_pmax simd/simd_f64x2_rounding \
		simd/simd_i32x4_trunc_sat_f32x4 simd/simd_i32x4_trunc_sat_f64x2; do
		pkgs+=("./internal/spectest/$d/...")
	done
fi

qemu() { # qemu <qemu arch name>: prints the qemu-user binary
	local n
	for n in "qemu-$1-static" "qemu-$1"; do
		if [ -n "${QEMU_DIR:-}" ] && [ -x "$QEMU_DIR/$n" ]; then
			echo "$QEMU_DIR/$n"
			return
		fi
		if command -v "$n" >/dev/null; then
			command -v "$n"
			return
		fi
	done
	echo "cross-targets.sh: no qemu-$1-static or qemu-$1 in QEMU_DIR or on PATH" >&2
	return 1
}

# The spec translations and their tests are generated, not committed.
echo "generating the spec test translations (go generate)"
go generate . >/dev/null || { echo "go generate failed" >&2; exit 1; }

logdir=${LOGDIR:-$root/experiments/cross-targets}
mkdir -p "$logdir"
wasmdir=$(go env GOROOT)/lib/wasm
failed=()
summary=()
for t in "${targets[@]}"; do
	goos=linux goarch=$t exec= env=()
	case $t in
	amd64) ;;
	amd64-v3) goarch=amd64 env=(GOAMD64=v3) ;;
	amd64-nosse41) goarch=amd64 env=(GODEBUG=cpu.sse41=off) ;;
	amd64-nofma) goarch=amd64 env=(GODEBUG=cpu.fma=off) ;;
	386) ;;
	arm) exec=$(qemu arm) || exit 1 ;;
	arm64) exec=$(qemu aarch64) || exit 1 ;;
	loong64) exec=$(qemu loongarch64) || exit 1 ;;
	mips) exec=$(qemu mips) || exit 1 ;;
	mipsle) exec=$(qemu mipsel) || exit 1 ;;
	mips64) exec=$(qemu mips64) || exit 1 ;;
	mips64le) exec=$(qemu mips64el) || exit 1 ;;
	ppc64) exec=$(qemu ppc64) || exit 1 ;;
	ppc64le) exec=$(qemu ppc64le) || exit 1 ;;
	riscv64) exec=$(qemu riscv64) || exit 1 ;;
	s390x) exec=$(qemu s390x) || exit 1 ;;
	wasip1) goos=wasip1 goarch=wasm exec="$wasmdir/go_wasip1_wasm_exec" ;;
	js) goos=js goarch=wasm exec="$wasmdir/go_js_wasm_exec" ;;
	*)
		echo "cross-targets.sh: unknown target $t" >&2
		exit 2
		;;
	esac
	args=(-p "$p" -count=1)
	[ -n "$exec" ] && args+=(-exec "$exec")
	path=$PATH
	[ -n "${WASMTIME:-}" ] && path=$(dirname "$WASMTIME"):$path
	echo "== $t"
	log=$logdir/$t.txt
	clean=()
	if [ "$t" = js ]; then
		# Go's js/wasm runtime refuses to start when its arguments and
		# environment exceed a small limit, so the tests get only what go needs.
		clean=(-i HOME="$HOME" GOCACHE="$(go env GOCACHE)" GOPATH="$(go env GOPATH)"
			GOMODCACHE="$(go env GOMODCACHE)" TMPDIR="${TMPDIR:-}" GOTMPDIR="$(go env GOTMPDIR)"
			CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=)
	fi
	run() { env ${clean[@]+"${clean[@]}"} ${env[@]+"${env[@]}"} PATH="$path" GOOS=$goos GOARCH=$goarch go test "${args[@]}" "$@"; }
	{
		run -run '^Test_(determinism|regression)_' . ./libc-gen/test_math/
		determinism=$?
		run "${pkgs[@]}"
		spec=$?
	} >"$log" 2>&1
	if [ $determinism -eq 0 ] && [ $spec -eq 0 ]; then
		summary+=("$t: ok ($(grep -c '^ok' "$log") packages)")
	else
		grep -v '^ok' "$log" | head -40
		summary+=("$t: FAIL (determinism tests exit $determinism, spec tests exit $spec; see $log)")
		failed+=("$t")
	fi
done
echo
printf '%s\n' "${summary[@]}"
[ ${#failed[@]} -eq 0 ]
