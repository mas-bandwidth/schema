package gotable

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestGoUnboundedDescriptorIsExplicit holds docs/SPEC-TABLES.md §8.1 (the
// UnboundedContainer column, with array_bound 0 beside it) in the Go backend.
// It is a pure-Go test: it reads the descriptor rows out of the emitter's own
// Generate output in-process and never compiles or runs a unit, so it costs no
// toolchain and needs no slowtest.Gate.
//
//   - a map (map[string(8)]int32 and map[int32]int32), a []T ([]int32) and a
//     text list []string(8) report UnboundedContainer true and ArrayBound 0, so
//     a reflection reader can tell "no bound" from a bound of 2^31 - 1;
//   - a bounded [..4]T reports UnboundedContainer false and ArrayBound 4;
//   - the same unit mixes unbounded TEXT (a bare-string text list []string and
//     a map[string]int32 key, §2.13.1) with the containers: the text element
//     and the text key report Unbounded true and UnboundedContainer false, and
//     the container fields report Unbounded false;
//   - bounded text is neither column, with ArrayBound 8.
//
// The round trip itself lives in unboundeddescriptor_slow_test.go behind
// slowtest.Gate.
func TestGoUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()
	files := unboundedDescriptorFiles(t)
	rows := parseDescriptorFields(t, files, "RootTableType")
	if len(rows) != len(unboundedDescriptorWant()) {
		t.Fatalf("RootTable descriptor: %d fields, want %d", len(rows), len(unboundedDescriptorWant()))
	}
	if err := checkDescriptorFields(rows, unboundedDescriptorWant()); err != nil {
		t.Fatal(err)
	}

	// unbounded TEXT is the other column (§2.13.1): on the element and the key
	names := parseDescriptorFields(t, files, rows[5].Table)
	if err := checkDescriptorFields(names, []descriptorField{{Name: "value", Unbounded: true}}); err != nil {
		t.Fatal(err)
	}
	byname := parseDescriptorFields(t, files, rows[6].Table)
	if err := checkDescriptorFields(byname, []descriptorField{{Name: "key", Unbounded: true}}); err != nil {
		t.Fatal(err)
	}

	// bounded text is neither
	tags := parseDescriptorFields(t, files, rows[0].Table)
	if err := checkDescriptorFields(tags, []descriptorField{{Name: "key", ArrayBound: 8}}); err != nil {
		t.Fatal(err)
	}
	words := parseDescriptorFields(t, files, rows[3].Table)
	if err := checkDescriptorFields(words, []descriptorField{{Name: "value", ArrayBound: 8}}); err != nil {
		t.Fatal(err)
	}
}

// TestGoUnboundedDescriptorCheckCatchesAWrongColumn is the REVERSED WITNESS:
// the checker above must refuse a descriptor row for tags that loses the §8.1
// tell, and name the field it refused.
func TestGoUnboundedDescriptorCheckCatchesAWrongColumn(t *testing.T) {
	t.Parallel()
	files := unboundedDescriptorFiles(t)
	for _, tc := range []struct {
		name   string
		mutate func(*descriptorField)
	}{
		{"UnboundedContainer false", func(f *descriptorField) { f.UnboundedContainer = false }},
		{"ArrayBound 2147483647", func(f *descriptorField) { f.ArrayBound = 2147483647 }},
	} {
		rows := parseDescriptorFields(t, files, "RootTableType")
		found := false
		for i := range rows {
			if rows[i].Name == "tags" {
				tc.mutate(&rows[i])
				found = true
			}
		}
		if !found {
			t.Fatal("tags: field not found in the descriptor")
		}
		err := checkDescriptorFields(rows, unboundedDescriptorWant())
		if err == nil || !strings.Contains(err.Error(), "tags") {
			t.Fatalf("%s: check accepted a wrong tags row (err %v)", tc.name, err)
		}
	}
}

const unboundedDescriptorSchema = `package probe

table Root
{
    tags map[string(8)]int32
    ints map[int32]int32
    nums []int32
    words []string(8)
    quad [..4]int32
    names []string
    byname map[string]int32
}
`

// descriptorField is one TableFieldInfo row as the emitter writes it: only the
// columns §8.1 and §2.13.1 are read here.
type descriptorField struct {
	Name               string
	IsArray            bool
	List, Map          bool
	UnboundedContainer bool
	Unbounded          bool
	ArrayBound         int32
	Table              string
}

func unboundedDescriptorWant() []descriptorField {
	return []descriptorField{
		{Name: "tags", IsArray: true, Map: true, UnboundedContainer: true},
		{Name: "ints", IsArray: true, Map: true, UnboundedContainer: true},
		{Name: "nums", IsArray: true, List: true, UnboundedContainer: true},
		{Name: "words", IsArray: true, List: true, UnboundedContainer: true},
		{Name: "quad", IsArray: true, ArrayBound: 4},
		{Name: "names", IsArray: true, List: true, UnboundedContainer: true},
		{Name: "byname", IsArray: true, Map: true, UnboundedContainer: true},
	}
}

func checkDescriptorFields(rows, want []descriptorField) error {
	for _, w := range want {
		var got *descriptorField
		for i := range rows {
			if rows[i].Name == w.Name {
				got = &rows[i]
				break
			}
		}
		if got == nil {
			return fmt.Errorf("descriptor %s: field not found", w.Name)
		}
		if got.IsArray != w.IsArray || got.List != w.List || got.Map != w.Map ||
			got.UnboundedContainer != w.UnboundedContainer || got.Unbounded != w.Unbounded ||
			got.ArrayBound != w.ArrayBound {
			return fmt.Errorf("descriptor %s: IsArray=%v List=%v Map=%v UnboundedContainer=%v Unbounded=%v ArrayBound=%d; want IsArray=%v List=%v Map=%v UnboundedContainer=%v Unbounded=%v ArrayBound=%d",
				w.Name, got.IsArray, got.List, got.Map, got.UnboundedContainer, got.Unbounded, got.ArrayBound,
				w.IsArray, w.List, w.Map, w.UnboundedContainer, w.Unbounded, w.ArrayBound)
		}
	}
	return nil
}

func unboundedDescriptorFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := GenerateLineageSurface(unitFrom(t, unboundedDescriptorSchema), nil, Surfaces{Wire: true})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return files
}

func parseDescriptorFields(t *testing.T, files map[string][]byte, typeName string) []descriptorField {
	t.Helper()
	marker := "var " + strings.TrimSuffix(typeName, "Type") + "Fields = []TableFieldInfo{"
	var body string
	for _, data := range files {
		src := string(data)
		if i := strings.Index(src, marker); i >= 0 {
			rest := src[i+len(marker):]
			j := strings.Index(rest, "\n}\n")
			if j < 0 {
				t.Fatalf("descriptor %s: unterminated fields variable", typeName)
			}
			body = rest[:j]
			break
		}
	}
	if body == "" {
		t.Fatalf("descriptor %s: fields variable not generated", typeName)
	}
	chunks := strings.Split(body, "\n\t{Name: \"")
	rows := make([]descriptorField, 0, len(chunks)-1)
	for _, chunk := range chunks[1:] {
		q := strings.Index(chunk, "\"")
		if q < 0 {
			t.Fatalf("descriptor %s: row without a name", typeName)
		}
		row := descriptorField{
			Name:               chunk[:q],
			IsArray:            regexpBool(isArrayRE, chunk),
			List:               regexpBool(listRE, chunk),
			Map:                regexpBool(mapRE, chunk),
			UnboundedContainer: regexpBool(containerRE, chunk),
			Unbounded:          regexpBool(unboundedRE, chunk),
			ArrayBound:         regexpBound(t, chunk),
		}
		if m := tableRE.FindStringSubmatch(chunk); len(m) == 2 && m[1] != "nil" {
			row.Table = m[1]
		}
		rows = append(rows, row)
	}
	return rows
}

var (
	isArrayRE   = regexp.MustCompile(`IsArray: (true|false)`)
	listRE      = regexp.MustCompile(`\n\t+List:\s*(true|false)`)
	mapRE       = regexp.MustCompile(`\n\t+Map:\s*(true|false)`)
	containerRE = regexp.MustCompile(`UnboundedContainer:\s*(true|false)`)
	unboundedRE = regexp.MustCompile(`\n\t+Unbounded:\s*(true|false)`)
	boundRE     = regexp.MustCompile(`\bArrayBound: (-?\d+)`)
	tableRE     = regexp.MustCompile(`\bTable: ([A-Za-z0-9_]+)`)
)

func regexpBool(re *regexp.Regexp, s string) bool {
	m := re.FindStringSubmatch(s)
	return len(m) == 2 && m[1] == "true"
}

func regexpBound(t *testing.T, s string) int32 {
	t.Helper()
	m := boundRE.FindStringSubmatch(s)
	if len(m) != 2 {
		t.Fatal("descriptor row without ArrayBound")
	}
	var n int32
	if _, err := fmt.Sscan(m[1], &n); err != nil {
		t.Fatalf("descriptor ArrayBound %q: %v", m[1], err)
	}
	return n
}
