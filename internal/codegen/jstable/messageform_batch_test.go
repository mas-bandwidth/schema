package jstable

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormBatchJavaScript holds the BATCH half of the message form
// (docs/SPEC-TABLES.md §3.3) on the JavaScript leg, one subtest per clause,
// over the message rows the tables manifest pins. The bytes ARE the C++
// reference's; the manifest names the units and the connections.
//
// EVERY CLAUSE IS ASSERTED OVER THE EMITTED MODULE, not a Go model of it: the
// generated tree is written and run under the leg's own node (jsNode), the way
// messageform_announce_test.go runs its columns. A surface the leg does not
// emit, or emits broken, turns a subtest red here.
func TestMessageFormBatchJavaScript(t *testing.T) {
	t.Parallel()

	units, connections, rows := parseMessageManifest(t)

	t.Run("round_trip", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				t.Parallel()
				conn, ok := connections[row.connection]
				if !ok {
					t.Fatalf("message %q names connection %q with no connection line", row.name, row.connection)
				}
				u := loadUnitPaths(t, units[conn.unit])
				if ir.VariableTables(u)[row.root] {
					// a variable table has no message form (§3.3), so the
					// JavaScript leg answers this message row ABSENT and
					// asserts nothing
					return
				}
				st := findTable(t, u, row.root)
				probe := messageRoundTripProbe(u, st, row.name, conn.path, row.batch)
				out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
					"ann.bin":   mustRead(t, conn.path),
					"batch.bin": mustRead(t, row.batch),
				}, probe)
				if err != nil {
					t.Fatalf("round trip %s: %v\n%s", row.name, err, out)
				}
			})
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		u := loadUnitPaths(t, units["backenddemo"])
		st := findTable(t, u, "LoginRequest")
		conn := connections["backend_conn"]
		probe := messageRefusalProbe(u, st, conn.path)
		out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
			"ann.bin": mustRead(t, conn.path),
		}, probe)
		if err != nil {
			t.Fatalf("refusals: %v\n%s", err, out)
		}
	})

	t.Run("damage", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		u := loadUnitPaths(t, units["backenddemo"])
		st := findTable(t, u, "LoginRequest")
		conn := connections["backend_conn"]
		probe := messageDamageProbe(u, st, conn.path)
		out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
			"ann.bin": mustRead(t, conn.path),
		}, probe)
		if err != nil {
			t.Fatalf("damage: %v\n%s", err, out)
		}
	})

	t.Run("compressed_float", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		u := loadUnitPaths(t, []string{repoPath("bench/corpus/BenchTable.schema")})
		st := findTable(t, u, "TableMixed")
		probe := messageCompressedFloatProbe(u, st)
		out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
			"ann.bin": ir.TableAnnouncement(u),
		}, probe)
		if err != nil {
			t.Fatalf("compressed float: %v\n%s", err, out)
		}
	})

	t.Run("keyed_array", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		u := loadSchemaString(t, "keyed_array.schema", `package keyedtest

enum Team { Red, Blue, Green }

fixed table KeyedTeam
{
    slots [Team]uint8
}
`)
		st := findTable(t, u, "KeyedTeam")
		probe := messageKeyedArrayProbe(u, st)
		out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
			"ann.bin": ir.TableAnnouncement(u),
		}, probe)
		if err != nil {
			t.Fatalf("keyed array: %v\n%s", err, out)
		}
	})

	t.Run("by_value_type", func(t *testing.T) {
		t.Parallel()
		node := jsNode(t)
		u := loadSchemaString(t, "by_value_type.schema", `package keyedtest2

type ScoreBoard
{
    points int32
}

fixed table KeyedScore
{
    scores ScoreBoard
}
`)
		st := findTable(t, u, "KeyedScore")
		probe := messageByValueTypeProbe(u, st)
		out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
			"ann.bin": ir.TableAnnouncement(u),
		}, probe)
		if err != nil {
			t.Fatalf("by-value type: %v\n%s", err, out)
		}
	})
}

// ---- the manifest, read here rather than copied: a new row is a new subtest

type messageConn struct {
	unit, path string
}

type messageRow struct {
	name, connection, root, file, batch string
}

func parseMessageManifest(t *testing.T) (map[string][]string, map[string]messageConn, []messageRow) {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	connections := map[string]messageConn{}
	var rows []messageRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "unit":
			if len(f) >= 3 {
				for i := 2; i < len(f); i++ {
					f[i] = repoPath(f[i])
				}
				units[f[1]] = f[2:]
			}
		case "connection":
			if len(f) >= 5 {
				connections[f[1]] = messageConn{unit: f[2], path: repoPath(f[4])}
			}
		case "message":
			if len(f) >= 6 && f[5] != "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
				rows = append(rows, messageRow{name: f[1], connection: f[2], root: f[3], file: repoPath(f[4]), batch: repoPath(f[5])})
			}
		}
	}
	if len(units) == 0 || len(rows) == 0 {
		t.Fatal("the manifest carries no message rows")
	}
	return units, connections, rows
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// repoPath prefixes a manifest path with the repository root, which the test
// reaches from internal/codegen/jstable (the manifest's paths are repo-relative).
func repoPath(p string) string { return "../../../" + p }

// loadUnitPaths parses one unit's source files, the announce test's rule: a
// directory contributes its *.schema files, a named file contributes itself.
func loadUnitPaths(t *testing.T, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			ast, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path:  name,
				Name:  filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data,
				AST:   ast,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// loadSchemaString builds a unit from a schema written straight into a temp
// file, for a clause that needs a construct the manifest's message rows do not
// carry (an enum-keyed array, a by-value `type`).
func loadSchemaString(t *testing.T, name, content string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}

// ---- the three probes

func messageRoundTripProbe(u *ir.Unit, st *ir.Struct, rowName, announcement, batch string) string {
	var b strings.Builder
	b.WriteString(messageProbeHeader(u, st, announcement))
	fmt.Fprintf(&b, `
// LoadMessages reads one buffer into the caller's storage and allocates
// nothing; SaveMessages takes an array of bodies of one root and writes one
// buffer; MeasureMessages sizes the buffer (docs/SPEC-TABLES.md §3.3).
const storage = Array.from({ length: 256 }, () => new %[1]s());
const batch = new Uint8Array(readFileSync("./batch.bin"));
const loadReport = {};
const count = %[1]sLoadMessages(storage, v, batch, loadReport);
if (count !== 1 || loadReport.Verdict !== 0 || loadReport.Malformed) {
  fail("LoadMessages: count=" + count + " " + JSON.stringify(loadReport));
}
// "loading either form and saving the other reproduces the other's pinned
// bytes" (§3.3): the value the batch decodes to, saved as a batch, is the
// batch byte for byte.
const buf = new Uint8Array(%[2]d);
const saveReport = {};
const n = %[1]sSaveMessages([storage[0]], buf, saveReport);
if (n < 0 || saveReport.Verdict !== 0) { fail("SaveMessages: " + JSON.stringify(saveReport)); }
const saved = buf.subarray(0, n);
if (saved.length !== batch.length || !bytesEq(saved, batch)) {
  fail("SaveMessages bytes differ from the reference's batch");
}
const measureReport = {};
const m = %[1]sMeasureMessages([storage[0]], measureReport);
if (m !== batch.length || measureReport.Verdict !== 0) { fail("MeasureMessages: " + m + " != " + batch.length); }
`, st.Name, 65536)
	b.WriteString("\n")
	emitJSONChecks(&b, u, st, rowName, "storage[0]", "  ")
	b.WriteString(probeTail())
	return b.String()
}

func messageRefusalProbe(u *ir.Unit, st *ir.Struct, announcement string) string {
	var b strings.Builder
	b.WriteString(messageProbeHeader(u, st, announcement))
	fmt.Fprintf(&b, `
// "`+"`M`"+` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME" (§3.3): Measure
// and Save both refuse and return -1, with the reason on the report.
const many = Array.from({ length: 257 }, () => new %[1]s());
const wmReport = {};
if (%[1]sMeasureMessages(many, wmReport) !== -1 || wmReport.Verdict !== 1 || wmReport.Reason !== "batch_too_large") {
  fail("MeasureMessages over 256 bodies: " + JSON.stringify(wmReport));
}
const wsReport = {};
if (%[1]sSaveMessages(many, new Uint8Array(65536), wsReport) !== -1 || wsReport.Verdict !== 1 || wsReport.Reason !== "batch_too_large") {
  fail("SaveMessages over 256 bodies: " + JSON.stringify(wsReport));
}
// "`+"`M`"+` ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME"
// (§3.3): the count is read before any body, nothing is decoded, and the
// returned count is the wire's M.
const three = Array.from({ length: 3 }, () => new %[1]s());
const threeBuf = new Uint8Array(65536);
const threeSave = {};
const threeN = %[1]sSaveMessages(three, threeBuf, threeSave);
const threeBatch = threeBuf.subarray(0, threeN);
const shortStorage = Array.from({ length: 2 }, () => new %[1]s());
const shortReport = {};
const shortCount = %[1]sLoadMessages(shortStorage, v, threeBatch, shortReport);
if (shortCount !== 3 || shortReport.Verdict !== 1 || shortReport.Reason !== "batch_too_large" || shortReport.Malformed) {
  fail("LoadMessages above capacity: count=" + shortCount + " " + JSON.stringify(shortReport));
}
// "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME" (§3.3).
const unannounced = new TableVocabulary();
unannounced.Init(new Array(%[2]d));
const noReport = {};
const noCount = %[1]sLoadMessages(shortStorage, unannounced, threeBatch, noReport);
if (noCount !== -1 || noReport.Verdict !== 1 || noReport.Reason !== "no_vocabulary") {
  fail("LoadMessages with no vocabulary: " + JSON.stringify(noReport));
}
`, st.Name, len(ir.TableVocabulary(u)))
	b.WriteString(probeTail())
	return b.String()
}

func messageDamageProbe(u *ir.Unit, st *ir.Struct, announcement string) string {
	var b strings.Builder
	b.WriteString(messageProbeHeader(u, st, announcement))
	fmt.Fprintf(&b, `
const storage = Array.from({ length: 256 }, () => new %[1]s());
// DAMAGE ANYWHERE IN THE BATCH DELIVERS NO BODY (§4, "Damage discards the
// batch"): malformed counts and the load answers -1. Body 1 is an empty body,
// body 2 opens with a reference above the entry count E.
const damaged = new Uint8Array([2, 1, 0xc0, 0x0f]);
const dReport = {};
const dCount = %[1]sLoadMessages(storage, v, damaged, dReport);
if (dCount !== -1 || !dReport.Malformed || dReport.Verdict !== 0) {
  fail("damage inside body 2 delivered a body: count=" + dCount + " " + JSON.stringify(dReport));
}
// BYTES AFTER THE PAD ARE DAMAGE after the last body, and discard the batch too.
const padded = new Uint8Array([2, 0, 0, 0]);
const pReport = {};
const pCount = %[1]sLoadMessages(storage, v, padded, pReport);
if (!pReport.Malformed) { fail("bytes after the pad did not read as malformed: " + JSON.stringify(pReport)); }
if (pCount !== -1) { fail("bytes after the pad delivered a body: count=" + pCount); }
// "FORM BYTE `+"`2`"+` IS THE MESSAGE FORM" (§3.3): a byte this reader does not
// carry is a REFUSAL by name, never damage.
const newer = new Uint8Array([3, 0, 0]);
const nReport = {};
const nCount = %[1]sLoadMessages(storage, v, newer, nReport);
if (nCount !== -1 || nReport.Verdict !== 1 || nReport.Reason !== "newer_form" || nReport.Malformed) {
  fail("newer form: " + JSON.stringify(nReport));
}
// "message_form_as_file" is the FILE reader's answer to a batch handed where a
// file was expected (§3.3, §3.4).
const plan = %[1]sFixedNewPlan();
const fReport = {};
const fCount = %[1]sFixedLoad([new %[1]s()], 1, new Uint8Array([2, 0, 0]), 3, plan, fReport);
if (fCount !== -1 || fReport.refused !== 2 || TableFixedRefusalName(fReport.refused) !== "message_form_as_file") {
  fail("message form as file: " + JSON.stringify(fReport));
}
`, st.Name)
	b.WriteString(probeTail())
	return b.String()
}

// messageCompressedFloatProbe is the one row the conformance corpus does not
// reach: a fixed root carrying a quantized float, over the bench corpus's
// `TableMixed` (bench/corpus/BenchTable.schema). Its `server_time`, `aim_x`,
// `aim_y`, `aim_z` and `ping` all declare `resolution`, so their bodies hold
// shape `Packing === 2` and the read walks the compressed-float lane.
func messageCompressedFloatProbe(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	b.WriteString(messageProbeHeader(u, st, ""))
	fmt.Fprintf(&b, `
// "the message form quantizes and reads floats by the packet wire's rule bit
// for bit and there is one rule for both wires" (§3.3): a compressed float on
// the announced grid comes back the same float, so one message saved then
// loaded reproduces the message byte for byte.
const value = new %[1]s();
value.serverTime = 123.45;
value.aimX = 0.5;
value.aimY = -0.25;
value.aimZ = 0.75;
value.Ping = 37.5;
const buf = new Uint8Array(65536);
const saveReport = {};
const n = %[1]sSaveMessages([value], buf, saveReport);
if (n < 0 || saveReport.Verdict !== 0) { fail("SaveMessages: " + JSON.stringify(saveReport)); }
const storage = Array.from({ length: 256 }, () => new %[1]s());
const loadReport = {};
const count = %[1]sLoadMessages(storage, v, buf.subarray(0, n), loadReport);
if (count !== 1 || loadReport.Verdict !== 0 || loadReport.Malformed) {
  fail("LoadMessages: count=" + count + " " + JSON.stringify(loadReport));
}
// "file to message to file reproduces a compressed float only when the float
// already lies on the announced grid" (§3.3): these five are grid points, so
// the value the batch decodes to, saved as a batch, is the batch byte for byte.
const buf2 = new Uint8Array(65536);
const saveReport2 = {};
const n2 = %[1]sSaveMessages([storage[0]], buf2, saveReport2);
if (n2 !== n || !bytesEq(buf.subarray(0, n), buf2.subarray(0, n2))) {
  fail("compressed-float round trip bytes differ");
}
if (Math.abs(storage[0].serverTime - 123.45) > 0.02) { fail("ServerTime " + storage[0].serverTime); }
if (Math.abs(storage[0].aimX - 0.5) > 0.02) { fail("AimX " + storage[0].aimX); }
if (Math.abs(storage[0].aimY + 0.25) > 0.02) { fail("AimY " + storage[0].aimY); }
if (Math.abs(storage[0].aimZ - 0.75) > 0.02) { fail("AimZ " + storage[0].aimZ); }
if (Math.abs(storage[0].Ping - 37.5) > 0.02) { fail("Ping " + storage[0].Ping); }
`, st.Name)
	b.WriteString(probeTail())
	return b.String()
}

// messageKeyedArrayProbe is the clause the manifest's message rows do not
// reach: an enum-keyed array, over a minimal fixed root. It asserts the page's
// rule that "the body carries `(variant id, element)` pairs" (docs/SPEC-TABLES.md
// §3.2): the writer writes the number of present slots as the count and a key
// reference before each present element, so a save then load reproduces the
// value and the bytes.
func messageKeyedArrayProbe(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	b.WriteString(messageProbeHeader(u, st, ""))
	fmt.Fprintf(&b, `
// "`+"`N`"+` is the number of SLOTS PRESENT, not the array's extent"
// (docs/SPEC-TABLES.md §3.2): the two set slots ride under their variant's
// name, the default slot does not, and save/load round-trips byte for byte.
const value = new %[1]s();
value.slots[0] = 7;
value.slots[2] = 9;
const buf = new Uint8Array(65536);
const saveReport = {};
const n = %[1]sSaveMessages([value], buf, saveReport);
if (n < 0 || saveReport.Verdict !== 0) { fail("SaveMessages: " + JSON.stringify(saveReport)); }
const storage = Array.from({ length: 256 }, () => new %[1]s());
const loadReport = {};
const count = %[1]sLoadMessages(storage, v, buf.subarray(0, n), loadReport);
if (count !== 1 || loadReport.Verdict !== 0 || loadReport.Malformed) {
  fail("LoadMessages: count=" + count + " " + JSON.stringify(loadReport));
}
if (storage[0].slots[0] !== 7 || storage[0].slots[1] !== 0 || storage[0].slots[2] !== 9) {
  fail("keyed slots: " + JSON.stringify(Array.from(storage[0].slots)));
}
const buf2 = new Uint8Array(65536);
const n2 = %[1]sSaveMessages([storage[0]], buf2, {});
if (n2 !== n || !bytesEq(buf.subarray(0, n), buf2.subarray(0, n2))) {
  fail("keyed round trip bytes differ");
}
`, st.Name)
	b.WriteString(probeTail())
	return b.String()
}

// messageByValueTypeProbe is the clause the manifest's message rows do not
// reach: a fixed root holding a `type` by value. It asserts the page's rule
// that a fixed table's BY-VALUE CLOSURE reaches "a `type` held by value"
// (docs/SPEC-TABLES.md §2.2, §3.3): the nested type's body verbs ride beside
// the root's own, so a save then load reproduces the value inside it.
func messageByValueTypeProbe(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	b.WriteString(messageProbeHeader(u, st, ""))
	fmt.Fprintf(&b, `
const value = new %[1]s();
value.scores.points = 42;
const buf = new Uint8Array(65536);
const saveReport = {};
const n = %[1]sSaveMessages([value], buf, saveReport);
if (n < 0 || saveReport.Verdict !== 0) { fail("SaveMessages: " + JSON.stringify(saveReport)); }
const storage = Array.from({ length: 256 }, () => new %[1]s());
const loadReport = {};
const count = %[1]sLoadMessages(storage, v, buf.subarray(0, n), loadReport);
if (count !== 1 || loadReport.Verdict !== 0 || loadReport.Malformed) {
  fail("LoadMessages: count=" + count + " " + JSON.stringify(loadReport));
}
if (storage[0].scores.points !== 42) { fail("by-value type: " + storage[0].scores.points); }
`, st.Name)
	b.WriteString(probeTail())
	return b.String()
}

func messageProbeHeader(u *ir.Unit, st *ir.Struct, announcement string) string {
	home := messageHomeBase(u)
	return fmt.Sprintf(`import { readFileSync } from "node:fs";
import { TableVocabulary, AnnounceRead, TableMessageEntriesHere, TableFixedRefusal, TableFixedRefusalName, %[2]s } from "./%[1]s.js";
import { %[2]sMeasureMessages, %[2]sSaveMessages, %[2]sLoadMessages, %[2]sFixedNewPlan, %[2]sFixedLoad } from "./%[3]s.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
function bytesEq(a, b) {
  if (a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}
const v = new TableVocabulary();
v.Init(new Array(TableMessageEntriesHere));
const ann = new Uint8Array(readFileSync("./ann.bin"));
const announceReport = {};
if (!AnnounceRead(v, ann, announceReport)) { fail("AnnounceRead: " + JSON.stringify(announceReport)); }
`, home, st.Name, messageRootBase(u, st))
}

func probeTail() string {
	return `
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
}

// messageHomeBase is the basename of the runtime home module (docs/
// SPEC-TABLES.md §19.2): the one that carries the announcement surface.
func messageHomeBase(u *ir.Unit) string {
	for name, b := range generatedTree(u) {
		if strings.Contains(string(b), "TableMessageAnnouncement = new Uint8Array(") {
			return strings.TrimSuffix(name, ".js")
		}
	}
	panic("no home module")
}

// messageRootBase is the basename of the module that carries one root's
// message verbs, found by the verb the root emits.
func messageRootBase(u *ir.Unit, st *ir.Struct) string {
	for name, b := range generatedTree(u) {
		if strings.Contains(string(b), "export function "+st.Name+"MeasureMessages") {
			return strings.TrimSuffix(name, ".js")
		}
	}
	panic("no root module for " + st.Name)
}

// generatedTree emits the whole generated JavaScript tree once, the packet
// modules and the table modules, for finding the home and the root modules.
func generatedTree(u *ir.Unit) map[string][]byte {
	files, err := js.Generate(u)
	if err != nil {
		panic(err)
	}
	tables, err := Generate(u)
	if err != nil {
		panic(err)
	}
	maps.Copy(files, tables)
	return files
}

// jsRunMessageProbe writes the unit's whole generated tree plus `extra` files
// into one directory and runs `probe` there under the leg's own node.
func jsRunMessageProbe(t *testing.T, node string, u *ir.Unit, extra map[string][]byte, probe string) ([]byte, error) {
	t.Helper()
	files := generatedTree(u)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// ---- the JSON value comparison, generated from the manifest's json files ----

func emitJSONChecks(b *strings.Builder, u *ir.Unit, st *ir.Struct, rowName, expr, ind string) {
	path := filepath.Join("../../../testdata/conformance/tables/json", rowName+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		// The message row names the same instance as an `instance` row, whose
		// text is the value's (§3.3): "its text is the file-form vector's
		// byte for byte". A row without a json file has nothing to compare.
		return
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var node any
	if err := dec.Decode(&node); err != nil {
		return
	}
	b.WriteString("// \"the two forms are two encodings of one value\" (§3.3): the batch\n")
	b.WriteString("// decodes to the same value the file form's text names.\n")
	emitStructChecks(b, st, node, expr, ind)
}

func emitStructChecks(b *strings.Builder, st *ir.Struct, node any, expr, ind string) {
	obj, _ := node.(map[string]any)
	for _, f := range st.Fields {
		val, ok := obj[f.Name]
		if !ok {
			continue
		}
		member := expr + "." + js.MemberName(f.Name)
		emitFieldCheck(b, f, val, member, ind)
	}
}

func emitFieldCheck(b *strings.Builder, f *ir.Field, val any, member, ind string) {
	switch {
	case f.Type.Kind == ir.TString:
		s, _ := val.(string)
		raw := []byte(s)
		fmt.Fprintf(b, "%sif (%sLength !== %d) { fail(%q); }\n", ind, member, len(raw), member+".Length")
		fmt.Fprintf(b, "%sif (!bytesEq(%s.subarray(0, %sLength), [%s])) { fail(%q); }\n", ind, member, member, byteList(raw), member)
	case f.Type.Kind == ir.TBytes:
		s, _ := val.(string)
		raw, _ := base64.StdEncoding.DecodeString(s)
		fmt.Fprintf(b, "%sif (%sLength !== %d) { fail(%q); }\n", ind, member, len(raw), member+".Length")
		fmt.Fprintf(b, "%sif (!bytesEq(%s.subarray(0, %sLength), [%s])) { fail(%q); }\n", ind, member, member, byteList(raw), member)
	case f.Type.Kind == ir.TWString:
		s, _ := val.(string)
		var units []uint16
		for _, r := range s {
			units = append(units, uint16(r))
		}
		fmt.Fprintf(b, "%sif (%sLength !== %d) { fail(%q); }\n", ind, member, len(units), member+".Length")
	case enumRefOf(f) != nil:
		e := enumRefOf(f)
		s, _ := val.(string)
		ord := 0
		if s != "None" {
			for i, v := range e.Variants {
				if v == s {
					ord = i + 1
				}
			}
		}
		fmt.Fprintf(b, "%sif (%s !== %d) { fail(%q + \" = \" + %s); }\n", ind, member, ord, member, member)
	case isStructRef(f.Type):
		emitStructChecks(b, f.Type.Ref.(*ir.Struct), val, member, ind)
	case f.Array != ir.ArrayNone:
		arr, _ := val.([]any)
		for i, elem := range arr {
			emitFieldCheck(b, messageElementField(f), elem, member+"["+strconv.Itoa(i)+"]", ind)
		}
	default:
		lit := jsonScalarLit(f, val)
		if lit != "" {
			fmt.Fprintf(b, "%sif (%s !== %s) { fail(%q + \" = \" + %s); }\n", ind, member, lit, member, member)
		}
	}
}

func jsonScalarLit(f *ir.Field, val any) string {
	switch f.Type.Kind {
	case ir.TBool:
		if bv, ok := val.(bool); ok {
			return fmt.Sprintf("%t", bv)
		}
	case ir.TFloat32, ir.TFloat64:
		if n, ok := val.(json.Number); ok {
			return n.String()
		}
	}
	n, ok := val.(json.Number)
	if !ok {
		return ""
	}
	bigv, ok := new(big.Int).SetString(n.String(), 10)
	if !ok {
		return n.String()
	}
	if fixedIsBig(f.Type) {
		return bigv.String() + "n"
	}
	return bigv.String()
}

func byteList(raw []byte) string {
	parts := make([]string, 0, len(raw))
	for _, b := range raw {
		parts = append(parts, strconv.Itoa(int(b)))
	}
	return strings.Join(parts, ", ")
}
