package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/passes"
)

var (
	output = flag.String("o", "", "output file (default stdout)")
	pkg    = flag.String("pkg", "", "package name (default module name, or wasm2go)")
	tags   = flag.String("tags", "", "go:build tags to include in the generated file")

	embed     = flag.Bool("embed", false, "go:embed data sections from a .dat file")
	nohost    = flag.Bool("nohost", false, "don't generate interfaces for imports")
	noopt     = flag.Bool("noopt", false, "disable all optimization passes")
	unsafe    = flag.Bool("unsafe", false, "allow importing unsafe (requires -o: writes output.go and output_generic.go)")
	dwarfline = flag.Bool("dwarfline", false, "use line numbers from DWARF metadata")
	version   = flag.Bool("version", false, "print version and exit")

	provided  stringFlags
	embedFile string
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("wasm2go: ")

	flag.Var(&provided, "provided", "file containing provided import functions")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [option]... [input.wasm]\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()

	if *version {
		fmt.Fprintln(flag.CommandLine.Output(), filepath.Base(os.Args[0]), getVersion())
		os.Exit(0)
	}

	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := checkOutputFlags(); err != nil {
		log.Fatal(err)
	}
	if *embed {
		embedFile = strings.TrimSuffix(*output, filepath.Ext(*output)) + ".dat"
	}

	in := os.Stdin
	if flag.NArg() > 0 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		in = f
	}

	out := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		out = f
	}
	var generic io.Writer
	var genericOut *os.File
	if *unsafe {
		f, err := os.Create(genericFile(*output))
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		generic, genericOut = f, f
	}

	if err := translate(in, out, generic); err != nil {
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
	if genericOut != nil {
		if err := genericOut.Close(); err != nil {
			log.Fatal(err)
		}
	}
}

// Checks the flags that need an output file.
func checkOutputFlags() error {
	if *output != "" {
		return nil
	}
	switch {
	case *dwarfline:
		return errors.New("-dwarfline requires `-o output.go` to be specified")
	case *embed:
		return errors.New("-embed requires `-o output.go` to be specified")
	case *unsafe:
		return errors.New("-unsafe requires `-o output.go` to be specified: it writes two files, " +
			"output.go, built on " + passes.ExpandPlatforms + ", and output_generic.go, built everywhere else")
	}
	return nil
}

// The file for the platforms the expanded file is not for:
// out.go becomes out_generic.go.
func genericFile(output string) string {
	return strings.TrimSuffix(output, filepath.Ext(output)) + "_generic.go"
}

func getVersion() string {
	if Version != "" && Version != "dev" {
		return "v" + Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown)"
}

type stringFlags []string

func (l *stringFlags) String() string {
	return strings.Join(*l, ", ")
}

func (l *stringFlags) Set(value string) error {
	*l = append(*l, value)
	return nil
}

var seenReturnCall bool

func warnReturnCall() {
	if !seenReturnCall {
		seenReturnCall = true
		log.Print("return_call does not guarantee tail behavior")
	}
}

func needsUnsafe(msg string) {
	if !*unsafe {
		log.Fatal("needs unsafe: " + msg)
	}
}
