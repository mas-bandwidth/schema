// R26: "a known hash whose lineage entry would not build → layout_malformed /
// plan_too_large by that entry's own lane, never a throw"
//
// docs/FIXED-FORM-ALGORITHM.md §5.3 step 8 (line 890): when a file's hash matches
// a known lineage entry but that entry's compiled plan could not be built (the layout
// would not parse, or the compile failed), the generated FixedLoad refuses by the
// name the entry's own Why field carries — layout_malformed, plan_too_large, or
// layout_record_too_large — and never panics.
//
// THE PRODUCTION CALL SITE is in the generated FixedLoad (e.g. tblv2.CfgFixedLoad):
//
//	if hash != CfgFixedHash {
//	    lane := &CfgFixedLineagePlans[pick]
//	    if lane.Why != "" {
//	        return tableFixedRefuse(report, lane.Why)
//	    }
//	    entries, entryCount = lane.Entries, lane.Count
//	    ...
//	}
//
// §5.9 #36: the name the entry's own lane carries, never a throw.

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tblv2"
)

// TestRowR26 verifies that when a file matches a known lineage entry whose plan
// could not be built (lane.Why != ""), FixedLoad refuses by that name, sets
// report.Verdict = TableOpenRefused, Malformed = false, moves no counters,
// leaves destination untouched, and never throws.
func TestRowR26(t *testing.T) {
	// 1. STATIC ASSERTIONS on the generated code and runtime
	root := findRepoRoot(t)
	tableFile := filepath.Join(root, "build", "tables-generated-go", "v2", "V2Table.go")
	codeBytes, err := os.ReadFile(tableFile)
	if err != nil {
		t.Fatalf("failed to read V2Table.go: %v", err)
	}
	code := string(codeBytes)

	if !strings.Contains(code, "lane.Why != \"\"") {
		t.Fatal("generated FixedLoad must check lane.Why")
	}
	if !strings.Contains(code, "tableFixedRefuse(report, lane.Why)") {
		t.Fatal("generated FixedLoad must refuse with lane.Why")
	}

	whyIdx := strings.Index(code, "lane.Why !=")
	entriesIdx := strings.Index(code, "entries, entryCount = lane.Entries, lane.Count")
	if whyIdx < 0 || entriesIdx < 0 || whyIdx > entriesIdx {
		t.Fatal("lane.Why check must precede lane.Entries assignment")
	}

	if !strings.Contains(code, `out[i].Why = "layout_malformed"`) {
		t.Fatal("tableFixedLineagePlans must record layout_malformed on parse/compile failure")
	}
	if !strings.Contains(code, `out[i].Why = "plan_too_large"`) {
		t.Fatal("tableFixedLineagePlans must record plan_too_large when room exhausted")
	}

	// 2. EXECUTION ASSERTIONS: test the production lane.Why refusal path in CfgFixedLoad
	peerHash := uint64(0xDEADBEEFCAFEBABE)
	knownLayout := tblv2.CfgFixedLayout

	origKnown := make([]tblv2.TableFixedKnownLayout, len(tblv2.CfgFixedKnown))
	copy(origKnown, tblv2.CfgFixedKnown)
	defer func() {
		copy(tblv2.CfgFixedKnown, origKnown)
		tblv2.CfgFixedLineagePlans[0].Why = ""
	}()

	// Point index 0 to peerHash so tableFixedSelect finds it as a peer entry
	tblv2.CfgFixedKnown[0].Hash = peerHash

	// Construct a valid form-3 file carrying peerHash and matching layout
	file := make([]byte, tblv2.TableFixedHeaderBytes+4+int64(len(knownLayout))+tblv2.CfgFixedRecordBytes)
	file[0] = tblv2.TableFixedForm
	binary.LittleEndian.PutUint64(file[tblv2.TableFixedHashAt:], peerHash)
	binary.LittleEndian.PutUint32(file[tblv2.TableFixedHeaderBytes:], uint32(len(knownLayout)))
	copy(file[tblv2.TableFixedHeaderBytes+4:], knownLayout)
	// Record: hash matching file header + zero body
	recAt := tblv2.TableFixedHeaderBytes + 4 + len(knownLayout)
	binary.LittleEndian.PutUint64(file[recAt:], peerHash)

	for _, refusalName := range []string{"layout_malformed", "plan_too_large"} {
		tblv2.CfgFixedLineagePlans[0].Why = refusalName

		values := make([]tblv2.Cfg, 1)
		tblv2.CfgReset(&values[0])
		values[0].A = 999.0 // sentinel value
		var report tblv2.TableReport
		plan := make([]tblv2.TableFixedEntry, 256)

		var n int64
		var panicked any
		func() {
			defer func() { panicked = recover() }()
			n = tblv2.CfgFixedLoad(values, file, plan, &report)
		}()

		if panicked != nil {
			t.Fatalf("refusal %s must NEVER throw, panicked with: %v", refusalName, panicked)
		}
		if n != -1 {
			t.Fatalf("refusal %s must return -1, got: %d", refusalName, n)
		}
		if report.Verdict != tblv2.TableOpenRefused {
			t.Fatalf("refusal %s must set verdict TableOpenRefused, got: %v", refusalName, report.Verdict)
		}
		if report.Reason != refusalName {
			t.Fatalf("refusal %s: report.Reason = %q, want %q", refusalName, report.Reason, refusalName)
		}
		if report.Malformed {
			t.Fatalf("refusal %s must NOT set Malformed = true", refusalName)
		}
		if report.Unknown != 0 || report.KindMismatch != 0 || report.Widened != 0 || report.Clamped != 0 {
			t.Fatalf("refusal %s must move no counters: %+v", refusalName, report)
		}
		if values[0].A != 999.0 {
			t.Fatalf("refusal %s must write no destination bytes: sentinel modified to %f", refusalName, values[0].A)
		}
	}
}

// TestRowR26NegativeControl verifies the biting negative control: if lane.Why
// check is bypassed (empty Why), CfgFixedLoad does not refuse with layout_malformed.
func TestRowR26NegativeControl(t *testing.T) {
	peerHash := uint64(0xFEEDFACECAFEBEEF)
	knownLayout := tblv2.CfgFixedLayout

	origKnown := make([]tblv2.TableFixedKnownLayout, len(tblv2.CfgFixedKnown))
	copy(origKnown, tblv2.CfgFixedKnown)
	defer func() {
		copy(tblv2.CfgFixedKnown, origKnown)
		tblv2.CfgFixedLineagePlans[0].Why = ""
	}()

	tblv2.CfgFixedKnown[0].Hash = peerHash
	tblv2.CfgFixedLineagePlans[0].Why = "" // Empty Why: should NOT refuse for lineage failure

	file := make([]byte, tblv2.TableFixedHeaderBytes+4+int64(len(knownLayout))+tblv2.CfgFixedRecordBytes)
	file[0] = tblv2.TableFixedForm
	binary.LittleEndian.PutUint64(file[tblv2.TableFixedHashAt:], peerHash)
	binary.LittleEndian.PutUint32(file[tblv2.TableFixedHeaderBytes:], uint32(len(knownLayout)))
	copy(file[tblv2.TableFixedHeaderBytes+4:], knownLayout)
	recAt := tblv2.TableFixedHeaderBytes + 4 + len(knownLayout)
	binary.LittleEndian.PutUint64(file[recAt:], peerHash)

	values := make([]tblv2.Cfg, 1)
	tblv2.CfgReset(&values[0])
	var report tblv2.TableReport
	plan := make([]tblv2.TableFixedEntry, 256)

	_ = tblv2.CfgFixedLoad(values, file, plan, &report)
	if report.Reason == "layout_malformed" || report.Reason == "plan_too_large" {
		t.Fatalf("negative control: with empty Why, should not refuse as unbuildable lineage, got: %q", report.Reason)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "cmd", "schema")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
