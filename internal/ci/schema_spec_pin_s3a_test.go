package ci

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Section 3 through 3.3's row IDs remain stable while the behavioral pins
// are verified port by port. An owed entry is deliberately not a test pin:
// the drift-guard plant below only checks row syntax and does not decode wire.
// Owed rows explicitly expose the unproved legs. The port cells come from the
// roadmap's file-envelope, id-table-wire, variable-class, blobs,
// enum-keyed-arrays, message-form and packet string-refusal rows.
type s3aRowDigest struct{ Row, Digest string }

var _ = registerSpecPins("internal/ci/schema_spec_pin_s3a_test.go", s3aPinEntries()...)

func s3aPinEntries() []specPin {
	rows := []s3aRowDigest{
		{Row: "T3-1", Digest: "57c8423285bc700d"},
		{Row: "T3.1-1", Digest: "123acda00b619706"},
		{Row: "T3.1-2", Digest: "0a815e1973fdb33c"},
		{Row: "T3.1-3", Digest: "44fcba3aed7b513d"},
		{Row: "T3.1-4", Digest: "49737edc5d7c7a7c"},
		{Row: "T3.1-5", Digest: "cfc344d45b8e6f7b"},
		{Row: "T3.1-6", Digest: "b6ff7ae6d7ffe932"},
		{Row: "T3.1-7", Digest: "7cee67a516e64ba3"},
		{Row: "T3.1-8", Digest: "c018b5f6abc3c5af"},
		{Row: "T3.1-9", Digest: "25809748efe3784e"},
		{Row: "T3.1-10", Digest: "735ab24ab16a1689"},
		{Row: "T3.1-11", Digest: "e7c483c2023907aa"},
		{Row: "T3.1-12", Digest: "5a6a49bc915def6c"},
		{Row: "T3.1-13", Digest: "e8ceeaf3d8632d3f"},
		{Row: "T3.1-14", Digest: "2cbb57fedaed2ef7"},
		{Row: "T3-2", Digest: "2feb245cb267f3fb"},
		{Row: "T3.2-1", Digest: "232497d897af21c9"},
		{Row: "T3.2-2", Digest: "2288f2bed198553c"},
		{Row: "T3.2-3", Digest: "3c4cc318401e08cb"},
		{Row: "T3.2-4", Digest: "c952fd3ccb22d164"},
		{Row: "T3-3", Digest: "8c79a9339e399260"},
		{Row: "T3.3-1", Digest: "6de509b24bc49c8f"},
		{Row: "T3.3-2", Digest: "3228499f5a30f7d0"},
		{Row: "T3.3-3", Digest: "fe1e817b51dbe69e"},
		{Row: "T3.3-4", Digest: "5bfb960bd97294a7"},
		{Row: "T3.3-5", Digest: "5f393c42fd283fba"},
		{Row: "T3.3-6", Digest: "7e814daa4c2ae2c5"},
		{Row: "T3.3-7", Digest: "7dc047accc29bb41"},
		{Row: "T3.3-8", Digest: "5735bbbe312beb70"},
		{Row: "T3.3-9", Digest: "335b3f4cd2b98094"},
		{Row: "T3.3-10", Digest: "bb52720a95d4e5a0"},
		{Row: "T3.3-11", Digest: "dd2a89e0e39bc39f"},
		{Row: "T3.3-12", Digest: "7b533315ec3161d4"},
		{Row: "T3.3-13", Digest: "1b152f72ecfcd3b4"},
		{Row: "T3.3-14", Digest: "1e63405afee0267f"},
		{Row: "T3.3-15", Digest: "5c9734ba9a89ae68"},
		{Row: "T3.3-16", Digest: "489a89c2e4493b88"},
		{Row: "T3.3-17", Digest: "f8eebe754a0a7bf0"},
		{Row: "T3.3-18", Digest: "6ddbd15dce3e905c"},
		{Row: "T3.3-19", Digest: "108068f128e98080"},
		{Row: "T3.3-20", Digest: "0317bab8bbb4ed2e"},
		{Row: "T3.3-21", Digest: "22a9909dcc490b66"},
		{Row: "T3.3-22", Digest: "19f952bcc8224ec9"},
		{Row: "T3.3-23", Digest: "5392e0da845977a1"},
		{Row: "T3.3-24", Digest: "e2b53eaaa11e6b9e"},
		{Row: "T3.3-25", Digest: "1860fee03d60e49e"},
		{Row: "T3.3-26", Digest: "de36b7bb84c6853f"},
		{Row: "T3.3-27", Digest: "23055e06f6e96ba2"},
		{Row: "T3.3-28", Digest: "8ded1992cac26d62"},
		{Row: "T3.3-29", Digest: "a36a7f51d2dd942d"},
		{Row: "T3.3-30", Digest: "e23812b5c795ccc5"},
		{Row: "T3.3-31", Digest: "3571f5ff9c1b768a"},
		{Row: "T3.3-32", Digest: "48cf61ef8f82da72"},
		{Row: "T3.3-33", Digest: "b8451ff743e59f36"},
		{Row: "T3.3-34", Digest: "2a1f62053c186276"},
		{Row: "T3.3-35", Digest: "e148b9a459b25ce9"},
		{Row: "T3.3-36", Digest: "f39c50ea2add8fd3"},
		{Row: "T3.3-37", Digest: "92d45bf7625a6e36"},
		{Row: "T3.3-38", Digest: "13c4e025fa415448"},
		{Row: "T3.3-39", Digest: "50bdb0eb862e8109"},
		{Row: "T3.3-40", Digest: "7d9911dbfc74e91f"},
		{Row: "T3.3-41", Digest: "f981d186ea53f135"},
		{Row: "T3.3-42", Digest: "e433cd89eafa8625"},
		{Row: "T3.3-43", Digest: "9fe629aabcaf0f73"},
		{Row: "T3.3-44", Digest: "44ad65ddab0d8ce3"},
		{Row: "T3.3-45", Digest: "16b0c97afd02b07f"},
		{Row: "T3.3-46", Digest: "cd03f09572c4c4b8"},
		{Row: "T3.3-47", Digest: "57e4fcf635cd41ef"},
		{Row: "T3.3-48", Digest: "c7d3233610d6261f"},
		{Row: "T3.3-49", Digest: "0a05855b8f874bbc"},
		{Row: "T3.3-50", Digest: "07922b8359ae86dd"},
		{Row: "T3.3-51", Digest: "951c251f563281a3"},
		{Row: "T3.3-52", Digest: "f38573b1fda96001"},
		{Row: "T3.3-53", Digest: "e3f7002ae561a03e"},
		{Row: "T3.3-54", Digest: "ba03b8ef925597f5"},
		{Row: "T3.3-55", Digest: "8f0deab99e825c71"},
		{Row: "T3.3-56", Digest: "aa22a283ce08aabb"},
		{Row: "T3.3-57", Digest: "a6d5354d3dfb6df1"},
		{Row: "T3.3-58", Digest: "06e530710c536810"},
		{Row: "T3.3-59", Digest: "1568feef327d1112"},
		{Row: "T3.3-60", Digest: "91342d8a9b124a54"},
		{Row: "T3.3-61", Digest: "ad2ffe02b1702bda"},
		{Row: "T3.3-62", Digest: "9d82f4b2a7402748"},
		{Row: "T3.3-63", Digest: "36cc4a60511a7e3f"},
		{Row: "T3.3-64", Digest: "332a48605853b097"},
		{Row: "T3.3-65", Digest: "8f63bb623496ab0c"},
		{Row: "T3.3-66", Digest: "63f3d7c96f8a9bd8"},
		{Row: "T3.3-67", Digest: "84fdfbd84402f5fb"},
		{Row: "T3.3-68", Digest: "7e93e77d5969ea87"},
		{Row: "T3.3-69", Digest: "7e316ecc83b393ad"},
		{Row: "T3.3-70", Digest: "2c0e42a6eadfb96d"},
		{Row: "T3.3-71", Digest: "5057252023bef4f0"},
		{Row: "T3.3-72", Digest: "0ac975d41274aea9"},
		{Row: "T3.3-73", Digest: "c47723873d53e7c7"},
		{Row: "T3.3-74", Digest: "497525be8d15c0e7"},
		{Row: "T3.3-75", Digest: "a51a0f9f2337fab8"},
		{Row: "T3.3-76", Digest: "12d1e93b37f96f4d"},
		{Row: "T3.3-77", Digest: "c917bb5f918766cf"},
		{Row: "T3.3-78", Digest: "ad110aae9d702f7c"},
		{Row: "T3.3-79", Digest: "b713ed2fed5500f7"},
		{Row: "T3.3-80", Digest: "d84aefeb9f3d4361"},
		{Row: "T3.3-81", Digest: "6e3c7d28f48555a3"},
		{Row: "T3.3-82", Digest: "82ebaf19940de031"},
		{Row: "T3.3-83", Digest: "eb2bc9c8c575ba7e"},
		{Row: "T3.3-84", Digest: "93fe8a01ecfc147a"},
		{Row: "T3.3-85", Digest: "fcffd61182b9f7f5"},
		{Row: "T3.3-86", Digest: "837867313504490f"},
		{Row: "T3.3-87", Digest: "8728b84185d2f524"},
		{Row: "T3.3-88", Digest: "dbc94f7976ceed69"},
		{Row: "T3.3-89", Digest: "8c4dfcd44af6b64b"},
		{Row: "T3.3-90", Digest: "4c9ab4c51134774f"},
		{Row: "T3.3-91", Digest: "57d972c974259ca3"},
		{Row: "T3.3-92", Digest: "51621cf15ae9e559"},
		{Row: "T3.3-93", Digest: "841e71f7d6de582a"},
		{Row: "T3.3-94", Digest: "a7161df48c0a90aa"},
		{Row: "T3.3-95", Digest: "914eda1f7039c22e"},
		{Row: "T3.3-96", Digest: "b975ccd12b3e4743"},
		{Row: "T3-4", Digest: "589371b19f421dd9"},
		{Row: "T3-5", Digest: "fdff20a3cdf2755e"},
		{Row: "T3-6", Digest: "c611f3c644994873"},
		{Row: "T3-7", Digest: "68a1dca53e7b89a0"},
		{Row: "T3-8", Digest: "96e08e38ebe830a0"},
		{Row: "T3-9", Digest: "e74d184b1a327abd"},
		{Row: "T3-10", Digest: "3abc4c5398450f9e"},
		{Row: "T3-11", Digest: "54e8cff8312c41e5"},
		{Row: "T3-12", Digest: "d5da05c0651b3653"},
		{Row: "T3-13", Digest: "063462f57a32340a"},
		{Row: "T3-14", Digest: "5052f7db136cfb17"},
		{Row: "T3-15", Digest: "2ae143dff021c8c6"},
		{Row: "T3-16", Digest: "3fedb24ade097d1d"},
		{Row: "T3-17", Digest: "17f86385574623b8"},
		{Row: "T3-18", Digest: "7b06ae6b23f9bb2c"},
		{Row: "T3-19", Digest: "2d6cd82db3d7e8d6"},
		{Row: "T3-20", Digest: "af20b9fbb6033d05"},
		{Row: "T3-21", Digest: "51ab386eae6e7041"},
	}
	ports := []string{"c", "cpp", "cs", "dart", "elixir", "go", "java", "js", "rust"}
	pins := make([]specPin, 0, len(rows)*len(ports))
	for _, row := range rows {
		if row.Row == "T3-19" {
			// Reserved node, build-version and vocabulary IDs are a compiler
			// declaration refusal, exercised with the hash-collision hook.
			pins = append(pins, specPin{Row: row.Row, Port: "compiler", Test: "internal/check/tables_test.go:TestTheIdRefusalsUnderAPlantedCollision", Digest: row.Digest})
			continue
		}
		for _, port := range ports {
			pin := specPin{Row: row.Row, Port: port, Digest: row.Digest,
				Owed: "no row-specific behavioral witness confirmed in this port",
				Want: "TestSpecPin" + strings.NewReplacer(".", "_", "-", "_").Replace(row.Row) + strings.ToUpper(port),
			}
			// Generated-Go TestGoMessageFormRefusals is a candidate witness
			// for several message rows, but it uses slowtest.Gate and is not
			// a valid pin until the tier-aware checker and nightly lane land.
			pins = append(pins, pin)
		}
	}
	return pins
}

// TestSpecPinsS3ARealSpecPlant is the planted-pattern witness for this
// section: an unpinned normative sentence in §3.1 must fail the guard on the
// real spec, and the unplanted spec must pass it. The plant is in memory, so
// the tree is never edited.
func TestSpecPinsS3ARealSpecPlant(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := os.DirFS(root)
	spec, err := fs.ReadFile(base, "docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	head := "### 3.1 Pointers on the wire: a flat node table"
	if !strings.Contains(string(spec), head) {
		t.Fatalf("the spec no longer carries %q", head)
	}
	planted := strings.Replace(string(spec), head, head+"\n\nA pointered table must name its node table in the header.", 1)
	over := overlayFS{FS: base, over: map[string][]byte{"docs/SPEC-TABLES.md": []byte(planted)}}
	_, findings := checkSpecPins(specPinsInput{fsys: over, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry})
	found := false
	for _, f := range findings {
		if strings.Contains(f, "normative sentence without a row id") && strings.Contains(f, "3.1 Pointers on the wire") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a planted unmarked §3.1 sentence did not fail the guard: %v", findings)
	}
	if _, clean := checkSpecPins(specPinsInput{fsys: base, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry}); len(clean) != 0 {
		t.Fatalf("the unplanted spec does not pass the guard: %v", clean)
	}
}
