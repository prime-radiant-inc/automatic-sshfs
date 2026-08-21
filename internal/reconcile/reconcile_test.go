package reconcile

import (
	"reflect"
	"sort"
	"testing"
)

func TestDiffAllMount(t *testing.T) {
	desired := map[string]bool{"web": true, "db": true}
	actual := map[string]bool{}
	p := Diff(desired, actual)
	if !sortedEqual(p.Mounts, []string{"db", "web"}) {
		t.Errorf("Mounts = %v, want [db web]", p.Mounts)
	}
	if len(p.Unmounts) != 0 {
		t.Errorf("Unmounts = %v, want []", p.Unmounts)
	}
}

func TestDiffAllUnmount(t *testing.T) {
	desired := map[string]bool{}
	actual := map[string]bool{"web": true, "db": true}
	p := Diff(desired, actual)
	if len(p.Mounts) != 0 {
		t.Errorf("Mounts = %v, want []", p.Mounts)
	}
	if !sortedEqual(p.Unmounts, []string{"db", "web"}) {
		t.Errorf("Unmounts = %v, want [db web]", p.Unmounts)
	}
}

func TestDiffMixed(t *testing.T) {
	desired := map[string]bool{"web": true, "api": true}
	actual := map[string]bool{"web": true, "db": true}
	p := Diff(desired, actual)
	if !sortedEqual(p.Mounts, []string{"api"}) {
		t.Errorf("Mounts = %v, want [api]", p.Mounts)
	}
	if !sortedEqual(p.Unmounts, []string{"db"}) {
		t.Errorf("Unmounts = %v, want [db]", p.Unmounts)
	}
}

func TestDiffStable(t *testing.T) {
	desired := map[string]bool{"web": true}
	actual := map[string]bool{"web": true}
	p := Diff(desired, actual)
	if len(p.Mounts) != 0 || len(p.Unmounts) != 0 {
		t.Errorf("expected no-op plan, got %+v", p)
	}
}

func TestDiffIgnoresFalseValues(t *testing.T) {
	// A host explicitly false should not be treated as present.
	desired := map[string]bool{"web": false, "db": true}
	actual := map[string]bool{"web": true}
	p := Diff(desired, actual)
	if !sortedEqual(p.Mounts, []string{"db"}) {
		t.Errorf("Mounts = %v, want [db]", p.Mounts)
	}
	if !sortedEqual(p.Unmounts, []string{"web"}) {
		t.Errorf("Unmounts = %v, want [web]", p.Unmounts)
	}
}

func sortedEqual(a, b []string) bool {
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	return reflect.DeepEqual(sa, sb)
}
