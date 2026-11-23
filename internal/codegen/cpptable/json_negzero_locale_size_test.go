package cpptable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cppFloatTextSchema = `package floatcheck
fixed table FloatBox
{
    wide   float64 = 0
    narrow float32 = 0
}
table GraphBox
{
    a    int32 = 0
    next *GraphBox
}
`

// TestCPPJsonNegativeZeroAnyLength: a negative-zero token reads as -0 whatever
// its length (SPEC-TABLES.md §16.2 — the value does not depend on how many
// bytes spell it). A token of 1024 bytes or more takes the normalising path,
// which used to drop the sign and answer +0; the short spelling answered -0.
func TestCPPJsonNegativeZeroAnyLength(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	u := loadUnitFromSource(t, cppFloatTextSchema)
	zeros := func(n int) string { return strings.Repeat("0", n) }
	tokens := []struct{ name, token string }{
		{"short", "-0.0"},
		{"bytes-1023", "-0." + zeros(1020)},
		{"bytes-1024", "-0." + zeros(1021)},
		{"bytes-1025", "-0." + zeros(1022)},
		{"bytes-5000", "-0." + zeros(4997)},
		{"long-exponent", "-0." + zeros(1500) + "e7"},
	}
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, tc := range tokens {
		for _, field := range []string{"wide", "narrow"} {
			text := fmt.Sprintf(`{"%s":%s}`, field, tc.token)
			fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        FloatBox value; FloatBoxReset(value);
        TableReport report;
        bool ok = FloatBoxFromJson(value, (const char *)text, (int64_t)sizeof(text), &report);
        uint64_t wide_bits = 0; uint32_t narrow_bits = 0;
        memcpy(&wide_bits, &value.wide, sizeof(wide_bits));
        memcpy(&narrow_bits, &value.narrow, sizeof(narrow_bits));
        printf("NEGZERO %s/%s: ok=%%d wide=%%016llx narrow=%%08x\n", (int)ok, (unsigned long long)wide_bits, narrow_bits);
        CHECK(ok, "%s/%s read");
        CHECK(%s, "%s/%s is -0 and the other field untouched");
    }
`, cppBytes([]byte(text)), tc.name, field, tc.name, field,
				map[string]string{
					"wide":   "wide_bits == 0x8000000000000000ULL && narrow_bits == 0",
					"narrow": "narrow_bits == 0x80000000U && wide_bits == 0",
				}[field], tc.name, field)
		}
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("negative-zero probe: %v\n%s", err, out)
	}
}

// TestCPPJsonFloatTextIsLocaleIndependent: under a locale whose decimal point
// is MORE THAN ONE BYTE (Pashto, U+066B), a float still writes and reads with
// '.', byte for byte what the "C" locale gives, and the program's own locale
// is the same after the call as before it.
func TestCPPJsonFloatTextIsLocaleIndependent(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++) and localedef")
	if _, err := exec.LookPath("localedef"); err != nil {
		t.Skip("building a multi-byte-point locale requires localedef")
	}
	if _, err := os.Stat("/usr/share/i18n/locales/ps_AF"); err != nil {
		t.Skip("the ps_AF locale source is not installed")
	}
	locDir := t.TempDir()
	if out, err := testproc.Command(t, "localedef", "-i", "ps_AF", "-f", "UTF-8", filepath.Join(locDir, "ps_AF.UTF-8")).CombinedOutput(); err != nil {
		if _, serr := os.Stat(filepath.Join(locDir, "ps_AF.UTF-8", "LC_NUMERIC")); serr != nil {
			t.Skipf("localedef could not build the locale: %v\n%s", err, out)
		}
	}
	t.Setenv("LOCPATH", locDir)
	u := loadUnitFromSource(t, cppFloatTextSchema)
	prelude := `#include <locale.h>
#include <stdlib.h>`
	body := `
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
    // the C-locale text, before the program changes its locale
    char want[256];
    FloatBox c; FloatBoxReset(c); c.wide = 1.5; c.narrow = 0.25f;
    int64_t want_n = FloatBoxToJson(c, want, (int64_t)sizeof(want));
    CHECK(want_n > 0, "C-locale write");
    if (setlocale(LC_ALL, "ps_AF.UTF-8") == NULL) { fprintf(stderr, "FAIL: the locale did not load\n"); return 1; }
    {
        char probe[64];
        snprintf(probe, sizeof(probe), "%.1f", 1.5);
        CHECK(strcmp(probe, "1.5") != 0, "the program's locale really has another point");
    }
    char got[256];
    FloatBox v; FloatBoxReset(v); v.wide = 1.5; v.narrow = 0.25f;
    int64_t got_n = FloatBoxToJson(v, got, (int64_t)sizeof(got));
    printf("LOCALE write: %lld bytes, want %lld: %.*s", (long long)got_n, (long long)want_n, (int)(got_n > 0 ? got_n : 0), got);
    CHECK(got_n == want_n && memcmp(got, want, (size_t)want_n) == 0, "write is byte-identical to the C locale");
    {
        char probe[64];
        snprintf(probe, sizeof(probe), "%.1f", 1.5);
        CHECK(strcmp(probe, "1.5") != 0, "the locale is restored after the write");
    }
    static const char text[] = "{\"wide\":1.5,\"narrow\":0.25}\n";
    FloatBox r; FloatBoxReset(r);
    TableReport report;
    bool ok = FloatBoxFromJson(r, text, (int64_t)(sizeof(text) - 1), &report);
    printf("LOCALE read: ok=%d wide=%.17g narrow=%.9g\n", (int)ok, r.wide, (double)r.narrow);
    CHECK(ok && r.wide == 1.5 && r.narrow == 0.25f, "read of a '.' number");
    {
        // a long token takes the normalising path: it must read the same
        static const char longtext[] = "LONGTEXT";
        FloatBox lr; FloatBoxReset(lr);
        TableReport lreport;
        bool lok = FloatBoxFromJson(lr, longtext, (int64_t)(sizeof(longtext) - 1), &lreport);
        CHECK(lok && lr.wide == 1.5, "read of a long '.' number");
    }
    {
        char probe[64];
        snprintf(probe, sizeof(probe), "%.1f", 1.5);
        CHECK(strcmp(probe, "1.5") != 0, "the locale is restored after the read");
    }
    if (failures != 0) return 1;
`
	longText := `{\"wide\":1.5` + strings.Repeat("0", 1500) + `}` + "\\n"
	body = strings.ReplaceAll(body, "LONGTEXT", longText)
	out, err := runCPPProbeWithPrelude(t, u, prelude, body)
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("locale probe: %v\n%s", err, out)
	}
}

// TestCPPJsonToJsonWritesNothingPastTheSize: after a successful ToJson the
// bytes of the caller's buffer from the returned size on are exactly what the
// caller left there (Go's ToJson never writes past what it returns). The
// writer used to put a NUL at buffer[size] when the capacity had room.
func TestCPPJsonToJsonWritesNothingPastTheSize(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	u := loadUnitFromSource(t, cppFloatTextSchema)
	body := `
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
    {
        FloatBox v; FloatBoxReset(v); v.wide = 1.5;
        char buffer[256];
        memset(buffer, 0x5A, sizeof(buffer));
        int64_t n = FloatBoxToJson(v, buffer, (int64_t)sizeof(buffer));
        printf("SIZE fixed: n=%lld next=%02x\n", (long long)n, (unsigned char)buffer[n > 0 ? n : 0]);
        CHECK(n > 0 && n < (int64_t)sizeof(buffer), "fixed write");
        bool intact = true;
        for (int64_t i = n; i < (int64_t)sizeof(buffer); i++) { if ((unsigned char)buffer[i] != 0x5A) { intact = false; } }
        CHECK(intact, "fixed: nothing past the size is touched");
    }
    {
        GraphBoxBuilder b; b.GetRoot()->a = 7;
        char buffer[256];
        memset(buffer, 0x5A, sizeof(buffer));
        int64_t n = GraphBoxToJson(b.GetRoot(), buffer, (int64_t)sizeof(buffer));
        printf("SIZE graph: n=%lld next=%02x\n", (long long)n, (unsigned char)buffer[n > 0 ? n : 0]);
        CHECK(n > 0 && n < (int64_t)sizeof(buffer), "graph write");
        bool intact = true;
        for (int64_t i = n; i < (int64_t)sizeof(buffer); i++) { if ((unsigned char)buffer[i] != 0x5A) { intact = false; } }
        CHECK(intact, "graph: nothing past the size is touched");
    }
    if (failures != 0) return 1;
`
	out, err := runCPPCountRefusalProbe(t, u, body)
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("size probe: %v\n%s", err, out)
	}
}
