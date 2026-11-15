package goosetable

// THE NON-CANONICAL SPELLING VECTORS ON THE GOOSE LEG (docs/SPEC-TABLES.md §3,
// §3.1, §3.3, §4; docs/rulings.json reads-refuse-non-canonical): every read of
// the variable form (form 1) and of the message form (form 2) refuses a file
// or batch that decodes to a good value but that no conforming writer of a
// lawful lineage version would produce.
//
// Each row of testdata/wire/non-canonical/MANIFEST.tsv is generated for its
// unit and run through every read entry Goose has for that form: plain load
// (load_table) and retaining load (load_table_retain) for variable form, batch
// load (<Root>LoadMessages) for message form, and both announce_read and
// announce_once for an announcement. Canonical rows read and re-save to the
// exact input bytes; every other row is refused. Goose has no LoadMeasure for
// form 1 (widening_test.go). The verdict is the contract
// (docs/rulings.json refusal-verdict-only), so no assertion compares a reason.

import (
	"bufio"
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
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type gooseNonCanonicalRow struct {
	file, form, spelling, verdict string
	schema, announce, root        string
}

func readGooseNonCanonicalManifest(t *testing.T) []gooseNonCanonicalRow {
	t.Helper()
	manifestPath := filepath.Join(fixedRoot, "testdata", "wire", "non-canonical", "MANIFEST.tsv")
	f, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []gooseNonCanonicalRow
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) != 4 {
			t.Fatalf("%s:%d: %d fields, want file, form, case, verdict", manifestPath, lineNo, len(cells))
		}
		if cells[0] == "file" && cells[1] == "form" && cells[2] == "case" && cells[3] == "verdict" {
			continue
		}
		row := gooseNonCanonicalRow{
			file:     cells[0],
			form:     cells[1],
			spelling: cells[2],
			verdict:  cells[3],
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s:%d: unknown verdict %q", manifestPath, lineNo, row.verdict)
		}
		if seen[row.file] {
			t.Fatalf("%s:%d: duplicate row %s", manifestPath, lineNo, row.file)
		}
		seen[row.file] = true
		if strings.HasPrefix(row.file, "../message-one-encoding/") {
			row.schema = "test/message-one-encoding/One.schema"
			row.announce = "testdata/wire/message-one-encoding/announcement.bin"
			row.root = "One"
		} else {
			row.schema = "test/non-canonical/NonCanonical.schema"
			row.announce = "testdata/wire/non-canonical/announcement.bin"
			switch row.form {
			case "variable":
				row.root = "Root"
			case "message":
				row.root = "Message"
			case "announcement":
			default:
				t.Fatalf("%s:%d: unknown form %q", manifestPath, lineNo, row.form)
			}
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vector rows", manifestPath)
	}
	return rows
}

func TestGooseReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it reads every shared non-canonical vector)")

	rows := readGooseNonCanonicalManifest(t)
	reached := map[string]bool{}
	bySchema := map[string][]gooseNonCanonicalRow{}
	for _, row := range rows {
		bySchema[row.schema] = append(bySchema[row.schema], row)
	}

	vectorDir := filepath.Join(fixedRoot, "testdata", "wire", "non-canonical")
	for _, schema := range slices.Sorted(maps.Keys(bySchema)) {
		unitRows := bySchema[schema]
		u, err := loadSchemaPaths([]string{filepath.Join(fixedRoot, filepath.FromSlash(schema))})
		if err != nil {
			t.Fatalf("load %s: %v", schema, err)
		}
		files, err := goose.Generate(u)
		if err != nil {
			t.Fatalf("%s: goose: %v", schema, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: goosetable: %v", schema, err)
		}
		maps.Copy(files, tables)

		var mods []string
		for name := range files {
			if before, ok := strings.CutSuffix(name, ".goose"); ok {
				mods = append(mods, before)
			}
		}
		slices.Sort(mods)
		ns := goose.Namespace(u)

		msgRoot := "Message"
		if schema == "test/message-one-encoding/One.schema" {
			msgRoot = "One"
		}

		var b strings.Builder
		b.WriteString("import os;\n")
		for _, m := range mods {
			fmt.Fprintf(&b, "import .%s;\n", m)
		}
		b.WriteString("\n" + gooserun.HexProbe + "\n")
		repl := strings.NewReplacer("NS", ns, "MSGROOT", msgRoot)
		b.WriteString(repl.Replace(`
fn probe_plain(name: const u8[:]) {
    var wire: u8[>..] = [];
    if !read_file(name, wire) { print("io-error"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(NS::TableUnit, "Root", wire, vals, nodes, r);
    if at < 0 || r.damaged || r.refused {
        print("refused");
        return;
    }
    var out: u8[>..<] = [];
    let sref = schema_rt::save_table(NS::TableUnit, "Root", wire, vals, nodes, at, out);
    if sref != 0 {
        print("save-refused");
        return;
    }
    hex(out);
}

fn probe_retain(name: const u8[:]) {
    var wire: u8[>..] = [];
    if !read_file(name, wire) { print("io-error"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: 65536, id_capacity: 256, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let at = schema_rt::load_table_retain(NS::TableUnit, "Root", wire, vals, nodes, r, store);
    if at < 0 || r.damaged || r.refused {
        print("refused");
        return;
    }
    var out: u8[>..<] = [];
    let sref = schema_rt::save_table_retain(NS::TableUnit, "Root", wire, vals, nodes, at, store, r, out);
    if sref != 0 {
        print("save-refused");
        return;
    }
    hex(out);
}

fn probe_message(ann_name: const u8[:], name: const u8[:]) {
    var ann: u8[>..] = [];
    var wire: u8[>..] = [];
    if !read_file(ann_name, ann) || !read_file(name, wire) { print("io-error"); return; }
    var voc = schema_rt::MsgVocabulary { .. };
    var arep = schema_rt::MsgReport { .. };
    if !schema_rt::announce_read(voc, ann, arep) || arep.damaged || arep.reason != 0 {
        print("announce-refused");
        return;
    }
    var values: NS::MSGROOT[1] = [NS::NewMSGROOT()];
    var r = schema_rt::MsgReport { .. };
    let count = NS::MSGROOTLoadMessages(values, voc, wire, r);
    if count <= 0 || r.damaged || r.reason != 0 {
        print("refused");
        return;
    }
    var mrep = schema_rt::MsgReport { .. };
    let need = NS::MSGROOTMeasureMessages(values[0..count], mrep);
    if need < 0 || mrep.reason != 0 {
        print("measure-refused");
        return;
    }
    var out: u8[>..] = [];
    var srep = schema_rt::MsgReport { .. };
    let wrote = NS::MSGROOTSaveMessages(values[0..count], out, srep);
    if wrote != need || srep.reason != 0 {
        print("save-refused");
        return;
    }
    hex(out);
}

fn probe_announce(name: const u8[:]) {
    var wire: u8[>..] = [];
    if !read_file(name, wire) { print("io-error"); return; }
    var voc1 = schema_rt::MsgVocabulary { .. };
    var r1 = schema_rt::MsgReport { .. };
    let ok1 = schema_rt::announce_read(voc1, wire, r1) && !r1.damaged && r1.reason == 0;
    var voc2 = schema_rt::MsgVocabulary { .. };
    var r2 = schema_rt::MsgReport { .. };
    let ok2 = schema_rt::announce_once(voc2, wire, r2) && !r2.damaged && r2.reason == 0;
    if ok1 && ok2 {
        print("read");
    } else if !ok1 && !ok2 {
        print("refused");
    } else {
        print("mismatch");
    }
}

fn main() {
`))

		inputs := map[string][]byte{}
		annInput := "announce.bin"
		annData, err := os.ReadFile(filepath.Join(fixedRoot, filepath.FromSlash(unitRows[0].announce)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[annInput] = annData

		type checkExpect struct {
			file, entry, verdict, wantHex string
		}
		var checks []checkExpect
		for i, row := range unitRows {
			inName := fmt.Sprintf("in%d.bin", i)
			raw, err := os.ReadFile(filepath.Join(vectorDir, filepath.FromSlash(row.file)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[inName] = raw
			wantHex := strings.ToLower(hex.EncodeToString(raw))
			reached[row.file] = true
			switch row.form {
			case "variable":
				fmt.Fprintf(&b, "    probe_plain(%q);\n", inName)
				checks = append(checks, checkExpect{file: row.file, entry: "load_table", verdict: row.verdict, wantHex: wantHex})
				fmt.Fprintf(&b, "    probe_retain(%q);\n", inName)
				checks = append(checks, checkExpect{file: row.file, entry: "load_table_retain", verdict: row.verdict, wantHex: wantHex})
			case "message":
				fmt.Fprintf(&b, "    probe_message(%q, %q);\n", annInput, inName)
				checks = append(checks, checkExpect{file: row.file, entry: row.root + "LoadMessages", verdict: row.verdict, wantHex: wantHex})
			case "announcement":
				fmt.Fprintf(&b, "    probe_announce(%q);\n", inName)
				checks = append(checks, checkExpect{file: row.file, entry: "announce_read", verdict: row.verdict})
			}
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
			t.Fatalf("%s: probe printed %d lines for %d checks:\n%s", schema, len(lines), len(checks), strings.Join(lines, "\n"))
		}
		for i, c := range checks {
			got := strings.TrimSpace(lines[i])
			switch c.verdict {
			case "read":
				if c.entry == "announce_read" {
					if got != "read" {
						t.Errorf("%s (%s): got %q, want read", c.file, c.entry, got)
					}
				} else if got != c.wantHex {
					t.Errorf("%s (%s): got %q, want canonical bytes %q", c.file, c.entry, got, c.wantHex)
				}
			case "refused":
				if got != "refused" {
					t.Errorf("%s (%s): got %q, want refused", c.file, c.entry, got)
				}
			}
		}
	}

	for _, row := range rows {
		if !reached[row.file] {
			t.Errorf("%s: manifest row was not reached", row.file)
		}
	}

	t.Run("shapes", testGooseNonCanonicalShapes)
}

const gooseNonCanonicalShapesSchema = `package shapes

enum Mode {
    alpha
    beta
    retired | deprecated
}

enum Axis {
    kx
    ky
}

fixed table Sub {
    x int32
}

union Pick {
    n int32
    s Sub
}

table Node {
    id int32
}

table VarRoot {
    a   *Node
    b   *Node
    txt string(8) = "hi"
    raw bytes(4)
    m1  Mode
    m2  Mode = beta
    sub Sub
    cnt [..2]int32
    lst []int32
    mp  map[int32]int32
    kd  [Axis]int32
    fxi [2]int32
    fxe [2]Mode
    fxu [2]Pick
    fxs [2]Sub
    opt ?int32
    w   int32
}

fixed table MsgRoot {
    txt string(8)
    raw bytes(4)
    m1  Mode
    m2  Mode = beta
    sub Sub
    cnt [..2]int32
    kd  [Axis]int32
    fxi [2]int32
    fxe [2]Mode
    fxu [2]Pick
    fxs [2]Sub
    opt ?int32
    w   int32
}
`

const gooseNonCanonicalShapesPeerSchema = `package shapes

fixed table MsgRoot {
    w int8
}
`

func testGooseNonCanonicalShapes(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	schFile := filepath.Join(tmp, "Shapes.schema")
	if err := os.WriteFile(schFile, []byte(gooseNonCanonicalShapesSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{schFile})
	if err != nil {
		t.Fatalf("load Shapes.schema: %v", err)
	}
	peerDir := filepath.Join(tmp, "peer")
	if err := os.MkdirAll(peerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	peerFile := filepath.Join(peerDir, "Shapes.schema")
	if err := os.WriteFile(peerFile, []byte(gooseNonCanonicalShapesPeerSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	uPeer, err := loadSchemaPaths([]string{peerFile})
	if err != nil {
		t.Fatalf("load peer Shapes.schema: %v", err)
	}

	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goosetable: %v", err)
	}
	maps.Copy(files, tables)

	leb := func(v uint64) []byte {
		var out [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(out[:], v)
		return out[:n]
	}
	i32 := func(v int32) []byte {
		var out [4]byte
		binary.LittleEndian.PutUint32(out[:], uint32(v))
		return out[:]
	}
	join := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	framed := func(p []byte) []byte { return join(leb(uint64(len(p))), p) }

	buildVar := func(fn func(ref func(string) []byte, refID func(uint64) []byte) (body []byte, nodes []byte)) []byte {
		var ids []uint64
		idx := map[uint64]uint64{}
		refID := func(id uint64) []byte {
			r, ok := idx[id]
			if !ok {
				r = uint64(len(ids) + 1)
				idx[id] = r
				ids = append(ids, id)
			}
			return leb(r)
		}
		ref := func(name string) []byte { return refID(ir.TableWireId(name)) }
		body, nodes := fn(ref, refID)
		wire := append([]byte{1}, body...)
		if nodes != nil {
			wire = append(wire, refID(ir.TableNodeWireId)...)
			wire = append(wire, byte(ir.TableKindString))
			wire = append(wire, framed(nodes)...)
		}
		wire = append(wire, 0)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}

	liveNode := func(ref func(string) []byte) []byte {
		typeRef := ref("Node")
		nodeBody := join(ref("id"), []byte{byte(ir.TableKindI32)}, i32(7), []byte{0})
		return join([]byte{1}, typeRef, framed(nodeBody))
	}

	type shapeCase struct {
		name, form, verdict string
		wire                []byte
	}
	var cases []shapeCase
	addVar := func(name, verdict string, wire []byte) {
		cases = append(cases, shapeCase{name: name, form: "var", verdict: verdict, wire: wire})
	}

	// Form 1 non-canonical spellings isolated per field shape, plus §4 positive controls.
	addVar("var-live-node-canonical", "read", buildVar(func(ref func(string) []byte, refID func(uint64) []byte) ([]byte, []byte) {
		body := join(ref("a"), []byte{byte(ir.TableKindPointer), 2})
		refID(ir.TableNodeWireId)
		return body, liveNode(ref)
	}))
	addVar("var-explicit-null-with-live-node", "refused", buildVar(func(ref func(string) []byte, refID func(uint64) []byte) ([]byte, []byte) {
		body := join(ref("a"), []byte{byte(ir.TableKindPointer), 2}, ref("b"), []byte{byte(ir.TableKindPointer), 0})
		refID(ir.TableNodeWireId)
		return body, liveNode(ref)
	}))
	addVar("var-empty-node-table-no-nodes", "refused", buildVar(func(_ func(string) []byte, refID func(uint64) []byte) ([]byte, []byte) {
		refID(ir.TableNodeWireId)
		return nil, []byte{0}
	}))
	addVar("var-string-at-default", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("txt"), []byte{byte(ir.TableKindString)}, framed([]byte("hi"))), nil
	}))
	addVar("var-bytes-at-default", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("raw"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindU8), 0})), nil
	}))
	addVar("var-enum-none-at-default", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("m1"), []byte{byte(ir.TableKindEnum), 0}), nil
	}))
	addVar("var-enum-named-at-default", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("m2"), []byte{byte(ir.TableKindEnum)}, ref("beta")), nil
	}))
	addVar("var-deprecated-enum-variant", "read", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("m2"), []byte{byte(ir.TableKindEnum)}, ref("retired")), nil
	}))
	addVar("var-empty-nested-body", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("sub"), []byte{byte(ir.TableKindTable), 1, 0}), nil
	}))
	addVar("var-unknown-field-on-nested", "read", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		fut := join(ref("sub"), []byte{byte(ir.TableKindTable)})
		inner := join(ref("future_field"), []byte{byte(ir.TableKindI32)}, i32(9), []byte{0})
		return join(fut, framed(inner)), nil
	}))
	addVar("var-empty-counted-array", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("cnt"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindI32), 0})), nil
	}))
	addVar("var-empty-list", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("lst"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindI32), 0})), nil
	}))
	addVar("var-empty-map", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("mp"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindTable), 0})), nil
	}))
	addVar("var-empty-keyed", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("kd"), []byte{byte(ir.TableKindKeyed)}, framed([]byte{byte(ir.TableKindI32), 0})), nil
	}))
	addVar("var-keyed-out-of-order", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		hdr := join(ref("kd"), []byte{byte(ir.TableKindKeyed)})
		pay := join([]byte{byte(ir.TableKindI32), 2}, ref("ky"), framed(i32(1)), ref("kx"), framed(i32(2)))
		return join(hdr, framed(pay)), nil
	}))
	addVar("var-keyed-slot-at-default", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		hdr := join(ref("kd"), []byte{byte(ir.TableKindKeyed)})
		pay := join([]byte{byte(ir.TableKindI32), 1}, ref("kx"), framed(i32(0)))
		return join(hdr, framed(pay)), nil
	}))
	addVar("var-all-default-fixed-scalars", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("fxi"), []byte{byte(ir.TableKindArray)}, framed(join([]byte{byte(ir.TableKindI32), 2}, i32(0), i32(0)))), nil
	}))
	addVar("var-all-default-fixed-enums", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("fxe"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindEnum), 2, 0, 0})), nil
	}))
	addVar("var-all-default-fixed-unions", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("fxu"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindUnion), 2, 0, 0})), nil
	}))
	addVar("var-fixed-union-with-none-elem", "read", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		hdr := join(ref("fxu"), []byte{byte(ir.TableKindArray)})
		e0 := join(ref("n"), []byte{byte(ir.TableKindI32), 4}, i32(3))
		return join(hdr, framed(join([]byte{byte(ir.TableKindUnion), 2}, e0, []byte{0}))), nil
	}))
	addVar("var-fixed-table-array-all-default", "read", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("fxs"), []byte{byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindTable), 2, 1, 0, 1, 0})), nil
	}))
	addVar("var-optional-at-default", "read", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("opt"), []byte{byte(ir.TableKindI32)}, i32(0)), nil
	}))
	addVar("var-widened-non-default", "read", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("w"), []byte{byte(ir.TableKindI8), 5}), nil
	}))
	addVar("var-widened-at-default", "refused", buildVar(func(ref func(string) []byte, _ func(uint64) []byte) ([]byte, []byte) {
		return join(ref("w"), []byte{byte(ir.TableKindI8), 0}), nil
	}))

	// Form 2 (message form) non-canonical spellings and §4 positive controls.
	ann := tablewire.Announce(u)
	vocab := tablewire.MessageVocabulary(u)
	refBits := ir.TableMessageBitsRequired(0, int64(len(vocab)))
	slotField := func(unit *ir.Unit, voc []ir.TableVocabularyEntry, stName, fName string) (uint64, ir.TableVocabularyEntry) {
		st := unit.Tables[stName]
		for _, f := range st.Fields {
			if f.Name == fName {
				want := ir.TableFieldEntry(f)
				for i, e := range voc {
					if e.Id == want.Id && e.Kind == want.Kind && e.Shape.Max == want.Shape.Max && e.Shape.Min == want.Shape.Min && e.Shape.Elem == want.Shape.Elem {
						return uint64(i + 1), e
					}
				}
			}
		}
		t.Fatalf("missing message field slot %s.%s", stName, fName)
		return 0, ir.TableVocabularyEntry{}
	}
	slotName := func(wireName string) uint64 {
		id := ir.TableWireId(wireName)
		for i, e := range vocab {
			if e.Kind == 0 && e.Id == id {
				return uint64(i + 1)
			}
		}
		t.Fatalf("missing message name slot %s", wireName)
		return 0
	}
	slotArm := func(unName, armName string) uint64 {
		uDecl := u.TableUnions[unName]
		if uDecl == nil {
			uDecl = u.Unions[unName]
		}
		if uDecl != nil {
			for _, v := range uDecl.Variants {
				if v.Name == armName {
					want := ir.TableArmEntry(v)
					for i, e := range vocab {
						if e.Id == want.Id && e.Kind == want.Kind {
							return uint64(i + 1)
						}
					}
				}
			}
		}
		t.Fatalf("missing message arm slot %s.%s", unName, armName)
		return 0
	}
	packMsgBits := func(rb int, fn func(put func(v uint64, n int), align func())) []byte {
		var out []byte
		var cur uint64
		var used int
		put := func(v uint64, n int) {
			left := n
			x := v
			for left > 0 {
				space := 8 - used
				take := min(left, space)
				mask := (uint64(1) << uint(take)) - 1
				if take == 64 {
					mask = ^uint64(0)
				}
				cur |= (x & mask) << uint(used)
				used += take
				left -= take
				x >>= uint(take)
				if used == 8 {
					out = append(out, byte(cur))
					cur, used = 0, 0
				}
			}
		}
		align := func() {
			if used > 0 {
				put(0, 8-used)
			}
		}
		put(2, 8)
		put(0, 8)
		fn(put, align)
		put(0, rb)
		align()
		return out
	}
	packMsg := func(fn func(put func(v uint64, n int), align func())) []byte {
		return packMsgBits(refBits, fn)
	}
	addMsg := func(name, verdict string, wire []byte) {
		cases = append(cases, shapeCase{name: name, form: "msg", verdict: verdict, wire: wire})
	}

	txtSlot, txtEnt := slotField(u, vocab, "MsgRoot", "txt")
	rawSlot, rawEnt := slotField(u, vocab, "MsgRoot", "raw")
	m1Slot, _ := slotField(u, vocab, "MsgRoot", "m1")
	m2Slot, _ := slotField(u, vocab, "MsgRoot", "m2")
	subSlot, _ := slotField(u, vocab, "MsgRoot", "sub")
	cntSlot, cntEnt := slotField(u, vocab, "MsgRoot", "cnt")
	kdSlot, kdEnt := slotField(u, vocab, "MsgRoot", "kd")
	fxiSlot, _ := slotField(u, vocab, "MsgRoot", "fxi")
	fxeSlot, _ := slotField(u, vocab, "MsgRoot", "fxe")
	fxuSlot, _ := slotField(u, vocab, "MsgRoot", "fxu")
	fxsSlot, _ := slotField(u, vocab, "MsgRoot", "fxs")
	optSlot, _ := slotField(u, vocab, "MsgRoot", "opt")
	wSlot, _ := slotField(u, vocab, "MsgRoot", "w")
	_ = slotName("alpha")
	betaSlot := slotName("beta")
	kxSlot := slotName("kx")
	kySlot := slotName("ky")
	retiredSlot := slotName("retired")
	pickNSlot := slotArm("Pick", "n")

	addMsg("msg-string-at-default", "refused", packMsg(func(put func(uint64, int), align func()) {
		put(txtSlot, refBits)
		put(0, ir.TableMessageBitsRequired(0, txtEnt.Shape.Max))
		align()
	}))
	addMsg("msg-bytes-at-default", "refused", packMsg(func(put func(uint64, int), align func()) {
		put(rawSlot, refBits)
		put(0, ir.TableMessageCountBits(rawEnt.Shape))
		align()
	}))
	addMsg("msg-enum-none-at-default", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(m1Slot, refBits)
		put(0, refBits)
	}))
	addMsg("msg-enum-named-at-default", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(m2Slot, refBits)
		put(betaSlot, refBits)
	}))
	addMsg("msg-deprecated-enum-variant", "read", packMsg(func(put func(uint64, int), _ func()) {
		put(m2Slot, refBits)
		put(retiredSlot, refBits)
	}))
	addMsg("msg-empty-nested-body", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(subSlot, refBits)
		put(0, refBits)
	}))
	addMsg("msg-unknown-field-on-nested", "read", packMsg(func(put func(uint64, int), _ func()) {
		put(subSlot, refBits)
		put(wSlot, refBits)
		put(9, 32)
		put(0, refBits)
	}))
	addMsg("msg-empty-counted-array", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(cntSlot, refBits)
		put(0, ir.TableMessageCountBits(cntEnt.Shape))
	}))
	addMsg("msg-empty-keyed", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(kdSlot, refBits)
		put(0, ir.TableMessageBitsRequired(0, kdEnt.Shape.Max))
	}))
	addMsg("msg-keyed-out-of-order", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(kdSlot, refBits)
		put(2, ir.TableMessageBitsRequired(0, kdEnt.Shape.Max))
		put(kySlot, refBits)
		put(1, 32)
		put(kxSlot, refBits)
		put(2, 32)
	}))
	addMsg("msg-keyed-slot-at-default", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(kdSlot, refBits)
		put(1, ir.TableMessageBitsRequired(0, kdEnt.Shape.Max))
		put(kxSlot, refBits)
		put(0, 32)
	}))
	addMsg("msg-all-default-fixed-scalars", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(fxiSlot, refBits)
		put(0, 32)
		put(0, 32)
	}))
	addMsg("msg-all-default-fixed-enums", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(fxeSlot, refBits)
		put(0, refBits)
		put(0, refBits)
	}))
	addMsg("msg-all-default-fixed-unions", "refused", packMsg(func(put func(uint64, int), _ func()) {
		put(fxuSlot, refBits)
		put(0, refBits)
		put(0, refBits)
	}))
	addMsg("msg-fixed-union-with-none-elem", "read", packMsg(func(put func(uint64, int), _ func()) {
		put(fxuSlot, refBits)
		put(pickNSlot, refBits)
		put(3, 32)
		put(0, refBits)
	}))
	addMsg("msg-fixed-struct-array-all-default", "read", packMsg(func(put func(uint64, int), _ func()) {
		put(fxsSlot, refBits)
		put(0, refBits)
		put(0, refBits)
	}))
	addMsg("msg-optional-at-default", "read", packMsg(func(put func(uint64, int), _ func()) {
		put(optSlot, refBits)
		put(0, 32)
	}))

	// Widened scalar in message form: peer announces `w` as int8 while reader declares int32.
	widenAnn := tablewire.Announce(uPeer)
	peerVocab := tablewire.MessageVocabulary(uPeer)
	peerRefBits := ir.TableMessageBitsRequired(0, int64(len(peerVocab)))
	peerWSlot, _ := slotField(uPeer, peerVocab, "MsgRoot", "w")
	cases = append(cases,
		shapeCase{name: "msg-widened-non-default", form: "msg-widen", verdict: "read", wire: packMsgBits(peerRefBits, func(put func(uint64, int), _ func()) {
			put(peerWSlot, peerRefBits)
			put(5, 8)
		})},
		shapeCase{name: "msg-widened-at-default", form: "msg-widen", verdict: "refused", wire: packMsgBits(peerRefBits, func(put func(uint64, int), _ func()) {
			put(peerWSlot, peerRefBits)
			put(0, 8)
		})},
	)

	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	slices.Sort(mods)
	ns := goose.Namespace(u)

	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	rp := strings.NewReplacer("NS", ns)
	b.WriteString(rp.Replace(`
fn check_var(name: const u8[:]) {
    var wire: u8[>..] = [];
    if !read_file(name, wire) { print("io-error"); return; }
    var vals1: schema_rt::LoadVal[>..] = [];
    var nodes1: schema_rt::LoadNode[>..] = [];
    var r1 = schema_rt::LoadReport { .. };
    let ok1 = schema_rt::load_table(NS::TableUnit, "VarRoot", wire, vals1, nodes1, r1) >= 0 && !r1.damaged && !r1.refused;

    var vals2: schema_rt::LoadVal[>..] = [];
    var nodes2: schema_rt::LoadNode[>..] = [];
    var r2 = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: 65536, id_capacity: 256, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let ok2 = schema_rt::load_table_retain(NS::TableUnit, "VarRoot", wire, vals2, nodes2, r2, store) >= 0 && !r2.damaged && !r2.refused;

    if ok1 && ok2 { print("read"); }
    else if !ok1 && !ok2 { print("refused"); }
    else { print("mismatch"); }
}

fn check_msg(ann_name: const u8[:], name: const u8[:]) {
    var ann: u8[>..] = [];
    var wire: u8[>..] = [];
    if !read_file(ann_name, ann) || !read_file(name, wire) { print("io-error"); return; }
    var voc = schema_rt::MsgVocabulary { .. };
    var arep = schema_rt::MsgReport { .. };
    if !schema_rt::announce_read(voc, ann, arep) || arep.damaged || arep.reason != 0 {
        print("announce-refused");
        return;
    }
    var values: NS::MsgRoot[1] = [NS::NewMsgRoot()];
    var r = schema_rt::MsgReport { .. };
    let count = NS::MsgRootLoadMessages(values, voc, wire, r);
    if count <= 0 || r.damaged || r.reason != 0 {
        print("refused");
        return;
    }
    print("read");
}

fn main() {
`))

	inputs := map[string][]byte{
		"ann.bin":       ann,
		"ann_widen.bin": widenAnn,
	}
	for i, c := range cases {
		inName := fmt.Sprintf("shape%d.bin", i)
		inputs[inName] = c.wire
		switch c.form {
		case "var":
			fmt.Fprintf(&b, "    check_var(%q);\n", inName)
		case "msg":
			fmt.Fprintf(&b, "    check_msg(\"ann.bin\", %q);\n", inName)
		case "msg-widen":
			fmt.Fprintf(&b, "    check_msg(\"ann_widen.bin\", %q);\n", inName)
		}
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
	if len(lines) != len(cases) {
		t.Fatalf("shapes probe printed %d lines for %d cases:\n%s", len(lines), len(cases), strings.Join(lines, "\n"))
	}
	for i, c := range cases {
		got := strings.TrimSpace(lines[i])
		if got != c.verdict {
			t.Errorf("%s: got %q, want %s", c.name, got, c.verdict)
		}
	}
}
