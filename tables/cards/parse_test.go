package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "epoch17", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func headerValue(t *testing.T, data []byte, key string) string {
	t.Helper()
	prefix := key + ": "
	i := bytes.Index(data, []byte(prefix))
	if i < 0 {
		t.Fatalf("fixture is missing header %q", key)
	}
	rest := data[i+len(prefix):]
	j := bytes.IndexByte(rest, '\n')
	if j < 0 {
		return string(rest)
	}
	return string(rest[:j])
}

// TestParseRenderCorpus parses the normalized seed and asserts every header
// value, the exact body suffix, the extras arrival order on an inline brief,
// and the PATHS empty-list spellings (docs/SPEC-TABLES.md §16).
func TestParseRenderCorpus(t *testing.T) {
	t.Parallel()
	data := readFixture(t, "normalized-valid.md")
	rec, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse normalized-valid: %v", err)
	}
	root := rec.root
	if got := rowStr(root.Id[:], root.IdLength); got != "a-cut-ignores-the-previous-tags-cancelled-release-run" {
		t.Fatalf("id = %q", got)
	}
	if got := rowStr(root.Sha[:], root.ShaLength); got != "000000000000" {
		t.Fatalf("sha = %q", got)
	}
	if root.Tier != Tierpro {
		t.Fatalf("tier = %v", root.Tier)
	}
	if got := rowStr(root.Repo[:], root.RepoLength); got != "mas-bandwidth/nova-tools" {
		t.Fatalf("repo = %q", got)
	}
	if got := rowStr(root.Base[:], root.BaseLength); got != "dev" {
		t.Fatalf("base = %q", got)
	}
	if root.Priority != Priorityhigh {
		t.Fatalf("priority = %v", root.Priority)
	}
	paths := collectPaths(&root.Paths)
	if got := strings.Join(paths, ", "); got != headerValue(t, data, "PATHS") {
		t.Fatalf("paths = %q", got)
	}
	if got := strings.Join(collectPaths(&root.Shared), ", "); got != headerValue(t, data, "SHARED") {
		t.Fatalf("shared = %q", got)
	}
	if root.Needs.Count != 0 {
		t.Fatalf("needs has %d entries, want 0", root.Needs.Count)
	}
	if root.Extra.Count != 0 {
		t.Fatalf("extra has %d entries, want 0", root.Extra.Count)
	}
	if got := stringData(&root.Source, nil); string(got) != headerValue(t, data, "SOURCE") {
		t.Fatalf("source = %q", got)
	}

	// The body is the exact suffix after SOURCE's terminator.
	start := bytes.Index(data, []byte("SOURCE: "))
	nl := bytes.IndexByte(data[start:], '\n')
	wantBody := data[start+nl+1:]
	if got := stringData(&root.Body, nil); !bytes.Equal(got, wantBody) {
		t.Fatalf("body is not the exact suffix: got %d bytes, want %d", len(got), len(wantBody))
	}

	// Extras ride in arrival order.
	t.Run("extras-order", func(t *testing.T) {
		t.Parallel()
		brief := "RESULT: x sha=0123456789ab tier: flash\nLEG: go\nTOOLCHAIN: the go leg\nORDER: first-second\n"
		r, err := Parse([]byte(brief))
		if err != nil {
			t.Fatalf("Parse extras: %v", err)
		}
		var got []string
		CardExtraEach(&r.root.Extra, func(e *KeyValueRow) bool {
			got = append(got, rowStr(e.Key[:], e.KeyLength)+"="+string(stringData(&e.Value, nil)))
			return true
		})
		want := []string{"LEG=go", "TOOLCHAIN=the go leg", "ORDER=first-second"}
		if len(got) != len(want) {
			t.Fatalf("extras = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("extra %d = %q, want %q", i, got[i], want[i])
			}
		}
	})

	// PATHS none / - / empty all parse as empty lists, and the empty spelling
	// survives the Render -> Parse round trip: an empty list renders no PATHS
	// line, so the re-parsed record is empty too and the wire is unchanged.
	for _, spelling := range []string{"none", "-", ""} {
		spelling := spelling
		t.Run("paths-empty-"+spelling, func(t *testing.T) {
			t.Parallel()
			brief := "RESULT: x sha=0123456789ab tier: flash\nPATHS: " + spelling + "\n"
			r, err := Parse([]byte(brief))
			if err != nil {
				t.Fatalf("Parse PATHS %q: %v", spelling, err)
			}
			if r.root.Paths.Count != 0 {
				t.Fatalf("PATHS %q parsed to %d entries", spelling, r.root.Paths.Count)
			}
			text, rep, err := Render(r)
			if err != nil {
				t.Fatalf("Render PATHS %q: %v (report %+v)", spelling, err, rep)
			}
			r2, err := Parse(text)
			if err != nil {
				t.Fatalf("Parse(Render(PATHS %q)): %v", spelling, err)
			}
			if r2.root.Paths.Count != 0 {
				t.Fatalf("PATHS %q re-parsed to %d entries after Render", spelling, r2.root.Paths.Count)
			}
			if !bytes.Equal(r2.Wire(), r.Wire()) {
				t.Fatalf("PATHS %q round trip changed the wire", spelling)
			}
		})
	}

	// NEW does not take the "none" sentinel.
	t.Run("new-none-is-literal", func(t *testing.T) {
		t.Parallel()
		r, err := Parse([]byte("RESULT: x sha=0123456789ab tier: flash\nNEW: none\n"))
		if err != nil {
			t.Fatalf("Parse NEW none: %v", err)
		}
		got := collectPaths(&r.root.New)
		if len(got) != 1 || got[0] != "none" {
			t.Fatalf("NEW none parsed to %v, want [none]", got)
		}
	})
}

// TestParseRefusesAStrandedKey refuses the customer's exact stranded-key set
// once prose has begun, and preserves opaque KEY-shaped prose lines.
func TestParseRefusesAStrandedKey(t *testing.T) {
	t.Parallel()
	t.Run("rejected-legacy", func(t *testing.T) {
		t.Parallel()
		data := readFixture(t, "rejected-legacy.md")
		_, err := Parse(data)
		if err == nil {
			t.Fatal("Parse accepted a stranded-key brief")
		}
		if !strings.Contains(err.Error(), "PRIORITY") || !strings.Contains(err.Error(), "line 5") {
			t.Fatalf("refusal did not name PRIORITY and line 5: %v", err)
		}
	})
	t.Run("source-after-prose", func(t *testing.T) {
		t.Parallel()
		_, err := Parse([]byte("RESULT: x sha=000000000000 tier: pro\nsome prose\nSOURCE: late\n"))
		if err == nil {
			t.Fatal("accepted SOURCE after prose")
		}
		if !strings.Contains(err.Error(), "SOURCE") {
			t.Fatalf("refusal did not name SOURCE: %v", err)
		}
	})
	for _, k := range []string{
		"KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
		"PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT",
	} {
		k := k
		t.Run("stranded-"+k, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte("RESULT: x sha=000000000000 tier: pro\nprose line\n" + k + ": v\n"))
			if err == nil {
				t.Fatalf("accepted stranded key %q", k)
			}
			if !strings.Contains(err.Error(), k) {
				t.Fatalf("refusal did not name %q: %v", k, err)
			}
		})
	}
	t.Run("opaque-prose-keys", func(t *testing.T) {
		t.Parallel()
		rec, err := Parse([]byte("RESULT: x sha=000000000000 tier: pro\nsome prose\nNOTE: an opaque line\nExample-Key: also opaque\n"))
		if err != nil {
			t.Fatalf("opaque prose keys refused: %v", err)
		}
		body := stringData(&rec.root.Body, nil)
		want := "some prose\nNOTE: an opaque line\nExample-Key: also opaque\n"
		if string(body) != want {
			t.Fatalf("body = %q, want %q", body, want)
		}
	})
}

// bodyBoundaryCases is the body-suffix matrix shared by TestParseBodyBoundary
// and TestRenderRoundTrip: blank lines, prose positions, CRLF, empty bodies and
// final-newline variants.
var bodyBoundaryCases = []struct {
	name string
	text string
	body string
}{
	{"direct-prose", "RESULT: x sha=000000000000 tier: pro\nprose\n", "prose\n"},
	{"no-final-newline", "RESULT: x sha=000000000000 tier: pro\nprose", "prose"},
	{"inter-header-blanks", "RESULT: x sha=000000000000 tier: pro\n\nREPO: r\n\nBASE: b\nbody\n", "body\n"},
	{"trailing-blank", "RESULT: x sha=000000000000 tier: pro\n\n", "\n"},
	{"blanks-before-prose", "RESULT: x sha=000000000000 tier: pro\n\n\nprose\n", "\n\nprose\n"},
	{"empty-body", "RESULT: x sha=000000000000 tier: pro\n", ""},
	{"crlf", "RESULT: x sha=000000000000 tier: pro\r\nprose\r\n", "prose\r\n"},
}

// TestParseBodyBoundary pins the body-suffix rule across blank lines, prose
// positions, CRLF, empty bodies and final-newline variants.
func TestParseBodyBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range bodyBoundaryCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := Parse([]byte(tc.text))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			body := stringData(&rec.root.Body, nil)
			if string(body) != tc.body {
				t.Fatalf("body = %q, want %q", body, tc.body)
			}
		})
	}
}

// TestParseRefusals pins the refusal matrix: bad RESULT parts, duplicates,
// disagreement, bad enum/deadline/list values and the extras/body ceilings.
func TestParseRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want string
	}{
		{"short-sha", "RESULT: x sha=00000000000 tier: pro\n", "sha"},
		{"long-sha", "RESULT: x sha=0123456789abcde tier: pro\n", "sha"},
		{"nonhex-sha", "RESULT: x sha=zzzzzzzzzzzz tier: pro\n", "sha"},
		{"bad-tier", "RESULT: x sha=000000000000 tier: unknown\n", "tier"},
		{"overbound-id", "RESULT: " + strings.Repeat("i", 65) + " sha=000000000000 tier: pro\n", "id"},
		{"bad-priority", "RESULT: x sha=000000000000 tier: pro\nPRIORITY: urgent\n", "priority"},
		{"bad-deadline", "RESULT: x sha=000000000000 tier: pro\nDEADLINE: soon\n", "DEADLINE"},
		{"deadline-over", "RESULT: x sha=000000000000 tier: pro\nDEADLINE: finish within 100001 minutes.\n", "DEADLINE"},
		{"tier-disagrees", "RESULT: x sha=000000000000 tier: pro\ntier: heavy\n", "disagrees"},
		{"duplicate-repo", "RESULT: x sha=000000000000 tier: pro\nREPO: a\nREPO: b\n", "duplicate"},
		{"overbound-list", "RESULT: x sha=000000000000 tier: pro\nPATHS: " + strings.Repeat("p", 193) + "\n", "192"},
		{"crlf-in-value", "RESULT: x sha=000000000000 tier: pro\nREPO: a\rb\n", "CR or LF"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := Parse([]byte(tc.text))
			if err == nil {
				t.Fatal("Parse accepted a bad input")
			}
			if rec != nil {
				t.Fatal("Parse returned a non-nil record for a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal does not name %q: %v", tc.want, err)
			}
		})
	}

	t.Run("extras-overflow", func(t *testing.T) {
		t.Parallel()
		var b strings.Builder
		b.WriteString("RESULT: x sha=000000000000 tier: pro\n")
		for i := 0; i < MaxListEntries+1; i++ {
			b.WriteString("K")
			b.WriteString(string(rune('A' + i%26)))
			b.WriteString(": v\n")
		}
		_, err := Parse([]byte(b.String()))
		if err == nil || !strings.Contains(err.Error(), "extra") {
			t.Fatalf("extras overflow not refused naming extra: %v", err)
		}
	})

	t.Run("body-overflow", func(t *testing.T) {
		t.Parallel()
		text := "RESULT: x sha=000000000000 tier: pro\n" + strings.Repeat("x", MaxBodyBytes+1)
		_, err := Parse([]byte(text))
		if err == nil || !strings.Contains(err.Error(), "body") {
			t.Fatalf("body overflow not refused naming body: %v", err)
		}
	})
}

// TestRenderRoundTrip proves Render emits RESULT first, the mapped keys in the
// fixed declaration order, extras in stored order, then the body bytes verbatim,
// and that Parse(Render(Parse(t))) keeps the owned wire while FromJSON(ToJSON)
// keeps every header field and the body with a clean report (docs/SPEC-TABLES.md
// §16).
func TestRenderRoundTrip(t *testing.T) {
	t.Parallel()
	data := readFixture(t, "normalized-valid.md")
	rec, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	start := bytes.Index(data, []byte("SOURCE: "))
	nl := bytes.IndexByte(data[start:], '\n')
	wantBody := data[start+nl+1:]

	text, rep, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v (report %+v)", err, rep)
	}
	if !bytes.HasPrefix(text, []byte("RESULT: a-cut-ignores-the-previous-tags-cancelled-release-run sha=000000000000 tier: pro\n")) {
		t.Fatalf("render does not start with RESULT: %q", text[:80])
	}
	for _, k := range []string{"RESULT:", "REPO:", "BASE:", "PRIORITY:", "PATHS:", "SHARED:", "SOURCE:"} {
		if !bytes.Contains(text, []byte(k)) {
			t.Fatalf("render is missing %q", k)
		}
	}
	if strings.Index(string(text), "REPO:") > strings.Index(string(text), "BASE:") ||
		strings.Index(string(text), "BASE:") > strings.Index(string(text), "PRIORITY:") ||
		strings.Index(string(text), "PRIORITY:") > strings.Index(string(text), "PATHS:") ||
		strings.Index(string(text), "PATHS:") > strings.Index(string(text), "SHARED:") ||
		strings.Index(string(text), "SHARED:") > strings.Index(string(text), "SOURCE:") {
		t.Fatalf("render emits mapped keys out of order:\n%s", text)
	}
	if !bytes.HasSuffix(text, wantBody) {
		t.Fatalf("render does not end with the exact body bytes")
	}

	rec2, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(Render(Parse(t))): %v", err)
	}
	if !bytes.Equal(rec2.Wire(), rec.Wire()) {
		t.Fatal("Parse(Render(Parse(t))) changed the wire")
	}
	text2, _, err := Render(rec2)
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if !bytes.Equal(text2, text) {
		t.Fatal("repeated round trips are not stable")
	}

	// Every body-boundary case also round trips: Parse -> Render -> Parse keeps
	// the owned wire, so the exact opaque suffix never grows a separator.
	for _, tc := range bodyBoundaryCases {
		tc := tc
		t.Run("boundary-"+tc.name, func(t *testing.T) {
			t.Parallel()
			r, err := Parse([]byte(tc.text))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			text, rep, err := Render(r)
			if err != nil {
				t.Fatalf("Render: %v (report %+v)", err, rep)
			}
			r2, err := Parse(text)
			if err != nil {
				t.Fatalf("Parse(Render(Parse(t))): %v", err)
			}
			if !bytes.Equal(r2.Wire(), r.Wire()) {
				t.Fatalf("Parse(Render(Parse(t))) changed the wire for %q", tc.name)
			}
		})
	}

	json, _, err := ToJSON(rec)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	rec3, err := FromJSON(json)
	if err != nil {
		t.Fatalf("FromJSON(ToJSON(parsed)): %v", err)
	}
	if !bytes.Equal(rec3.Wire(), rec.Wire()) {
		t.Fatal("FromJSON(ToJSON(parsed)) changed the wire")
	}
	if got := stringData(&rec3.root.Body, nil); !bytes.Equal(got, wantBody) {
		t.Fatal("FromJSON(ToJSON(parsed)) changed the body")
	}
}

// TestDoneWhenTextRoundTrip proves the renamed done field keeps its DONE-WHEN
// brief spelling (§16): Parse reads DONE-WHEN, Render emits DONE-WHEN (the wire
// rename via `was` does not rename the brief header), and the value survives
// Parse(Render(Parse(t))) byte for byte.
func TestDoneWhenTextRoundTrip(t *testing.T) {
	t.Parallel()
	text := []byte("RESULT: done-round-trip sha=000000000000 tier: pro\n" +
		"REPO: mas-bandwidth/schema\n" +
		"KIND: feature\n" +
		"TEST: test\n" +
		"DONE-WHEN: make cards-test\n" +
		"SOURCE: source\n")
	rec, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rendered, _, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Contains(rendered, []byte("DONE-WHEN: make cards-test")) {
		t.Fatalf("Render lost the DONE-WHEN spelling:\n%s", rendered)
	}
	rec2, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse(Render): %v", err)
	}
	if !bytes.Equal(rec2.Wire(), rec.Wire()) {
		t.Fatal("Parse(Render(Parse(t))) changed the wire")
	}
}

// TestExtrasCheckedWriteRefused proves the shared checked-write validation
// refuses an extra whose key is not an admitted header key or whose key or
// value carries a CR or LF, and an extra key duplicating a mapped header name,
// so Render's informational text stays a sequence of admitted header lines
// (HEADER MAPPING, docs/SPEC-TABLES.md §16).
func TestExtrasCheckedWriteRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		json string
		want string
	}{
		{"space-in-key", `{"id":"x","sha":"000000000000","tier":"pro","extra":[{"key":"BAD KEY","value":"v"}]}`, "not an admitted header key"},
		{"colon-in-key", `{"id":"x","sha":"000000000000","tier":"pro","extra":[{"key":"A:B","value":"v"}]}`, "not an admitted header key"},
		{"lf-in-value", `{"id":"x","sha":"000000000000","tier":"pro","extra":[{"key":"LEG","value":"a\nb"}]}`, "CR or LF"},
		{"cr-in-value", `{"id":"x","sha":"000000000000","tier":"pro","extra":[{"key":"LEG","value":"a\rb"}]}`, "CR or LF"},
		{"reserved-key", `{"id":"x","sha":"000000000000","tier":"pro","extra":[{"key":"REPO","value":"v"}]}`, "duplicates a mapped header key"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := FromJSON([]byte(tc.json))
			if err == nil {
				t.Fatalf("FromJSON accepted malformed extras: %v", rec)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal does not name %q: %v", tc.want, err)
			}
		})
	}
}

// TestRenderInformationalMachineState proves a loaded record whose machine
// state (stream, epoch, column, holder, attempt, head, branch) is populated
// renders its informational text — RESULT, the mapped keys and the body — while
// the record's owned wire is unchanged; parsing that text leaves the machine
// fields at their declaration defaults (docs/SPEC-TABLES.md §16).
func TestRenderInformationalMachineState(t *testing.T) {
	t.Parallel()
	wire := buildPopulated(t)
	rec, rep, err := Load(wire)
	if err != nil {
		t.Fatalf("Load: %v (report %+v)", err, rep)
	}
	text, rep, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v (report %+v)", err, rep)
	}
	if !bytes.Equal(rec.Wire(), wire) {
		t.Fatal("Render changed the record's wire")
	}
	if !bytes.HasPrefix(text, []byte("RESULT: card-1234 sha=")) {
		t.Fatalf("render does not start with RESULT: %q", text[:40])
	}
	if !bytes.Contains(text, []byte("REPO: mas-bandwidth/schema\n")) {
		t.Fatal("render is missing REPO")
	}
	if !bytes.HasSuffix(text, []byte(makeBody(t))) {
		t.Fatal("render does not end with the exact body bytes")
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(render): %v", err)
	}
	if back.root.Epoch != 0 || back.root.Attempt != 0 {
		t.Fatal("parse of the informational text reconstructed machine scalars")
	}
	if s := rowStr(back.root.Stream[:], back.root.StreamLength); s != "" {
		t.Fatalf("parse reconstructed stream %q", s)
	}
	if s := rowStr(back.root.Holder[:], back.root.HolderLength); s != "" {
		t.Fatalf("parse reconstructed holder %q", s)
	}
	if s := rowStr(back.root.Head[:], back.root.HeadLength); s != "" {
		t.Fatalf("parse reconstructed head %q", s)
	}
	if s := rowStr(back.root.Branch[:], back.root.BranchLength); s != "" {
		t.Fatalf("parse reconstructed branch %q", s)
	}
}
