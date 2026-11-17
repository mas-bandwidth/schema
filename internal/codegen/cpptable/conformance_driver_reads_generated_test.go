package cpptable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCppConformanceDriverReadsThroughGeneratedReader holds the C++ conformance
// driver's SOURCE (test/conformance/cpp/main.cpp) to the one-read contract
// (docs/SPEC-TABLES.md §6.5): a form-1 wire's verdict is the generated load's
// verdict and nothing else. The driver must not carry its own strict walk —
// neither framing_strict_ok nor any of the walk's helpers — and read_answer's
// verdict must come from the load it calls and that load's report, never from a
// walk the driver runs before or instead of the generated read.
//
// It is a source test: it reads no wire, builds nothing and needs no compiler.
func TestCppConformanceDriverReadsThroughGeneratedReader(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "cpp", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	// the strict walk is the generated reader's, so none of its spellings may
	// remain in the driver: not the entry, not the body walk, not its helpers
	for _, name := range []string{
		"framing_strict_ok",
		"FramingWalk",
		"FramingRecord",
		"framing_scan_records",
		"framing_graph",
		"framing_body",
		"framing_payload",
		"framing_content",
		"framing_array",
		"framing_keyed",
		"framing_union",
		"framing_scalar",
		"framing_array_header",
		"framing_text_valid",
		"framing_leb",
		"framing_u64le",
		"framing_name_id",
		"framing_fixed_kind",
		"framing_kind_width",
	} {
		if bytes.Contains(src, []byte(name)) {
			t.Errorf("test/conformance/cpp/main.cpp still spells %q: the driver must read through the generated load, not its own strict walk", name)
		}
	}
	// the verdict gate the walk used to ride (read_answer's framing_ok) is gone:
	// read_answer calls the generated load and reports only what it answers
	if bytes.Contains(src, []byte("framing_ok")) {
		t.Errorf("test/conformance/cpp/main.cpp still gates the read on framing_ok: read_answer's verdict must come from the load's return and its report")
	}
	// read_answer calls the generated load in both shapes, and its verdict is
	// the load's return with its report's refusal flag
	for _, want := range []string{
		"variable->load( region, wire.data(), (int64_t) wire.size(), &report )",
		"ok = root != NULL && !report.refused",
		"( form1 ? codec->load_form1 : codec->load )( value, wire.data(), (int64_t) wire.size(), &report ) && !report.refused",
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("test/conformance/cpp/main.cpp does not spell %q: read_answer's verdict must be the generated load's", want)
		}
	}
}
