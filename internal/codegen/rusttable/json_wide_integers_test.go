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
// integer is written as a STRING of decimal digits and reads as a string or a
// number; every integer field takes an integer token only — no fraction, no
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
