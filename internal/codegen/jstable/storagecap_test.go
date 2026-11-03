package jstable

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or node record of it costs two wire
// bytes, so 8191 of them measure under the 2^40-byte cap and 8192 measure past
// it: the list vectors reach the cap through the list term, the node vectors
// through the node records.
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

// Big's constructor throws in every probe below, so a load that built even one
// Big before refusing fails the probe instead of allocating 2^24 elements a
// record: the refusal is held to come before anything is materialized.
const (
	storageCapBigField    = "this.a = Array.from({ length: 16777216 }, () => 0n);"
	storageCapBigSentinel = `throw new Error("Big materialized");`
)

// TestStorageCapRefusedJavaScript: RootLoadMeasureReason answers a size in
// (0, 2^40] for each at vector and -1 with storage_over_cap for each over
// vector; RootLoad and RootLoadRetain refuse each over vector as
// storage_over_cap, answering null with no malformed event and no Big built. A
// load is never run on an at vector, which would build 2^40 bytes.
func TestStorageCapRefusedJavaScript(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated module)")
	if out, err := runStorageCapJS(t, nil); err != nil {
		t.Fatalf("storage cap probe: %v\n%s", err, out)
	}
}

// TestStorageCapJavaScriptNegativeControl removes each check in the generated
// module and requires the probe to go red where that check alone holds the
// vectors. Each sabotaged probe stops before a load once its measure has
// failed, and Big's sentinel stops a load that passes the gate, so no control
// allocates.
func TestStorageCapJavaScriptNegativeControl(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated module)")
	const (
		term  = "n > Math.floor((TableStorageCap - at) / fd.esz)"
		node  = `if (data > TableStorageCap) { return [-1, "storage_over_cap"]; }`
		final = `if (size > TableStorageCap) { return [-1, "storage_over_cap"]; }`
		gate  = `c.measure(bytes)[1] === "storage_over_cap"`
	)
	for _, c := range []struct {
		name  string
		edits [][2]string
		red   []string // labels the probe must print
		green []string // labels it must not print
	}{
		// the totals: the shared vectors' list term is exactly the cap, so
		// only a total refuses them; term_over.bin stays refused by its term
		{"totals", [][2]string{{node, ""}, {final, ""}}, []string{"list_over.bin measure", "nodes_over.bin measure"}, []string{"term_over.bin measure"}},
		// the term check as well: nothing refuses term_over.bin
		{"term", [][2]string{{term, "false"}, {node, ""}, {final, ""}}, []string{"term_over.bin measure"}, nil},
		// the load's gate: the load builds a Big
		{"gate", [][2]string{{gate, "false"}}, []string{"Big materialized"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := runStorageCapJS(t, func(src string) string {
				for _, e := range c.edits {
					if !strings.Contains(src, e[0]) {
						t.Fatalf("sabotage matched nothing: %q", e[0])
					}
					src = strings.ReplaceAll(src, e[0], e[1])
				}
				return src
			})
			if err == nil {
				t.Fatalf("sabotage %s stayed green:\n%s", c.name, out)
			}
			for _, label := range c.red {
				if !strings.Contains(string(out), label) {
					t.Errorf("control %s missed %q:\n%s", c.name, label, out)
				}
			}
			for _, label := range c.green {
				if strings.Contains(string(out), label) {
					t.Errorf("control %s also broke %q:\n%s", c.name, label, out)
				}
			}
		})
	}
}

// storageCapTermWire is a Root whose index map (one empty entry) comes before
// its items list of 8192 empty Big bodies: the list term starts past 0, so the
// term alone passes the cap and the term check refuses it before any total.
func storageCapTermWire(u *ir.Unit) []byte {
	root := u.Tables["Root"]
	ids := []uint64{ir.TableFieldWireId(root.Fields[2]), ir.TableFieldWireId(root.Fields[0])}
	framed := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	wire := append([]byte{1, 1, byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindTable), 1, 1, 0})...)
	list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, 8192)
	for range 8192 {
		list = append(list, 1, 0)
	}
	wire = append(append(wire, 2, byte(ir.TableKindArray)), framed(list)...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func runStorageCapJS(t *testing.T, edit func(string) string) ([]byte, error) {
	t.Helper()
	dir, err := filepath.Abs(storageCapDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadSchemaString(t, "Cap.schema", string(src))
	gen, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	text := string(gen[runtimeHome(u)+"Variable.js"])
	if !strings.Contains(text, storageCapBigField) {
		t.Fatal("the variable module holds no Big field to guard")
	}
	text = strings.Replace(text, storageCapBigField, storageCapBigSentinel, 1)
	if edit != nil {
		text = edit(text)
	}
	work := t.TempDir()
	for name, b := range map[string][]byte{"module.js": []byte(text), "term_over.bin": storageCapTermWire(u), "probe.mjs": []byte(strings.ReplaceAll(storageCapJSProbe, "DIR", dir))} {
		if err := os.WriteFile(filepath.Join(work, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(jsNode(t), "probe.mjs")
	cmd.Dir = work
	return cmd.CombinedOutput()
}

const storageCapJSProbe = `import { readFileSync } from "node:fs";
import * as m from "./module.js";
let failures = 0;
function fail(why) { console.log("FAIL: " + why); failures++; }
const read = (name) => new Uint8Array(readFileSync(name.startsWith("term") ? name : "DIR/" + name));
const capBytes = 2 ** 40;
for (const name of ["list_at.bin", "nodes_at.bin"]) {
  const wire = read(name);
  const [size, why] = m.RootLoadMeasureReason(wire);
  if (!(size > 0 && size <= capBytes && why === null && m.RootLoadMeasure(wire) === size)) fail(name + " measure " + size + " " + why);
}
for (const name of ["list_over.bin", "nodes_over.bin", "term_over.bin"]) {
  const wire = read(name);
  const [size, why] = m.RootLoadMeasureReason(wire);
  if (size !== -1 || why !== "storage_over_cap" || m.RootLoadMeasure(wire) !== -1) { fail(name + " measure " + size + " " + why); continue; }
  const r = new m.TableVariableReport();
  r.unknown = 3;
  if (m.RootLoad(wire, r) !== null || !r.refused || r.reason !== "storage_over_cap" || r.malformed || r.unknown !== 3) fail(name + " load " + JSON.stringify(r));
  const keep = new m.TableVariableRetain(new Uint8Array(1024), new Array(64));
  const kr = new m.TableVariableReport();
  if (m.RootLoadRetain(wire, keep, kr) !== null || !kr.refused || kr.reason !== "storage_over_cap" || kr.malformed || keep.used !== 0 || keep.count !== 0) fail(name + " retained load " + JSON.stringify(kr));
  // a refusal does not outlive its read
  const clean = m.RootLoad(new Uint8Array([1, 0, 0, 0, 0, 0, 0, 0, 0, 0]), r);
  if (clean === null || r.refused || r.reason !== null || r.malformed) fail(name + " refusal cleared by the next read " + JSON.stringify(r));
}
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
