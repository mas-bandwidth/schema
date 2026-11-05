//go:build !linux && !darwin

package fixedproof

// setAddressSpaceLimit has no kernel address-space limit on this platform, so
// it leaves the process untouched. The Go GC target from SetMemoryCeiling
// still applies.
func setAddressSpaceLimit(uint64) func() {
	return func() {}
}
