package goosetable

// THE MESSAGE FORM'S CANONICAL-SPELLING GATE ON THE GOOSE LEG
// (docs/SPEC-TABLES.md §3.3(a), (b)): a field written at the default the
// writer elides, and fields out of the writer's order, refuse ONLY when the
// sender announced this reader's own build version. Under another build
// version the sender's default and order are not known, so the same bodies
// read as lawful version evolution. An explicit empty union arm (§3.3(c))
// refuses whatever the build. The file (variable) form is not gated, and
// TestGooseReadsRefuseNonCanonical holds it.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestGooseMessageCanonicalGateIsTheBuildVersion(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it reads message batches under two announced build versions)")

	u, err := loadSchemaPaths([]string{filepath.Join(fixedRoot, "test", "message-one-encoding", "One.schema")})
	if err != nil {
		t.Fatal(err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	vectors := filepath.Join(fixedRoot, "testdata", "wire", "message-one-encoding")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(vectors, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	same := read("announcement.bin")
	// The other build's announcement: the same vocabulary under another build
	// version, the one fact that differs between the two senders.
	var want [8]byte
	binary.LittleEndian.PutUint64(want[:], ir.BuildVersion(u))
	if n := bytes.Count(same, want[:]); n != 1 {
		t.Fatalf("announcement.bin holds the unit's build version %d times, want 1", n)
	}
	other := bytes.Clone(same)
	at := bytes.Index(other, want[:])
	other[at] ^= 0x01
	canonical := strings.ToLower(hex.EncodeToString(read("canonical.bin")))

	cases := []struct {
		file             string
		sameBuild, other string
	}{
		{"canonical.bin", canonical, canonical},
		{"explicit-default.bin", "refused", canonical},
		{"fields-reordered.bin", "refused", canonical},
		{"union-none.bin", "refused", "refused"},
	}

	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	slices.Sort(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString(strings.ReplaceAll(`
fn probe(ann_name: const u8[:], name: const u8[:]) {
    var ann: u8[>..] = [];
    var wire: u8[>..] = [];
    if !read_file(ann_name, ann) || !read_file(name, wire) { print("io-error"); return; }
    var voc = schema_rt::MsgVocabulary { .. };
    var arep = schema_rt::MsgReport { .. };
    if !schema_rt::announce_read(voc, ann, arep) || arep.damaged || arep.reason != 0 {
        print("announce-refused");
        return;
    }
    var values: NS::One[1] = [NS::NewOne()];
    var r = schema_rt::MsgReport { .. };
    let count = NS::OneLoadMessages(values, voc, wire, r);
    if count <= 0 || r.damaged || r.reason != 0 {
        print("refused");
        return;
    }
    var out: u8[>..] = [];
    var srep = schema_rt::MsgReport { .. };
    let wrote = NS::OneSaveMessages(values[0..count], out, srep);
    if wrote < 0 || srep.reason != 0 {
        print("save-refused");
        return;
    }
    hex(out);
}

fn main() {
`, "NS", goose.Namespace(u)))
	inputs := map[string][]byte{"same.bin": same, "other.bin": other}
	type check struct{ what, want string }
	var checks []check
	for i, c := range cases {
		in := fmt.Sprintf("in%d.bin", i)
		inputs[in] = read(c.file)
		fmt.Fprintf(&b, "    probe(\"same.bin\", %q);\n", in)
		checks = append(checks, check{c.file + " under the reader's build version", c.sameBuild})
		fmt.Fprintf(&b, "    probe(\"other.bin\", %q);\n", in)
		checks = append(checks, check{c.file + " under another build version", c.other})
	}
	b.WriteString("}\n")

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n"), "\n")
	if len(lines) != len(checks) {
		t.Fatalf("probe printed %d lines for %d checks:\n%s", len(lines), len(checks), strings.Join(lines, "\n"))
	}
	for i, c := range checks {
		if got := strings.TrimSpace(lines[i]); got != c.want {
			t.Errorf("%s: got %q, want %q", c.what, got, c.want)
		}
	}
}
