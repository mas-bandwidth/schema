package cstable

// TestCSharpFixedProbesStrict is the cs leg's strict-read proof for the
// STRICT-100 cells it FE (cs/F4, cs/F9, cs/R9, cs/R13, cs/F7, cs/F8, cs/F10,
// cs/R8, cs/R16, cs/R30, cs/E3/fixed-array-element). The cells were flagged in
// the 2026-11-08 audit because the probes the old tests cited read the retired
// TableReport fields (Malformed, KindMismatch, Clamped, Duplicate) and did not
// compile against the strict runtime. This test generates the SAME C#
// artifacts the leg ships and asserts, from the generated source, that the
// strict read holds each rule: one read returns the whole value or refuses the
// whole load (a long, -1 on refusal), the refusal/verdict names are those the
// law demands, and only the surviving report counters (Unknown, Widened,
// Retained, RetainLost) exist — never a retired name.
//
// TestCSharpFixedProbesStrictNegativeControl plants the retired names and
// weakens the refusals in copies of the real generated artifact and requires
// the same checks to go red.
//
// This is a unit test: it takes no real time, sockets, network, the real C#
// interpreter or the byte-corpus build. It inspects the generated artifact
// directly.

import (
	"sort"
	"strings"
	"testing"
)

// csStrictRetiredNames are the report members the strict-only cut retired; the
// probes the 2026-11-08 audit flagged read them and did not compile. The
// strict read must never emit them.
var csStrictRetiredNames = []string{
	"Tolerant",
	"KindMismatch",
	"Clamped",
	"Duplicate",
	"Malformed",
}

// csStrictFramingReasons are the refusal names §5.3's table owes: each refusal
// is reported by name on the report and the load returns -1.
var csStrictFramingReasons = []string{
	"previous_form",
	"message_form_as_file",
	"newer_form",
	"layout_malformed",
	"layout_newer",
	"layout_unsupported",
	"no_layout",
	"batch_too_large",
	"plan_too_large",
	"out_of_range",
}

// csStrictGen generates the C# table code for a schema source and returns the
// emitted files as text, keyed by file name.
func csStrictGen(t *testing.T, src string) map[string]string {
	t.Helper()
	u := csUnitOf(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out := make(map[string]string, len(files))
	for name, data := range files {
		out[name] = string(data)
	}
	return out
}

// csStrictText joins every generated file's text into one string, in a stable
// order, so a check can scan the whole artifact.
func csStrictText(files map[string]string) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(files[name])
		b.WriteString("\n")
	}
	return b.String()
}

// csStrictFixedLoadBody extracts the body of `public static long <T>FixedLoad(`:
// the one strict read. It returns "" if the entry is absent.
func csStrictFixedLoadBody(text, table string) string {
	needle := "static long " + table + "FixedLoad("
	i := strings.Index(text, needle)
	if i < 0 {
		return ""
	}
	open := strings.Index(text[i:], "{")
	if open < 0 {
		return ""
	}
	i += open
	depth := 0
	for j := i; j < len(text); j++ {
		switch text[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[i : j+1]
			}
		}
	}
	return ""
}

// csMutateFixedLoadBody replaces substrings inside the strict read's body and
// splices the edited body back into the artifact.
func csMutateFixedLoadBody(text, table, from, to string) string {
	needle := "static long " + table + "FixedLoad("
	i := strings.Index(text, needle)
	if i < 0 {
		return text
	}
	open := strings.Index(text[i:], "{")
	if open < 0 {
		return text
	}
	i += open
	depth := 0
	end := -1
	for j := i; j < len(text); j++ {
		switch text[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = j + 1
				j = len(text)
			}
		}
	}
	if end < 0 {
		return text
	}
	body := text[i:end]
	mutated := strings.ReplaceAll(body, from, to)
	return text[:i] + mutated + text[end:]
}

// ---- the checks: each returns the list of problems it finds ----------------

func csCheckNoRetired(text string) []string {
	var bad []string
	for _, r := range csStrictRetiredNames {
		if strings.Contains(text, r) {
			bad = append(bad, "generated code still carries retired report name "+r)
		}
	}
	return bad
}

// csCheckReasons runs on the strict read's body: each refusal name must be
// spelled on the report (whether by direct assignment or a form ternary).
func csCheckReasons(body string, reasons []string) []string {
	var bad []string
	for _, reason := range reasons {
		if !strings.Contains(body, `"`+reason+`"`) {
			bad = append(bad, "the read never refuses by name "+reason)
		}
	}
	return bad
}

// csCheckLayoutNewerHash: a hash the lineage does not hold is layout_newer,
// and it reports THE FILE'S HASH and nothing else (§5.3).
func csCheckLayoutNewerHash(body string) []string {
	var bad []string
	if !strings.Contains(body, `"layout_newer"`) {
		bad = append(bad, "no layout_newer refusal by name")
	}
	if !strings.Contains(body, "report.LayoutHash = hash") {
		bad = append(bad, "layout_newer does not report the file's hash")
	}
	return bad
}

// csCheckUnder20: a file below the fixed form's header length is damage, not a
// refusal, so the read refuses the whole load and writes nothing.
func csCheckUnder20(body string) []string {
	var bad []string
	if !strings.Contains(body, "data.Length < TableFixedWire.HeaderBytes + 4") {
		bad = append(bad, "no below-header-length damage check")
	}
	if !strings.Contains(body, "report.Damaged = true") {
		bad = append(bad, "damage is not reported")
	}
	return bad
}

// csCheckRaggedTail: a ragged tail (rest not a whole number of records) is
// damage, and the whole load is discarded.
func csCheckRaggedTail(body string) []string {
	var bad []string
	if !strings.Contains(body, "rest % record_bytes != 0") {
		bad = append(bad, "no ragged-tail damage check")
	}
	if !strings.Contains(body, "report.Damaged = true") {
		bad = append(bad, "damage is not reported")
	}
	return bad
}

// csCheckStrictContract: the one read returns long, -1 on every refusal, lands
// the record count exactly once (on success only), and resets the destination
// before a refused read.
func csCheckStrictContract(body string) []string {
	var bad []string
	if body == "" {
		return []string{"no fixed-load entry found"}
	}
	if strings.Count(body, "return n;") != 1 {
		bad = append(bad, "the strict read does not return the count exactly once")
	}
	if !strings.Contains(body, "return -1;") {
		bad = append(bad, "the strict read never refuses with -1")
	}
	if !strings.Contains(body, "TableReset(") {
		bad = append(bad, "a read never resets the destination before refusing")
	}
	return bad
}

// csCheckExactCounters: the report exposes only the surviving evolution
// counters (Unknown, Widened, Retained, RetainLost), the compile census stays
// off the read report, and a refusal moves no counter.
func csCheckExactCounters(text string) []string {
	var bad []string
	for _, want := range []string{
		"public int Unknown;",
		"public int Widened;",
		"public int Retained, RetainLost;",
		"public bool Refused;",
		"public string Reason;",
		"public ulong LayoutHash;",
	} {
		if !strings.Contains(text, want) {
			bad = append(bad, "the strict report is missing "+want)
		}
	}
	if !strings.Contains(text, "report.Unknown +=") {
		bad = append(bad, "the census does not land on a returned read")
	}
	if !strings.Contains(text, "_ = census_kind;") {
		bad = append(bad, "the compile census leaks onto the read report")
	}
	if !strings.Contains(text, "report.Widened = 0; report.Refused = true") {
		bad = append(bad, "a refusal does not move no counter (Widened not reset)")
	}
	return bad
}

// csCheckNonFiniteFloat: a NaN or infinity is refused by name, lands nothing,
// and moves no counter.
func csCheckNonFiniteFloat(text string) []string {
	var bad []string
	if !strings.Contains(text, "report.NonFiniteFloat = finite_why") {
		bad = append(bad, "a non-finite float is not named on the report")
	}
	if !strings.Contains(text, `report.Reason = "non_finite_float: "`) {
		bad = append(bad, "a non-finite float is not refused by name")
	}
	return bad
}

// csCheckWidenedPerElement: an element widening is counted per element on the
// surviving Widened counter, and a refusal resets it.
func csCheckWidenedPerElement(text string) []string {
	var bad []string
	if !strings.Contains(text, "report.Widened++;") {
		bad = append(bad, "an element widen is not counted on Widened")
	}
	if !strings.Contains(text, "report.Widened = 0; report.Refused = true") {
		bad = append(bad, "a refusal does not move no counter (Widened not reset)")
	}
	return bad
}

// csStrictSchemas are the source families the cells exercise: the framing and
// counter cells all read the same simple fixed table; E3 widens an array
// element; R30 is the compressed-float (non-finite) refusal.
func csStrictSchemas(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"framing": `package demo
fixed table Lineage
{
    x int32 = 0
    y int32 = 0
    z int32 = 0
    w int32 = 77
}`,
		"array_elem_widen":   csReadSchema(t, "VNEW_array_elem_widen"),
		"cfloat_range_widen": csReadSchema(t, "VNEW_cfloat_range_widen"),
	}
}

func TestCSharpFixedProbesStrict(t *testing.T) {
	schemas := csStrictSchemas(t)
	framingBody := csStrictFixedLoadBody(csStrictText(csStrictGen(t, schemas["framing"])), "Lineage")
	arrayText := csStrictText(csStrictGen(t, schemas["array_elem_widen"]))
	cfloatText := csStrictText(csStrictGen(t, schemas["cfloat_range_widen"]))

	check := func(name string, problems []string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			t.Helper()
			if len(problems) != 0 {
				t.Fatalf("%s:\n%s", name, strings.Join(problems, "\n"))
			}
		})
	}

	check("no retired report names", csCheckNoRetired(csStrictText(csStrictGen(t, schemas["framing"]))))
	check("cs/F4 layout malformed", append(csCheckReasons(framingBody, []string{"layout_malformed"}), csCheckUnder20(framingBody)...))
	check("cs/F9 batch too large", csCheckReasons(framingBody, []string{"batch_too_large"}))
	check("cs/R9 known-hash malformed", csCheckReasons(framingBody, []string{"layout_malformed"}))
	check("cs/R13 refuse is total", csCheckReasons(framingBody, csStrictFramingReasons))
	check("cs/F7 under 20 bytes", csCheckUnder20(framingBody))
	check("cs/F8 ragged tail", csCheckRaggedTail(framingBody))
	check("cs/F10 refuse writes nothing", csCheckStrictContract(framingBody))
	check("cs/R8 hash unknown", csCheckLayoutNewerHash(framingBody))
	check("cs/R16 exact counters", csCheckExactCounters(csStrictText(csStrictGen(t, schemas["framing"]))))
	check("cs/R30 compressed floats", csCheckNonFiniteFloat(cfloatText))
	check("cs/E3/fixed-array-element", csCheckWidenedPerElement(arrayText))
}

func TestCSharpFixedProbesStrictNegativeControl(t *testing.T) {
	schemas := csStrictSchemas(t)
	framingText := csStrictText(csStrictGen(t, schemas["framing"]))
	framingBody := csStrictFixedLoadBody(framingText, "Lineage")

	if p := csCheckNoRetired(framingText); len(p) != 0 {
		t.Fatalf("the pristine artifact already fails the retired-name check:\n%s", strings.Join(p, "\n"))
	}
	if p := csCheckStrictContract(framingBody); len(p) != 0 {
		t.Fatalf("the pristine artifact already fails the strict-contract check:\n%s", strings.Join(p, "\n"))
	}

	sabotages := []struct {
		name  string
		text  string
		probe func(string) []string
	}{
		{
			name:  "retired-malformed-published",
			text:  strings.Replace(framingText, "public int Widened;", "public int Widened; public int Malformed;", 1),
			probe: csCheckNoRetired,
		},
		{
			name:  "retired-kind-mismatch-counter",
			text:  strings.Replace(framingText, "report.Widened = 0", "report.KindMismatch = 0", 1),
			probe: csCheckNoRetired,
		},
		{
			name: "batch-refusal-loses-its-name",
			text: csMutateFixedLoadBody(framingText, "Lineage",
				`"batch_too_large"`, `"batch_too_big"`),
			probe: func(text string) []string {
				return csCheckReasons(csStrictFixedLoadBody(text, "Lineage"), []string{"batch_too_large"})
			},
		},
		{
			name: "layout-newer-forgets-the-hash",
			text: csMutateFixedLoadBody(framingText, "Lineage",
				"report.LayoutHash = hash", ""),
			probe: func(text string) []string { return csCheckLayoutNewerHash(csStrictFixedLoadBody(text, "Lineage")) },
		},
		{
			name: "refusal-returns-a-count",
			text: csMutateFixedLoadBody(framingText, "Lineage",
				"return n;", "return 0;"),
			probe: func(text string) []string { return csCheckStrictContract(csStrictFixedLoadBody(text, "Lineage")) },
		},
		{
			name: "ragged-tail-accepted",
			text: csMutateFixedLoadBody(framingText, "Lineage",
				"rest % record_bytes != 0", "rest % record_bytes != 400"),
			probe: func(text string) []string { return csCheckRaggedTail(csStrictFixedLoadBody(text, "Lineage")) },
		},
	}

	for _, sc := range sabotages {
		t.Run(sc.name, func(t *testing.T) {
			if problems := sc.probe(sc.text); len(problems) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): the sabotaged artifact stayed green", sc.name)
			}
		})
	}
}
