package darttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// integrityRuntime is shared source, emitted in each independent runtime home.
// The lookup table uses the reflected Castagnoli polynomial (SPEC §2.14).
func integrityRuntime(u *ir.Unit) string {
	if len(ir.TableIntegrityTables(u)) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("const tableIntegrityTable = <int>[\n")
	for i := range 256 {
		c := uint32(i)
		for range 8 {
			if c&1 != 0 {
				c = (c >> 1) ^ 0x82f63b78
			} else {
				c >>= 1
			}
		}
		fmt.Fprintf(&b, "0x%08x,", c)
		if i%8 == 7 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("];\n")
	b.WriteString(`
int tableIntegrityCRC(List<int> data,int length) {
 var c=0xffffffff;
 for(var i=0;i<length;i++){c=(c>>8)^tableIntegrityTable[(c^data[i])&255];}
 return c^0xffffffff;
}
bool tableIntegrityValid(Uint8List data,int length,int minimum) {
 if(length<minimum||length>data.length)return false;
 final at=length-4;
 final expected=data[at]|(data[at+1]<<8)|(data[at+2]<<16)|(data[at+3]<<24);
 return tableIntegrityCRC(data,at)==expected;
}
void tableIntegrityWrite(Uint8List data,int at) {
 final crc=tableIntegrityCRC(data,at);
 for(var i=0;i<4;i++){data[at+i]=(crc>>(8*i))&255;}
}
Uint8List tableIntegritySeal(List<int> data) {
 final out=Uint8List(data.length+4)..setRange(0,data.length,data);
 tableIntegrityWrite(out,data.length);
 return out;
}
`)
	return b.String()
}

func (g *wireGen) emitIntegrityRead(st *ir.Struct, report, empty string) {
	if st.Integrity == "" {
		return
	}
	g.pf("\tif (data.isNotEmpty && data[0] == 1) {\n")
	g.pf("\t\tif (!tableIntegrityValid(data, data.length, 5)) { %s.refuse('wire_damaged'); %s.integrityMismatch = true; return %s; }\n", report, report, empty)
	g.pf("\t\tdata = Uint8List.sublistView(data, 0, data.length - 4);\n\t}\n")
}
