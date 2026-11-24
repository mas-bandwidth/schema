package jstable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// jsUnlawfulAnswer is the JavaScript leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type jsUnlawfulAnswer struct {
	// poison sets back[k] of the NEW type to values that are not its declared
	// defaults, so a load that leaves a row alone is seen.
	poison string
	// atDefaults is a JS expression, true when back[k] holds the NEW type's
	// declared defaults (lead = 1, trail = 2, every unlisted field its type's
	// default). A union's declared default is the None tag; its dormant arms
	// keep their storage under Init (SPEC §4.8), so only the tag is held.
	atDefaults string
}

// jsVerdictPrefix marks the line a probe prints for its verdict.
const jsVerdictPrefix = "VERDICT "

var jsUnlawfulAnswers = map[string]jsUnlawfulAnswer{
	"unknown_census": {
		poison:     `back[k].lead = 99; back[k].trail = 88; for (let i = 0; i < 4; i++) { back[k].items[i].a = 5 + i; }`,
		atDefaults: `back[k].lead === 1 && back[k].trail === 2 && back[k].items.every((it) => it.a === 0)`,
	},
	"unknown_census_many": {
		poison:     `back[k].lead = 99; back[k].trail = 88; for (let i = 0; i < 4; i++) { back[k].items[i].a = 5 + i; }`,
		atDefaults: `back[k].lead === 1 && back[k].trail === 2 && back[k].items.every((it) => it.a === 0)`,
	},
	"kind_moved": {
		poison:     `back[k].lead = 99; back[k].v = 5.5; back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].v === 0 && back[k].trail === 2`,
	},
	"arm_removed": {
		poison:     `back[k].lead = 99; back[k].pick.type = 2; back[k].pick.gamma.p = 77; back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].pick.type === 0 && back[k].trail === 2`,
	},
	"variant_removed": {
		poison:     `back[k].lead = 99; back[k].tier = 2; back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].tier === 0 && back[k].trail === 2`,
	},
	"key_removed": {
		poison:     `back[k].lead = 99; for (let i = 0; i < back[k].slots.length; i++) { back[k].slots[i] = 70 + i; } back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].slots.length === 2 && back[k].slots.every((s) => s === 0) && back[k].trail === 2`,
	},
	"array_shrunk": {
		poison:     `back[k].lead = 99; for (let i = 0; i < back[k].vals.length; i++) { back[k].vals[i] = 70 + i; } back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].vals.length === 2 && back[k].vals.every((v) => v === 0) && back[k].trail === 2`,
	},
	"counted_shrunk": {
		poison:     `back[k].lead = 99; back[k].valsCount = 2; back[k].vals[0] = 70; back[k].vals[1] = 71; back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].valsCount === 0 && back[k].vals.every((v) => v === 0) && back[k].trail === 2`,
	},
	"text_narrowed": {
		poison:     `back[k].lead = 99; back[k].text[0] = 0x7A; back[k].text[1] = 0x7A; back[k].textLength = 2; back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].textLength === 0 && back[k].text.length === 4 && back[k].text.every((b) => b === 0) && back[k].trail === 2`,
	},
	"bytes_narrowed": {
		poison:     `back[k].lead = 99; back[k].blob[0] = 0x5A; back[k].blobLength = 1; back[k].trail = 88;`,
		atDefaults: `back[k].lead === 1 && back[k].blobLength === 0 && back[k].blob.length === 4 && back[k].blob.every((b) => b === 0) && back[k].trail === 2`,
	},
	// The LAWFUL control: rows poisoned, the OLD file loaded, the landed values
	// printed. lead and trail are uint32, v is the widened uint16 -> uint32.
	"lawful_control": {
		poison: `back[k].lead = 99; back[k].v = 5; back[k].trail = 88;`,
	},
}

// jsLawfulProbe is the control's probe body: poison, load the OLD file the NEW
// build was handed as a lineage entry, and print the verdict from what landed
// in record 0. A load that left a row alone prints the poison.
func jsLawfulProbe(file, row, poison string) string {
	return fmt.Sprintf(`
const data = readFileSync(%[1]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const plan = TNewPlan(4096, 4096);
for (let k = 0; k < back.length; k++) { %[3]s }
plan.image.fill(0x5A);
const r = new TableFixedReport();
const n = TLoad(back, back.length, data, data.length, plan, r);
if (n < 1) { fail(%[2]q + ": the LAWFUL control did not load: n=" + n + " " + reason(r)); }
else { console.log(%[4]q + "loaded " + n + ": lead=" + back[0].lead + " v=" + back[0].v + " trail=" + back[0].trail); }
`, file, row, poison, jsVerdictPrefix)
}

// jsVerdict reads the verdict line the probe printed.
func jsVerdict(t *testing.T, name string, out []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, jsVerdictPrefix); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("%s: the probe printed no verdict:\n%s", name, out)
	return ""
}

// TestJSFixedUnlawfulCorpus is the JavaScript leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file before any record, every counter zero,
// every poisoned row back at its declared defaults, on a first load and a
// second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30;
// SPEC-TABLES §6.5). The refusal's NAME, layout_malformed, is this leg's own
// pin (jsUnlawfulProbe) and is never compared across legs
// (refusal-verdict-only). JavaScript refuses a hostile load in every build.
func TestJSFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	fixedunlawful.Run(t, "jstable", jsUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a jsUnlawfulAnswer) string {
		t.Parallel()
		older := jsReadSchema(t, c.Old)
		newer := jsReadSchema(t, c.New)
		file := filepath.Join(corpus, c.File)
		var src string
		if c.Lawful {
			src = jsLawfulProbe(file, c.Name, a.poison)
		} else {
			// The verdict is printed only after every check held (a failed
			// check bumps failures first).
			src = jsUnlawfulProbe(file, c.Name, a.poison, a.atDefaults) +
				"if (failures === 0) { console.log(" + fmt.Sprintf("%q", jsVerdictPrefix) + " + \"refused\"); }\n"
		}
		out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, c.Root, src)
		if err != nil {
			t.Fatalf("NEW-READS-OLD %s (%s): %v\n%s", c.Name, c.Change, err, out)
		}
		return jsVerdict(t, c.Name, out)
	})
}
