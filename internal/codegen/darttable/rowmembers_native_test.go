package darttable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestCookRowCursorMembersNativeDart(t *testing.T) {
	slowtest.Gate(t, "dart")
	for _, variable := range []bool{false, true} {
		t.Run(fmt.Sprint(variable), func(t *testing.T) {
			schema := `package probe
fixed table Cursor { line int32 }
fixed table Edit {
 at Cursor
 view int32
 row_size int32
 origin int32
}
`
			if variable {
				schema = strings.Replace(schema, "fixed table Edit", "table Edit", 1)
				schema = strings.Replace(schema, " origin int32", " origin int32\n enabled bool\n if enabled { extra int32 }", 1)
			}
			u := unitFrom(t, schema)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packetFiles(t, u))
			model := tabletext.NewModel(u)
			value := model.New(u.Tables["Edit"])
			if !model.Read(value, []byte(`{"at":{"line":7},"view":41,"row_size":42,"origin":43}`), &tabletext.Report{}) {
				t.Fatal("fixture JSON")
			}
			var b strings.Builder
			b.WriteString("import 'ProbeFixed.dart' as f;\nimport 'ProbeCook.dart' as c;\nimport 'ProbeBlock.dart' as b;\n")
			b.WriteString(dartClosurePrelude)
			b.WriteString(`
 final value=f.Edit();f.initEdit(value);
 value.at.line=7;value.view=41;value.rowSize=42;value.origin=43;
`)
			for _, big := range []bool{false, true} {
				pin, err := tablecook.Cook(model, value, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				order := "Endian.little"
				if big {
					order = "Endian.big"
				}
				fmt.Fprintf(&b, `{
 final size=c.EditCook.cookMeasure(value);
 final output=Uint8List(size+8)..fillRange(0,size+8,0xa5);
 final report=c.TableCookWriteReport();
 check(c.EditCook.cook(value,output,%s,report:report)&&report.reason=='ok','cursor cook');
 check(hex(output.sublist(0,size))==%q,'independent cursor bytes');
 check(output.skip(size).every((x)=>x==0xa5),'bounded cursor cook output');
 final opened=c.EditCook.openWithReason(output,0,size);
`, order, hex.EncodeToString(pin))
				if !big {
					b.WriteString(`
 check(opened.$1!=null,'native-order cursor cook opens');
 final cook=opened.$1!;
 final row=cook.cursor();
 check(identical(cook.root(row),row),'root reuses supplied cursor');
 final nested=b.CursorRow(cook.view,0);
 check(identical(row.at$(nested),nested),'at field reuses supplied cursor');
 check(nested.line==7&&row.view$==41&&row.rowSize$==42&&row.origin==43,'escaped accessors read values');
 nested.view.setInt32(nested.at,99,Endian.little);
 check(row.at$(nested).line==99,'cursor reads consumer-owned bytes');
`)
				} else {
					b.WriteString("check(opened.$1==null&&opened.$2==b.TableRefuseReason.foreignOrder,'foreign order refusal');\n")
				}
				b.WriteString("}\n")
			}
			b.WriteString("}\n")
			source := b.String()
			if variable {
				source = strings.Replace(source, "final value=f.Edit();f.initEdit(value);", "final value=w.Edit();", 1)
				source = strings.ReplaceAll(source, "b.CursorRow(", "c.CursorRow(")
			}
			runFormsProbe(t, files, source)
		})
	}
}
