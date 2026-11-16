package darttable

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func runDartWireIdFirstUse(t *testing.T, sabotage bool) ([]byte, error) {
	t.Helper()
	u := unitFrom(t, wireLebSchema)
	ids := []uint64{0x1111111111111111, 0x2222222222222222}
	cases := []struct {
		name string
		body []byte
		ids  []uint64
		want bool
	}{
		{"empty", []byte{0}, nil, false},
		{"unused", []byte{0}, ids[:1], true},
		{"unknown-field", []byte{1, 2, 7, 0}, ids[:1], false},
		{"out-of-order", []byte{2, 2, 7, 1, 2, 8, 0}, ids, true},
		{"nonfinite-unused", []byte{1, 10, 0, 0, 192, 127, 0}, ids, true},
		{"nested", []byte{1, 13, 4, 2, 2, 7, 0, 0}, ids, false},
		{"enum", []byte{1, 30, 2, 0}, ids, false},
		{"union", []byte{1, 15, 2, 2, 1, 7, 0}, ids, false},
		{"keyed", []byte{1, 16, 5, 2, 1, 2, 1, 7, 0}, ids, false},
		{"enum-array", []byte{1, 14, 3, 30, 1, 2, 0}, ids, false},
	}
	nodeIDs := []uint64{ir.TableWireId("child"), ir.TableNodeWireId, ir.TableWireId("Child"), ir.TableWireId("x")}
	nodeBody := []byte{1, 17, 2, 2, 12, 14, 1, 3, 11, 4, 9, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	cases = append(cases, struct {
		name string
		body []byte
		ids  []uint64
		want bool
	}{"node-type", nodeBody, nodeIDs, false})
	original := append([]struct {
		name string
		body []byte
		ids  []uint64
		want bool
	}{}, cases[5:]...)
	for _, c := range original {
		cases = append(cases, struct {
			name string
			body []byte
			ids  []uint64
			want bool
		}{c.name + "-unused", c.body, append(append([]uint64{}, c.ids...), 0x3333333333333333), true})
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	if sabotage {
		const anchor = "  bool wireIdOrderBad() {\n"
		found := false
		for name, data := range files {
			src := string(data)
			if strings.Contains(src, anchor) {
				files[name] = []byte(strings.Replace(src, anchor, anchor+"    return false; // SABOTAGED\n", 1))
				found = true
			}
		}
		if !found {
			t.Fatal("ID-order control anchor absent")
		}
	}
	m := tabletext.NewModel(u)
	var script strings.Builder
	script.WriteString(dartClosurePrelude)
	for _, c := range cases {
		wire := formOneWire(c.body, c.ids)
		var report tabletext.Report
		ok, err := tablewire.DecodeVariable(m, m.New(u.Tables["Root"]), wire, &report)
		refused := !ok || err != nil || report.Refused
		if refused != c.want {
			t.Fatalf("%s: Go refused=%t err=%v report=%+v", c.name, refused, err, report)
		}
		extra := ""
		if c.want {
			extra = "check(r.unknown==0 && rr.unknown==0,'refusal counters changed');"
		}
		fmt.Fprintf(&script, `{final b=bytes(%q);final r=w.TableWireReport();w.rootWireLoad(b,r);check(r.refused==%t,'%s plain');final rr=w.TableWireReport();final store=w.TableRetain(4096,List<int>.filled(512,0));w.rootWireLoadRetain(b,store,rr);check(rr.refused==%t,'%s retain');%s}`+"\n", hex.EncodeToString(wire), c.want, c.name, c.want, c.name, extra)
	}
	seed := formOneWire([]byte{1, 2, 7, 0}, ids[:1])
	unused := formOneWire([]byte{0}, ids[:1])
	fmt.Fprintf(&script, `{final store=w.TableRetain(4096,List<int>.filled(512,0));final good=w.TableWireReport();w.rootWireLoadRetain(bytes(%q),store,good);check(!good.refused && store.used>0,'retained seed');final bad=w.TableWireReport();w.rootWireLoadRetain(bytes(%q),store,bad);check(bad.refused && store.used==0 && store.idUsed==0,'retained reset');}`+"\n", hex.EncodeToString(seed), hex.EncodeToString(unused))
	script.WriteString("}\n")
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(script.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestDartWireIdFirstUseFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Dart generated plain and retaining readers")
	if out, err := runDartWireIdFirstUse(t, false); err != nil {
		t.Fatalf("ID order probe: %v\n%s", err, out)
	}
}

func TestDartWireIdFirstUseNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Dart generated plain and retaining readers")
	out, err := runDartWireIdFirstUse(t, true)
	if err == nil || !strings.Contains(string(out), "unused plain") {
		t.Fatalf("ID-order sabotage escaped: %v\n%s", err, out)
	}
}

// An ID census must defer oversized array counts to the ordinary count refusal.
func TestDartWireIdFirstUseCountLimit(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Dart generated count-limit probe")
	u := unitFrom(t, `package probe
table Root { values []int32 }`)
	wire := formOneWire([]byte{1, 14, 6, 4, 0x80, 0x80, 0x80, 0x80, 8, 0}, []uint64{ir.TableWireId("values")})
	voidWire := formOneWire([]byte{1, 14, 6, 32, 0xff, 0xff, 0xff, 0xff, 7, 0}, []uint64{ir.TableWireId("values")})
	m := tabletext.NewModel(u)
	var voidReport tabletext.Report
	voidOK, voidErr := tablewire.DecodeVariable(m, m.New(u.Tables["Root"]), voidWire, &voidReport)
	if voidOK && voidErr == nil && !voidReport.Refused {
		t.Fatalf("Go accepted invalid kind 32: %+v", voidReport)
	}
	t.Logf("Go kind 32 refusal: err=%v report=%+v", voidErr, voidReport)
	var report tabletext.Report
	_, err := tablewire.DecodeVariable(m, m.New(u.Tables["Root"]), wire, &report)
	var count *tablewire.CountRefusal
	if !errors.As(err, &count) {
		t.Fatalf("Go count oracle: %v report=%+v", err, report)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	script := fmt.Sprintf(dartClosurePrelude+`final b=bytes(%q);final scan=w.TableWireReader(b,b.length);check(!scan.wireIdOrderBad(),'census must defer count');final z=bytes(%q);check(!w.TableWireReader(z,z.length).wireIdOrderBad(),'census must defer invalid zero-width kind');final zr=w.TableWireReport();w.rootWireLoad(z,zr);check(zr.refused,'invalid kind must refuse');final r=w.TableWireReport();w.rootWireLoad(b,r);check(r.refused && r.reason=='count_over_extent_cap','count refusal: ${r.reason}');final rr=w.TableWireReport();final store=w.TableRetain(4096,List<int>.filled(512,0));w.rootWireLoadRetain(b,store,rr);check(rr.refused && rr.reason=='count_over_extent_cap','retain count refusal: ${rr.reason}');}`+"\n", hex.EncodeToString(wire), hex.EncodeToString(voidWire))
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bounded count probe: %v: %s", err, out)
	}
}
