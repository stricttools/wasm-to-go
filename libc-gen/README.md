# C standard library generator

`libc-gen` is a utility that provides a minimal C standard library
to aid in translating C projects to Go (via `wasm2go`)
using `clang --target=wasm32 -ffreestanding -nostdlib`.

While primarily developed for translating SQLite,
it is suitable for porting other C projects.

## Overview

Compiling C to Wasm without a standard library leaves a gap:
modules still need memory allocation, basic algorithms,
and host-provided capabilities.

`libc-gen` bridges this gap with two components:

1. **C sources**:
a minimal C library containing header files,
and some bits best implemented in C,
such as a novel `qsort` implementation, STB's `sprintf`,
Doug Lea's `malloc`, or musl's `libm`.

2. **Go host functions**:
a code generator that emits testable Go methods
to back C functions best implemented in Go.

## Usage

```
Usage: libc-gen [option]... [func]...
  -c-out string
        extract libc C source and header files to directory
  -deref-mem
        dereference memory (*m.memory instead of m.memory)
  -m64
        use 64-bit pointers (int64)
  -o string
        output file (default stdout)
  -pkg string
        package name (default module name, or wasm2go)
  -version
        print version and exit
  -wasm string
        input.wasm file
```

## Future development

This library aims to provide a minimal implementation
of the C standard library.

Functions added to it should be part of the C standard.

Besides, the C component should not grow much beyond:
- _macros_ and _function declarations_ added to header files;
- _simple one-liners_ added to source files.

The big exceptions are `malloc` and `libm`, as they're best implemented in C.
I provide 3 alternative implementations:
- Doug Lea's public domain [allocator](https://gee.cs.oswego.edu/dl/html/malloc.html), configured for Wasm;
- a simple bump allocator for short lived modules;
- a newly developed [TLSF](http://www.gii.upv.es/tlsf/main/docs.html) allocator.

The `math.h` functions for `double` that are not compiler builtins
are [musl](https://musl.libc.org/)'s (`c/libm`, with musl's `COPYRIGHT`),
compiled into the module by `libc.c` (through the generated `c/libm.c`),
so they compute the same bits on every CPU once translated
(Go's `math` package, which host functions would call,
gives different results on different CPUs).
`fma` is the exception, a host function calling Go's `math.FMA`:
IEEE 754 defines its result exactly, so it is the same on every CPU,
and it is many times faster than musl's `fma` compiled to WebAssembly,
which has no fused multiply-add (`musl.sh` still copies musl's `fma.c`,
which [`scripts/libm-reference.sh`](../scripts/libm-reference.sh) checks it against).
[`musl.sh`](musl.sh) downloads musl, checks its SHA-256,
and copies the sources and regenerates `c/libm.c`;
it needs the tools of [`tools.sh`](tools.sh).
[`test_math`](test_math) checks every function's results bit for bit.

The Go component will contain stuff that's best implemented in Go:
- `string.h` because `bytes.Index`, `IndexByte`, etc are hard to beat;
- `fma` because `math.FMA` is exact and uses the CPU's instruction where there is one.

I will not be adding file I/O to this, or any other OS stuff.

I _can_ add the necessary standard C declarations,
but you will need to implement the Go component within your own host module.

Or just use WASI and either implement your own WASI host module,
or use [github.com/lbe/wasm2go-wasi-host](https://github.com/lbe/wasm2go-wasi-host).
