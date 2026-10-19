package cstable

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCsFixedFormAdmitsPayloadFreeArm pins the v1.3 blocker: a union arm with no
// payload (`signal`) is a Variant whose F is nil (docs/SPEC-TABLES.md §2.6), and
// the C# fixed-form walk must skip it — never read v.F — the way the C++ emitter
// does. Before the guards it panicked at
// cstable.fixedCollectTypes fixedform.go:181, and the payload-free arm's entry
// must still land in the layout so an arm appended after it moves the hash
// (ir.tableFixedWalkArm, pushNoPayloadArm).
func TestCsFixedFormAdmitsPayloadFreeArm(t *testing.T) {
	t.Parallel()
	const base = `package probe
type Cell { x int32 }
union Choice
{
    signal
    many [..4]Cell
}
fixed table Root { history [..8]Choice }
`
	files, err := Generate(unitFrom(t, base))
	if err != nil {
		t.Fatalf("generating a fixed table holding a payload-free arm: %v", err)
	}
	body := concatCS(files)
	if !strings.Contains(body, "RootFixedWriteBody") {
		t.Fatal("no RootFixedWriteBody emitted for a union with a payload-free arm")
	}

	// The arm list IS the layout; appending an arm after the payload-free one
	// must move the hash. Were the payload-free arm given no entry, the arm
	// appended right after it would leave the hash still, and a C# root could
	// not tell the two records apart.
	const extended = `package probe
type Cell { x int32 }
union Choice
{
    signal
    extra
    many [..4]Cell
}
fixed table Root { history [..8]Choice }
`
	efiles, err := Generate(unitFrom(t, extended))
	if err != nil {
		t.Fatalf("generating with an arm appended after a payload-free arm: %v", err)
	}
	lo, hi := rootFixedHash(t, body), rootFixedHash(t, concatCS(efiles))
	if lo == hi {
		t.Errorf("appending an arm after a payload-free arm did not move Root's layout hash: both %#016x", lo)
	}
}

var rootFixedHashRe = regexp.MustCompile(`public const ulong RootFixedHash = 0x([0-9a-fA-F]+)ul;`)

func rootFixedHash(t *testing.T, src string) uint64 {
	t.Helper()
	m := rootFixedHashRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("no RootFixedHash emitted")
	}
	v, err := strconv.ParseUint(m[1], 16, 64)
	if err != nil {
		t.Fatalf("parsing RootFixedHash: %v", err)
	}
	return v
}

func concatCS(files map[string][]byte) string {
	var b strings.Builder
	for _, body := range files {
		b.Write(body)
	}
	return b.String()
}
