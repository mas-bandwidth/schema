//go:build !linux && !darwin

package fixedproof

// addressSpaceLimit reports that this platform does not expose a kernel
// address-space limit.
func addressSpaceLimit() (uint64, bool) {
	return 0, false
}

// addressSpaceCeilingInclude has no kernel address-space limit on this
// platform: there is nothing to include.
func addressSpaceCeilingInclude() string {
	return ""
}

// addressSpaceCeilingApply has no kernel address-space limit on this platform:
// spawned probes run with the host default limits, and the Go GC target from
// SetMemoryCeiling still bounds the test process.
func addressSpaceCeilingApply() string {
	return ""
}
