package stackweight

import (
	"os"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".wasm")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var opts = Options{StackLimit: 2048, NativeStack: 2048}

// Every refusal that names a fix clears when the fix is applied: each
// module is refused, and the same module with the fix is weighed.
func TestRefusalsClearWithTheirFix(t *testing.T) {
	for _, tt := range []struct{ refused, fixed, says string }{
		{"export-missing", "export-added", "link it with -Wl,--export=__stack_pointer"},
		{"tail-call", "tail-call-removed", "build without -mtail-call"},
		{"exceptions", "exceptions-removed", "build without -fwasm-exceptions"},
	} {
		_, err := Weigh(read(t, tt.refused), opts)
		if err == nil || !strings.Contains(err.Error(), tt.says) {
			t.Errorf("%s: got %v, want an error saying %q", tt.refused, err, tt.says)
		}
		if _, err := Weigh(read(t, tt.fixed), opts); err != nil {
			t.Errorf("%s: %v", tt.fixed, err)
		}
	}
}

// A stack limit reaching below address 0 is refused, and a lower one is
// accepted; so is a charge larger than the room below the limit, until the
// native stack is raised.
func TestLimitsThatDoNotFitAreRefused(t *testing.T) {
	m := read(t, "export-added")
	_, err := Weigh(m, Options{StackLimit: 8192, NativeStack: 8192})
	if err == nil || !strings.Contains(err.Error(), "lower the stack limit") {
		t.Fatalf("a stack limit past address 0: got %v", err)
	}
	if _, err := Weigh(m, Options{StackLimit: 3000, NativeStack: 3000}); err != nil {
		t.Fatalf("the lowered stack limit: %v", err)
	}
	// f's estimate is 274 bytes: 3000/16 of it is more than the 1,096
	// bytes between the limit and address 0.
	_, err = Weigh(m, Options{StackLimit: 3000, NativeStack: 16})
	if err == nil || !strings.Contains(err.Error(), "raise the native stack") {
		t.Fatalf("a charge past the room below the limit: got %v", err)
	}
	if _, err := Weigh(m, Options{StackLimit: 3000, NativeStack: 3000}); err != nil {
		t.Fatalf("the raised native stack: %v", err)
	}
}

// The pass records itself in the module and refuses to run on its own
// output: a second run would charge every function twice.
func TestWeighingTwiceIsRefused(t *testing.T) {
	res, err := Weigh(read(t, "export-added"), opts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Weigh(res.Module, opts)
	if err == nil || !strings.Contains(err.Error(), "the pass ran on it before") {
		t.Fatalf("got %v, want the second run refused", err)
	}
}

// A charge is the estimate scaled by the stack limit over the native
// stack, rounded up to 16 bytes.
func TestChargeIsTheScaledEstimate(t *testing.T) {
	res, err := Weigh(read(t, "export-added"), Options{StackLimit: 3000, NativeStack: 1000})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Functions[0]
	// One slot: 272 + 2, times 3, rounded up to 16.
	if f.Estimate != 274 || f.Charge != 832 || !f.Recursive {
		t.Fatalf("got estimate %d, charge %d, recursive %t; want 274, 832, true", f.Estimate, f.Charge, f.Recursive)
	}
}
