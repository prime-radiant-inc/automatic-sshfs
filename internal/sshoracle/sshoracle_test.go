package sshoracle

import (
	"reflect"
	"testing"
)

const sampleG = `user deploy
hostname 10.0.0.1
port 2222
controlmaster auto
controlpath /tmp/cm/be4420faf0ccd33bd13a61e0bfc1768c49e461db
controlpersist 30
host example.com
`
const sampleGNoControl = `user jesse
hostname example.com
port 22
controlmaster no
controlpath none
controlpersist no
`

func TestParseGFull(t *testing.T) {
	got, err := ParseG(sampleG)
	if err != nil {
		t.Fatal(err)
	}
	want := Resolved{
		User:           "deploy",
		HostName:       "10.0.0.1",
		Port:           "2222",
		ControlPath:    "/tmp/cm/be4420faf0ccd33bd13a61e0bfc1768c49e461db",
		ControlMaster:  "auto",
		ControlPersist: "30",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseG = %+v, want %+v", got, want)
	}
}

func TestParseGNoControl(t *testing.T) {
	got, err := ParseG(sampleGNoControl)
	if err != nil {
		t.Fatal(err)
	}
	if got.ControlPath != "none" {
		t.Errorf("ControlPath = %q, want none", got.ControlPath)
	}
	if got.User != "jesse" {
		t.Errorf("User = %q, want jesse", got.User)
	}
}

func TestParseGEmpty(t *testing.T) {
	_, err := ParseG("")
	if err == nil {
		t.Error("expected error for empty ssh -G output")
	}
}

func TestParseGMalformed(t *testing.T) {
	// No recognizable keys at all.
	_, err := ParseG("garbage line\nanother\n")
	if err == nil {
		t.Error("expected error when no ssh keys present")
	}
}

func TestResolveRealSSHG(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real ssh -G in short mode")
	}
	// example.com is always resolvable; ssh -G does not connect.
	r, err := Resolve("example.com")
	if err != nil {
		t.Skipf("ssh -G unavailable in this environment: %v", err)
	}
	if r.HostName != "example.com" {
		t.Errorf("HostName = %q, want example.com", r.HostName)
	}
	if r.Port != "22" {
		t.Errorf("Port = %q, want 22", r.Port)
	}
}
