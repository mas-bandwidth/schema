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

// cTableKeyedSlot writes one keyed slot: a key reference, the element's L and
// its framed body.
func cTableKeyedSlot(elemKind byte, elem []byte) []byte {
	out := []byte{elemKind, 1}
	out = binary.AppendUvarint(out, 1)
	out = binary.AppendUvarint(out, uint64(len(elem)))
	return append(out, elem...)
}

// cTableKeyedChain is a keyed body that counts one level and, when deeper,
// holds a keyed element that counts the rest.
func cTableKeyedChain(levels int) []byte {
	if levels <= 1 {
		return []byte{byte(ir.TableKindKeyed), 0}
	}
	return cTableKeyedSlot(byte(ir.TableKindKeyed), cTableKeyedChain(levels-1))
}

// cTableBodyWithKeyed is a table body that counts one level and, when deeper,
// holds an unknown keyed field that counts the rest.
func cTableBodyWithKeyed(levels int) []byte {
	if levels <= 1 {
		return []byte{0}
	}
	keyed := cTableKeyedTableChain(levels - 1)
	out := []byte{1, byte(ir.TableKindKeyed)}
	out = binary.AppendUvarint(out, uint64(len(keyed)))
	out = append(out, keyed...)
	return append(out, 0)
}

// cTableKeyedTableChain alternates a keyed body with a table element, so each
// structural payload costs one level.
func cTableKeyedTableChain(levels int) []byte {
	if levels <= 1 {
		return []byte{byte(ir.TableKindTable), 0}
	}
	return cTableKeyedSlot(byte(ir.TableKindTable), cTableBodyWithKeyed(levels-1))
}

// cTableKeyedFile frames a keyed body as the root's one unknown field.
func cTableKeyedFile(body []byte) []byte {
	out := []byte{ir.TableWireForm, 1, byte(ir.TableKindKeyed)}
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, 0x1234567890abcdef)
	return binary.LittleEndian.AppendUint64(out, 1)
}

// TestCTableSkipCountsKeyedNesting holds docs/SPEC-TABLES.md §4's one nesting
// bound on the C unknown-field skip: every structural payload the walk enters
// is one level, including a keyed body and a keyed element that is itself a
// keyed body or a table body. A chain of 128 framed payloads reads and one of
// 129 refuses, with the same verdict the engine gives.
func TestCTableSkipCountsKeyedNesting(t *testing.T) {
	t.Parallel()
	const source = `package probe
table Root { z int32 }
`
	u := unitFromSource(t, source)
	model := tabletext.NewModel(u)
	root := model.Lookup("Root")
	cases := []struct {
		name   string
		body   []byte
		levels int
	}{
		{"keyed of keyed at 128", cTableKeyedChain(ir.TableBinaryMaxDepth), ir.TableBinaryMaxDepth},
		{"keyed of keyed at 129", cTableKeyedChain(ir.TableBinaryMaxDepth + 1), ir.TableBinaryMaxDepth + 1},
		{"keyed of table at 128", cTableKeyedTableChain(ir.TableBinaryMaxDepth), ir.TableBinaryMaxDepth},
		{"keyed of table at 129", cTableKeyedTableChain(ir.TableBinaryMaxDepth + 1), ir.TableBinaryMaxDepth + 1},
	}
	files := make([][]byte, len(cases))
	for i, c := range cases {
		files[i] = cTableKeyedFile(c.body)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(root), files[i], &report)
		want := c.levels <= ir.TableBinaryMaxDepth
		if ok != want {
			t.Fatalf("%s: engine verdict ok=%v err=%v %+v, want ok=%v", c.name, ok, err, report, want)
		}
		if want && report.Unknown != 1 {
			t.Fatalf("%s: engine skipped %d unknown fields, want 1", c.name, report.Unknown)
		}
	}
	var arrays, checks strings.Builder
	for i, c := range cases {
		want := c.levels <= ir.TableBinaryMaxDepth
		fmt.Fprintf(&arrays, "static const uint8_t c%d[]={%s};\n", i, cOctets(files[i]))
		if want {
			fmt.Fprintf(&checks, ` memset(&report,0,sizeof(report));
 root=root_load(region,sizeof(region),c%d,sizeof(c%d),&report);
 if (root==NULL || report.refused || report.unknown!=1) { fprintf(stderr,"case %d accepted: root=%%p refused=%%d unknown=%%d\n",(void*)root,report.refused,report.unknown); return %d; }
`, i, i, i, i+1)
		} else {
			fmt.Fprintf(&checks, ` memset(&report,0,sizeof(report));
 root=root_load(region,sizeof(region),c%d,sizeof(c%d),&report);
 if (root!=NULL || !report.refused) { fprintf(stderr,"case %d not refused: root=%%p refused=%%d\n",(void*)root,report.refused); return %d; }
`, i, i, i, i+1)
		}
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
static uint8_t region[16384];
%s
int main(void) {
 const Root * root; TableReport report;
%s return 0;
}
`, arrays.String(), checks.String()))
}
