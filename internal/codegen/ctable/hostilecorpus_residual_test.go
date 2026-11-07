package ctable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCHostileCorpusResidualCases pins the seven C-leg divergences the
// shared hostile corpus named at 42c88a7a86b6 (schema-hostile-corpus-green):
// the generated C JSON reader must refuse the whole load on a lone UTF-16
// surrogate escape and on a repeated keyed key the reader cannot place,
// and the generated C wire reader must refuse a text list past its
// declared total. The manifest rows are conformance-keyed-duplicate-none,
// conformance-keyed-duplicate-unknown, conformance-str-lone-lead-surrogate,
// conformance-str-lone-trail-surrogate, fuzz-text-lone_lead_surrogate,
// wire-text-total-and-count and wire-text-total-past. Controls beside each
// refusal keep the same reader accepting the well-formed neighbour.
func TestCHostileCorpusResidualCases(t *testing.T) {
	t.Parallel()
	reader := `package hostile
enum ShipType { Fighter, Bomber, Scout }
fixed table ShipEntry { name string(32) }
fixed table PackConfig { ships [ShipType]ShipEntry
    note string(48) }
table Root { labels [..4]string(8) | total=8 }
`
	andCount := hostileWire(t, `{"labels":["abcdef","abc","a","b","c"]}`)
	past := hostileWire(t, `{"labels":["abcde","abcde"]}`)
	inside := hostileWire(t, `{"labels":["abc","de"]}`)
	runCHostileResidual(t, reader, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static int json_refused(const char * text)
{
    PackConfig value;
    TableReport report;
    memset(&report, 0, sizeof(report));
    pack_config_reset(&value);
    return !pack_config_from_json(&value, text, (int64_t) strlen(text), &report) || report.refused;
}
static int json_accepted(const char * text)
{
    PackConfig value;
    TableReport report;
    memset(&report, 0, sizeof(report));
    pack_config_reset(&value);
    return pack_config_from_json(&value, text, (int64_t) strlen(text), &report) && !report.refused;
}
static int wire_load_refused(const uint8_t * data, int64_t size)
{
    int64_t extent = root_load_measure(data, size);
    uint8_t region[4096];
    TableReport report;
    const Root * root;
    memset(&report, 0, sizeof(report));
    if (extent < 0 || extent > (int64_t) sizeof(region)) { return 0; }
    root = root_load(region, extent, data, size, &report);
    return root == NULL || report.refused;
}
int main(void)
{
    /* conformance-keyed-duplicate-none: "None" repeated; the reader
       places no slot for it, but the key is still spelled twice. */
    CHECK(json_refused("{\"ships\":{\"None\":{\"name\":\"A\"},\"None\":{\"name\":\"B\"}}}"));
    /* conformance-keyed-duplicate-unknown: an unknown key repeated. */
    CHECK(json_refused("{\"ships\":{\"Frigate\":{\"name\":\"A\"},\"Frigate\":{\"name\":\"B\"}}}"));
    /* control: a placed key spelled once still reads. */
    CHECK(json_accepted("{\"ships\":{\"Scout\":{\"name\":\"A\"}}}"));
    /* control: a placed key spelled twice already refuses. */
    CHECK(json_refused("{\"ships\":{\"Scout\":{\"name\":\"A\"},\"Scout\":{\"name\":\"B\"}}}"));
    /* conformance-str-lone-lead-surrogate */
    CHECK(json_refused("{\"note\":\"\\uD800x\"}"));
    /* conformance-str-lone-trail-surrogate */
    CHECK(json_refused("{\"note\":\"\\uDC00x\"}"));
    /* fuzz-text-lone_lead_surrogate */
    CHECK(json_refused("{\"note\":\"\\uD800\"}"));
    /* control: a well-formed surrogate pair still reads. */
    CHECK(json_accepted("{\"note\":\"\\uD83D\\uDE00\"}"));
    /* wire-text-total-and-count and wire-text-total-past: wire written
       under a spelling with no total, read under total=8. */
    {
        static const uint8_t wire[] = { %s };
        CHECK(wire_load_refused(wire, (int64_t) sizeof(wire)));
    }
    {
        static const uint8_t wire[] = { %s };
        CHECK(wire_load_refused(wire, (int64_t) sizeof(wire)));
    }
    /* control: a list inside its total still loads. */
    {
        static const uint8_t wire[] = { %s };
        int64_t extent;
        uint8_t region[4096];
        TableReport report;
        const Root * root;
        extent = root_load_measure(wire, (int64_t) sizeof(wire));
        CHECK(extent > 0 && extent <= (int64_t) sizeof(region));
        memset(&report, 0, sizeof(report));
        root = root_load(region, extent, wire, (int64_t) sizeof(wire), &report);
        CHECK(root != NULL && !report.refused);
        CHECK(root->labels_count == 2);
    }
    return 0;
}
`, andCount, past, inside))
}

// hostileWire encodes text under a writer spelling with no total, the
// provenance the corpus's wire text-total rows carry.
func hostileWire(t *testing.T, text string) string {
	t.Helper()
	u := unitFrom(t, "package hostilew\ntable Root { labels [..5]string(8) }\n")
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("writer text did not place: %s %+v", text, r)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, len(wire))
	for i, b := range wire {
		parts[i] = fmt.Sprintf("0x%02x", b)
	}
	return strings.Join(parts, ",")
}

func runCHostileResidual(t *testing.T, readerSrc, source string) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	files := map[string][]byte{}
	{
		u := unitFrom(t, readerSrc)
		packet, err := cgen.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, packet)
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c")}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	if output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
}
