package rusttable

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const rustShortBodiesSchema = `package shortbodies
enum Slot {
    alpha
    beta
}
table Leaf {
    n int32 = 0
    next *Leaf
}
union Choice {
    xs [..4]int32
    raw bytes(4)
}
table Root {
    xs []int32
    opt_xs ?[..4]int32
    raw bytes(4)
    opt_fixed ?[2]int32
    keyed [Slot]int32
    lookup map[int32]int32
    choice Choice
    head *Leaf
    blob *bytes
    text *string
}
`

type shortBodyVector struct {
	name string
	wire []byte
	want bool
}

func buildShortBodiesAndUnreachableVectors(t *testing.T, u *ir.Unit) []shortBodyVector {
	t.Helper()
	m := tabletext.NewModel(u)

	makeWire := func(build func(ref func(uint64) []byte) []byte) []byte {
		var ids []uint64
		ref := func(id uint64) []byte {
			for i, v := range ids {
				if v == id {
					return binary.AppendUvarint(nil, uint64(i+1))
				}
			}
			ids = append(ids, id)
			return binary.AppendUvarint(nil, uint64(len(ids)))
		}
		// Prepend a valid unknown scalar field so retaining loads must clear
		// captured records when a later short body or unreachable node refuses.
		unk := append(append(ref(ir.TableWireId("unknown_prefix")), byte(ir.TableKindI32)), 7, 0, 0, 0)
		body := append(unk, build(ref)...)
		body = append(body, 0)
		wire := append([]byte{1}, body...)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}

	framed := func(ref func(uint64) []byte, name string, kind byte, payload []byte) []byte {
		out := append(ref(ir.TableWireId(name)), kind)
		out = binary.AppendUvarint(out, uint64(len(payload)))
		return append(out, payload...)
	}

	ptrField := func(ref func(uint64) []byte, name string, index uint64) []byte {
		out := append(ref(ir.TableWireId(name)), byte(ir.TableKindPointer))
		return binary.AppendUvarint(out, index)
	}

	nodeTable := func(ref func(uint64) []byte, recs ...struct {
		tid  uint64
		body []byte
	}) []byte {
		nt := binary.AppendUvarint(nil, uint64(len(recs)))
		for _, r := range recs {
			nt = append(nt, ref(r.tid)...)
			nt = binary.AppendUvarint(nt, uint64(len(r.body)))
			nt = append(nt, r.body...)
		}
		out := append(ref(math.MaxUint64), byte(ir.TableKindString))
		out = binary.AppendUvarint(out, uint64(len(nt)))
		return append(out, nt...)
	}

	leafTID := ir.TableWireId(u.Tables["Leaf"].WireName())
	strangerTID := uint64(0x0bad0bad0bad0bad)

	rawVectors := []shortBodyVector{
		{
			name: "valid-empty",
			wire: makeWire(func(ref func(uint64) []byte) []byte { return nil }),
			want: true,
		},
		{
			name: "valid-reachable-nodes",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				var b []byte
				b = append(b, framed(ref, "xs", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32), 1, 5, 0, 0, 0})...)
				b = append(b, framed(ref, "opt_xs", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32), 0})...)
				b = append(b, framed(ref, "raw", byte(ir.TableKindArray), []byte{byte(ir.TableKindU8), 1, 0x2a})...)
				b = append(b, framed(ref, "opt_fixed", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32), 2, 1, 0, 0, 0, 2, 0, 0, 0})...)
				b = append(b, framed(ref, "keyed", byte(ir.TableKindKeyed), []byte{byte(ir.TableKindI32), 0})...)
				b = append(b, framed(ref, "lookup", byte(ir.TableKindArray), []byte{byte(ir.TableKindTable), 0})...)
				b = append(b, ptrField(ref, "head", 2)...)
				b = append(b, ptrField(ref, "blob", 3)...)
				b = append(b, ptrField(ref, "text", 4)...)
				b = append(b, nodeTable(ref,
					struct {
						tid  uint64
						body []byte
					}{leafTID, []byte{0}},
					struct {
						tid  uint64
						body []byte
					}{ir.BytesWireTypeId, []byte{1, 2}},
					struct {
						tid  uint64
						body []byte
					}{ir.StringWireTypeId, []byte("ok")},
				)...)
				return b
			}),
			want: true,
		},
		{
			name: "valid-unknown-pointer-reaches-leaf",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				var b []byte
				b = append(b, ptrField(ref, "unknown_ptr", 2)...)
				b = append(b, nodeTable(ref, struct {
					tid  uint64
					body []byte
				}{leafTID, []byte{0}})...)
				return b
			}),
			want: true,
		},
		{
			name: "valid-pointer-to-unnameable-record",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				var b []byte
				b = append(b, ptrField(ref, "head", 2)...)
				b = append(b, nodeTable(ref, struct {
					tid  uint64
					body []byte
				}{strangerTID, []byte{0}})...)
				return b
			}),
			want: true,
		},
		{
			name: "short-array-0",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "xs", byte(ir.TableKindArray), nil)
			}),
			want: false,
		},
		{
			name: "short-array-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "xs", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
			}),
			want: false,
		},
		{
			name: "short-opt-array-0",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "opt_xs", byte(ir.TableKindArray), nil)
			}),
			want: false,
		},
		{
			name: "short-opt-array-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "opt_xs", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
			}),
			want: false,
		},
		{
			name: "short-bytes-0",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "raw", byte(ir.TableKindArray), nil)
			}),
			want: false,
		},
		{
			name: "short-bytes-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "raw", byte(ir.TableKindArray), []byte{byte(ir.TableKindU8)})
			}),
			want: false,
		},
		{
			name: "short-opt-fixed-0",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "opt_fixed", byte(ir.TableKindArray), nil)
			}),
			want: false,
		},
		{
			name: "short-opt-fixed-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "opt_fixed", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
			}),
			want: false,
		},
		{
			name: "short-keyed-0",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "keyed", byte(ir.TableKindKeyed), nil)
			}),
			want: false,
		},
		{
			name: "short-keyed-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "keyed", byte(ir.TableKindKeyed), []byte{byte(ir.TableKindI32)})
			}),
			want: false,
		},
		{
			name: "short-map-0",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "lookup", byte(ir.TableKindArray), nil)
			}),
			want: false,
		},
		{
			name: "short-map-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "lookup", byte(ir.TableKindArray), []byte{byte(ir.TableKindTable)})
			}),
			want: false,
		},
		{
			name: "short-union-array-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				arm := append(ref(ir.TableWireId("xs")), byte(ir.TableKindArray), 1, byte(ir.TableKindI32))
				return append(append(ref(ir.TableWireId("choice")), byte(ir.TableKindUnion)), arm...)
			}),
			want: false,
		},
		{
			name: "short-union-bytes-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				arm := append(ref(ir.TableWireId("raw")), byte(ir.TableKindArray), 1, byte(ir.TableKindU8))
				return append(append(ref(ir.TableWireId("choice")), byte(ir.TableKindUnion)), arm...)
			}),
			want: false,
		},
		{
			name: "short-unknown-array-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "unknown_arr", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
			}),
			want: false,
		},
		{
			name: "short-unknown-keyed-1",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return framed(ref, "unknown_keyed", byte(ir.TableKindKeyed), []byte{byte(ir.TableKindI32)})
			}),
			want: false,
		},
		{
			name: "unreachable-table-node",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return nodeTable(ref, struct {
					tid  uint64
					body []byte
				}{leafTID, []byte{0}})
			}),
			want: false,
		},
		{
			name: "unreachable-chained-nodes",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				next3 := append(ptrField(ref, "next", 3), 0)
				return nodeTable(ref,
					struct {
						tid  uint64
						body []byte
					}{leafTID, next3},
					struct {
						tid  uint64
						body []byte
					}{leafTID, []byte{0}},
				)
			}),
			want: false,
		},
		{
			name: "unreachable-blob-bytes",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return nodeTable(ref, struct {
					tid  uint64
					body []byte
				}{ir.BytesWireTypeId, []byte{1, 2, 3}})
			}),
			want: false,
		},
		{
			name: "unreachable-blob-string",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return nodeTable(ref, struct {
					tid  uint64
					body []byte
				}{ir.StringWireTypeId, []byte("unreferenced")})
			}),
			want: false,
		},
		{
			name: "unreachable-unknown-record",
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return nodeTable(ref, struct {
					tid  uint64
					body []byte
				}{strangerTID, []byte{0}})
			}),
			want: false,
		},
	}

	for _, tc := range rawVectors {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(u.Tables["Root"]), tc.wire, &rep)
		if ok != tc.want {
			t.Fatalf("oracle verdict mismatch on %s: got ok=%v err=%v, want %v", tc.name, ok, err, tc.want)
		}
		if !tc.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("oracle refusal on %s is not a ReadRefusal: %v", tc.name, err)
		}
	}
	return rawVectors
}

func runRustShortBodiesProbe(t *testing.T, edit func(string) string) (string, error) {
	t.Helper()
	u := unitFrom(t, rustShortBodiesSchema)
	vectors := buildShortBodiesAndUnreachableVectors(t, u)

	var body strings.Builder
	body.WriteString("let mut failed = false;\n")
	for _, tc := range vectors {
		fmt.Fprintf(&body, `{
    let wire: &[u8] = &[%s];
    for retaining in [false, true] {
        let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::with_capacity(256) };
        let mut report = TableReport::default();
        let value = if retaining { root_load_retain(wire, &mut store, &mut report) } else { root_load(wire, &mut report) };
        let want = %t;
        if value.is_some() != want || (!want && !(report.damaged || report.refused)) || (want && (report.damaged || report.refused)) {
            println!("FAIL: %s (retaining={retaining}): some={} damaged={} refused={}", value.is_some(), report.damaged, report.refused);
            failed = true;
        }
        if !want && (!store.bytes.is_empty() || !store.ids.is_empty()) {
            println!("FAIL: %s (retaining={retaining}): store not cleared on refusal");
            failed = true;
        }
    }
}
`, rustBytes(tc.wire), tc.want, tc.name, tc.name)
	}
	body.WriteString("if failed { std::process::exit(1); }\nprintln!(\"SHORT_BODIES_AND_UNREACHABLE_OK\");\n")

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		patched := 0
		for name, data := range files {
			if strings.HasSuffix(name, "_table.rs") {
				s := string(data)
				ns := edit(s)
				if ns != s {
					files[name] = []byte(ns)
					patched++
				}
			}
		}
		if patched == 0 {
			t.Fatal("negative control edit matched nothing in generated _table.rs files")
		}
	}
	return runRustGeneratedFilesProbe(t, files, body.String())
}

func TestRustShortBodiesAndUnreachableNodesRefuse(t *testing.T) {
	slowtest.Gate(t, "rustc (short collection bodies and unreachable nodes refuse)")
	keyedRequireRustc(t)
	out, err := runRustShortBodiesProbe(t, nil)
	if err != nil || !strings.Contains(out, "SHORT_BODIES_AND_UNREACHABLE_OK") {
		t.Fatalf("short bodies and unreachable nodes probe failed: %v\n%s", err, out)
	}
}

func TestRustShortBodiesAndUnreachableNodesRefuseNegativeControl(t *testing.T) {
	slowtest.Gate(t, "rustc (negative control for short collection bodies and unreachable nodes)")
	keyedRequireRustc(t)

	// 1. Sabotage the short-collection-body refusal in the generated artifact.
	outShort, errShort := runRustShortBodiesProbe(t, func(src string) string {
		return strings.ReplaceAll(src, "} else { report.damaged = true; }", "} else {} // SABOTAGED")
	})
	if errShort == nil {
		t.Fatalf("probe stayed green when short-collection-body refusal was sabotaged:\n%s", outShort)
	}
	for _, wantFail := range []string{"short-array-0", "short-array-1", "short-bytes-0", "short-keyed-0", "short-map-0"} {
		if !strings.Contains(outShort, "FAIL: "+wantFail) {
			t.Errorf("short-body sabotage did not fail on %s:\n%s", wantFail, outShort)
		}
	}

	// 2. Sabotage the unreachable-node refusal in the generated artifact.
	const unreachSig = "if !report.cycle_on_save && nodes.has_unreachable(&edges) { report.damaged = true; return None; }"
	outUnreach, errUnreach := runRustShortBodiesProbe(t, func(src string) string {
		return strings.ReplaceAll(src, unreachSig, "if false && !report.cycle_on_save && nodes.has_unreachable(&edges) { report.damaged = true; return None; } // SABOTAGED")
	})
	if errUnreach == nil {
		t.Fatalf("probe stayed green when unreachable-node refusal was sabotaged:\n%s", outUnreach)
	}
	for _, wantFail := range []string{"unreachable-table-node", "unreachable-chained-nodes", "unreachable-blob-bytes", "unreachable-blob-string", "unreachable-unknown-record"} {
		if !strings.Contains(outUnreach, "FAIL: "+wantFail) {
			t.Errorf("unreachable-node sabotage did not fail on %s:\n%s", wantFail, outUnreach)
		}
	}
}
