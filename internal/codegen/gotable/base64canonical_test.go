package gotable

import (
	"fmt"
	"os"
	"testing"
)

// TestGoLoadTextRefusesNonCanonicalBase64 pins docs/SPEC-TABLES.md §16.8: the
// base64 text carrier is CANONICAL — standard alphabet, padded, pad bits zero,
// no byte outside the alphabet and `=` — and the strict read `LoadText` refuses
// a text that decodes but is not canonical by the new name `base64_not_canonical`,
// while a text that does not decode stays `base64_damaged`. SaveText already
// writes canonical text; this pins it. Red at base: the fuzz repro
// `AQAAAAAAAAAAAB==` (nonzero pad bits) decoded leniently and read silently.
func TestGoLoadTextRefusesNonCanonicalBase64(t *testing.T) {
	t.Parallel()
	fixedSchema, err := os.ReadFile("../../../tables/examples/Tables.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(fixedSchema), fmt.Sprintf(`package tabledemo
import("errors";"testing")
func TestFixedWeaponConfig(t *testing.T) {
	// the fuzz repro: nonzero pad bits decode leniently and must refuse by name
	var r TableReport
	if _, err := WeaponConfigLoadText("AQAAAAAAAAAAAB==", &r); !errors.Is(err, TableRefuseBase64NotCanonical) || err.Error() != "base64_not_canonical" {
		t.Fatalf("nonzero pad bits: err=%%v", err)
	}
	// the canonical SaveText of a sample
	var v WeaponConfig
	WeaponConfigReset(&v)
	v.Damage = 42
	v.Speed = 120.5
	v.Homing = true
	text, err := WeaponConfigSaveText(&v)
	if err != nil { t.Fatalf("savetext: %%v", err) }
	// reads clean and re-encodes to the same text
	loaded, err := WeaponConfigLoadText(text, &r)
	if err != nil { t.Fatalf("clean load: %%v", err) }
	again, err := WeaponConfigSaveText(loaded)
	if err != nil || again != text { t.Fatalf("re-encode: %%q %%v", again, err) }
	// a newline inserted mid-text refuses the same way
	mid := text[:10] + "\n" + text[10:]
	if _, err := WeaponConfigLoadText(mid, &r); !errors.Is(err, TableRefuseBase64NotCanonical) {
		t.Fatalf("mid newline: err=%%v", err)
	}
	// a newline (\r\n) appended refuses the same way
	app := text + "\r\n"
	if _, err := WeaponConfigLoadText(app, &r); !errors.Is(err, TableRefuseBase64NotCanonical) {
		t.Fatalf("appended crlf: err=%%v", err)
	}
	// a byte outside the alphabet is base64_damaged
	bad := "!" + text[1:]
	if _, err := WeaponConfigLoadText(bad, &r); !errors.Is(err, TableRefuseBase64Damaged) {
		t.Fatalf("byte outside alphabet: err=%%v", err)
	}
}
`))
	guardedSchema, err := os.ReadFile("../../../tables/examples/Guarded.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(guardedSchema), fmt.Sprintf(`package tabledemo
import("errors";"testing")
func TestVariablePatrol(t *testing.T) {
	var b PatrolBuilder
	if !b.Init() { t.Fatal("init") }
	defer b.Shutdown()
	var r TableReport
	j := []byte(`+"`"+`{"active":true,"speed":2.0,"has_target":false,"wander":0.25}`+"`"+`)
	if !PatrolFromJson(&b, j, &r) || r != (TableReport{}) {
		t.Fatalf("fromjson: %%+v", r)
	}
	v := b.GetRoot()
	text, err := PatrolSaveText(v, &b.Arena)
	if err != nil { t.Fatalf("savetext: %%v", err) }
	if loaded, err := PatrolLoadText(text, &r); err != nil {
		t.Fatalf("clean load: %%v", err)
	} else if back, err := PatrolSaveText(loaded, &b.Arena); err != nil || back != text {
		t.Fatalf("re-encode: %%q %%v", back, err)
	}
	// nonzero pad bits refuse by name
	if _, err := PatrolLoadText("AQAAAAAAAAAAAB==", &r); !errors.Is(err, TableRefuseBase64NotCanonical) {
		t.Fatalf("nonzero pad bits: err=%%v", err)
	}
	// a newline inserted mid-text refuses the same way
	mid := text[:10] + "\n" + text[10:]
	if _, err := PatrolLoadText(mid, &r); !errors.Is(err, TableRefuseBase64NotCanonical) {
		t.Fatalf("mid newline: err=%%v", err)
	}
}
`))
}
