// Package reconcile computes the mount/unmount plan that brings the actual
// set of mounts into agreement with the desired set (control sockets present).
package reconcile

// Plan is the set of mount and unmount operations to perform.
type Plan struct {
	Mounts   []string // hosts to mount
	Unmounts []string // hosts to unmount
}

// Diff returns the operations needed to converge actual onto desired.
// A host is "present" in a set only if its value is true. Hosts present in
// desired but not actual must be mounted; present in actual but not desired
// must be unmounted.
func Diff(desired, actual map[string]bool) Plan {
	var p Plan
	for h, want := range desired {
		if !want {
			continue
		}
		if !actual[h] {
			p.Mounts = append(p.Mounts, h)
		}
	}
	for h, isMounted := range actual {
		if !isMounted {
			continue
		}
		if !desired[h] {
			p.Unmounts = append(p.Unmounts, h)
		}
	}
	return p
}
