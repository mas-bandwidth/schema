package javatable

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

type idOrderCase struct {
	name string
	wire []byte
	read bool
}

func idOrderWire(body []byte, ids ...uint64) []byte {
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}
func idOrderCases(t *testing.T) []idOrderCase {
	scalar := []byte{4, 7, 0, 0, 0}
	field := append(append([]byte{1}, scalar...), 0)
	rows := []idOrderCase{
		{"field", idOrderWire(field, 101), true},
		{"unused", idOrderWire(field, 101, 999), false},
		{"field_order", idOrderWire(append(append(append([]byte{2}, scalar...), append([]byte{1}, scalar...)...), 0), 101, 202), false},
		{"enum", idOrderWire([]byte{1, 30, 2, 0}, 101, 202), true},
		{"enum_order", idOrderWire([]byte{1, 30, 3, 0}, 101, 202, 303), false},
		{"enum_array", idOrderWire([]byte{1, 14, 4, 30, 2, 2, 2, 0}, 101, 202), true},
		{"enum_array_order", idOrderWire([]byte{1, 14, 4, 30, 2, 3, 3, 0}, 101, 202, 303), false},
		{"union", idOrderWire([]byte{1, 15, 2, 4, 4, 7, 0, 0, 0, 0}, 101, 202), true},
		{"union_order", idOrderWire([]byte{1, 15, 3, 4, 4, 7, 0, 0, 0, 0}, 101, 202, 303), false},
		{"union_array", idOrderWire([]byte{1, 14, 9, 15, 1, 2, 4, 4, 7, 0, 0, 0, 0}, 101, 202), true},
		{"union_array_order", idOrderWire([]byte{1, 14, 9, 15, 1, 3, 4, 4, 7, 0, 0, 0, 0}, 101, 202, 303), false},
		{"keyed", idOrderWire([]byte{1, 16, 8, 4, 1, 2, 4, 7, 0, 0, 0, 0}, 101, 202), true},
		{"keyed_order", idOrderWire([]byte{1, 16, 8, 4, 1, 3, 4, 7, 0, 0, 0, 0}, 101, 202, 303), false},
		{"nested", idOrderWire([]byte{1, 13, 7, 2, 4, 7, 0, 0, 0, 0, 0}, 101, 202), true},
		{"nested_order", idOrderWire([]byte{1, 13, 7, 3, 4, 7, 0, 0, 0, 0, 0}, 101, 202, 303), false},
		{"node", idOrderWire([]byte{1, 17, 2, 2, 12, 10, 1, 3, 7, 4, 4, 7, 0, 0, 0, 0, 0}, 101, ^uint64(0), 303, 404), true},
		{"node_order", idOrderWire([]byte{1, 17, 2, 2, 12, 10, 1, 4, 7, 5, 4, 7, 0, 0, 0, 0, 0}, 101, ^uint64(0), 303, 404, 505), false},
		{"node_body_order", idOrderWire([]byte{1, 17, 2, 2, 12, 10, 1, 3, 7, 5, 4, 7, 0, 0, 0, 0, 0}, 101, ^uint64(0), 303, 404, 505), false},
		{"reused_in_nested_body", idOrderWire([]byte{1, 13, 7, 2, 4, 7, 0, 0, 0, 0, 3, 30, 2, 0}, 101, 202, 303), true},
	}
	root, _ := filepath.Abs(filepath.Join("..", "..", ".."))
	pin, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing/frame_unreferenced_entry.bin"))
	if err != nil {
		t.Fatal(err)
	}
	rows = append(rows, idOrderCase{"shared_unused", pin, false})
	return rows
}

const idOrderDriver = `import java.nio.file.*;
public final class IdOrderDriver {
  public static void main(String[] a) throws Exception {
    for (int i=0; i<a.length; i+=2) {
      byte[] wire=Files.readAllBytes(Paths.get(a[i+1]));
      for (boolean keep: new boolean[]{false,true}) {
        graphdemo.TableVariable.Report r=new graphdemo.TableVariable.Report();
        graphdemo.TableRetain store=new graphdemo.TableRetain(65536,1024);
        graphdemo.SceneVariable.Value v=keep ? graphdemo.SceneRetain.loadRetain(wire,store,r) : graphdemo.SceneVariable.load(wire,r);
        String result = v != null && !r.refused && !r.damaged ? "read" : v == null && r.damaged ? "damaged" : r.refused ? "refused:" + r.reason.split(":", 2)[0] : "other";
        if (v == null && keep && (store.used != 0 || store.idUsed != 0)) result="retained";
        System.out.println(a[i]+" "+(keep?"retain":"plain")+" "+result);
      }
    }
  }
}`

func javaIdOrderAnswers(t *testing.T, mutate bool) map[string]string {
	t.Helper()
	javac, java := javaToolsRequired(t)
	root, _ := filepath.Abs(filepath.Join("..", "..", ".."))
	unit := unitFromDir(t, filepath.Join(root, "tables/pointers"))
	model := tabletext.NewModel(unit)
	rows := idOrderCases(t)
	files := generateJavaUnitFiles(t, filepath.Join(root, "tables/pointers"))
	if mutate {
		src := string(files["TableVariable.java"])
		anchor := "if (firstUseBad || nextRef - 1 != ids.length)"
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("negative control anchor missing: %d", strings.Count(src, anchor))
		}
		files["TableVariable.java"] = []byte(strings.Replace(src, anchor, "if (false)", 1))
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.Mkdir(classes, 0755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for n, data := range files {
		if !strings.HasSuffix(n, ".java") {
			continue
		}
		p := filepath.Join(dir, filepath.Base(n))
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := filepath.Join(dir, "IdOrderDriver.java")
	if err := os.WriteFile(driver, []byte(idOrderDriver), 0644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	if out, err := testproc.Command(t, javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	args := []string{"-ea", "-cp", classes, "IdOrderDriver"}
	for _, row := range rows {
		for _, retain := range []bool{false, true} {
			r := new(tabletext.Report)
			var ok bool
			var err error
			if retain {
				ok, err = tablewire.DecodeRetain(model, model.New(unit.Tables["Scene"]), row.wire, &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}, r)
			} else {
				ok, err = tablewire.Decode(model, model.New(unit.Tables["Scene"]), row.wire, r)
			}
			if err != nil && !tablewire.ReadRefusal(err) {
				t.Fatalf("%s oracle: %v", row.name, err)
			}
			if got := ok && !r.Refused; got != row.read {
				t.Fatalf("%s oracle read=%v, want %v: %+v %v", row.name, got, row.read, r, err)
			}
		}
		p := filepath.Join(dir, row.name+".bin")
		if err := os.WriteFile(p, row.wire, 0644); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.name, p)
	}
	// A count cap stops the wire walk: unused IDs after that point must not
	// replace the existing refusal. A complete walk can judge unused IDs even
	// when it met a non-finite float first.
	priority := []struct {
		name         string
		wire         []byte
		oracle, java string
	}{
		{"count_then_unused", idOrderWire([]byte{1, 14, 6, 4, 0x80, 0x80, 0x80, 0x80, 8, 0}, 101, 999), "count_over_extent_cap", "refused:count_over_extent_cap"},
		{"float_then_unused", idOrderWire([]byte{1, 10, 0, 0, 0x80, 0x7f, 0}, 101, 999), "wire_damaged", "damaged"},
		{"float_then_count", idOrderWire([]byte{1, 10, 0, 0, 0x80, 0x7f, 2, 14, 6, 4, 0x80, 0x80, 0x80, 0x80, 8, 0}, 101, 202, 999), "count_over_extent_cap", "refused:non_finite_float"},
	}
	for _, row := range priority {
		for _, retain := range []bool{false, true} {
			r := new(tabletext.Report)
			var ok bool
			var err error
			if retain {
				ok, err = tablewire.DecodeRetain(model, model.New(unit.Tables["Scene"]), row.wire, &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}, r)
			} else {
				ok, err = tablewire.Decode(model, model.New(unit.Tables["Scene"]), row.wire, r)
			}
			if ok || tablewire.RefusalReason(err) != row.oracle {
				t.Fatalf("%s oracle: ok=%v reason=%q err=%v", row.name, ok, tablewire.RefusalReason(err), err)
			}
		}
		p := filepath.Join(dir, row.name+".bin")
		if err := os.WriteFile(p, row.wire, 0644); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.name, p)
	}
	out, err := testproc.Command(t, java, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	answers := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("bad verdict %q", line)
		}
		answers[f[0]+" "+f[1]] = f[2]
	}
	if len(answers) != 2*(len(rows)+len(priority)) {
		t.Fatalf("missing answers: %s", out)
	}
	for _, row := range priority {
		want := row.java
		// The planted fault intentionally makes the completed, noncanonical
		// float case reach the existing non-finite refusal.
		if mutate && row.name == "float_then_unused" {
			want = "refused:non_finite_float"
		}
		for _, mode := range []string{"plain", "retain"} {
			if got := answers[row.name+" "+mode]; got != want {
				t.Errorf("%s %s: got %s want %s", row.name, mode, got, want)
			}
		}
	}
	return answers
}
func TestJavaTransportIDFirstUse(t *testing.T) {
	slowtest.Gate(t, "generated Java transport ID order")
	got := javaIdOrderAnswers(t, false)
	for _, row := range idOrderCases(t) {
		for _, mode := range []string{"plain", "retain"} {
			want := "damaged"
			if row.read {
				want = "read"
			}
			key := row.name + " " + mode
			if got[key] != want {
				t.Errorf("%s: got %s want %s", key, got[key], want)
			}
		}
	}
}
func TestJavaTransportIDNegativeControl(t *testing.T) {
	slowtest.Gate(t, "generated Java transport ID negative control")
	got := javaIdOrderAnswers(t, true)
	for _, row := range idOrderCases(t) {
		for _, mode := range []string{"plain", "retain"} {
			want := "read"
			key := row.name + " " + mode
			if got[key] != want {
				t.Errorf("disabled first-use check %s: got %s want %s", key, got[key], want)
			}
		}
	}
}
