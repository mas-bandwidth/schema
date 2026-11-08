package luautable_test

import (
	"bytes"
	"encoding/hex"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var luauGroupCValues = map[string]string{
	"counted-arrays":      `local a=T.NewT();local b=T.NewT();b.Nums[1]=10;b.Nums[2]=65535;b.Nums[3]=32768;b.NumsCount=3;return {a,b}`,
	"keyed-arrays":        `local v=T.NewT();v.Slots[1]=-2147483648;v.Slots[2]=2147483647;return {v}`,
	"nested-keyed-arrays": `local v=T.NewT();v.Slots[1].N=11;v.Slots[2].N=-22;return {v}`,
	"union-values":        `local a=T.NewT();a.Body.Type=1;a.Body.V.X=11;a.Body.V.Y=-22;local b=T.NewT();b.Body.Type=2;b.Body.T.N=41;local c=T.NewT();c.Body.Type=3;c.Body.N=1073741821;return {a,b,c}`,
	"union-field-arms":    `local a=T.NewT();a.Body.Type=1;a.Body.Sa=-5;local b=T.NewT();b.Body.Type=2;buffer.writestring(b.Body.Ta,0,"abc");b.Body.TaLength=3;local c=T.NewT();c.Body.Type=3;c.Body.Aa[1]=-1;c.Body.Aa[2]=0;c.Body.Aa[3]=1;c.Body.AaCount=3;return {a,b,c}`,
	"payload-free-arms":   `local a=T.NewT();a.Lead=10;a.Pick.Type=1;a.Pick.Alpha=99;a.Trail=20;local b=T.NewT();b.Lead=30;b.Pick.Alpha=777;b.Pick.Type=2;b.Trail=40;local c=T.NewT();c.Lead=50;c.Pick.Alpha=888;c.Pick.Type=3;c.Trail=60;return {a,b,c}`,
	"union-arrays":        `local v=T.NewT();v.PicksCount=3;v.Picks[1].Type=1;v.Picks[1].A=3;v.Picks[2].Type=2;v.Picks[2].B=-1099511627776i;v.Picks[3].Type=1;v.Picks[3].A=0;return {v}`,
	"optional-scalars":    `local a=T.NewT();local b=T.NewT();b.N=-123456;b.NPresent=true;b.G=2;b.GPresent=true;return {a,b}`,
	"optional-nesting":    `local a=T.NewT();local b=T.NewT();b.P.X=99;b.P.Y=-99;b.PPresent=true;return {a,b}`,
	"optional-arrays":     `local a=T.NewT();local b=T.NewT();b.Xs[1]=7;b.Xs[2]=-8;b.Xs[3]=9;b.XsCount=3;b.XsPresent=true;return {a,b}`}

func groupCFixedTests(t *testing.T, tc fixedproof.RowCase, cpp []byte, mutate bool) (saved, loaded []byte, refused bool) {
	t.Helper()
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	f, e := luau.Generate(u)
	if e != nil {
		t.Fatal(e)
	}
	g, e := luautable.Generate(u)
	if e != nil {
		t.Fatal(e)
	}
	maps.Copy(f, g)
	if mutate {
		n := "schema_fixed.luau"
		s := string(f[n])
		a := "\t-- FIXED-WRITE-ANCHOR\n\tfor _, f in d.fields do"
		if strings.Count(s, a) != 1 {
			t.Fatalf("writer anchor count %d", strings.Count(s, a))
		}
		f[n] = []byte(strings.Replace(s, a, "\t-- FIXED-WRITE-ANCHOR\n\tfor _, f in {} do", 1))
	}
	d := t.TempDir()
	for n, b := range f {
		if e = os.WriteFile(filepath.Join(d, n), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	p := `local T=require("./probeTable");local F=require("./schema_fixed");local I=require("./inputs");local fx={hash=T.TFixedHash,recordBytes=T.TFixedRecordBytes,layout=T.TFixedLayout};local function hx(b:buffer):string local x={} for i=0,buffer.len(b)-1 do x[i+1]=string.format("%02x",buffer.readu8(b,i)) end return table.concat(x) end;local v=(function() ` + luauGroupCValues[tc.ID] + ` end)();local ok,b=pcall(F.load,T.TDescriptor,fx,I.cpp);if ok then print("LOAD "..hx(F.save(T.TDescriptor,fx,b))) else print("REFUSED") end;local so,w=pcall(F.save,T.TDescriptor,fx,v);if so then print("SAVE "..hx(w)) end`
	if e = os.WriteFile(filepath.Join(d, "main.luau"), []byte(p), 0644); e != nil {
		t.Fatal(e)
	}
	out := string(luaurun.Run(t, d, "main.luau", map[string][]byte{"cpp": cpp}))
	for l := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if after, ok := strings.CutPrefix(l, "LOAD "); ok {
			loaded, e = hex.DecodeString(after)
			if e != nil {
				t.Fatal(e)
			}
		} else if after, ok := strings.CutPrefix(l, "SAVE "); ok {
			saved, e = hex.DecodeString(after)
			if e != nil {
				t.Fatal(e)
			}
		} else if l == "REFUSED" {
			refused = true
		}
	}
	return
}
func groupCFixedCases(t *testing.T, run func(*testing.T, fixedproof.RowCase)) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range fixedproof.Cases() {
		if _, ok := luauGroupCValues[tc.ID]; !ok {
			continue
		}
		seen[tc.ID] = true
		tc := tc
		t.Run(tc.ID, func(t *testing.T) { t.Parallel(); run(t, tc) })
	}
	if len(seen) != len(luauGroupCValues) {
		t.Fatalf("selected %d rows, want %d", len(seen), len(luauGroupCValues))
	}
}
func TestLuauFixedProofGroupC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	groupCFixedCases(t, func(t *testing.T, tc fixedproof.RowCase) {
		u := fixedproof.ParseCheckUnit(t, tc.Schema)
		cpp, vfy := fixedproof.CppOracle(t, tc, u)
		s, l, r := groupCFixedTests(t, tc, cpp, false)
		if r {
			t.Fatal("Luau refused C++ bytes")
		}
		if !bytes.Equal(s, cpp) {
			t.Fatalf("save %x != C++ %x", s, cpp)
		}
		if !bytes.Equal(l, cpp) {
			t.Fatalf("load/resave %x != C++ %x", l, cpp)
		}
		if e := vfy(s); e != nil {
			t.Fatal(e)
		}
	})
}
func TestLuauFixedProofGroupCNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	groupCFixedCases(t, func(t *testing.T, tc fixedproof.RowCase) {
		u := fixedproof.ParseCheckUnit(t, tc.Schema)
		cpp, vfy := fixedproof.CppOracle(t, tc, u)
		layout, _, body := fixedproof.RootFacts(fixedproof.RootStruct(t, u, tc.Root))
		form := bytes.Clone(cpp)
		form[0] ^= 1
		if vfy(form) == nil {
			t.Fatal("C++ accepted corrupt form")
		}
		_, _, r := groupCFixedTests(t, tc, form, false)
		if !r {
			t.Fatal("Luau accepted corrupt form")
		}
		bad := bytes.Clone(cpp)
		at := 20 + len(layout) + (tc.Count-1)*int(8+body) + 8
		if at >= len(bad) || body <= 0 {
			t.Fatalf("bad payload offset %d", at)
		}
		bad[at] ^= 1
		if vfy(bad) == nil {
			t.Fatal("C++ accepted mutated payload")
		}
		_, round, r := groupCFixedTests(t, tc, bad, false)
		if !r && bytes.Equal(round, cpp) {
			t.Fatal("mutated payload normalized")
		}
		w, _, r := groupCFixedTests(t, tc, cpp, true)
		if !r && bytes.Equal(w, cpp) {
			t.Fatal("writer mutation still matched")
		}
		if !r && vfy(w) == nil {
			t.Fatal("C++ accepted mutated writer")
		}
	})
}
