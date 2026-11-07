package rusttable

import (
	"strings"
	"testing"
)

// THE ROOT NODE SCAN REFUSES NOTHING (docs/SPEC-TABLES.md §3.1, §4; the fuzz
// vector root_scan_passes_damage_to_body holds the behaviour against the
// oracle). A variable root's gathering scan that meets a field it cannot pass
// stops with what it found, and the body read counts the damage, as the
// oracle's nodeTableBytes does. This is the toolchain-free witness that the
// runtime still spells it, with the REVERSED WITNESS putting the refusal back.
func TestRootNodeScanPassesDamageToTheBodyRust(t *testing.T) {
	t.Parallel()
	var runtime string
	for name, src := range generate(t, pointered) {
		if strings.HasSuffix(name, "variable_runtime.rs") {
			runtime = string(src)
		}
	}
	scan := rustFn(runtime, "scan")
	if scan == "" {
		t.Fatal("the generated runtime has no TableNodeReader::scan")
	}
	stops := []string{
		"if off >= end { break; }",
		"let Some(id) = ref_id(ids, r) else { break; };",
		"if !leb_skip(wire, &mut off, end, kind) { break; }",
	}
	holds := func(text string) bool {
		for _, s := range stops {
			if !strings.Contains(text, s) {
				return false
			}
		}
		return true
	}
	if !holds(scan) {
		t.Errorf("the root node scan refuses the file on damage it should pass to the body")
	}
	reverted := strings.Replace(scan, stops[1], "let Some(id) = ref_id(ids, r) else { report.malformed = true; return None; };", 1)
	if holds(reverted) {
		t.Errorf("the reversed witness still passes, so the check cannot go red")
	}
}
