package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A FIXED VALUE CARRIED INTO THE VARIABLE VALUE REPLACES THE BORN LIST. A
// [A..B]T list is born holding A elements (docs/SPEC.md §4.2), so fromFixed,
// which fills a fresh variable value from a fixed record, places the record's
// elements over the born ones and never after them: a record with three
// octets gives a list of three, and the text writer accepts it.
const fromFixedBornSchema = `package probe

fixed table Rec
{
    a int32
}

fixed table Box
{
    octets [2..8]uint8
    recs   [1..4]Rec
    tail   [..4]int16
}
`

const fromFixedBornDriver = `package probe;

public final class Driver {
    public static void main(String[] args) {
        BoxFixed.Value fixed = new BoxFixed.Value();
        fixed.octetsCount = 3;
        fixed.octets[0] = 1;
        fixed.octets[1] = 2;
        fixed.octets[2] = 3;
        fixed.recsCount = 2;
        fixed.recs[0].a = 7;
        fixed.recs[1].a = 8;
        fixed.tailCount = 1;
        fixed.tail[0] = 9;
        BoxVariable.Value v = BoxVariable.fromFixed(fixed);
        StringBuilder recs = new StringBuilder();
        for (RecVariable.Value r : v.recs) { recs.append(r.a).append(' '); }
        System.out.println("octets=" + v.octets + " recs=" + recs.toString().trim() + " tail=" + v.tail
                + " text=" + (BoxVariable.toJson(v) == null ? "refused" : "written"));
        // the fixed value at its own birth: the minimum of each, once
        BoxVariable.Value born = BoxVariable.fromFixed(new BoxFixed.Value());
        System.out.println("born octets=" + born.octets.size() + " recs=" + born.recs.size() + " tail=" + born.tail.size()
                + " text=" + (BoxVariable.toJson(born) == null ? "refused" : "written"));
    }
}
`

func TestJavaFromFixedReplacesTheBornList(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, fromFixedBornSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	// the emitted fromFixed clears each counted list before it fills it
	variable := string(files["BoxVariable.java"])
	at := strings.Index(variable, "public static Value fromFixed(")
	if at < 0 {
		t.Fatal("BoxVariable.java has no fromFixed")
	}
	body := variable[at:]
	for _, member := range []string{"octets", "recs", "tail"} {
		clear := strings.Index(body, "v."+member+".clear();")
		add := strings.Index(body, "v."+member+".add(")
		if clear < 0 || add < 0 || clear > add {
			t.Errorf("fromFixed does not clear %s before filling it", member)
		}
	}

	t.Run("jdk", func(t *testing.T) {
		slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
		javac, java := javaTools(t)
		packet, err := javacodegen.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range packet {
			if _, ok := files[name]; !ok {
				files[name] = data
			}
		}
		files["Driver.java"] = []byte(fromFixedBornDriver)
		dir := t.TempDir()
		var sources []string
		for name, data := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".java") {
				sources = append(sources, p)
			}
		}
		sort.Strings(sources)
		args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		out, err := exec.Command(java, "-ea", "-cp", dir, u.Package+".Driver").CombinedOutput()
		if err != nil {
			t.Fatalf("java: %v\n%s", err, out)
		}
		want := "octets=[1, 2, 3] recs=7 8 tail=[9] text=written\nborn octets=2 recs=1 tail=0 text=written\n"
		if string(out) != want {
			t.Errorf("fromFixed:\ngot  %q\nwant %q", out, want)
		}
	})
}
