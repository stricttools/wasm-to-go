// Command stack-weight runs the stack-weight pass over a WebAssembly module
// (see the README's section on the pass).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/stackweight"
)

func main() {
	stackLimit := flag.Int64("stack-limit", 0, "bytes __stack_pointer may go below its initial value before a charging function traps (required)")
	nativeStack := flag.Int64("native-stack", 0, "native stack, in bytes, the estimated frames may take at the stack limit (required)")
	out := flag.String("o", "", "the rewritten module (required)")
	report := flag.String("report", "", "also write a table of every function's estimate and charge here")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: stack-weight -stack-limit BYTES -native-stack BYTES -o out.wasm [-report file.tsv] in.wasm\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 || *stackLimit == 0 || *nativeStack == 0 || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	in, err := os.ReadFile(flag.Arg(0))
	check(err)
	res, err := stackweight.Weigh(in, stackweight.Options{StackLimit: *stackLimit, NativeStack: *nativeStack})
	check(err)
	check(os.WriteFile(*out, res.Module, 0o644))
	if *report != "" {
		var b strings.Builder
		b.WriteString("function\trecursive\tparams\tlocals\testimate\tcharge\n")
		for _, f := range res.Functions {
			fmt.Fprintf(&b, "%d\t%t\t%s\t%s\t%d\t%d\n", f.Index, f.Recursive, types(f.Params), types(f.Locals), f.Estimate, f.Charge)
		}
		check(os.WriteFile(*report, []byte(b.String()), 0o644))
	}
}

// types writes value types as their text-format names' first letters and
// sizes, for example "i32 i32 f64".
func types(ts []byte) string {
	names := map[byte]string{0x7f: "i32", 0x7e: "i64", 0x7d: "f32", 0x7c: "f64", 0x7b: "v128", 0x70: "funcref", 0x6f: "externref"}
	var s []string
	for _, t := range ts {
		s = append(s, names[t])
	}
	return strings.Join(s, " ")
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "stack-weight:", err)
		os.Exit(1)
	}
}
