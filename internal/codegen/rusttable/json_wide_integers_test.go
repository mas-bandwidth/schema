package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// rustJSONIntegersSchema is the unit the json-integers vectors read: an int32,
// an int64, a uint64, a ranged int128, a ranged int64, a wide fixed-point field
// and a map keyed by uint64 (docs/SPEC-TABLES.md §16.2).
const rustJSONIntegersSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

// TestRustJsonWideIntegersAreStrings holds every manifest row of
// testdata/wire/json-integers/ through the Rust writer and reader: a wide
// integer is written as a STRING of decimal digits and reads only as a string; every integer field takes an integer token only — no fraction, no
// exponent, no negative zero — and its range is tested on the exact value
// (docs/SPEC-TABLES.md §16.2, §16.14 W10 and W11).
func TestRustJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (the JSON wide-integer writer and strict integer reader)")
	requireRustcForCards(t)

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		name, json, verdict string
	}
	var vectors []vector
	accepts, refusals := 0, 0
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		switch fields[3] {
		case "read":
			accepts++
		case "refused":
			refusals++
		default:
			t.Fatalf("%s: verdict %q, want read or refused", fields[0], fields[3])
		}
		vectors = append(vectors, vector{name: fields[0], json: fields[2], verdict: fields[3]})
	}
	if accepts == 0 || refusals == 0 {
		t.Fatalf("the manifest names %d accept and %d refuse rows; both are needed", accepts, refusals)
	}

	numberManifest, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	numberRows := 0
	for i, line := range strings.Split(string(numberManifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[1] != "Integers" || fields[3] != "refused" {
			t.Fatalf("NUMBER-TOKEN.tsv:%d: invalid row %q", i+1, line)
		}
		vectors = append(vectors, vector{name: fields[0], json: fields[2], verdict: fields[3]})
		numberRows++
	}
	if numberRows != 13 {
		t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", numberRows)
	}

	var body strings.Builder
	fmt.Fprintf(&body, "let canonical = %q;\n", string(canonical))
	for _, v := range vectors {
		text, err := os.ReadFile(filepath.Join(dir, v.json))
		if err != nil {
			t.Fatal(err)
		}
		if v.verdict == "refused" {
			fmt.Fprintf(&body, `{
    let mut report = TableReport::default();
    assert!(integers_from_json(%q, &mut report).is_none(), "refuse row %s: the read accepted it");
}
`, string(text), v.name)
			continue
		}
		// Every accept row lands the same values, and the writer spells the
		// wide fields as strings of digits: the canonical text byte for byte.
		fmt.Fprintf(&body, `{
    let mut report = TableReport::default();
    let value = integers_from_json(%q, &mut report).unwrap_or_else(|| panic!("accept row %s: the read refused: {:?}", report));
    assert!(!report.damaged, "accept row %s: the read reports damage");
    assert_eq!(value.small, 7i32, "accept row %s: small");
    assert_eq!(value.wide, -42i64, "accept row %s: wide");
    assert_eq!(value.uwide, 18446744073709551615u64, "accept row %s: uwide");
    assert_eq!(value.wide128, 170141183460469231731687303715884105727i128, "accept row %s: wide128");
    assert_eq!(value.ranged, 9007199254740993i64, "accept row %s: ranged");
    assert_eq!(value.frac, 32768i64, "accept row %s: frac");
    assert_eq!(value.counts.len(), 2, "accept row %s: map size");
    let mut zero = false;
    let mut big = false;
    for entry in &value.counts {
        if entry.key == 0 { assert_eq!(entry.value, 1i32, "accept row %s: key 0"); zero = true; }
        if entry.key == 9007199254740993u64 { assert_eq!(entry.value, 2i32, "accept row %s: wide key"); big = true; }
    }
    assert!(zero && big, "accept row %s: the map keys did not land");
    assert_eq!(integers_to_json(&value).trim_end_matches('\n'), canonical, "accept row %s: the written text diverged from the canonical text");
    println!("accept %s ok");
}
`, string(text), v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name, v.name)
	}
	body.WriteString("println!(\"wide integers ok\");\n")

	u := unitFrom(t, rustJSONIntegersSchema)
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("JSON wide-integer probe: %v\n%s", err, out)
	}
	if !strings.Contains(out, "wide integers ok\n") {
		t.Fatalf("the probe did not reach its end: %s", out)
	}
}

// TestRustJsonWideIntegerPositions checks every value-bearing container.
func TestRustJsonWideIntegerPositions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (wide integer JSON positions)")
	requireRustcForCards(t)
	kinds := []string{"int64", "uint64", "uint128", "fixed(48, 16)", "fixed(80, 48)", "int32"}
	var schema, body strings.Builder
	schema.WriteString("package positions\nenum Key { A, B }\n")
	for i, kind := range kinds {
		bounds := ""
		if strings.HasPrefix(kind, "fixed(") {
			bounds = " | min = -10, max = 10"
		}
		fmt.Fprintf(&schema, "union Pick%d { value %s%s }\ntable Probe%d { plain %s%s\n array [..2]%s%s\n optional ?%s%s\n choice Pick%d\n keyed [Key]%s%s\n", i, kind, bounds, i, kind, bounds, kind, bounds, kind, bounds, i, kind, bounds)
		// Fixed-point fields require bounds, which the schema disallows on maps.
		if bounds == "" {
			fmt.Fprintf(&schema, " entries map[uint32]%s\n", kind)
		}
		schema.WriteString("}\n")
		for _, shape := range []string{`{"plain":%s}`, `{"array":[%s]}`, `{"optional":%s}`, `{"choice":{"value":%s}}`, `{"entries":{"1":%s}}`, `{"keyed":{"A":%s}}`} {
			if bounds != "" && strings.Contains(shape, "entries") {
				continue
			}
			quoted := fmt.Sprintf(shape, `"1"`)
			bare := fmt.Sprintf(shape, `1`)
			if i < len(kinds)-1 {
				fmt.Fprintf(&body, "{ let mut r = TableReport::default(); assert!(probe%d_from_json(%q, &mut r).is_some(), \"{}\", %q); }\n", i, quoted, kind+" quoted "+shape)
				fmt.Fprintf(&body, "{ let mut r = TableReport::default(); assert!(probe%d_from_json(%q, &mut r).is_none(), \"{}\", %q); }\n", i, bare, kind+" bare "+shape)
			} else {
				fmt.Fprintf(&body, "{ let mut r = TableReport::default(); assert!(probe%d_from_json(%q, &mut r).is_some(), \"{}\", %q); }\n", i, bare, kind+" bare "+shape)
			}
		}
	}
	schema.WriteString("table Boundary { narrow bits(32)\n wide bits(33)\n}\n")
	body.WriteString(`{ let mut r = TableReport::default(); assert!(boundary_from_json(r#"{"narrow":1,"wide":"1"}"#, &mut r).is_some()); }
 { let mut r = TableReport::default(); assert!(boundary_from_json(r#"{"wide":1}"#, &mut r).is_none()); }
`)
	out, err := rustConstructProbe(t, unitFrom(t, schema.String()), body.String())
	if err != nil {
		t.Fatalf("position probe: %v\n%s", err, out)
	}
}
