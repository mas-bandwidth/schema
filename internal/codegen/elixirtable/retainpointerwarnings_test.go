package elixirtable

import (
	"strings"
	"testing"
)

// The complete stream closure narrows the pointer reader's return type. It
// exposed a dead halt clause which smaller mixed-arm units did not diagnose.
func TestRetainingPointerArmStreamWarningsElixir(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "../../../tables/stream/Stream.schema")
	out := runCompleteUnitProbe(t, u, "IO.puts(\"STRICT_STREAM_POINTER_OK\")\n")
	if !strings.Contains(out, "STRICT_STREAM_POINTER_OK") {
		t.Fatalf("strict stream pointer: %s", out)
	}
}
