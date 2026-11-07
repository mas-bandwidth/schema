package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The complete stream closure narrows the pointer reader's return type. It
// exposed a dead halt clause which smaller mixed-arm units did not diagnose.
func TestRetainingPointerArmStreamWarningsElixir(t *testing.T) {
	slowtest.Gate(t, "complete Elixir stream unit must compile without pointer-arm type warnings")
	u := loadUnit(t, "../../../tables/stream/Stream.schema")
	out := runCompleteUnitProbe(t, u, "IO.puts(\"STRICT_STREAM_POINTER_OK\")\n")
	if !strings.Contains(out, "STRICT_STREAM_POINTER_OK") {
		t.Fatalf("strict stream pointer: %s", out)
	}
}
