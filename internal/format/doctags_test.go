// The formatter's two doc-and-tags consequences (SPEC §7.4 rules 2 and 5): a
// `///` line does not break an alignment group, and a variant list in which
// any variant is qualified or documented is one per line with no commas, its
// | column aligned. Every other multi-line list is the comma form, and a
// mixed list is normalized to it.
package format

import "testing"

func format(t *testing.T, src string) string {
	t.Helper()
	out, err := Format("Probe.schema", []byte(src))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	return string(out)
}

func TestDocCommentDoesNotBreakAnAlignmentGroup(t *testing.T) {
	src := "package t\n\ntable Ship | designer_facing\n{\n    /// the hull\n    health float32 = 100.0 | ui_slider, min = 0.0, max = 1000.0, resolution = 0.5\n    texture uint64 | asset_ref\n    // a working note breaks the group\n    nickname string(32) | localized\n}\n"
	want := "package t\n\ntable Ship | designer_facing\n{\n    /// the hull\n    health  float32 = 100.0 | ui_slider, min = 0.0, max = 1000.0, resolution = 0.5\n    texture uint64          | asset_ref\n    // a working note breaks the group\n    nickname string(32) | localized\n}\n"
	if got := format(t, src); got != want {
		t.Errorf("--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestQualifiedVariantListIsOnePerLineWithoutCommas(t *testing.T) {
	src := "package t\n\nenum Rarity | loot\n{\n    Common, Uncommon,\n    Rare | celebrate\n    /// the top\n    Legendary | celebrate, loud // and a note\n}\n"
	want := "package t\n\nenum Rarity | loot\n{\n    Common\n    Uncommon\n    Rare      | celebrate\n    /// the top\n    Legendary | celebrate, loud // and a note\n}\n"
	if got := format(t, src); got != want {
		t.Errorf("--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestDocumentedVariantListIsOnePerLine(t *testing.T) {
	src := "package t\n\nflags Perks\n{\n    /// bit zero\n    Shielded, Cloaked,\n    Turbo\n}\n"
	want := "package t\n\nflags Perks\n{\n    /// bit zero\n    Shielded\n    Cloaked\n    Turbo\n}\n"
	if got := format(t, src); got != want {
		t.Errorf("--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestMixedVariantListNormalizesToTheCommaForm(t *testing.T) {
	src := "package t\n\nenum Big\n{\n    A, B\n    C\n    D,\n}\n"
	want := "package t\n\nenum Big\n{\n    A, B,\n    C,\n    D,\n}\n"
	if got := format(t, src); got != want {
		t.Errorf("--- got ---\n%s--- want ---\n%s", got, want)
	}
	for _, canonical := range []string{
		"package t\n\nenum Big { A, B, C }\n",
		"package t\n\nenum Big\n{\n    A,\n    B,\n    C\n}\n",
		"package t\n\nenum Big\n{\n    A,\n    B,\n    C,\n}\n",
		"package t\n\nenum Big | max = 8\n{\n    A,\n    B\n}\n",
	} {
		if got := format(t, canonical); got != canonical {
			t.Errorf("not a fixed point:\n--- got ---\n%s--- want ---\n%s", got, canonical)
		}
	}
}

func TestQualificationsOnEveryLineKindAreCanonical(t *testing.T) {
	src := "package t\n\n/// gold\nconst StarterGold = 500 | tuning\nconst Other = 1 | a\n\nunion Effect | fx\n{\n    /// bare\n    none_at_all | bare\n    v Vec | min = 0, max = 1, tag\n}\n\ntype Vec | vec3\n{\n    x int32 | min = 0, max = 1, axis\n}\n"
	want := "package t\n\n/// gold\nconst StarterGold = 500 | tuning\nconst Other       = 1 | a\n\nunion Effect | fx\n{\n    /// bare\n    none_at_all     | bare\n    v           Vec | tag, min = 0, max = 1\n}\n\ntype Vec | vec3\n{\n    x int32 | axis, min = 0, max = 1\n}\n"
	if got := format(t, src); got != want {
		t.Errorf("--- got ---\n%s--- want ---\n%s", got, want)
	}
}
