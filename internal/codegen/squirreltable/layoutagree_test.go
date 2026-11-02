package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Squirrel has one reflective block/cook reader, not a second generated
// accessor. Hold its emitted layout to the compiler's layout model for
// every record of every manifest unit, including fields absent from fixtures.
// The probe reads every field's bytes at the emitted offsets in seeded storage;
// the oracle addresses the same storage through ir.RecordLayout/ir.Blocks.
const squirrelLayoutProbe = `local S=dofile("unit/schema_module.nut",true);
local T=S.require(MODULE);
local record=function(kind,name,d) {
 print(kind+" "+name+" "+d.size+" "+d.align+"\n");
 local bytes=blob(d.size);for(local i=0;i<d.size;i++)bytes[i]=(i*37+11)&255;
 foreach(f in d.fields) {
  local sum=0; local inbounds=f.offset>=0 && f.size>=0 && f.offset<=d.size && f.size<=d.size-f.offset;
  if(inbounds)for(local i=0;i<f.size;i++)sum=(sum*31+(bytes[f.offset+i]&255))&0x7fffffff;
  print(kind+" "+name+"."+f.name+" "+f.offset+" "+f.size+" "+(inbounds?sum:-1)+"\n");
 }
};
foreach(name,d in T.cookForms)record("cook",name,d);
foreach(name,d in T.blockRecords)record("block",name,d);
foreach(name,d in T.blockForms) {
 record("projection",name,d.projection);
 foreach(a in d.arrays)print("array "+name+"."+a.name+" "+a.offsetOfOffset+" "+a.countOffset+" "+a.strideOffset+" "+a.stride+" "+a.max+" "+a.align+"\n");
}
`

func squirrelLayoutWant(u *ir.Unit) []string {
	var lines []string
	record := func(kind, name string, l *ir.MemberLayout) {
		lines = append(lines, fmt.Sprintf("%s %s %d %d", kind, name, l.Size, l.Align))
		for _, f := range l.Fields {
			sum := int64(0)
			for i := int64(0); i < f.Size; i++ {
				sum = (sum*31 + ((f.Offset+i)*37+11)&255) & 0x7fffffff
			}
			lines = append(lines, fmt.Sprintf("%s %s.%s %d %d %d", kind, name, f.Field.Name, f.Offset, f.Size, sum))
		}
	}
	for name := range ir.TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st != nil {
			record("cook", name, ir.RecordLayout(u, st))
		}
	}
	if b := ir.Blocks(u); b != nil {
		for _, name := range b.Order {
			if l := b.Layout(name); l != nil {
				record("block", name, l)
			}
		}
		for _, f := range b.Tables {
			record("projection", f.Table.Name, &f.Projection)
			for _, a := range f.Arrays {
				align := max(int64(ir.BlockAlign), a.ElemAlign())
				lines = append(lines, fmt.Sprintf("array %s.%s %d %d %d %d %d %d", f.Table.Name, a.Field.Name, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride, a.Max, align))
			}
		}
	}
	sort.Strings(lines)
	return lines
}

// mutate names an actual generated descriptor field, scoped to one form.
type squirrelLayoutMutation struct {
	section, record, field string
	delta                  int64
}

func squirrelLayoutLines(t *testing.T, u *ir.Unit, mutate *squirrelLayoutMutation) []string {
	t.Helper()
	dir := t.TempDir()
	mod := emitUnit(t, dir, "unit", u)
	if mutate != nil {
		path := filepath.Join(dir, "unit", mod+".nut")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		start := strings.Index(src, "_M."+mutate.section+" <- {")
		if start < 0 {
			t.Fatal("descriptor section absent")
		}
		at := strings.Index(src[start:], "name = "+fmt.Sprintf("%q", mutate.record)+",")
		if at < 0 {
			t.Fatal("descriptor record absent")
		}
		at += start
		recordEnd := strings.Index(src[at:], "\n  ] },")
		if recordEnd < 0 {
			t.Fatal("descriptor record end absent")
		}
		field := strings.Index(src[at:at+recordEnd], "{ name = "+fmt.Sprintf("%q", mutate.field)+", offset = ")
		if field < 0 {
			t.Fatal("descriptor field absent")
		}
		field += at
		end := strings.Index(src[field:], "\n")
		if end < 0 {
			t.Fatal("descriptor line absent")
		}
		end += field
		line := src[field:end]
		var offset int64
		rest := strings.SplitN(line, ", offset = ", 2)[1]
		if _, err := fmt.Sscanf(rest, "%d", &offset); err != nil {
			t.Fatal(err)
		}
		replacement := strings.Replace(line, fmt.Sprintf("offset = %d,", offset), fmt.Sprintf("offset = %d,", offset+mutate.delta), 1)
		writeFile(t, path, []byte(src[:field]+replacement+src[end:]))
	}
	program := strings.Replace(squirrelLayoutProbe, "MODULE", fmt.Sprintf("%q", mod), 1)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(program))
	lines := strings.Split(strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil))), "\n")
	sort.Strings(lines)
	return lines
}
func squirrelLayoutChanges(want, got []string) []string {
	expected := map[string]string{}
	actual := map[string]string{}
	for _, line := range want {
		f := strings.Fields(line)
		expected[f[0]+" "+f[1]] = line
	}
	for _, line := range got {
		f := strings.Fields(line)
		actual[f[0]+" "+f[1]] = line
	}
	var changed []string
	for key, line := range expected {
		if actual[key] != line {
			changed = append(changed, key+": got "+actual[key]+", want "+line)
		}
		delete(actual, key)
	}
	for key, line := range actual {
		changed = append(changed, key+": unexpected "+line)
	}
	sort.Strings(changed)
	return changed
}
func TestSquirrelDescriptorLayoutAgreement(t *testing.T) {
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, entry := range manifestUnits(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt")) {
		t.Run(entry.key, func(t *testing.T) {
			u := loadUnit(t, root, entry.paths)
			want := squirrelLayoutWant(u)
			total += len(want)
			for _, bad := range squirrelLayoutChanges(want, squirrelLayoutLines(t, u, nil)) {
				t.Error(bad)
			}
		})
	}
	if total < 100 {
		t.Fatalf("only %d layout lines: gate is blind", total)
	}
	t.Logf("%d record, field and array lines agree", total)
}
func TestSquirrelDescriptorLayoutNegativeControl(t *testing.T) {
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path     string
		mutation squirrelLayoutMutation
	}{
		{"tables/block", squirrelLayoutMutation{"blockRecords", "RenderCamera", "camera_id", 4}},
		{"tables/pointers", squirrelLayoutMutation{"cookForms", "TreeNode", "left", 8}},
	} {
		t.Run(c.mutation.record+"."+c.mutation.field, func(t *testing.T) {
			u := loadUnit(t, root, []string{c.path})
			want := squirrelLayoutWant(u)
			if bad := squirrelLayoutChanges(want, squirrelLayoutLines(t, u, nil)); len(bad) != 0 {
				t.Fatalf("baseline: %v", bad)
			}
			changed := squirrelLayoutChanges(want, squirrelLayoutLines(t, u, &c.mutation))
			if len(changed) != 1 || !strings.Contains(changed[0], c.mutation.record+"."+c.mutation.field+":") {
				t.Fatalf("offset plant changed %v", changed)
			}
		})
	}
}
