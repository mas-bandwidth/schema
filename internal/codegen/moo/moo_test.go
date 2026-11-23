package moo

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/irtest"
)

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestWideNewIsLeastSignificantLimbFirst(t *testing.T) {
	cases := []struct {
		class string
		v     string
		width int
		want  string
	}{
		{"Uint64", "0x1122334455667788", 64, "serialize.Uint64.new(0x55667788, 0x11223344)"},
		{"Int64", "-1", 64, "serialize.Int64.new(0xffffffff, 0xffffffff)"},
		{"Int128", "-250", 128, "serialize.Int128.new(0xffffff06, 0xffffffff, 0xffffffff, 0xffffffff)"},
	}
	for _, c := range cases {
		v, _ := new(big.Int).SetString(c.v, 0)
		if got := wideNew(c.class, v, c.width); got != c.want {
			t.Errorf("wideNew(%s, %s) = %s, want %s", c.class, c.v, got, c.want)
		}
	}
}

func TestFixedNumberLiteralIsExact(t *testing.T) {
	for raw, want := range map[int64]string{
		2981888:    "45.5",
		-809119744: "-12346.1875",
		1:          "0.0000152587890625",
		0:          "0",
		-196608:    "-3",
	} {
		if got := fixedNumberLit(big.NewInt(raw)); got != want {
			t.Errorf("fixedNumberLit(%d) = %s, want %s", raw, got, want)
		}
	}
}

// TestMooRefusesARuntimeCollision: a schema file whose module is a runtime
// module's name, folding case, is refused by name.
func TestMooRefusesARuntimeCollision(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte("package t\n\ntype A\n{\n    x uint8\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the checker refuses a serialize_ basename first (SPEC §4.6); this
	// backend's own guard sits behind it
	u := unitFromFiles(t, []string{p})
	irtest.Rebase(t, u, "Probe", "Serialize_Stream")
	_, err := Generate(u)
	if err == nil || !strings.Contains(err.Error(), "serialize_stream.moo") {
		t.Fatalf("want a refusal naming serialize_stream.moo, got %v", err)
	}
}

// TestMooGenerationIsDeterministic: two generations are byte-identical.
func TestMooGenerationIsDeterministic(t *testing.T) {
	u := unitFromDir(t, repo("examples"))
	a, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(unitFromDir(t, repo("examples")))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) == 0 {
		t.Fatal("a is empty: the loop below would check nothing")
	}
	for name := range a {
		if string(a[name]) != string(b[name]) {
			t.Errorf("%s differs between two generations", name)
		}
	}
}
