package main

import (
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// applyNeeds gives every unit that runs Go tests by go test itself (a gotest
// or gobench unit, a test the registry does not hold yet) the builds its
// tests read (testreg.GoNeedsPath): the union over its tests, run in the
// clone before the unit's command. A registry unit needs none here: schema-
// test builds them itself (the gotest adapter's Prepare).
func applyNeeds(units []Unit, needs []testreg.GoNeed) {
	for i := range units {
		u := &units[i]
		if u.Kind != KindGoTest && u.Kind != KindGoBench {
			continue
		}
		set := map[string]bool{}
		for _, n := range u.Names {
			for _, t := range testreg.GoNeedsOf(needs, n.Pkg, n.Test) {
				set[t] = true
			}
		}
		u.Needs = nil
		for t := range set {
			u.Needs = append(u.Needs, t)
		}
		sort.Strings(u.Needs)
	}
}
