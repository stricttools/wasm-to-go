// Command listcases prints the determinism test's calls, one per line:
// the name, the export, and the operands in hex and as signed decimal
// (the form wasmtime's --invoke takes). scripts/determinism-reference.sh
// uses it.
package main

import (
	"fmt"

	determinism "github.com/stricttools/wasm-to-go/testdata/determinism"
)

func main() {
	for _, c := range determinism.Cases() {
		fmt.Printf("%s %s %#x %#x %d %d\n", c.Name, c.Export, c.A, c.B, int64(c.A), int64(c.B))
	}
}
