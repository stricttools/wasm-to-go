//go:build !generator && linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// translateMemoryBound is the most memory, in MiB of peak resident set
// size, translating the reference module (testdata/quickjs) may take in
// Test_translate_memory, the least peak of three runs: just above the
// least peaks measured (see the README's
// compile cost section). A change that makes the translator hold more
// fails the test.
const translateMemoryBound = 250

// The translate mode of the test binary: "test.binary -wasm2go-translate
// args..." runs the translator's command with args.
const translateFlag = "-wasm2go-translate"

// The translator, run as a command on the reference module (QuickJS-ng,
// written as several packages), peaks under translateMemoryBound: the
// least peak of three runs, since the collector's timing moves one run's
// peak by up to a seventh.
func Test_translate_memory(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	least := 0.0
	for i := range 3 {
		dir := t.TempDir()
		out := filepath.Join(dir, "qjs", "qjs.go")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(self, translateFlag, "-embed", "-pkg", "qjs", "-importpath", "qjstest/qjs",
			"-o", out, "testdata/quickjs/qjs.wasm")
		cmd.Env = append(os.Environ(), "GOGC=100", "GOMEMLIMIT=off")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("translating: %v\n%s", err, b)
		}
		// On Linux, Maxrss is in KiB.
		mib := float64(cmd.ProcessState.SysUsage().(*syscall.Rusage).Maxrss) / 1024
		t.Logf("translating QuickJS-ng peaked at %.0f MiB", mib)
		if i == 0 || mib < least {
			least = mib
		}
	}
	if least > translateMemoryBound {
		t.Errorf("translating QuickJS-ng took at least %.0f MiB, over the bound (%d MiB)", least, translateMemoryBound)
	}
}
