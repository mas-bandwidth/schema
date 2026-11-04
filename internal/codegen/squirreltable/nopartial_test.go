// A REFUSED WRITE LEAVES THE CALLER'S BUFFER UNTOUCHED (docs/SPEC.md §5.1):
// SaveMessages and FixedSave refuse before the first byte is stored, whatever
// the refusal — a value outside its declared set, a deprecated enum variant,
// or a batch the caller's buffer cannot hold. Each case saves a batch whose
// FIRST value is lawful and whose second is refused (so a writer that walks
// and writes at once would already have stored the first) into a buffer
// poisoned with 0xA5, and checks every byte of the poison is intact. The
// lawful batch saved into the same poisoned buffer moves it: the witness that
// the check sees a write.
package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const noPartialSchema = `package probe
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
fixed table Root {
    level int32 = 1 | min = 0, max = 10
    name  string(4)
    ships [..2]Ship
    speed float32
}
`

const noPartialScript = `local S = dofile("schema_module.nut", true);
local T = S.require("__MODULE__");
local function lawful() { local v = T.Root(); v.Level = 3; v.Name = "ab"; v.Ships = ["Scout"]; v.Speed = 1.5; return v; }
local function poisoned(n) { local b = blob(n); for (local i = 0; i < n; i++) b[i] = 0xA5; return b; }
local function intact(b) { for (local i = 0; i < b.len(); i++) if ((b[i] & 0xff) != 0xA5) return false; return true; }
local bad = {};
bad.level <- function(v) { v.Level = 11; };
bad.name <- function(v) { v.Name = "abcde"; };
bad.count <- function(v) { v.Ships = ["Scout", "Scout", "Scout"]; };
bad.deprecated <- function(v) { v.Ships = ["Bomber"]; };
bad.nan <- function(v) { v.Speed = 0.0 / 0.0; };
bad.zero <- function(v) { v.Name = "a\x00b"; };
foreach (k, f in bad) {
  local v = lawful(); f(v);
  local mb = poisoned(4096); local mr = {};
  local mn = T.RootSaveMessages([lawful(), v], mb, mr);
  print("message." + k + "=" + mn + "|" + (intact(mb) ? "intact" : "written") + "\n");
  local fb = poisoned(4096);
  local fn = T.RootFixedSave(fb, [lawful(), v]);
  print("fixed." + k + "=" + fn + "|" + (intact(fb) ? "intact" : "written") + "\n");
}
local need = T.RootMeasureMessages([lawful(), lawful()], {});
local sb = poisoned(need - 1); local sr = {};
print("message.storage=" + T.RootSaveMessages([lawful(), lawful()], sb, sr) + "|" + (intact(sb) ? "intact" : "written") + "\n");
local ok = poisoned(need); local okr = {};
print("message.lawful=" + (T.RootSaveMessages([lawful(), lawful()], ok, okr) == need) + "|" + (intact(ok) ? "intact" : "written") + "\n");
local fok = poisoned(4096);
print("fixed.lawful=" + (T.RootFixedSave(fok, [lawful(), lawful()]) > 0) + "|" + (intact(fok) ? "intact" : "written") + "\n");
`

func TestRefusedWriteLeavesTheBufferUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	u := loadInlineUnit(t, filepath.Join(dir, "probe.schema"), noPartialSchema)
	gen := filepath.Join(dir, "gen")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGenerated(t, gen, u)
	writeFile(t, filepath.Join(gen, "main.nut"), []byte(variantScript(t, noPartialScript, u)))
	out := string(sqrun.Run(t, gen, "main.nut", nil))

	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	want := map[string]string{
		"message.storage": "-1|intact",
		"message.lawful":  "true|written",
		"fixed.lawful":    "true|written",
	}
	for _, k := range []string{"level", "name", "count", "deprecated", "nan"} {
		want["message."+k] = "-1|intact"
		want["fixed."+k] = "-1|intact"
	}
	// a zero byte is refused by the fixed writer, whose reader refuses it as
	// text_ill_formed; the message writer carries it
	want["fixed.zero"] = "-1|intact"
	if strings.HasPrefix(got["message.zero"], "-1") {
		t.Errorf("message.zero reads %q: the message writer carries a zero byte", got["message.zero"])
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s reads %q, want %q", k, got[k], v)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	}
}
