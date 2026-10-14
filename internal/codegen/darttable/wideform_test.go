package darttable

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartWideKindsThroughGeneratedWire(t *testing.T) {
	dir := t.TempDir()
	var imports, checks strings.Builder
	for index, spelling := range []string{"int128", "uint128", "fixed(4,4)", "ufixed(4,4)", "fixed(8,8)", "ufixed(8,8)", "fixed(16,16)", "ufixed(16,16)", "fixed(32,32)", "ufixed(32,32)", "fixed(64,64)", "ufixed(64,64)"} {
		definition := spelling
		if spelling == "int128" {
			definition += " | min=-170141183460469231731687303715884105728,max=170141183460469231731687303715884105727"
		} else if strings.Contains(spelling, "fixed") {
			var ib, fb int
			fmt.Sscanf(strings.TrimPrefix(spelling, "u"), "fixed(%d,%d)", &ib, &fb)
			power := ib
			if !strings.HasPrefix(spelling, "u") {
				power--
			}
			if power > 63 {
				power = 63
			}
			low := new(big.Int)
			high := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(power)), big.NewInt(1))
			if !strings.HasPrefix(spelling, "u") {
				low.Neg(new(big.Int).Lsh(big.NewInt(1), uint(power)))
			}
			definition += fmt.Sprintf(" | min=%s,max=%s", low, high)
		}
		mapDecl := ""
		if spelling == "uint128" {
			mapDecl = "entries map[uint8]uint128\n"
		}
		source := fmt.Sprintf("package probe\nenum Slot { alpha,beta }\ntable Root { value %s\n vals [..3]%s\n slots [Slot]%s\n opt ?%s\n points [2]%s\n %s body *string }\n", definition, definition, definition, definition, definition, mapDecl)
		u := unitFrom(t, source)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for name := range files {
			if strings.HasSuffix(name, "Cook.dart") {
				t.Fatal("wide graph cook remains outside this slice", name)
			}
		}
		if _, ok := files["ProbeWire.dart"]; !ok {
			t.Fatalf("%s must emit form1", spelling)
		}
		sub := fmt.Sprintf("v%d", index)
		if err := os.Mkdir(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, sub, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		fmt.Fprintf(&imports, "import '%s/ProbeWire.dart' as p%d;\n", sub, index)
		f := u.Tables["Root"].Fields[0]
		lo, hi := wideBounds(f)
		values := []*big.Int{new(big.Int), big.NewInt(1), lo, hi}
		m := tabletext.NewModel(u)
		for _, raw := range values {
			root := m.New(u.Tables["Root"])
			root.Fields[0].Cell.Wide = new(big.Int).Set(raw)
			root.Fields[1].Count = 1
			root.Fields[1].Elems = []tabletext.Cell{{Wide: new(big.Int).Set(raw)}}
			root.Fields[2].Elems[0].Wide = new(big.Int).Set(raw)
			root.Fields[3].Present = true
			root.Fields[3].Cell.Wide = new(big.Int).Set(raw)
			root.Fields[4].Elems[0].Wide = new(big.Int).Set(raw)
			mapSetup := ""
			if mapDecl != "" {
				entry := m.New(u.Tables["Root"].Fields[5].MapEntry)
				entry.Fields[0].Cell.U = 1
				entry.Fields[0].Cell.I = 1
				entry.Fields[1].Cell.Wide = new(big.Int).Set(raw)
				root.Fields[5].Entries = []tabletext.Cell{{Tab: entry}}
				mapSetup = fmt.Sprintf("v.entries.add(p%d.%s()..key=1..value=value);", index, u.Tables["Root"].Fields[5].MapEntry.Name)
			}

			wire, err := tablewire.Encode(m, root)
			if err != nil {
				t.Fatal(err)
			}
			text, err := m.Write(root)
			if err != nil {
				t.Fatal(err)
			}

			stored := wideWireStorageValue(f, "raw")
			fmt.Fprintf(&checks, "{final raw=BigInt.parse('%s');final value=%s;final v=p%d.Root()..value=value..vals=[value]..optPresent=true..opt=value;v.slots[1]=value;v.points[0]=value;%sfinal wire=p%d.rootWireSave(v);check(hex(wire)==%q,'Go wire %s raw%s got${hex(wire)}');final r=p%d.TableWireReport();final back=p%d.rootWireLoad(wire,r);check(!r.malformed&&r.kindMismatch==0&&r.clamped==0&&r.widened==0,'clean load');check(hex(p%d.rootWireSave(back))==hex(wire),'wire resave');final text=p%d.rootToJson(v);check(text==%q,'Go text %s');final tr=p%d.TableTextReport();final parsed=p%d.rootFromJson(text,tr);check(!tr.malformed&&tr.kindMismatch==0&&tr.clamped==0,'clean JSON');check(hex(p%d.rootWireSave(parsed))==hex(wire),'JSON wire identity');}\n", raw, stored, index, mapSetup, index, hex.EncodeToString(wire), spelling, raw, index, index, index, index, string(text)+"\n", spelling, index, index, index)
		}
		if index < 2 {
			kind := byte(5)
			if index == 1 {
				kind = 9
			}
			body := []byte{1, kind, 0, 0, 0, 0, 0}
			file := append([]byte{1}, body...)
			file = binary.LittleEndian.AppendUint64(file, fieldId(u.Tables["Root"].Fields[3]))
			file = binary.LittleEndian.AppendUint64(file, 1)
			truncated := m.New(u.Tables["Root"])
			tr := tabletext.Report{}
			if _, err := tablewire.Decode(m, truncated, file, &tr); err != nil {
				t.Fatal(err)
			}
			if !tr.Malformed || truncated.Fields[3].Present {
				t.Fatal("Go optional widened framing oracle", tr)
			}
			fmt.Fprintf(&checks, "{final data=base64Decode(%q);final r=p%d.TableWireReader(data,data.length);r.refToId[1]=%s;final v=p%d.Root();p%d.rootWireLoadBody(r,v,false);check(!v.optPresent&&r.report.malformed,'incomplete widened optional framing stays absent');}\n", base64.StdEncoding.EncodeToString(body), index, dartU64Literal(fieldId(u.Tables["Root"].Fields[3])), index, index)

			oldType := "int64"
			raw := big.NewInt(-42)
			if index == 1 {
				oldType = "uint64"
				raw, _ = new(big.Int).SetString("9223372036854775809", 10)
			}
			oldSource := strings.Replace(source, "value "+definition, "value "+oldType, 1)
			oldSource = strings.Replace(oldSource, "vals [..3]"+definition, "vals [..3]"+oldType, 1)
			oldUnit := unitFrom(t, oldSource)
			oldModel := tabletext.NewModel(oldUnit)
			old := oldModel.New(oldUnit.Tables["Root"])
			old.Fields[0].Cell.I = raw.Int64()
			old.Fields[0].Cell.U = raw.Uint64()
			if index == 0 {
				old.Fields[0].Cell.U = uint64(raw.Int64())
			}
			old.Fields[1].Count = 1
			old.Fields[1].Elems = []tabletext.Cell{{I: raw.Int64(), U: old.Fields[0].Cell.U}}
			input, err := tablewire.Encode(oldModel, old)
			if err != nil {
				t.Fatal(err)
			}
			normalized := m.New(u.Tables["Root"])
			normalized.Fields[0].Cell.Wide = new(big.Int).Set(raw)
			normalized.Fields[1].Count = 1
			normalized.Fields[1].Elems = []tabletext.Cell{{Wide: new(big.Int).Set(raw)}}
			wire, err := tablewire.Encode(m, normalized)
			if err != nil {
				t.Fatal(err)
			}
			decoded := m.New(u.Tables["Root"])
			report := tabletext.Report{}
			if _, err := tablewire.Decode(m, decoded, input, &report); err != nil {
				t.Fatal(err)
			}
			if report.Malformed || report.Widened != 2 || tabletext.WideValue(&decoded.Fields[0].Cell).Cmp(raw) != 0 {
				t.Fatal("Go widening oracle", report)
			}
			fmt.Fprintf(&checks, "{final r=p%d.TableWireReport();final v=p%d.rootWireLoad(base64Decode(%q),r);check(!r.malformed&&r.kindMismatch==0&&r.widened==2,'64 to128 scalar and array');final saved=hex(p%d.rootWireSave(v));check(saved==%q,'Go canonical widening got$saved');}\n", index, index, base64.StdEncoding.EncodeToString(input), index, hex.EncodeToString(wire))
		}

		highJSON := "{\"value\":" + tabletext.FormatWide(new(big.Int).Add(hi, big.NewInt(1)), ir.TableWireScalarKind(f), f.Type.FracBits) + "}"
		for _, input := range []string{highJSON, `{"value":0.1}`, `{"value":1,"value":0.1}`, `{"opt":1,"opt":null,"opt":0.1}`} {
			v := m.New(u.Tables["Root"])
			r := tabletext.Report{}
			if !m.Read(v, []byte(input), &r) {
				t.Fatal("Go wide text oracle", input, r)
			}
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&checks, "{final r=p%d.TableTextReport();final v=p%d.rootFromJson(%q,r);check(r.clamped==%d&&r.kindMismatch==%d&&r.duplicate==%d&&!r.malformed,'wide JSON tolerant events');check(hex(p%d.rootWireSave(v))==%q,'Go wide tolerant JSON');}\n", index, index, input, r.Clamped, r.KindMismatch, r.Duplicate, index, hex.EncodeToString(wire))
		}

	}
	t.Run("runtime-production-oracle", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		probe := "import 'dart:convert';\nimport 'dart:typed_data';\n" + imports.String() + "String hex(List<int> b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid check(bool b,String why){if(!b)throw StateError(why);}\nvoid main(){\n final frame=p0.TableWireReader(Uint8List(8),8)..pos=1;check(!frame.has(9223372036854775807)&&frame.pos==1,'length preflight cannot overflow');\n" + checks.String() + "}\n"
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"analyze", "--fatal-infos", "probe.dart"}, {"--enable-asserts", "probe.dart"}} {
			cmd := exec.Command(dartBinary(t), args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
	})
}
