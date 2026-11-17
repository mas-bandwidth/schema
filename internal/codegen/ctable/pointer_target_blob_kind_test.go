// A blob record's kind in the variable form (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused). The two reserved
// blob ids are types every reader can name, whatever its root reaches: a slot
// this reader declares that names a blob record of the other kind refuses the
// whole read wire_damaged, and so does a blob record no pointer reaches. A blob
// record named only from a field this reader does not declare is a newer
// writer's evolution: it reads, counted unknown with the field that names it.
//
// The engine (internal/tablewire) is the oracle. Every row of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv and the three
// closure rows of testdata/conformance/feature-witnesses/pointer-targets are
// read by the engine and by every C read of the variable form (the region
// read, the retaining read, the builder read and the measure), and the C
// answer must be the engine's: the verdict, the refusal's reason, the unknown
// count and whether the declared pointer reads set. The C writer's saves (the
// builder save, the region save and the retaining save) of each root with its
// blob null, empty and set are decoded by the engine and must read back
// accepted with nothing unknown: a writer never emits a blob record no pointer
// reaches.
package ctable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cBlobKindCase is one wire read at one root, with the engine's answer.
type cBlobKindCase struct {
	name, root, field string // field: the root's declared pointer
	wire              []byte
	verdict, reason   string // the engine's verdict and, for a refusal, its reason
	unknown           int
	set               bool // a read: the declared pointer reads set
}

// cBlobKindPaths are the C reads of the variable form each case runs through.
var cBlobKindPaths = []string{"load", "retain", "builder", "measure"}

// cBlobKindDecoders are the engine's two reads; they must agree on every case.
var cBlobKindDecoders = []struct {
	name string
	read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
}{
	{"strict", tablewire.Decode},
	{"retaining", func(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		return tablewire.DecodeRetain(m, inst, wire, &retain, report)
	}},
}

// cBlobKindOracle is the engine's answer for one wire at one root.
func cBlobKindOracle(t *testing.T, m *tabletext.Model, c *cBlobKindCase) {
	t.Helper()
	st := m.Lookup(c.root)
	if st == nil {
		t.Fatalf("%s: no root %s", c.name, c.root)
	}
	for i, d := range cBlobKindDecoders {
		inst := m.New(st)
		var report tabletext.Report
		ok, err := d.read(m, inst, c.wire, &report)
		verdict, reason, unknown, set := "refused", tablewire.RefusalReason(err), 0, false
		if ok && err == nil && !report.Refused {
			verdict, reason, unknown = "read", "", report.Unknown
			fv, found := inst.FieldByKey(c.field)
			if !found {
				t.Fatalf("%s: %s has no field %s", c.name, c.root, c.field)
			}
			set = fv.Cell.Blob != nil || fv.Cell.Node != nil
		} else if reason == "" {
			t.Fatalf("%s (%s): the engine failed without a refusal: %v %+v", c.name, d.name, err, report)
		}
		if i == 0 {
			c.verdict, c.reason, c.unknown, c.set = verdict, reason, unknown, set
		} else if verdict != c.verdict || reason != c.reason || unknown != c.unknown || set != c.set {
			t.Fatalf("%s: the engine's reads disagree: %s %s %d %v against %s %s %d %v", c.name, verdict, reason, unknown, set, c.verdict, c.reason, c.unknown, c.set)
		}
	}
}

// cBlobKindReadSource is the C program that reads every case through every
// read path and prints one ROW line per path:
//
//	ROW <case> <path> read <unknown> <set|null>
//	ROW <case> <path> refused <reason>
//
// The wires are files in the program's working directory, named <case>.bin.
func cBlobKindReadSource(cases []cBlobKindCase, extra string) string {
	var b strings.Builder
	b.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static uint8_t region[65536] __attribute__((aligned(64)));
static uint8_t retain_bytes[4096];
static TableRetainId retain_ids[64];

static void die(const char * what) { printf("FAIL: %s\n", what); exit(1); }

static uint8_t * read_file(const char * path, int64_t * size)
{
    FILE * f = fopen(path, "rb"); long n; uint8_t * data;
    if (f == NULL || fseek(f, 0, SEEK_END) != 0 || (n = ftell(f)) < 0 || fseek(f, 0, SEEK_SET) != 0) { die(path); }
    data = (uint8_t *)malloc((size_t)n + 1);
    if (data == NULL || fread(data, 1, (size_t)n, f) != (size_t)n) { die(path); }
    fclose(f); *size = (int64_t)n; return data;
}

static __attribute__((unused)) void write_file(const char * path, const uint8_t * data, int64_t size)
{
    FILE * f = fopen(path, "wb");
    if (f == NULL || fwrite(data, 1, (size_t)size, f) != (size_t)size || fclose(f) != 0) { die(path); }
}

static const char * reason_name(int reason)
{
    switch (reason) {
        case SCHEMA_TABLE_REFUSE_WIRE_DAMAGED: return "wire_damaged";
        case SCHEMA_TABLE_REFUSE_KIND_MISMATCH: return "kind_mismatch";
        default: return "other";
    }
}

static void row(const char * name, const char * path, int ok, const TableReport * report, int set)
{
    if (ok && !report->refused) { printf("ROW %s %s read %d %s\n", name, path, (int)report->unknown, set ? "set" : "null"); }
    else { printf("ROW %s %s refused %s\n", name, path, reason_name((int)report->refuse_reason)); }
}
`)
	roots := map[string]string{}
	for _, c := range cases {
		roots[c.root] = c.field
	}
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		s, field := ir.RustSnake(root), roots[root]
		fmt.Fprintf(&b, `
static void probe_%[1]s(const char * name, const char * path)
{
    int64_t size; uint8_t * wire = read_file(path, &size); int retaining;
    for (retaining = 0; retaining <= 1; retaining++) {
        TableReport report; TableRetain store; const %[2]s * root;
        memset(region, 0, sizeof(region)); memset(&report, 0, sizeof(report)); memset(&store, 0, sizeof(store));
        store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
        store.ids = retain_ids; store.id_capacity = 64;
        root = retaining
            ? %[1]s_load_retain(region, (int64_t)sizeof(region), wire, size, &store, &report)
            : %[1]s_load(region, (int64_t)sizeof(region), wire, size, &report);
        row(name, retaining ? "retain" : "load", root != NULL, &report, root != NULL && root->%[3]s.value != 0);
    }
    {
        %[2]sBuilder builder; TableReport report; int ok;
        if (!%[1]s_builder_init(&builder)) { die("builder init"); }
        memset(&report, 0, sizeof(report));
        ok = %[1]s_load_builder(&builder, wire, size, &report);
        row(name, "builder", ok, &report, ok && %[1]s_builder_root(&builder)->%[3]s.value != 0);
        %[1]s_builder_shutdown(&builder);
    }
    {
        int64_t attribution = 0; TableRefuseReason reason = SCHEMA_TABLE_REFUSE_OK;
        int64_t bytes = %[1]s_load_measure_ex(wire, size, &attribution, &reason);
        printf("ROW %%s measure %%s\n", name, bytes >= 0 ? "read" : "refused");
    }
    free(wire);
}
`, s, root, field)
	}
	b.WriteString(extra)
	b.WriteString("\nint main(void)\n{\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "    probe_%s(%q, %q);\n", ir.RustSnake(c.root), c.name, c.name+".bin")
	}
	b.WriteString("    cblobkind_saves();\n    return 0;\n}\n")
	return b.String()
}

// cBlobKindSaveSource is the C writer half: for each root, its blob null (0),
// empty (1) and set to "hello" (2), the builder save, the region save of that
// wire read back, and the retaining save of it read back retained, each into
// save-<root>-<case>-<path>.bin.
func cBlobKindSaveSource(roots map[string]string) string {
	var b strings.Builder
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		s, field := ir.RustSnake(root), roots[root]
		fmt.Fprintf(&b, `
static void save_%[1]s(int which)
{
    static uint8_t out[4096], again[4096]; char path[128]; int64_t written, rewritten;
    %[2]sBuilder builder; %[2]s * root; TableCtx ctx;
    if (!%[1]s_builder_init(&builder)) { die("builder init"); }
    root = %[1]s_builder_root(&builder);
    if (root == NULL) { die("builder root"); }
    if (which > 0) {
        int64_t length = which == 2 ? 5 : 0;
        %[3]s
    }
    memset(&ctx, 0, sizeof(ctx)); ctx.arena = &builder.arena;
    written = %[1]s_save(&ctx, root, out, (int64_t)sizeof(out));
    %[1]s_builder_shutdown(&builder);
    if (written < 0) { die("builder save"); }
    snprintf(path, sizeof(path), "save-%[2]s-%%d-builder.bin", which); write_file(path, out, written);
    {
        TableReport report; const %[2]s * loaded;
        memset(region, 0, sizeof(region)); memset(&report, 0, sizeof(report));
        loaded = %[1]s_load(region, (int64_t)sizeof(region), out, written, &report);
        if (loaded == NULL) { die("region read of the builder save"); }
        rewritten = %[1]s_save(NULL, loaded, again, (int64_t)sizeof(again));
        if (rewritten < 0) { die("region save"); }
        snprintf(path, sizeof(path), "save-%[2]s-%%d-region.bin", which); write_file(path, again, rewritten);
    }
    {
        TableReport report; TableRetain store; const %[2]s * loaded;
        memset(region, 0, sizeof(region)); memset(&report, 0, sizeof(report)); memset(&store, 0, sizeof(store));
        store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
        store.ids = retain_ids; store.id_capacity = 64;
        loaded = %[1]s_load_retain(region, (int64_t)sizeof(region), out, written, &store, &report);
        if (loaded == NULL) { die("retaining read of the builder save"); }
        memset(&report, 0, sizeof(report));
        rewritten = %[1]s_save_retain(loaded, &store, again, (int64_t)sizeof(again), &report);
        if (rewritten < 0) { die("retaining save"); }
        snprintf(path, sizeof(path), "save-%[2]s-%%d-retain.bin", which); write_file(path, again, rewritten);
    }
}
`, s, root, cBlobKindEmplace(field))
	}
	b.WriteString("\nstatic void cblobkind_saves(void)\n{\n    int which;\n    for (which = 0; which <= 2; which++) {\n")
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		fmt.Fprintf(&b, "        save_%s(which);\n", ir.RustSnake(root))
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

// cBlobKindEmplace places a blob of `length` bytes ("hello" or empty) in the
// root's field: a *string through the string emplace, a *bytes through the
// bytes emplace. The reader unit's Text.body is the one, Bin.data the other.
func cBlobKindEmplace(field string) string {
	if field == "body" {
		return `if (table_string_emplace(&builder.main, &root->body, "hello", length) == NULL) { die("string emplace"); }`
	}
	return `{ uint8_t * data = table_bytes_emplace(&builder.main, &root->` + field + `, length); if (data == NULL) { die("bytes emplace"); } memcpy(data, "hello", (size_t)length); }`
}

// runCBlobKind generates the unit into a temporary directory, lets `plant`
// alter the generated files (a negative control), writes each case's wire
// beside them, compiles the program and runs it in that directory.
func runCBlobKind(t *testing.T, u *ir.Unit, cases []cBlobKindCase, source string, plant func(map[string][]byte)) (dir, output string, err error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
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
	if plant != nil {
		plant(files)
	}
	dir = t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, c.name+".bin"), c.wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "probe"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return dir, string(out), err
}

// cBlobKindProblems names every C answer that is not the engine's.
func cBlobKindProblems(cases []cBlobKindCase, output string) []string {
	got := map[string]string{}
	for line := range strings.Lines(output) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ROW "); ok {
			name, tail, _ := strings.Cut(rest, " ")
			path, answer, _ := strings.Cut(tail, " ")
			got[name+" "+path] = answer
		}
	}
	var problems []string
	for _, c := range cases {
		for _, path := range cBlobKindPaths {
			want := "refused " + c.reason
			switch {
			case path == "measure":
				want = c.verdict
			case c.verdict == "read":
				want = "read " + strconv.Itoa(c.unknown) + map[bool]string{true: " set", false: " null"}[c.set]
			}
			answer, ok := got[c.name+" "+path]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s (%s): no answer", c.name, path))
			} else if answer != want {
				problems = append(problems, fmt.Sprintf("%s (%s): C answers %q, the engine %q", c.name, path, answer, want))
			}
		}
	}
	return problems
}

// cBlobKindWriterProblems decodes a saved wire with the engine at `root` and
// names each read that is not accepted with nothing unknown and the field's
// blob as saved (nil for null).
func cBlobKindWriterProblems(m *tabletext.Model, root, field string, wire, want []byte) []string {
	var problems []string
	for _, d := range cBlobKindDecoders {
		inst := m.New(m.Lookup(root))
		var report tabletext.Report
		ok, err := d.read(m, inst, wire, &report)
		if !ok || err != nil || report.Refused || report.Unknown != 0 {
			problems = append(problems, fmt.Sprintf("(%s) %v %+v", d.name, err, report))
			continue
		}
		fv, _ := inst.FieldByKey(field)
		if blob := fv.Cell.Blob; (blob == nil) != (want == nil) || (blob != nil && !bytes.Equal(blob.Data, want)) {
			problems = append(problems, fmt.Sprintf("(%s) the field reads %v, want %q", d.name, blob, want))
		}
	}
	return problems
}

// cBlobKindPlantRootClosure plants the old rule in a copy of the generated
// reader: a blob kind this root's pointer closure lacks is a type the reader
// cannot name, so Text forgets the bytes id and Bin the string id.
func cBlobKindPlantRootClosure(t *testing.T, pkg string) func(map[string][]byte) {
	return func(files map[string][]byte) {
		both := fmt.Sprintf("        case UINT64_C(0x%016x):\n        case UINT64_C(0x%016x): return 1;\n", ir.BytesWireTypeId, ir.StringWireTypeId)
		for root, keep := range map[string]uint64{"text": ir.StringWireTypeId, "bin": ir.BytesWireTypeId} {
			header := "int schema_" + pkg + "_" + root + "_node_declared_(uint64_t id)\n{\n"
			planted := 0
			for name, data := range files {
				src := string(data)
				at := strings.Index(src, header)
				if at < 0 {
					continue
				}
				end := at + strings.Index(src[at:], "default: return 0;")
				body := strings.Replace(src[at:end], both, fmt.Sprintf("        case UINT64_C(0x%016x): return 1;\n", keep), 1)
				if body == src[at:end] {
					t.Fatalf("plant: %s's node_declared in %s does not name both blob ids", root, name)
				}
				files[name] = []byte(src[:at] + body + src[end:])
				planted++
			}
			if planted != 1 {
				t.Fatalf("plant: %s's node_declared found in %d files, want 1", root, planted)
			}
		}
	}
}

func TestCPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	rows := cPointerTargetRows(t, vectors)
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	unit := rows[0].unit
	src, err := os.ReadFile(filepath.Join(repo, unit, "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	m := tabletext.NewModel(u)
	fields := map[string]string{"Text": "body", "Bin": "data"}

	// THE MANIFEST, generically: the engine must answer each row's verdict and
	// unknown count, and the C reads must answer the engine.
	var cases []cBlobKindCase
	for _, row := range rows {
		if row.unit != unit || fields[row.root] == "" {
			t.Fatalf("row %s reads %s at %s; this probe reads %s at Text or Bin", row.name, row.unit, row.root, unit)
		}
		wire, err := os.ReadFile(filepath.Join(vectors, row.file))
		if err != nil {
			t.Fatalf("row %s: %v", row.name, err)
		}
		c := cBlobKindCase{name: row.name, root: row.root, field: fields[row.root], wire: wire}
		cBlobKindOracle(t, m, &c)
		if c.verdict != row.verdict || (row.verdict == "read" && row.unknown >= 0 && c.unknown != row.unknown) {
			t.Fatalf("row %s: the engine answers %s unknown %d; MANIFEST.tsv %s unknown %d", row.name, c.verdict, c.unknown, row.verdict, row.unknown)
		}
		cases = append(cases, c)
	}
	source := cBlobKindReadSource(cases, cBlobKindSaveSource(fields))

	t.Run("every row answers the engine", func(t *testing.T) {
		t.Parallel()
		dir, output, err := runCBlobKind(t, u, cases, source, nil)
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, output)
		}
		for _, p := range cBlobKindProblems(cases, output) {
			t.Error(p)
		}
		// THE WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: every save
		// reads back in the engine accepted, nothing unknown, the blob as saved.
		for root, field := range fields {
			for which, want := range [][]byte{nil, {}, []byte("hello")} {
				for _, path := range []string{"builder", "region", "retain"} {
					file := fmt.Sprintf("save-%s-%d-%s.bin", root, which, path)
					wire, err := os.ReadFile(filepath.Join(dir, file))
					if err != nil {
						t.Fatalf("%s: %v", file, err)
					}
					for _, p := range cBlobKindWriterProblems(m, root, field, wire, want) {
						t.Errorf("%s: the C save does not read back clean: %s", file, p)
					}
				}
			}
		}
	})
	t.Run("negative control: the writer criterion refuses an orphan and an unknown record", func(t *testing.T) {
		t.Parallel()
		for _, c := range cases {
			if c.name == "text-orphan-bytes" || c.name == "text-unknown-field-bytes" {
				if len(cBlobKindWriterProblems(m, "Text", "body", c.wire, nil)) != len(cBlobKindDecoders) {
					t.Errorf("%s: the writer criterion passed a wire a save must never produce", c.name)
				}
			}
		}
	})
	t.Run("negative control: the root's closure decides which blob ids it can name", func(t *testing.T) {
		t.Parallel()
		_, output, err := runCBlobKind(t, u, cases, source, cBlobKindPlantRootClosure(t, u.Package))
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, output)
		}
		problems := cBlobKindProblems(cases, output)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "text-names-bytes (load)") }) {
			t.Errorf("the planted root-closure rule did not go red on text-names-bytes: %q", problems)
		}
	})
	t.Run("feature witnesses: blobs and tables outside the root closure", func(t *testing.T) {
		t.Parallel()
		testCPointerTargetWitnessesOutsideClosure(t, repo)
	})
}

// testCPointerTargetWitnessesOutsideClosure holds rows 8, 9 and 10 of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json: a value
// the engine saves at Old and the C reads at OnlyB, OnlyString and OnlyBytes,
// where the slot's target kind is outside the root's closure. Each refuses.
func testCPointerTargetWitnessesOutsideClosure(t *testing.T, repo string) {
	dir := filepath.Join(repo, "testdata", "conformance", "feature-witnesses", "pointer-targets")
	src, err := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	m := tabletext.NewModel(u)
	fields := map[string]string{"OnlyB": "left", "OnlyString": "payload", "OnlyBytes": "text"}
	want := []string{"table outside root closure", "bytes outside root closure", "string outside root closure"}
	if len(rows) < 10 {
		t.Fatalf("cases.json holds %d rows, want rows 8 to 10", len(rows))
	}
	var cases []cBlobKindCase
	for i, row := range rows[7:10] {
		if row.Name != want[i] || fields[row.Reader] == "" {
			t.Fatalf("cases.json row %d is %q read at %s, want %q", i+8, row.Name, row.Reader, want[i])
		}
		v := m.New(m.Lookup(row.Writer))
		var r tabletext.Report
		if !m.Read(v, row.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", row.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", row.Name, err)
		}
		c := cBlobKindCase{name: strings.ReplaceAll(row.Name, " ", "-"), root: row.Reader, field: fields[row.Reader], wire: wire}
		cBlobKindOracle(t, m, &c)
		if (c.verdict == "refused") != (row.KindMismatch > 0) || (c.verdict == "read" && c.unknown != row.Unknown) {
			t.Fatalf("%s: the engine answers %s unknown %d; cases.json kind_mismatch %d unknown %d", row.Name, c.verdict, c.unknown, row.KindMismatch, row.Unknown)
		}
		cases = append(cases, c)
	}
	source := cBlobKindReadSource(cases, "\nstatic void cblobkind_saves(void) {}\n")
	_, output, err := runCBlobKind(t, u, cases, source, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, output)
	}
	for _, p := range cBlobKindProblems(cases, output) {
		t.Error(p)
	}
}
