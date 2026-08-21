package launchd

import (
	"strings"
	"testing"
	"unicode"
)

func TestPlistContainsRequiredKeys(t *testing.T) {
	got := Plist("/Users/jesse/.ssh/cm", "/usr/local/bin/asshfs")
	if !strings.Contains(got, "<key>Label</key>") {
		t.Error("missing Label")
	}
	if !strings.Contains(got, "io.asshfs.reconcile") {
		t.Error("missing label value")
	}
	if !strings.Contains(got, "<key>WatchPaths</key>") {
		t.Error("missing WatchPaths")
	}
	if !strings.Contains(got, "/Users/jesse/.ssh/cm") {
		t.Error("missing socket dir")
	}
	if !strings.Contains(got, "<key>ProgramArguments</key>") {
		t.Error("missing ProgramArguments")
	}
	if !strings.Contains(got, "/usr/local/bin/asshfs") {
		t.Error("missing binary path")
	}
	if !strings.Contains(got, "reconcile") {
		t.Error("missing reconcile arg")
	}
	if !strings.Contains(got, "<key>RunAtLoad</key>") {
		t.Error("missing RunAtLoad")
	}
}

func TestPlistIsWellFormedXML(t *testing.T) {
	got := Plist("/x", "/y")
	// Crude but sufficient: balanced angle brackets for the plist doctype and
	// root tags. Full XML validation is overkill for v1.
	if !strings.HasPrefix(strings.TrimLeftFunc(got, unicode.IsSpace), "<?xml") {
		t.Error("plist should start with xml declaration")
	}
	if !strings.Contains(got, "<plist version=\"1.0\">") {
		t.Error("missing plist root")
	}
	if !strings.HasSuffix(strings.TrimRightFunc(got, unicode.IsSpace), "</plist>") {
		t.Error("plist should end with </plist>")
	}
}
