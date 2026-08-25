// Package sshconfig enumerates Host entries from an ssh config file.
// It does NOT expand ssh tokens — that is delegated to `ssh -G`.
package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hosts returns the concrete Host aliases declared in the given config file,
// in declaration order, de-duplicated. Pattern-only Host values (containing
// any of * ? !) are skipped. Include directives are followed (paths relative
// to the config file's directory or absolute). Include cycles are detected
// and skipped to avoid infinite recursion.
func Hosts(configPath string) ([]string, error) {
	seen := map[string]bool{}
	visited := map[string]bool{}
	var out []string
	if err := collect(configPath, &out, seen, visited); err != nil {
		return nil, err
	}
	return out, nil
}

func collect(configPath string, out *[]string, seen map[string]bool, visited map[string]bool) error {
	abs, err := filepath.Abs(configPath)
	if err == nil {
		if visited[abs] {
			return nil // already opened; break include cycle
		}
		visited[abs] = true
	}
	f, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("sshconfig: open %s: %w", configPath, err)
	}
	defer f.Close()

	baseDir := filepath.Dir(configPath)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// ssh config keys are case-insensitive.
		keyword, rest := splitKeyword(line)
		switch strings.ToLower(keyword) {
		case "host":
			for _, name := range fields(rest) {
				if isPattern(name) {
					continue
				}
				if !seen[name] {
					seen[name] = true
					*out = append(*out, name)
				}
			}
		case "include":
			for _, pattern := range fields(rest) {
				p := pattern
				if !filepath.IsAbs(p) {
					p = filepath.Join(baseDir, p)
				}
				matches, _ := filepath.Glob(p)
				if len(matches) == 0 {
					matches = []string{p} // try as literal
				}
				for _, m := range matches {
					if err := collect(m, out, seen, visited); err != nil {
						// Missing includes are non-fatal in ssh; skip silently.
						_ = err
					}
				}
			}
		}
	}
	return sc.Err()
}

// splitKeyword returns the first whitespace-delimited keyword and the rest.
func splitKeyword(line string) (keyword, rest string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

// fields splits on whitespace (spaces and tabs).
func fields(s string) []string {
	return strings.Fields(s)
}

// isPattern reports whether a Host value is a glob pattern rather than a
// concrete alias. ssh patterns use *, ?, and ! for negation.
func isPattern(name string) bool {
	if strings.ContainsAny(name, "*?!") {
		return true
	}
	return false
}

// KnownHosts returns hostnames from ~/.ssh/known_hosts. These are hosts the
// user has connected to before, even if they have no explicit Host entry in
// ~/.ssh/config. Each line of known_hosts is "hostname key-type key-data" or
// "hostname:port key-type key-data". Multiple hostnames can be
// comma-separated. Hashed entries (|1|...) are skipped since they can't be
// reversed.
func KnownHosts(knownHostsPath string) ([]string, error) {
	f, err := os.Open(knownHostsPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip hashed entries: |1|hash|hash
		if strings.HasPrefix(line, "|1|") {
			continue
		}
		// First field is the hostname(s), comma-separated.
		// Can also be hostname:port.
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		for _, host := range strings.Split(parts[0], ",") {
			host = strings.TrimSpace(host)
			// Strip :port suffix if present (e.g. github.com:22)
			if idx := strings.LastIndex(host, ":"); idx > 0 && !strings.Contains(host[idx+1:], ":") {
				host = host[:idx]
			}
			if host == "" || isPattern(host) {
				continue
			}
			if !seen[host] {
				seen[host] = true
				out = append(out, host)
			}
		}
	}
	return out, sc.Err()
}
