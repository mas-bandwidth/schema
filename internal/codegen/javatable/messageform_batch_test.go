package javatable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runBatchJava runs all generated fixed value types beside a batch probe. It
// uses the same Java toolchain helper as the announcement tests, not a mock.
func runBatchJava(t *testing.T, u *ir.Unit, main string) { t.Helper(); runMessageJava(t, u, main) }

func TestMessageFormBatchJava(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("2.1-reference-batches", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
		if err != nil {
			t.Fatal(err)
		}
		dirs := map[string]string{"backend_conn": "tables/backend", "vocab_conn": "tables/vocab", "vocab9_conn": "tables/vocab9"}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 6 || fields[0] != "message" {
				continue
			}
			dir, ok := dirs[fields[2]]
			if !ok {
				if fields[1] == "graph_tree" && fields[2] == "graph_conn" {
					t.Run(fields[1], func(t *testing.T) {
						t.Parallel()
						t.Skip("unresolved: graph_tree is a plain table, but this card restricts the message surface to fixed roots")
					})
					continue
				}
				t.Fatalf("message row %s has an unmapped connection %s", fields[1], fields[2])
			}
			t.Run(fields[1], func(t *testing.T) {
				t.Parallel()
				u := loadUnitDir(t, root, dir)
				wire, err := os.ReadFile(filepath.Join(root, fields[5]))
				if err != nil {
					t.Fatal(err)
				}
				expected := messageOracleImage(t, u, u.Tables[fields[3]], filepath.Join(root, fields[4]))
				cls := fixedClass(fields[3])
				runBatchJava(t, u, fmt.Sprintf(`
    public static void main(String[] args) {
        TableVocabulary v=new TableVocabulary();v.reset(4096);
        TableVocabulary.Report a=new TableVocabulary.Report();
        if(!TableVocabulary.announceRead(v,TableVocabulary.tableAnnouncement,a))fail("announcement");
        byte[] wire=fromHex(%q); byte[] image=new byte[%s.bodyBytes];
        %s.Value[] values={new %s.Value()};TableVocabulary.Report r=new TableVocabulary.Report();
        // SPEC-TABLES §3.3: LoadMessages gives the batch's values, and SaveMessages reproduces the pinned bytes.
        int count=TableVocabulary.%sLoadMessages(values,1,wire,v,image,r);
        if(count!=1||r.malformed||r.refused)fail("reference load "+count+" "+r.reason);
        byte[] observed=new byte[%s.bodyBytes];%s.writeBody(observed,0,values[0]);
        if(!hex(observed).equals(%q))fail("form-1 value differs");
        int n=TableVocabulary.%sMeasureMessages(values,count,image,new TableVocabulary.Report());
        if(n!=wire.length)fail("measure "+n+" != "+wire.length);
        byte[] out=new byte[n];if(TableVocabulary.%sSaveMessages(values,count,out,image,new TableVocabulary.Report())!=n||!java.util.Arrays.equals(out,wire))fail("reference bytes");
    }
`, hexOf(wire), cls, cls, cls, javaName(fields[3]), cls, cls, expected, javaName(fields[3]), javaName(fields[3])))
			})
		}
	})

}

// messageOracleImage decodes the independent form-1 pin with the Go reference
// codec and projects those values into the declared fixed storage image. It
// never decodes the message pin, so read/save symmetry cannot hide wrong values.
func messageOracleImage(t *testing.T, u *ir.Unit, st *ir.Struct, path string) string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if bytes.Contains(data, []byte("github.com/mas-bandwidth/serialize.go")) || strings.HasSuffix(name, "View.go") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf("package %s\nimport(\"encoding/binary\";\"encoding/hex\";\"fmt\";\"math\";\"os\";\"testing\")\nfunc TestOracle(t *testing.T){t.Parallel();data,err:=os.ReadFile(%q);if err!=nil{t.Fatal(err)};var want %s;var report TableReport;if !%sLoad(&want,data,&report){t.Fatalf(\"form-1 read: %%+v\",report)};image:=make([]byte,%d);_ = binary.LittleEndian;_ = math.Float32bits;\n%s\nfmt.Println(\"IMAGE=\"+hex.EncodeToString(image))}\n", u.Package, path, st.Name, st.Name, fixedTypeBytes(st), oracleImageStores(st, "want", "0", 0))
	if err := os.WriteFile(filepath.Join(dir, "oracle_test.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-v", "-count=1", "-timeout", "600s", "-run", "TestOracle")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("form-1 oracle: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "IMAGE=") {
			return strings.TrimPrefix(line, "IMAGE=")
		}
	}
	t.Fatalf("no form-1 image: %s", out)
	return ""
}

func oracleImageStores(st *ir.Struct, value, base string, depth int) string {
	var b strings.Builder
	var off int64
	for _, f := range st.Fields {
		at := fmt.Sprintf("(%s+%d)", base, off)
		expr := value + "." + ir.GoExportName(f.Name)
		if f.Type.Optional {
			fmt.Fprintf(&b, "if %sPresent {image[%s]=1};\n", expr, at)
			at = "(" + at + "+1)"
		}
		slots := f.ArrayBound
		if f.Array == ir.ArrayCounted {
			fmt.Fprintf(&b, "binary.LittleEndian.PutUint32(image[%s:],uint32(%sCount));\n", at, expr)
			at = "(" + at + "+4)"
		}
		if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
			fmt.Fprintf(&b, "binary.LittleEndian.PutUint32(image[%s:],uint32(%sLength));copy(image[%s+4:],%s[:]);\n", at, expr, at, expr)
		} else if slots > 0 {
			idx := fmt.Sprintf("i%d", depth)
			fmt.Fprintf(&b, "for %s:=0;%s<%d;%s++ {\n%s}\n", idx, idx, slots, idx, oracleElementStore(f, expr+"["+idx+"]", fmt.Sprintf("(%s+%s*%d)", at, idx, fixedElementBytes(f)), depth+1))
		} else {
			b.WriteString(oracleElementStore(f, expr, at, depth+1))
		}
		off += fixedFieldBytes(f)
	}
	return b.String()
}
func oracleElementStore(f *ir.Field, expr, at string, depth int) string {
	if child, ok := f.Type.Ref.(*ir.Struct); ok {
		return oracleImageStores(child, expr, at, depth)
	}
	width := fixedElementBytes(f)
	cast := "uint64(" + expr + ")"
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		if f.Type.Kind == ir.TFloat32 {
			cast = "uint64(math.Float32bits(" + expr + "))"
		} else {
			cast = "math.Float64bits(" + expr + ")"
		}
	}
	if f.Type.Kind == ir.TBool {
		return fmt.Sprintf("if %s {image[%s]=1}\n", expr, at)
	}
	if width == 1 {
		return fmt.Sprintf("image[%s]=byte(%s);\n", at, expr)
	}
	return fmt.Sprintf("binary.LittleEndian.PutUint%d(image[%s:],uint%d(%s));\n", width*8, at, width*8, cast)
}
