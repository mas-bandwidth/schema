package lobstertable_test

import (
	"encoding/hex"
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S -0 LANDS AS +0 (docs/SPEC-TABLES.md §3.3; the
// rulings file's fixed-compressed-negzero): the variable-form strict load
// stores a ranged float whose four bytes hold -0 as +0, as the reference
// read does, and the JSON writer then prints 0. A plain float keeps its sign.

const rangedZeroSchema = `package rangedzero

table Probe
{
    a float32
    r float32 = 0.0 | min = -10.0, max = 10.0, resolution = 0.5
}
`

const rangedZeroBody = `
def zclean() -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of("Probe"))
    fslot(rec, "a").cell.f = 1.75
    fslot(rec, "r").cell.f = 2.5
    return rec

def zsign(x:float) -> string:
    if x != 0.0:
        return "nonzero"
    return if 1.0 / x > 0.0: "+0" else: "-0"

def zread(file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict("Probe", data, rep)
    if rec:
        print("ok")
        print("a " + zsign(fslot(rec, "a").cell.f))
        print("r " + zsign(fslot(rec, "r").cell.f))
        let js, why = schema_rt.json_write(rec)
        print(why + "|" + js)
    else:
        print("refused " + reason)
        print("-")
        print("-")
        print("-")
`

// TestLobsterLoadRangedFloatZeroUnsigned builds a lawful form-1 save of the
// probe, sets each float's four bytes to -0 and loads it strictly: the ranged
// field lands as +0 (1/x is +inf) and the JSON writer prints 0 for it; the
// plain field keeps its sign.
func TestLobsterLoadRangedFloatZeroUnsigned(t *testing.T) {
	t.Parallel()
	g := finiteGenerate(t, rangedZeroSchema)
	out := finiteRun(t, g, rangedZeroBody+"let w, why = schema_rt.save_reasoned(zclean())\nprint(why)\nprint(fhex(w))\n", nil)
	if len(out) != 2 || out[0] != "" {
		t.Fatalf("the clean probe does not save: %q", out)
	}
	clean, err := hex.DecodeString(out[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goSave(g, "Probe", clean); err != nil {
		t.Fatalf("the Go engine refuses the clean probe wire: %v", err)
	}
	const minusZero = 0x80000000
	wire := patch32(t, patch32(t, clean, 0x3FE00000, minusZero), 0x40200000, minusZero)
	got := finiteRun(t, g, rangedZeroBody+"zread(\"w0\")\n", map[string][]byte{"w0": wire})
	if len(got) < 4 || got[0] != "ok" {
		t.Fatalf("the strict load of the -0 wire: got %q, want it read", got)
	}
	if got[1] != "a -0" {
		t.Errorf("the plain float32: got %q, want its -0 kept", got[1])
	}
	if got[2] != "r +0" {
		t.Errorf("the ranged float32: got %q, want -0 landed as +0", got[2])
	}
	js := strings.Join(got[3:], "\n")
	if !strings.HasPrefix(js, "|") || !strings.Contains(js, `"r": 0`+"\n") || !strings.Contains(js, `"a": -0,`) {
		t.Errorf("the JSON writer: got %q, want the ranged field printed as 0 and the plain one as -0", js)
	}
}
