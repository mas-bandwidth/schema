//go:build linux || darwin

package fixedproof

import "syscall"

// addressSpaceLimit reads the current soft address-space limit. It reports
// false when the platform does not expose the limit.
func addressSpaceLimit() (uint64, bool) {
	var cur syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &cur); err != nil {
		return 0, false
	}
	return cur.Cur, true
}

// setAddressSpaceLimit lowers the process soft address-space limit to limit
// bytes. The kernel enforces the limit on this process and on every child it
// spawns, so the native C++ oracle probes inherit the same ceiling. It returns
// a function that restores the previous soft limit, or a no-op when the
// platform refuses the change.
func setAddressSpaceLimit(limit uint64) (restore func()) {
	var prev syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &prev); err != nil {
		return func() {}
	}
	next := prev
	next.Cur = limit
	if next.Max < next.Cur {
		next.Cur = next.Max
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_AS, &next); err != nil {
		return func() {}
	}
	return func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_AS, &prev)
	}
}
