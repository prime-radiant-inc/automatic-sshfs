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
	if !strings.Contains(got, "<key>StartInterval</key>") {
		t.Error("missing StartInterval")
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
	if !strings.Contains(got, "<key>EnvironmentVariables</key>") {
		t.Error("missing EnvironmentVariables")
	}
	if !strings.Contains(got, "/usr/local/bin:/opt/homebrew/bin") {
		t.Error("missing PATH with homebrew dirs")
	}
	// Should NOT contain WatchPaths — we use StartInterval instead.
	if strings.Contains(got, "WatchPaths") {
		t.Error("should not contain WatchPaths (replaced by StartInterval)")
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
