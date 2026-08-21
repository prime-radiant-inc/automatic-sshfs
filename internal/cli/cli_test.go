package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoArgsReturns2(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs"}, &out, &errOut)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr = %q, want usage", errOut.String())
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "bogus"}, &out, &errOut)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown subcommand") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestHelpReturns0(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "--help"}, &out, &errOut)
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "reconcile") {
		t.Errorf("stdout = %q", out.String())
	}
}

// Reconcile with an empty/missing ssh config should succeed (no-op) with exit 0,
// not crash. This is the smoke test for the wiring.
func TestReconcileNoConfig(t *testing.T) {
	// Point HOME at a temp dir with no ~/.ssh/config so the reconcile is a no-op.
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "reconcile"}, &out, &errOut)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, errOut.String())
	}
}
