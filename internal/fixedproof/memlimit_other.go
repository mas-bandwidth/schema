//go:build !linux && !darwin

package fixedproof

// addressSpaceLimit reports that this platform does not expose a kernel
// address-space limit.
func addressSpaceLimit() (uint64, bool) {
	return 0, false
}

// setAddressSpaceLimit has no kernel address-space limit on this platform, so
// it leaves the process untouched. The Go GC target from SetMemoryCeiling
// still applies.
func setAddressSpaceLimit(uint64) func() {
	return func() {}
}
