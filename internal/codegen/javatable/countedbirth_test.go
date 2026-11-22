package javatable

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save, on the message form and in
// the JSON text, and rides at any other count or with one live element. The
// Java writer's bytes are held to the engine's, case by case.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const countedMatrixSchema = `package birth

type Rec
{
    n int32 = 55
    on bool
}

fixed table FixI32 { few [2..4]int32 }
fixed table FixRec { few [2..4]Rec }
fixed table FixF64 { few [2..4]float64 }
table VarI32 { few [2..4]int32 }
table VarRec { few [2..4]Rec }
table VarF64 { few [2..4]float64 }
`

// countedMatrixCase names a case, the engine's text for it, and the Java
// statements that place the same value on the fixed and the variable class.
type countedMatrixCase struct {
	name, fix, vari, text, placeFix, placeVar string
}

func countedMatrixCases() []countedMatrixCase {
	return []countedMatrixCase{
		{"fresh", "FixI32", "VarI32", "", "", ""},
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`, "v.few[0] = 0; v.few[1] = 0;", "v.few.set(0, 0); v.few.set(1, 0);"},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`, "v.few[1] = 7;", "v.few.set(1, 7);"},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`, "v.fewCount = 3;", "v.few.add(0);"},
		{"fresh", "FixRec", "VarRec", "", "", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`, "v.few[0] = new RecFixed.Value(); v.few[1] = new RecFixed.Value();", "v.few.set(0, new RecVariable.Value()); v.few.set(1, new RecVariable.Value());"},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`, "v.few[1].n = 56;", "v.few.get(1).n = 56;"},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`, "v.fewCount = 3;", "v.few.add(new RecVariable.Value());"},
		{"fresh", "FixF64", "VarF64", "", "", ""},
		{"min_default", "FixF64", "VarF64", `{"few":[0,0]}`, "v.few[0] = 0.0; v.few[1] = 0.0;", "v.few.set(0, 0.0); v.few.set(1, 0.0);"},
		{"min_negzero", "FixF64", "VarF64", `{"few":[0,-0.0]}`, "v.few[1] = -0.0;", "v.few.set(1, -0.0);"},
	}
}

func TestJavaCountedArrayBornAtItsMinimum(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	javac, java := javaTools(t)
	unit := unitFrom(t, countedMatrixSchema)
	model := tabletext.NewModel(unit)
	place := func(root, text string) *tabletext.Instance {
		inst := model.New(model.Lookup(root))
		if text != "" {
			var rep tabletext.Report
			if !model.Read(inst, []byte(text), &rep) {
				t.Fatalf("%s: the engine refused %s: %+v", root, text, rep)
			}
		}
		return inst
	}
	var p strings.Builder
	p.WriteString(countedMatrixDriverHead)
	want := map[string]string{}
	for _, c := range countedMatrixCases() {
		key := c.fix + "_" + c.name
		vkey := c.vari + "_" + c.name
		fmt.Fprintf(&p, "        { %sFixed.Value v = new %sFixed.Value(); %s\n", c.fix, c.fix, c.placeFix)
		fmt.Fprintf(&p, "          msg(%q, %sMessage.measureMessages(new %sFixed.Value[] { v }, 1, new TableVocabulary.Report()), (n, b) -> %sMessage.saveMessages(new %sFixed.Value[] { v }, 1, b, new TableVocabulary.Report())); }\n", key, c.fix, c.fix, c.fix, c.fix)
		fmt.Fprintf(&p, "        { %sVariable.Value v = new %sVariable.Value(); %s var(%q, %sVariable.save(v)); json(%q, %sVariable.toJson(v)); }\n", c.vari, c.vari, c.placeVar, vkey, c.vari, vkey, c.vari)
		msg, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place(c.fix, c.text)})
		if err != nil {
			t.Fatal(err)
		}
		variable, err := tablewire.EncodeVariable(model, place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		text, err := model.Write(place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		want[key+" msg"] = hex.EncodeToString(msg)
		want[vkey+" var"] = hex.EncodeToString(variable)
		want[vkey+" json"] = hex.EncodeToString(bytes.TrimRight(text, "\n"))
	}
	p.WriteString("    }\n}\n")
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	files["Driver.java"] = []byte(p.String())
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, path)
		}
	}
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, java, "-ea", "-cp", dir, "birth.Driver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			// the JSON writer ends its text with a newline; the text's
			// content is what the engine's is held to
			got[f[0]+" "+f[1]] = f[2]
			if f[1] == "json" {
				got[f[0]+" "+f[1]] = strings.TrimSuffix(f[2], "0a")
			}
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: java wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}

const countedMatrixDriverHead = `package birth;

import java.util.function.BiFunction;

public final class Driver {
    static String hex(byte[] b, int n) {
        final StringBuilder s = new StringBuilder();
        for (int i = 0; i < n; i++) { s.append(String.format("%02x", b[i] & 0xff)); }
        return s.toString();
    }

    static void var(String key, byte[] b) {
        System.out.println(key + " var " + (b == null ? "refused" : hex(b, b.length)));
    }

    static void json(String key, byte[] b) {
        System.out.println(key + " json " + (b == null ? "refused" : hex(b, b.length)));
    }

    static void msg(String key, int measured, BiFunction<Integer, byte[], Integer> save) {
        if (measured < 0) { System.out.println(key + " msg refused"); return; }
        final byte[] b = new byte[measured];
        final int wrote = save.apply(measured, b);
        System.out.println(key + " msg " + (wrote != measured ? "refused" : hex(b, measured)));
    }

    public static void main(String[] args) {
`
