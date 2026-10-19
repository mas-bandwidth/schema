// THE C++ TableRefuseReason CARRIES EVERY VALUE GO'S DOES, IN GO'S ORDER
// (docs/SPEC-TABLES.md §6.5, §11).
//
// The enum is one closed vocabulary with the same value names in every target,
// and the values a later card's strict read, carrier, integrity check or blob
// rule adds are APPENDED after data_cycle and never renumbered. Go is the
// oracle on this base (schema-go d808c3f54): internal/codegen/gotable's
// refusal.go is the iota block every Go unit emits, and the C++ emitter must
// spell the same names, in the same order, so a caller comparing a refusal
// against a value never has to know which language refused it.
//
// The test generates one unit for each target from the same schema text and
// reads the two enums back by name. Nothing is compiled: the emitter's output
// is the artefact under test, and reading it is what makes a missing value a
// failure even in an environment with no C++ compiler.
package compiler

import (
	"strings"
	"testing"
)

const cppRefuseReasonSchema = `package refusereason

fixed table Probe
{
    value int32
}
`

func TestCPPRefuseReasonMatchesGosOrder(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, cppRefuseReasonSchema)

	cppFiles, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("generate cpp: %v", err)
	}
	// standalone, not shared: the iota block is the oracle, and a shared unit
	// aliases the names out of tables/goruntime instead of spelling them.
	goFiles, err := New().Generate(u, "go", Options{"runtime": "standalone"})
	if err != nil {
		t.Fatalf("generate go: %v", err)
	}

	cpp := refusedReasonFile(t, cppFiles, "Table.h", "enum TableRefuseReason")
	gosrc := refusedReasonFile(t, goFiles, "Table.go", "type TableRefuseReason uint8")

	cppNames := cppRefuseReasonNames(t, string(cpp))
	goNames := goRefuseReasonNames(t, string(gosrc))

	// The oracle is the Go enum: every name it carries, in its order, after
	// the TableRefuse prefix is stripped and the CamelCase identifier turned
	// back into the snake_case spelling both targets emit.
	if len(cppNames) < len(goNames) {
		t.Fatalf("TableRefuseReason stops short of Go's: C++ has %d values %v, Go has %d %v",
			len(cppNames), cppNames, len(goNames), goNames)
	}
	for i, want := range goNames {
		if cppNames[i] != want {
			t.Fatalf("TableRefuseReason value %d is %q, want Go's %q\n  C++: %v\n   Go: %v",
				i, cppNames[i], want, cppNames, goNames)
		}
	}
}

// refusedReasonFile picks the one generated table file with suffix that
// carries marker. A target may emit several Table.* files (the unit's home
// runtime beside the per-schema one), and only the runtime carries the enum.
func refusedReasonFile(t *testing.T, files map[string][]byte, suffix, marker string) []byte {
	t.Helper()
	var name string
	for candidate, data := range files {
		if strings.HasSuffix(candidate, suffix) && strings.Contains(string(data), marker) {
			if name != "" {
				t.Fatalf("more than one generated file ends in %q and carries %q: %s and %s", suffix, marker, name, candidate)
			}
			name = candidate
		}
	}
	if name == "" {
		t.Fatalf("no generated file ends in %q and carries %q", suffix, marker)
	}
	return files[name]
}

// cppRefuseReasonNames reads the enumerators between "enum TableRefuseReason"
// and its closing "};", in source order. Each line holds one value and an
// optional // comment; the emitted layout is one value per line.
func cppRefuseReasonNames(t *testing.T, src string) []string {
	t.Helper()
	const marker = "enum TableRefuseReason"
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatalf("the generated C++ header carries no %q", marker)
	}
	rest := src[at:]
	open := strings.Index(rest, "{")
	if open < 0 {
		t.Fatalf("the C++ %s carries no opening brace", marker)
	}
	body := rest[open+1:]
	end := strings.Index(body, "};")
	if end < 0 {
		t.Fatalf("the C++ %s carries no closing \"};\"", marker)
	}
	var names []string
	for _, line := range strings.Split(body[:end], "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		if line == "" {
			continue
		}
		names = append(names, strings.Fields(line)[0])
	}
	return names
}

// goRefuseReasonNames reads the TableRefuse* identifiers of the generated
// iota block, in source order, and returns Go's name stripped of its
// TableRefuse prefix and snake_cased, which is the spelling §11 gives C++.
func goRefuseReasonNames(t *testing.T, src string) []string {
	t.Helper()
	const prefix = "TableRefuse"
	var names []string
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "const (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			break
		}
		if !inBlock || !strings.HasPrefix(line, prefix) {
			continue
		}
		name := strings.Fields(line)[0]
		names = append(names, snakeRefuseReasonName(strings.TrimPrefix(name, prefix)))
	}
	if len(names) == 0 {
		t.Fatalf("the generated Go source carries no %s* enum values", prefix)
	}
	return names
}

// snakeRefuseReasonName turns Go's CamelCase enumerator into the snake_case
// spelling shared by the ports. Acronyms and digit runs are boundaries too:
// NotACook -> not_a_cook, Base64Damaged -> base64_damaged, NonFinite ->
// non_finite.
func snakeRefuseReasonName(name string) string {
	var b strings.Builder
	r := []rune(name)
	for i, c := range r {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				prev := r[i-1]
				lowerOrDigit := prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9'
				acronymEnd := prev >= 'A' && prev <= 'Z' && i+1 < len(r) && r[i+1] >= 'a' && r[i+1] <= 'z'
				if lowerOrDigit || acronymEnd {
					b.WriteByte('_')
				}
			}
			b.WriteRune(c + ('a' - 'A'))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}
