package goosetable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageStringDefaultElidedOnlyWhenEqual holds the message form's text
// elision to the reference (docs/SPEC-TABLES.md §3.3): a string or bytes
// field is left out only when its length AND its bytes are the declared
// default's. A value of the default's length ("xyz" under "abc") and the
// empty value both ride and round-trip unchanged; the default itself is
// still elided, so its bytes never reach the wire.
func TestMessageStringDefaultElidedOnlyWhenEqual(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(path, []byte(`package probe

fixed table Probe
{
    s string(8) = "abc"
    b bytes(4) = "ab"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatalf("the probe schema does not load: %v", err)
	}
	text := func(field, v string) []string {
		var out []string
		for i := range len(v) {
			out = append(out, fmt.Sprintf(".%s[%d] = %d", field, i, v[i]))
		}
		return append(out, fmt.Sprintf(".%sLength = %d", field, len(v)))
	}
	body := func(s, b string) []string { return append(text("S", s), text("B", b)...) }
	pairs := [][2]string{{"xyz", "zz"}, {"", ""}, {"abc", "ab"}}
	var values [][]string
	for _, p := range pairs {
		values = append(values, body(p[0], p[1]))
	}

	saved := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "Probe", values: values})
	if saved.saveN <= 0 {
		t.Fatalf("ProbeSaveMessages refused: %d %s\n%s", saved.saveN, saved.saveReason, saved.raw)
	}
	data, err := hex.DecodeString(saved.saved)
	if err != nil {
		t.Fatalf("the saved batch is not hex: %v\n%s", err, saved.raw)
	}
	if !strings.Contains(saved.saved, hex.EncodeToString([]byte("xyz"))) {
		t.Errorf("the saved batch does not carry \"xyz\": %s", saved.saved)
	}
	if strings.Contains(saved.saved, hex.EncodeToString([]byte("abc"))) {
		t.Errorf("the default \"abc\" was written, not elided: %s", saved.saved)
	}

	r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "Probe", values: values, data: data, capacity: len(values)})
	if r.loadN != int64(len(values)) {
		t.Fatalf("ProbeLoadMessages landed %d (%s), want %d\n%s", r.loadN, r.loadReason, len(values), r.raw)
	}
	for i := range values {
		if r.got[i] != r.want[i] {
			t.Errorf("body %d did not round-trip: got %s want %s", i, r.got[i], r.want[i])
		}
	}
}
