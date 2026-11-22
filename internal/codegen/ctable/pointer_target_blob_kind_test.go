// The BLOB-KIND vectors (docs/SPEC-TABLES.md §3.1, §6.5; docs/rulings.json
// blob-target-outside-closure-refused): the two reserved blob ids are types
// every reader of the variable form can name, whatever its root reaches. A
// blob record named from a pointer slot this reader DECLARES whose target is
// not that blob kind refuses the whole read, whether or not the root's closure
// has an edge of that kind; a blob record of a kind the root's closure lacks,
// reached only from fields this reader does not declare, is skipped and
// counted unknown; a record no pointer reaches is damage.
//
// TestCPointerTargetBlobKindRefused holds every row of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv through every C read
// of the variable form (the strict region read, the retaining read, the
// builder read and the measure that answers a verdict), and holds the leg's
// own writer to never emitting a blob record no pointer reaches: each saved
// wire is decoded by the engine and must read back accepted with nothing
// unknown. The negative control plants the old root-closure rule in a copy of
// the generated reader and must go red by name on text-names-bytes.
package ctable

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

type cBlobKindRow struct {
	name, unit, root, file, verdict string
	unknown                         int
}

func cBlobKindRows(t *testing.T, dir string) []cBlobKindRow {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	defer f.Close()
	var rows []cBlobKindRow
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 6 {
			t.Fatalf("MANIFEST.tsv row %q has %d columns, want 6", line, len(cols))
		}
		row := cBlobKindRow{name: cols[0], unit: cols[1], root: cols[2], file: cols[3], verdict: cols[4], unknown: -1}
		if cols[5] != "-" {
			n, err := strconv.Atoi(cols[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv row %s: unknown %q: %v", row.name, cols[5], err)
			}
			row.unknown = n
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv row %s: verdict %q", row.name, row.verdict)
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

// cBlobKindField answers the root's one blob-pointer field: its C member
// spelling and its blob kind, so the probe can name the slot generically.
func cBlobKindField(t *testing.T, u *ir.Unit, root string) (name string, stringKind bool) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("the unit declares no table %s", root)
	}
	for _, f := range st.Fields {
		if f.Type.Pointer && f.Type.Blob() {
			return f.Name, f.Type.Kind == ir.TString
		}
	}
	t.Fatalf("%s declares no blob-pointer field", root)
	return "", false
}

func TestCPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "pointer-target-type", "blobkind")
	rows := cBlobKindRows(t, dir)

	var units []string
	seen := map[string]bool{}
	for _, row := range rows {
		if !seen[row.unit] {
			seen[row.unit] = true
			units = append(units, row.unit)
		}
	}

	if len(units) == 0 {
		t.Fatal("units is empty: the loop below would check nothing")
	}
	for _, unit := range units {
		t.Run(unit, func(t *testing.T) {
			var unitRows []cBlobKindRow
			var wires [][]byte
			for _, row := range rows {
				if row.unit != unit {
					continue
				}
				wire, err := os.ReadFile(filepath.Join(dir, row.file))
				if err != nil {
					t.Fatalf("row %s: %v", row.name, err)
				}
				unitRows = append(unitRows, row)
				wires = append(wires, wire)
			}
			src, err := os.ReadFile(filepath.Join("..", "..", "..", unit, "Reader.schema"))
			if err != nil {
				t.Fatal(err)
			}
			u := unitFrom(t, string(src))
			model := tabletext.NewModel(u)
			output, err := runCBlobKindProbe(t, u, cBlobKindReadProbe(t, u, unitRows, wires)+cBlobKindWriterProbe(t, u), nil)
			if err != nil {
				t.Fatalf("blob-kind probe: %v\n%s", err, output)
			}
			// THE WRITER'S OWN SAVE: every saved wire reads back accepted with
			// nothing unknown through the engine, which also refuses a record
			// no pointer reaches.
			allCBlobKindSaved := cBlobKindSaved(t, output)
			if len(allCBlobKindSaved) == 0 {
				t.Fatal("cBlobKindSaved(t, output) is empty: the loop below would check nothing")
			}
			for _, saved := range allCBlobKindSaved {
				inst := model.New(model.Lookup(saved.root))
				if inst == nil {
					t.Fatalf("saved %s/%s: no root %s in %s", saved.root, saved.store, saved.root, unit)
				}
				var report tabletext.Report
				ok, err := tablewire.Decode(model, inst, saved.wire, &report)
				if !ok || err != nil || report.Refused || report.Unknown != 0 {
					t.Errorf("%s/%s: the leg's own save does not read back clean: ok=%v err=%v %+v", saved.root, saved.store, ok, err, report)
				}
			}
		})
	}

	// NEGATIVE CONTROL: THE OLD RULE, planted. "Can name" taken from the
	// ROOT'S pointer closure for blobs drops the reserved id its closure lacks
	// from node_declared, so a declared `*string` slot naming a `*bytes`
	// record is skipped and the pointer reads null. The probe must go red on
	// text-names-bytes.
	t.Run("negative control: the root-closure rule reads text-names-bytes", func(t *testing.T) {
		var row cBlobKindRow
		found := false
		for _, r := range rows {
			if r.name == "text-names-bytes" {
				row, found = r, true
				break
			}
		}
		if !found {
			t.Fatal("MANIFEST.tsv has no text-names-bytes row")
		}
		src, err := os.ReadFile(filepath.Join("..", "..", "..", row.unit, "Reader.schema"))
		if err != nil {
			t.Fatal(err)
		}
		u := unitFrom(t, string(src))
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatal(err)
		}
		body := cBlobKindReadProbe(t, u, []cBlobKindRow{row}, [][]byte{wire})
		output, err := runCBlobKindProbe(t, u, body, func(files map[string][]byte) error {
			return cBlobKindPlantRootClosure(files, row.root)
		})
		if err == nil {
			t.Fatalf("the planted root-closure rule was not red: the probe read text-names-bytes\n%s", output)
		}
		if !strings.Contains(output, "FAIL: text-names-bytes") {
			t.Fatalf("the planted rule went red without naming text-names-bytes:\n%s", output)
		}
	})
}

const cBlobKindReadBlock = `
    {
        static const uint8_t wire[] = { @WIRE@ };
        const int accept = @ACCEPT@, unknown = @UNKNOWN@;
        int retaining;
        for (retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store;
            const @ROOT@ * root;
            memset(region, 0, sizeof(region));
            memset(&report, 0, sizeof(report));
            memset(&store, 0, sizeof(store));
            store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
            store.ids = retain_ids; store.id_capacity = 64;
            root = retaining
                ? @ROOTL@_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : @ROOTL@_load(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK((root != NULL) == accept, retaining ? "@NAME@: @ROOTL@_load_retain verdict" : "@NAME@: @ROOTL@_load verdict");
            if (root == NULL) { CHECK(!accept || report.refused, retaining ? "@NAME@: @ROOTL@_load_retain report" : "@NAME@: @ROOTL@_load report"); }
            if (root != NULL) {
                CHECK(unknown < 0 || report.unknown == unknown, retaining ? "@NAME@: @ROOTL@_load_retain unknown" : "@NAME@: @ROOTL@_load unknown");
                CHECK(unknown <= 0 || root->@FIELD@.value == 0, retaining ? "@NAME@: @ROOTL@_load_retain reads null" : "@NAME@: @ROOTL@_load reads null");
                CHECK(unknown != 0 || root->@FIELD@.value != 0, retaining ? "@NAME@: @ROOTL@_load_retain reads its blob" : "@NAME@: @ROOTL@_load reads its blob");
            }
        }
        {
            @ROOT@Builder builder;
            TableReport report;
            int ok;
            CHECK(@ROOTL@_builder_init(&builder), "@NAME@: @ROOTL@_builder_init");
            memset(&report, 0, sizeof(report));
            ok = @ROOTL@_load_builder(&builder, wire, (int64_t)sizeof(wire), &report);
            CHECK((ok != 0) == accept, "@NAME@: @ROOTL@_load_builder verdict");
            CHECK(!ok || unknown < 0 || report.unknown == unknown, "@NAME@: @ROOTL@_load_builder unknown");
            CHECK(!ok || unknown <= 0 || @ROOTL@_builder_root(&builder)->@FIELD@.value == 0, "@NAME@: @ROOTL@_load_builder reads null");
            @ROOTL@_builder_shutdown(&builder);
        }
        {
            int64_t attribution = 0;
            TableRefuseReason reason = SCHEMA_TABLE_REFUSE_OK;
            int64_t bytes = @ROOTL@_load_measure_ex(wire, (int64_t)sizeof(wire), &attribution, &reason);
            CHECK((bytes >= 0) == accept, "@NAME@: @ROOTL@_load_measure_ex verdict");
        }
    }
`

// cBlobKindReadProbe is the C body that reads each row through the strict
// region read, the retaining read, the builder read and the measure.
func cBlobKindReadProbe(t *testing.T, u *ir.Unit, rows []cBlobKindRow, wires [][]byte) string {
	t.Helper()
	var body strings.Builder
	for i, row := range rows {
		field, _ := cBlobKindField(t, u, row.root)
		accept := "0"
		if row.verdict == "read" {
			accept = "1"
		}
		unknown := "-1"
		if row.unknown >= 0 {
			unknown = strconv.Itoa(row.unknown)
		}
		body.WriteString(strings.NewReplacer(
			"@WIRE@", cBytes(wires[i]),
			"@ACCEPT@", accept,
			"@UNKNOWN@", unknown,
			"@ROOT@", row.root,
			"@ROOTL@", strings.ToLower(row.root),
			"@FIELD@", field,
			"@NAME@", row.name,
		).Replace(cBlobKindReadBlock))
	}
	return body.String()
}

const cBlobKindWriteBlock = `
    {
        @ROOT@Builder b;
        @ROOT@ * v;
        TableCtx ctx;
        TableReport report;
        TableRetain retain;
        uint8_t out[4096], retained[4096];
        const @ROOT@ * kept;
        int64_t n;
        int set;
        for (set = 0; set <= 1; set++) {
            memset(&b, 0, sizeof(b));
            CHECK(@ROOTL@_builder_init(&b), "@NAME@: writer builder init");
            v = @ROOTL@_builder_root(&b);
            CHECK(v != NULL, "@NAME@: writer root");
            if (set) {
                TableBlob * blob = table_blob_emplace(&b.main, &v->@FIELD@, (int64_t)3, @BLOB@);
                CHECK(blob != NULL, "@NAME@: writer blob");
                memcpy(blob + 1, "hi!", 3);
                if (@BLOB@) { ((char *)(blob + 1))[3] = 0; }
            }
            memset(&ctx, 0, sizeof(ctx)); ctx.arena = &b.arena;
            n = @ROOTL@_measure(&ctx, v);
            CHECK(n >= 0 && n <= (int64_t)sizeof(out), "@NAME@: writer measure");
            if (n >= 0 && n <= (int64_t)sizeof(out)) {
                CHECK(@ROOTL@_save(&ctx, v, out, (int64_t)sizeof(out)) == n, "@NAME@: writer save");
                print_wire("SAVED @ROOT@", set ? "set" : "null", out, n);
                memset(region, 0, sizeof(region));
                memset(&report, 0, sizeof(report));
                kept = @ROOTL@_load(region, (int64_t)sizeof(region), out, n, &report);
                CHECK(kept != NULL && !report.refused, "@NAME@: writer reload");
                if (kept != NULL) {
                    memset(&report, 0, sizeof(report));
                    memset(&retain, 0, sizeof(retain));
                    retain.bytes = retain_bytes; retain.capacity = (int64_t)sizeof(retain_bytes);
                    retain.ids = retain_ids; retain.id_capacity = 64;
                    n = @ROOTL@_measure_retain(kept, &retain);
                    CHECK(n >= 0 && n <= (int64_t)sizeof(retained), "@NAME@: writer retain measure");
                    if (n >= 0 && n <= (int64_t)sizeof(retained)) {
                        CHECK(@ROOTL@_save_retain(kept, &retain, retained, (int64_t)sizeof(retained), &report) == n && !report.refused, "@NAME@: writer retain save");
                        print_wire("RETAIN @ROOT@", set ? "set" : "null", retained, n);
                    }
                }
            }
            @ROOTL@_builder_shutdown(&b);
        }
    }
`

// cBlobKindWriterProbe is the C body that saves each root with its blob set
// and with it null, through the plain save and the retaining save, and prints
// every wire for the engine to judge.
func cBlobKindWriterProbe(t *testing.T, u *ir.Unit) string {
	t.Helper()
	var roots []string
	for name := range u.Tables {
		roots = append(roots, name)
	}
	sort.Strings(roots)
	var body strings.Builder
	for _, name := range roots {
		field, stringKind := cBlobKindField(t, u, name)
		blob := "0"
		if stringKind {
			blob = "1"
		}
		body.WriteString(strings.NewReplacer(
			"@ROOT@", name,
			"@ROOTL@", strings.ToLower(name),
			"@FIELD@", field,
			"@BLOB@", blob,
			"@NAME@", name,
		).Replace(cBlobKindWriteBlock))
	}
	return body.String()
}

type cBlobKindWire struct {
	root, store string
	wire        []byte
}

// cBlobKindSaved parses the writer probe's SAVED/RETAIN lines.
func cBlobKindSaved(t *testing.T, output string) []cBlobKindWire {
	t.Helper()
	var saved []cBlobKindWire
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || (fields[0] != "SAVED" && fields[0] != "RETAIN") {
			continue
		}
		wire := make([]byte, len(fields[3])/2)
		for i := range wire {
			n, err := strconv.ParseUint(fields[3][2*i:2*i+2], 16, 8)
			if err != nil {
				t.Fatalf("saved wire %q: %v", line, err)
			}
			wire[i] = byte(n)
		}
		saved = append(saved, cBlobKindWire{root: fields[1], store: fields[0] + "/" + fields[2], wire: wire})
	}
	if len(saved) == 0 {
		t.Fatalf("the writer probe printed no saved wire:\n%s", output)
	}
	return saved
}

// cBlobKindPlantRootClosure plants the OLD rule in a copy of the generated
// header: the root's node_declared keeps a reserved blob id only when the
// root's pointer closure carries an edge of that kind, so the bytes id is
// dropped from a root whose closure holds only `*string`.
func cBlobKindPlantRootClosure(files map[string][]byte, root string) error {
	needle := "_" + strings.ToLower(root) + "_node_declared_(uint64_t id)"
	bytesCase := fmt.Sprintf("        case UINT64_C(0x%016x):\n", ir.BytesWireTypeId)
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.h") {
			continue
		}
		header := string(data)
		at := strings.Index(header, needle)
		if at < 0 {
			continue
		}
		fnStart := strings.LastIndex(header[:at], "static SCHEMA_UNUSED int ")
		if fnStart < 0 {
			return fmt.Errorf("the planted rule found no declaration of %s", needle)
		}
		fnEnd := strings.Index(header[at:], "\n}\n")
		if fnEnd < 0 {
			return fmt.Errorf("the planted rule found no end of %s", needle)
		}
		fnEnd += at + len("\n}\n")
		fn := header[fnStart:fnEnd]
		if !strings.Contains(fn, bytesCase) {
			return fmt.Errorf("the planted rule found no reserved bytes id in %s", needle)
		}
		files[name] = []byte(header[:fnStart] + strings.Replace(fn, bytesCase, "", 1) + header[fnEnd:])
		return nil
	}
	return fmt.Errorf("the planted rule found no generated header declaring %s", needle)
}

// runCBlobKindProbe generates the unit and runs a C probe over it. `edit` may
// rewrite the generated files before they are compiled (the negative control
// plants the old rule there).
func runCBlobKindProbe(t *testing.T, u *ir.Unit, body string, edit func(map[string][]byte) error) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		if err := edit(files); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures = 0;
static uint8_t region[65536] __attribute__((aligned(64)));
static uint8_t retain_bytes[65536];
static TableRetainId retain_ids[256];
#define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)

static SCHEMA_UNUSED void print_wire(const char * tag, const char * state, const uint8_t * wire, int64_t n)
{
    int64_t i;
    printf("%s %s ", tag, state);
    for (i = 0; i < n; i++) { printf("%02x", wire[i]); }
    printf("\n");
}

int main(void) {
` + body + `
    if (failures != 0) { return 1; }
    return 0;
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(t, cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		return string(output), fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := testproc.CommandContext(t, ctx, filepath.Join(dir, "probe"))
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	output, err := cmd.CombinedOutput()
	return string(output), err
}
