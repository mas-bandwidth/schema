package compiler

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCTableKeyedSlotReadOnce holds docs/SPEC-TABLES.md §4 and §3.2 on the C
// target: a keyed body that names the same key twice is damage, whether the
// keyed field holds scalars or table elements and whether or not this reader
// names the key. The clean control is accepted.
func TestCTableKeyedSlotReadOnce(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe
enum Tier { Low, High }
table Layer { n uint32 }
table Root {
    flags [Tier]uint32
    banks [Tier]Layer
}
`)
	tier := u.Enums["Tier"]
	low := ir.TableWireId(tier.VariantWireNameOf("Low"))
	high := ir.TableWireId(tier.VariantWireNameOf("High"))
	unknown := ir.TableWireId("Other")
	root := u.Tables["Root"]
	layer := u.Tables["Layer"]

	scalar := func(v uint32) []byte {
		return binary.LittleEndian.AppendUint32(nil, v)
	}
	layerBody := func(w *keyedOnceBuilder, n uint32) []byte {
		field := w.ref(ir.TableFieldWireId(layer.Fields[0]))
		body := append([]byte(nil), field...)
		body = append(body, byte(ir.TableKindU32))
		body = binary.LittleEndian.AppendUint32(body, n)
		return append(body, 0)
	}
	makeWire := func(flagSlots func(*keyedOnceBuilder) []keyedOnceSlot, bankSlots func(*keyedOnceBuilder) []keyedOnceSlot) []byte {
		w := &keyedOnceBuilder{}
		flags := flagSlots(w)
		banks := bankSlots(w)
		w.keyed(root.Fields[0], ir.TableKindU32, flags)
		w.keyed(root.Fields[1], ir.TableKindTable, banks)
		return w.finish()
	}
	cleanFlags := func(w *keyedOnceBuilder) []keyedOnceSlot {
		return []keyedOnceSlot{{key: low, elem: scalar(1)}, {key: high, elem: scalar(2)}}
	}
	cleanBanks := func(w *keyedOnceBuilder) []keyedOnceSlot {
		return []keyedOnceSlot{{key: low, elem: layerBody(w, 3)}, {key: high, elem: layerBody(w, 4)}}
	}
	cases := []struct {
		name string
		wire []byte
		want bool
	}{
		{"control", makeWire(cleanFlags, cleanBanks), true},
		{"repeated_scalar_key", makeWire(func(w *keyedOnceBuilder) []keyedOnceSlot {
			return []keyedOnceSlot{{key: low, elem: scalar(1)}, {key: low, elem: scalar(2)}}
		}, cleanBanks), false},
		{"repeated_table_key", makeWire(cleanFlags, func(w *keyedOnceBuilder) []keyedOnceSlot {
			return []keyedOnceSlot{{key: low, elem: layerBody(w, 3)}, {key: low, elem: layerBody(w, 4)}}
		}), false},
		{"repeated_unnamed_key", makeWire(func(w *keyedOnceBuilder) []keyedOnceSlot {
			return []keyedOnceSlot{{key: low, elem: scalar(1)}, {key: unknown, elem: scalar(2)}, {key: unknown, elem: scalar(3)}}
		}, cleanBanks), false},
	}

	model := tabletext.NewModel(u)
	var wireList, wantList strings.Builder
	for i, tc := range cases {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(root), tc.wire, &report)
		if ok != tc.want || report.Refused != !tc.want || (tc.want && err != nil) || (!tc.want && err == nil) {
			t.Fatalf("%s: the engine answered ok=%v err=%v report=%+v, want %v", tc.name, ok, err, report, tc.want)
		}
		fmt.Fprintf(&wireList, "static const uint8_t wire_%d[]={%s};\n", i, cOctets(tc.wire))
		if i > 0 {
			wantList.WriteString(",")
		}
		if tc.want {
			wantList.WriteString("1")
		} else {
			wantList.WriteString("0")
		}
	}

	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
%s
static const int want[]={%s};
static int verdict(const uint8_t * wire, int64_t bytes)
{
    TableReport report; const Root * root; uint8_t * region; int64_t need;
    need = root_load_measure(wire, bytes);
    if (need <= 0) { return 0; }
    region = (uint8_t *) malloc((size_t) need);
    if (region == NULL) { return -1; }
    memset(&report, 0, sizeof(report));
    root = root_load(region, need, wire, bytes, &report);
    free(region);
    if (root == NULL) { return report.refused ? 0 : -1; }
    return 1;
}
int main(void)
{
    CHECK(verdict(wire_0,sizeof(wire_0))==want[0]);
    CHECK(verdict(wire_1,sizeof(wire_1))==want[1]);
    CHECK(verdict(wire_2,sizeof(wire_2))==want[2]);
    CHECK(verdict(wire_3,sizeof(wire_3))==want[3]);
    return 0;
}
`, wireList.String(), wantList.String()))
}

type keyedOnceSlot struct {
	key  uint64
	elem []byte
}

type keyedOnceBuilder struct {
	ids  []uint64
	body []byte
}

func keyedOnceLEB(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func (w *keyedOnceBuilder) ref(id uint64) []byte {
	for i, seen := range w.ids {
		if seen == id {
			return keyedOnceLEB(nil, uint64(i+1))
		}
	}
	w.ids = append(w.ids, id)
	return keyedOnceLEB(nil, uint64(len(w.ids)))
}

func (w *keyedOnceBuilder) keyed(f *ir.Field, elemKind int, slots []keyedOnceSlot) {
	fieldRef := w.ref(ir.TableFieldWireId(f))
	inner := []byte{byte(elemKind)}
	inner = keyedOnceLEB(inner, uint64(len(slots)))
	for _, s := range slots {
		inner = append(inner, w.ref(s.key)...)
		inner = keyedOnceLEB(inner, uint64(len(s.elem)))
		inner = append(inner, s.elem...)
	}
	w.body = append(w.body, fieldRef...)
	w.body = append(w.body, byte(ir.TableKindKeyed))
	w.body = keyedOnceLEB(w.body, uint64(len(inner)))
	w.body = append(w.body, inner...)
}

func (w *keyedOnceBuilder) finish() []byte {
	out := []byte{ir.TableWireForm}
	out = append(out, w.body...)
	out = append(out, 0)
	for _, id := range w.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(w.ids)))
}
