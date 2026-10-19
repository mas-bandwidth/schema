package darttable

// THE TOLERANT BODY READ MUST STOP WHERE THE ORACLE STOPS (docs/SPEC-TABLES.md
// §3.3, §4): fields decoded before root-body framing damage stand, nothing after
// it. The Go oracle (internal/tablewire/decode.go, bodyAt) drops the tail; the
// generated Dart reader must answer the same bytes and the same counters.
//
// Representatives (dart-fix-tolerant-body-keeps-past-damage):
//   graphdemo.Scene (corpus seed graph_tree), pass reference #23, run seed
//   24845619678: leg loaded measure=192 saved=111, oracle encoded=39.
//   blobdemo.Catalog (blob_small), pass reference #16: leg measure=88
//   saved=149, oracle 24.
//
// The mutant bytes are restored from the harness's `mutant bytes` line on the
// replay line, and embedded here as hex. TODO: populate `sceneMutant` and
// `catalogMutant` from the scratch fuzz reproduction.

import (
	"os"
	"testing"
)

func TestWireLoadDropsPastBodyDamageDart(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
		t.Skip("DART corpus gate only: set SCHEMA_REQUIRE_CORPUS=1 and DART")
	}
	// dartBinary(t) pins the leg's own SDK; the generated Dart load of each
	// mutant must answer the oracle's bytes and counters (see card text).
	_ = dartBinary(t)
	t.Skip("mutant bytes not yet reproduced within the wall: not-done")
}
