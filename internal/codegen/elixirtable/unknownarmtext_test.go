package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPinnedUnknownArmTextElixir pins the fuzz vector unknown_arm_text_nul
// (testdata/wire/tables/fuzz-vectors/INDEX.txt) on the Elixir leg. Every read
// refuses it: docs/SPEC-TABLES.md §6.5 says the strict read's rules are
// decidable from the wire alone and apply to every byte, in fields and
// records a reader declares and in those it does not. streamdemo.Feed's known
// union field carries an arm Feed cannot name, of the text kind, whose text
// holds a NUL. The Elixir read once skipped an unknown arm's payload by its L
// and loaded the value.
//
// REVERSED WITNESS: with the walk of every body by its framing
// (strict_syntax!) removed from the emitted reads, the probe goes red.
func TestPinnedUnknownArmTextElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	u := loadUnit(t, "../../../tables/stream/Stream.schema")
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/unknown_arm_text_nul.bin")
	if e != nil {
		t.Fatal(e)
	}
	probe := fmt.Sprintf(`alias Streamdemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Feed",wire),T.variable_load("Feed",wire),T.load_retain("Feed",wire,store,zero)] do
 unless elem(result,0)==:error and elem(result,2).reason != "",do: raise("unknown arm text "<>inspect(result))
end
IO.puts("UNKNOWN_ARM_TEXT_OK")
`, hex.EncodeToString(wire))
	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil || !strings.Contains(out, "UNKNOWN_ARM_TEXT_OK") {
		t.Fatalf("native unknown arm text: %v\n%s", err, out)
	}
	elixirPatch(t, files, "        strict_syntax!(body, nodes, ids)\n", "")
	out, err = runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil || !strings.Contains(out, "unknown arm text") {
		t.Fatalf("REVERSED WITNESS FAILED: without the framing walk the probe stayed green\n%s", out)
	}
}
