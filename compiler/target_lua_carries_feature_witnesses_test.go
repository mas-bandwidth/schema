package compiler

import "testing"

// The Lua target registers the four constructs the featurewitnesses unit
// (testdata/conformance/feature-witnesses/full) needs beyond what it already
// carried: the timestamp kind (SPEC.md §4.13, docs/SPEC-TABLES.md §2.11), the
// integrity trailer (§2.14), the one-sided integer range (§4, SPEC §4.6) and
// ?map[K]V (§2.8). The runtime under internal/codegen/luatable already
// carries each; the conformance leg's featurewitnesses rows hold it there.
func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryTimestamp, carryIntegrity, carryOneSidedRange, carryOptionalMap)
}

// TestTargetLuaCarriesTheFeatureWitnessConstructs generates one probe per
// construct for target lua and requires files and no error.
func TestTargetLuaCarriesTheFeatureWitnessConstructs(t *testing.T) {
	t.Parallel()
	probes := []struct {
		construct string
		source    string
	}{
		{carryTimestamp, "package tsprobe\ntable Event\n{\n    at timestamp\n}\n"},
		{carryIntegrity, "package integrityprobe\ntable R | integrity=\"crc32c\" { value int32 }\nfixed table F | integrity=\"crc32c\" { value int32 }\n"},
		{carryOneSidedRange, "package rangeprobe\ntable R\n{\n    lower int32 = 5 | min=5\n}\n"},
		{carryOptionalMap, "package mapprobe\ntable R\n{\n    tags ?map[string(8)]int32\n}\n"},
	}
	for _, p := range probes {
		if !carries("lua", p.construct) {
			t.Errorf("lua is not registered as carrying %s", p.construct)
		}
		files, err := New().Generate(unitFromSource(t, p.source), "lua", Options{})
		if err != nil || len(files) == 0 {
			t.Errorf("lua must carry %s: files=%d err=%v", p.construct, len(files), err)
		}
	}
}
