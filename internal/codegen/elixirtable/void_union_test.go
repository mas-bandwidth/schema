package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestElixirVoidUnionNative(t *testing.T) {
	slowtest.Gate(t, "native void union compilation and round trips")
	elixirBin := elixirBinary(t)
	strict, err := filepath.Abs("../../../make/elixir-strict.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, arms string }{
		{"single", "Empty"},
		{"multiple", "Empty\nOther"},
		{"mixed", "Empty\nValue int32"},
		{"required_context", "Empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package probe\nunion U {\n" + tc.arms + "\n}\ntable R { u U }\n"
			if tc.name == "required_context" {
				source += voidUnionRequiredContext
			}
			u := unitFrom(t, source)
			files := generateElixir(t, u)
			dir := t.TempDir()
			names := writeModuleDir(t, dir, files)
			ebin := filepath.Join(dir, "ebin")
			if err := os.Mkdir(ebin, 0o700); err != nil {
				t.Fatal(err)
			}
			// OTP29 emits some type diagnostics outside its warning counter; use
			// the repository's strict wrapper rather than trusting the flag alone.
			args := append([]string{strict, filepath.Join(filepath.Dir(elixirBin), "elixirc"), "--warnings-as-errors", "-o", ebin}, names...)
			cmd := exec.Command("sh", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(out), "warning:") {
				t.Fatalf("generated unit must compile without warnings: %v\n%s", err, out)
			}
			m := tabletext.NewModel(u)
			var cases strings.Builder
			un := u.Unions["U"]
			if un == nil {
				un = u.TableUnions["U"]
			}
			for tag := 0; tag <= len(un.Variants); tag++ {
				v := m.New(m.Lookup("R"))
				value := "nil"
				if tag > 0 {
					arm := un.Variants[tag-1]
					v.Fields[0].Cell.U = uint64(tag)
					payload := "nil"
					if arm.F != nil {
						v.Fields[0].Cell.Arm = m.NewArm(arm)
						v.Fields[0].Cell.Arm.Cell.I = 17
						v.Fields[0].Cell.Arm.Cell.U = 17
						payload = "17"
					}
					value = fmt.Sprintf("{%q, %s}", arm.Name, payload)
				}
				wire, err := tablewire.Encode(m, v)
				if err != nil {
					t.Fatal(err)
				}
				text, err := m.Write(v)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&cases, "{%s, Base.decode16!(%q, case: :lower), %s},\n", value, hex.EncodeToString(wire), elixirString(string(text)+"\n"))
			}
			probe := `
check = fn yes, why -> if !yes, do: raise(why) end
for {arm, wire, text} <- [
` + cases.String() + `] do
 value = %{"u" => arm}
 {:ok, ^wire} = Probe.Table.save("R", value)
 {:ok, back, report} = Probe.Table.load("R", wire)
 check.(back["u"] == arm and report.reason == "", "plain read")
 {:ok, ^text} = Probe.Table.to_json("R", value)
 {:ok, json, jr} = Probe.Table.from_json("R", text)
 check.(json["u"] == arm and jr.reason == "" and true, "JSON read")
 retain = %{records: [], used: 0, cap: 65536, ids: [], id_cap: 4096}
 report = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
 {:ok, retained, rr, store} = Probe.Table.load_retain("R", wire, retain, report)
 check.(retained["u"] == arm and rr.reason == "", "retaining read")
 {:ok, ^wire, _, _} = Probe.Table.save_retain("R", retained, store, rr)
end
case Probe.Table.from_json("R", ~s({"u":{"Empty":17}})) do
  {:error, a, wr} -> check.(a == :malformed or wr.reason != "", "void payload refusal " <> inspect(wr))
  other -> check.(false, "void payload refusal " <> inspect(other))
end
{:ok, unknown, ur} = Probe.Table.from_json("R", ~s({"u":{"Missing":null}}))
check.(unknown["u"] == nil and ur.unknown == 1 and ur.reason == "", "unknown arm")
IO.puts("void unions passed")
`
			if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd = exec.Command(elixirBin, "-pa", ebin, "probe.exs")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "void unions passed") {
				t.Fatalf("native union probe: %v\n%s", err, out)
			}
		})
	}
}

// The richer closure exercises the BEAM analyzer's literal arm typing, which
// a minimal union-only module does not necessarily expose.
const voidUnionRequiredContext = `
 enum Choice { A, B }
 table Leaf { x int32 }
 table All {
 b bool | required
 i int32 | required
 f float32 | required
 s string(8) | required
 w wstring(8) | required
 buf bytes(8) | required
 choice Choice | required
 u U | required
 ptr *Leaf | required
 text *string | required
 blob *bytes | required
 leaf Leaf | required
 list []int32 | required
 slots [2]int32 | required
 tags map[uint8]int32 | required
 keyed [Choice]int32 | required
 }
 table Kid { x int32 | required }
 table Nest { child Kid
 items []Kid
 tags map[uint8]Kid
 ptr *Kid }
 table Guard { on bool
 if on { needed int32 | required } }
 table Range { n int32 | min=5, required }
 table GuardRange { on bool
 if on { n int32 | min=5, required } }
 table Branch { outer bool
 if outer { inner bool
 if inner { yes int32 | required } else { no int32 | required } } }
`
