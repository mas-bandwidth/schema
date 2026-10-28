package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// unionKeyedVectors is every fixed corpus root that holds a union or an
// enum-keyed array (docs/SPEC-TABLES.md §2.4, §2.6, §3.4): the corpus file,
// the schema that wrote it, its Squirrel table module, and its root class.
var unionKeyedVectors = []fixedVector{
	{"keyed", "tables/examples/Keyed.schema", "TabledemoTables", "KeyedConfig", "keyed.bin"},
	{"fu1", "test/tables/FU1.schema", "Tblfu1Tables", "FuRoot", "fu1.bin"},
	{"fu2", "test/tables/FU2.schema", "Tblfu2Tables", "FuRoot", "fu2.bin"},
	{"union_append_old", "test/tables/VOLD_union_append.schema", "Vold_union_appendTables", "Lineage", "old_union_append.bin"},
	{"union_append_new", "test/tables/VNEW_union_append.schema", "Vnew_union_appendTables", "Lineage", "new_union_append.bin"},
	{"union_widen_old", "test/tables/VOLD_union_arm_payload_widen.schema", "Vold_union_arm_payload_widenTables", "Lineage", "old_union_arm_payload_widen.bin"},
	{"union_widen_new", "test/tables/VNEW_union_arm_payload_widen.schema", "Vnew_union_arm_payload_widenTables", "Lineage", "new_union_arm_payload_widen.bin"},
	{"keyed_enum_old", "test/tables/VOLD_keyed_array_enum_append.schema", "Vold_keyed_array_enum_appendTables", "Lineage", "old_keyed_array_enum_append.bin"},
	{"keyed_enum_new", "test/tables/VNEW_keyed_array_enum_append.schema", "Vnew_keyed_array_enum_appendTables", "Lineage", "new_keyed_array_enum_append.bin"},
}

// TestSquirrelFixedFormUnionsAndKeyedArrays carries the tag lane and the
// payload, and the keyed slots, in the emitted fixed codec: every fixed corpus
// root holding a union or a keyed array saves the byte oracle's bytes, and a
// load of them salvages the value (the write-read-write round trip proves the
// load reconstructed the value, since re-saving matches the oracle byte for
// byte).
func TestSquirrelFixedFormUnionsAndKeyedArrays(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedRecords(t, corpus)
	nonFinite := fixedNonFinite(t, corpus)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inputs := map[string][]byte{}
	var p strings.Builder
	p.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	p.WriteString("::hexlineN <- function(b, n) { local s = \"\"; for (local i = 0; i < n; i++) s += format(\"%02x\", b[i] & 0xff); print(s + \"\\n\"); };\n")
	for _, v := range unionKeyedVectors {
		n, ok := records[v.file]
		if !ok {
			t.Fatalf("the manifest carries no line for %s", v.file)
		}
		data, err := os.ReadFile(filepath.Join(corpus, v.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[v.key] = data
		u := fixedLoadUnit(t, root, v.schema)
		sub := filepath.Join(dir, v.key)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		writeGenerated(t, sub, u)
		fmt.Fprintf(&p, "{ local S = dofile(%q, true); local T = S.require(%q);\n", v.key+"/schema_module.nut", v.module)
		fmt.Fprintf(&p, "  local b = inputs[%q]; local back = []; for (local k = 0; k < %d; k++) back.append(T.%s());\n", v.key, n, v.root)
		fmt.Fprintf(&p, "  local got = T.%sFixedLoadTolerant(b, back, null);\n", v.root)
		fmt.Fprintf(&p, "  if (got != %d) { print(\"LOAD:%s:\" + got + \":\" + T.FixedReason + \"\\n\"); }\n", n, v.key)
		fmt.Fprintf(&p, "  else { local out = blob(b.len()); T.%sFixedSave(out, back); print(%q + \" \"); ::hexlineN(out, b.len()); }\n", v.root, v.key)
		p.WriteString("}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	got := map[string][]byte{}
	refused := map[string]string{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "LOAD:"); ok {
			key, why, _ := strings.Cut(rest, ":")
			refused[key] = why
			continue
		}
		key, text, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not a key and hex", line)
		}
		b, err := decodeHex(text)
		if err != nil {
			t.Fatalf("probe line %q: %v", line, err)
		}
		got[key] = b
	}
	fixed := 0
	for _, v := range unionKeyedVectors {
		// A FILE THE REFERENCE WROTE WITH A NaN OR AN INFINITY is a
		// MUST-REFUSE vector under the finite rule (docs/SPEC-TABLES.md §3.3):
		// the manifest decides, so the identity row comes back by itself the
		// day the corpus turns finite
		if field := nonFinite[v.file]; field != "" {
			if refused[v.key] != "-1:non_finite_float" {
				t.Errorf("%s: the reference wrote a non-finite %s; the load answered %q, want -1:non_finite_float", v.key, field, refused[v.key])
			}
			fixed++
			continue
		}
		if why, ok := refused[v.key]; ok {
			t.Errorf("the oracle file did not read clean: %s: %s", v.key, why)
		}
		want := inputs[v.key]
		if !strings.EqualFold(fmt.Sprintf("%x", got[v.key]), fmt.Sprintf("%x", want)) {
			diff := -1
			for i := 0; i < len(got[v.key]) && i < len(want); i++ {
				if got[v.key][i] != want[i] {
					diff = i
					break
				}
			}
			t.Errorf("%s: write-read-write differs (%d bytes against the oracle's %d), first difference at %d: got %x want %x", v.key, len(got[v.key]), len(want), diff, got[v.key][diff], want[diff])
		}
		fixed++
	}
	if fixed != len(unionKeyedVectors) {
		t.Fatalf("compared %d union/keyed roots, want %d", fixed, len(unionKeyedVectors))
	}
	t.Logf("compared %d fixed roots holding a union or a keyed array against the byte oracle", fixed)
}
