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

func TestReconcilePlaceholder(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "reconcile"}, &out, &errOut)
	if code != 1 {
		t.Errorf("code = %d, want 1 (not yet implemented)", code)
	}
}
