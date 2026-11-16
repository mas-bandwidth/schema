package goosetable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

func firstUseWire(body []byte, ids ...uint64) []byte {
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func gooseFirstUseAnswers(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, "graphdemo", c.units["graphdemo"], override)
	scalar := []byte{4, 7, 0, 0, 0}
	field := append(append([]byte{1}, scalar...), 0)
	cases := []struct {
		name string
		wire []byte
		read bool
	}{
		{"field", firstUseWire(field, 101), true},
		{"unused", firstUseWire(field, 101, 999), false},
		{"field_order", firstUseWire(append(append(append([]byte{2}, scalar...), append([]byte{1}, scalar...)...), 0), 101, 202), false},
		{"enum", firstUseWire([]byte{1, 30, 2, 0}, 101, 202), true},
		{"enum_order", firstUseWire([]byte{1, 30, 3, 0}, 101, 202, 303), false},
		{"enum_none", firstUseWire([]byte{1, 30, 0, 0}, 101), true},
		{"union", firstUseWire([]byte{1, 15, 2, 4, 4, 7, 0, 0, 0, 0}, 101, 202), true},
		{"union_order", firstUseWire([]byte{1, 15, 3, 4, 4, 7, 0, 0, 0, 0}, 101, 202, 303), false},
		{"union_none", firstUseWire([]byte{1, 15, 0, 0}, 101), true},
		{"keyed", firstUseWire([]byte{1, 16, 8, 4, 1, 2, 4, 7, 0, 0, 0, 0}, 101, 202), true},
		{"keyed_order", firstUseWire([]byte{1, 16, 8, 4, 1, 3, 4, 7, 0, 0, 0, 0}, 101, 202, 303), false},
		{"nested", firstUseWire([]byte{1, 13, 7, 2, 4, 7, 0, 0, 0, 0, 0}, 101, 202), true},
		{"node", firstUseWire([]byte{1, 17, 2, 2, 12, 10, 1, 3, 7, 4, 4, 7, 0, 0, 0, 0, 0}, 101, ^uint64(0), 303, 404), true},
		{"node_order", firstUseWire([]byte{1, 17, 2, 2, 12, 10, 1, 4, 7, 5, 4, 7, 0, 0, 0, 0, 0}, 101, ^uint64(0), 303, 404, 505), false},
		{"duplicate", firstUseWire(append(append(append([]byte{1}, scalar...), append([]byte{1}, scalar...)...), 0), 101), false},
	}
	canonical, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		wire []byte
		read bool
	}{"canonical_graph", canonical, true})
	pinned, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing/frame_unreferenced_entry.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		wire []byte
		read bool
	}{"shared_unused", pinned, false})
	var main strings.Builder
	main.WriteString(idBoundProbe)
	main.WriteString("fn main() {\n")
	for _, tc := range cases {
		files[tc.name+".bin"] = tc.wire
		fmt.Fprintf(&main, "probe(%q,\"Scene\");\n", tc.name)
	}
	main.WriteString("probe(\"field\",\"Scene\");\n}\n") // fresh per-file state after failures
	files["main.goose"] = []byte(main.String())
	out := string(gooserun.Run(t, t.TempDir(), "main.goose", files))
	want := map[string]bool{}
	for _, tc := range cases {
		want[tc.name] = tc.read
	}
	var wrong []string
	seen := 0
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 {
			t.Fatalf("unexpected probe line %q", line)
		}
		read, known := want[f[0]]
		if !known {
			t.Fatalf("unexpected probe %q", line)
		}
		got := f[2] == "1"
		if got != read {
			wrong = append(wrong, f[0]+" "+f[1])
		}
		if !got && (f[4] != "0" || f[5] != "wire_damaged_") {
			t.Errorf("refusal decoded values or changed diagnostic: %s", line)
		}
		seen++
	}
	if seen != (len(cases)+1)*2 {
		t.Fatalf("probe answers %d", seen)
	}
	return wrong
}

func TestGooseIdFirstUse(t *testing.T) {
	for _, name := range gooseFirstUseAnswers(t, nil) {
		t.Errorf("wrong read verdict: %s", name)
	}
}

func TestGooseUnusedIdNegativeControl(t *testing.T) {
	override := sabotage(t, "wire_load.goose", "    if w.next_ref - 1 != ids_n as! u64 { wc_damage(w); return w.fail, w.non_finite; }\n", "    // planted: unused trailer ids are accepted\n")
	got := gooseFirstUseAnswers(t, override)
	want := []string{"unused plain", "unused retain", "shared_unused plain", "shared_unused retain"}
	if !slices.Equal(got, want) {
		t.Fatalf("negative control got %v, want exactly %v", got, want)
	}
}
