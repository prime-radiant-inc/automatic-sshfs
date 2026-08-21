// Package sshoracle resolves a host's effective ssh options by parsing the
// output of `ssh -G`, which expands all tokens (including %C) exactly as ssh
// itself would. We do not reimplement ssh's token expansion.
package sshoracle

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Resolved holds the ssh options asshfs cares about, as ssh -G reports them.
type Resolved struct {
	User           string
	HostName       string
	Port           string
	ControlPath    string
	ControlMaster  string
	ControlPersist string
}

// ParseG parses the text output of `ssh -G`. Keys are case-insensitive and
// lowercase in real output; we normalize defensively. Returns an error if the
// output contains no recognizable keys (e.g. not actually ssh -G output).
func ParseG(output string) (Resolved, error) {
	var r Resolved
	found := 0
	sc := bufio.NewScanner(strings.NewReader(output))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "user":
			r.User = val
			found++
		case "hostname":
			r.HostName = val
			found++
		case "port":
			r.Port = val
			found++
		case "controlpath":
			r.ControlPath = val
			found++
		case "controlmaster":
			r.ControlMaster = val
			found++
		case "controlpersist":
			r.ControlPersist = val
			found++
		}
	}
	if err := sc.Err(); err != nil {
		return r, err
	}
	if found == 0 {
		return r, fmt.Errorf("sshoracle: no ssh keys found in -G output")
	}
	return r, nil
}

// Resolve shells out to `ssh -G <host>` and parses the result.
func Resolve(host string) (Resolved, error) {
	cmd := exec.Command("ssh", "-G", host)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Resolved{}, fmt.Errorf("ssh -G %s: %w (%s)", host, err, stderr.String())
	}
	return ParseG(stdout.String())
}

// ResolveAll resolves each host, dropping any that error. Errors are returned
// as a map of host->error for the caller to log.
func ResolveAll(hosts []string) (map[string]Resolved, map[string]error) {
	resolved := make(map[string]Resolved, len(hosts))
	errs := make(map[string]error)
	for _, h := range hosts {
		r, err := Resolve(h)
		if err != nil {
			errs[h] = err
			continue
		}
		resolved[h] = r
	}
	return resolved, errs
}

func splitKV(line string) (key, val string, ok bool) {
	// ssh -G output is "key value" separated by whitespace.
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}
