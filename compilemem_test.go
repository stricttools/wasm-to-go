//go:build !generator && linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// compileMemoryBound is the most memory, in MiB of peak resident set size,
// any compile process of the reference module's packages may take in
// Test_quickjs, the least peak of three compiles: the measured peak on
// linux/amd64 and js/wasm, with the margin its run-to-run variation needs
// (see the README's compile cost section). A change that makes the
// translation cost more fails the test.
const compileMemoryBound = 300

// The toolexec mode of the test binary: `go build -toolexec` runs it as
// "test.binary -wasm2go-toolexec-log=FILE tool args...", and it runs the
// tool, recording each compile's peak memory in FILE.
const toolexecFlag = "-wasm2go-toolexec-log="

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && strings.HasPrefix(os.Args[1], toolexecFlag) {
		os.Exit(toolexec(strings.TrimPrefix(os.Args[1], toolexecFlag), os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == translateFlag {
		os.Args = append([]string{"wasm2go"}, os.Args[2:]...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Runs the tool args[0] with args[1:], and appends "package peak_kb
// seconds" to log if it is the compiler.
func toolexec(log string, args []string) int {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	start := time.Now()
	err := cmd.Run()
	if cmd.ProcessState == nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if filepath.Base(args[0]) == "compile" {
		pkg := ""
		for i, a := range args {
			if a == "-p" && i+1 < len(args) {
				pkg = args[i+1]
			}
		}
		// On Linux, Maxrss is in KiB.
		rss := cmd.ProcessState.SysUsage().(*syscall.Rusage).Maxrss
		f, ferr := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr == nil {
			fmt.Fprintf(f, "%s %d %.2f\n", pkg, rss, time.Since(start).Seconds())
			f.Close()
		}
	}
	return cmd.ProcessState.ExitCode()
}

// The reference module, QuickJS-ng (testdata/quickjs), compiles with every
// compile process of its packages under compileMemoryBound, one package at
// a time and with GOMAXPROCS=4 (the backend concurrency of builds that
// compile several packages at once), for linux/amd64 and js/wasm, and runs
// test.js as native QuickJS does.
func Test_quickjs(t *testing.T) {
	testQuickJS(t, compileMemoryBound, "linux/amd64", "js/wasm")
}

func testQuickJS(t *testing.T, bound float64, targets ...string) {
	saved := [...]any{*importPath, *embed, *pkg, embedFile, *unsafe}
	t.Cleanup(func() {
		*importPath, *embed, *pkg, embedFile, *unsafe = saved[0].(string), saved[1].(bool), saved[2].(string), saved[3].(string), saved[4].(bool)
	})
	dir := t.TempDir()
	*importPath, *embed, *pkg, *unsafe = "qjstest/qjs", true, "qjs", false
	embedFile = filepath.Join(dir, "qjs", "qjs.dat")
	if err := os.MkdirAll(filepath.Join(dir, "qjs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := translateFile("testdata/quickjs/qjs.wasm", filepath.Join(dir, "qjs", "qjs")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module qjstest\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver, err := os.ReadFile("testdata/quickjs/driver/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "driver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "driver", "main.go"), driver, 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A constant unique to this compile in each package of the
	// translation (or in the package pkgDir), so the build cache holds
	// none of them and each is compiled, and measured.
	writeNonce := func(pkgDir string) {
		nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
		err := filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			if path != pkgDir && pkgDir != filepath.Join(dir, "qjs") {
				return filepath.SkipDir
			}
			if gos, _ := filepath.Glob(filepath.Join(path, "*.go")); len(gos) == 0 {
				return nil
			}
			name := filepath.Base(path)
			src := fmt.Sprintf("package %s\n\nconst _ = %q\n", name, nonce)
			return os.WriteFile(filepath.Join(path, "zz_nonce.go"), []byte(src), 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	writeNonce(filepath.Join(dir, "qjs"))

	builds := 0
	build := func(target string) map[string]float64 {
		goos, goarch, _ := strings.Cut(target, "/")
		builds++
		log := filepath.Join(dir, "compile-"+goarch+"-"+strconv.Itoa(builds)+".log")
		args := []string{"build", "-p", "1", "-toolexec", self + " " + toolexecFlag + log}
		pkgs := "./qjs/..."
		if target == "linux/amd64" {
			args = append(args, "-o", filepath.Join(dir, "driver.bin"))
			pkgs = "./driver"
		} else {
			args = append(args, "-o", os.DevNull)
		}
		cmd := exec.Command("go", append(args, pkgs)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch,
			"GOMAXPROCS=4", "GOFLAGS=", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOGC=100", "GOMEMLIMIT=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build for %s: %v\n%s", target, err, out)
		}
		peaks, err := readCompileLog(log, "qjstest/qjs")
		if err != nil {
			t.Fatal(err)
		}
		return peaks
	}
	for _, target := range targets {
		peaks := build(target)
		if len(peaks) == 0 {
			t.Fatalf("%s: no compile of the translation was measured (was it cached?)", target)
		}
		max, maxPkg := 0.0, ""
		for p, mib := range peaks {
			if mib > max {
				max, maxPkg = mib, p
			}
		}
		t.Logf("%s: %d packages, largest compile %.0f MiB (%s)", target, len(peaks), max, maxPkg)
		// A compile over the bound is measured twice more, and the least
		// of its three peaks counts: the collector's timing moves one
		// compile's peak by a tenth.
		for p, least := range peaks {
			for range 2 {
				if least <= bound {
					break
				}
				writeNonce(filepath.Join(dir, strings.TrimPrefix(p, "qjstest/")))
				again, ok := build(target)[p]
				if !ok {
					t.Fatalf("%s: %s was not compiled again", target, p)
				}
				t.Logf("%s: compiling %s again took %.0f MiB", target, p, again)
				least = min(least, again)
			}
			if least > bound {
				t.Errorf("%s: compiling %s took at least %.0f MiB, over the bound (%.0f MiB)", target, p, least, bound)
			}
		}
	}

	out, err := exec.Command(filepath.Join(dir, "driver.bin"), "testdata/quickjs/test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("driver: %v\n%s", err, out)
	}
	want, err := os.ReadFile("testdata/quickjs/expected.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(want) {
		t.Errorf("test.js gives\n%s\nwant\n%s", out, want)
	}
}

// The peak memory in MiB of each compile of the packages under prefix
// the log records.
func readCompileLog(log, prefix string) (map[string]float64, error) {
	f, err := os.Open(log)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	peaks := map[string]float64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 || !strings.HasPrefix(fields[0], prefix) {
			continue
		}
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, err
		}
		peaks[fields[0]] = max(peaks[fields[0]], kb/1024)
	}
	return peaks, sc.Err()
}
