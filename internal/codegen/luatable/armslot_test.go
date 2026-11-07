package luatable

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaArmSlotKeepsTheDiscriminator holds the value representation of a table
// union (docs/SPEC-TABLES.md §2.6): { tag = <arm index>, <slot> = <payload> }.
// An arm named `tag` must not store its payload over the discriminator, so its
// slot is the name with underscores appended until it names no arm; every other
// arm's slot is its name. The shared wire pin, the conformance instance
// stream_arm_tag (tables/stream Frame.tag), is proposed in #2358.
func TestLuaArmSlotKeepsTheDiscriminator(t *testing.T) {
	un := func(names ...string) *ir.Union {
		u := &ir.Union{Name: "U"}
		for _, n := range names {
			u.Variants = append(u.Variants, ir.UnionVariant{Name: n})
		}
		return u
	}
	for _, c := range []struct {
		arms []string
		want []string
	}{
		{[]string{"header", "chunk"}, []string{"header", "chunk"}},
		{[]string{"header", "tag"}, []string{"header", "tag_"}},
		{[]string{"tag", "tag_"}, []string{"tag__", "tag_"}},
		{[]string{"tag_", "tag", "tag__"}, []string{"tag_", "tag___", "tag__"}},
	} {
		got := armSlots(un(c.arms...))
		if !slices.Equal(got, c.want) {
			t.Errorf("arms %v: slots %v, want %v", c.arms, got, c.want)
		}
		seen := map[string]bool{"tag": true}
		for _, s := range got {
			if seen[s] && s != "tag" || s == "tag" {
				t.Errorf("arms %v: slot %q collides with the discriminator or another slot", c.arms, s)
			}
			seen[s] = true
		}
	}
}
