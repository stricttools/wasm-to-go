#!/bin/sh
# rlsbl pre-release hook. Declaring it replaces the release's built-in tests
# (`go test ./... -race -short`, which walks every generated spec-test package
# and exceeds the release's time limit); every preflight check still runs.
#
# It runs the test sequence of .github/workflows/ci.yml, in the same order:
# the translate tests generate the spec-suite packages under internal/spectest,
# so the root package is vetted and tested before the packages that need them.
# Keep the two in step.

set -eu

go vet .
go test -args -unsafe
go test ./helpers ./internal/... ./libc-gen/...
