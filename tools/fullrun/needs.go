package main

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testvolume"
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

// benchProperties are the machine properties a bench may declare
// (--bench HOST=SHARDS+casefold) and a registry row may need (testreg.Test's
// Needs), each with what it is. The fleet's benches declare what they
// provide where the plan reads every other machine fact, the --bench value
// (its GOOS too); a declaration is held to the machine by probeProperty on
// the bench before any unit that needs it is sent there, so a bench that
// says it has a property it lacks takes no such unit.
var benchProperties = map[string]string{
	"casefold": "a volume that takes two spellings of one name for one entry: " + testvolume.FleetScratch + " (the fleet's ext4 casefold image) or $" + testvolume.EnvVar + "; captain, WSL2 with no ext4 casefold, has none",
}

// probeProperty reports why this machine does not provide the property, or
// nil when it does. Run on the bench (fullrun probe PROPERTY).
func probeProperty(name string) error {
	if name == "casefold" {
		return testvolume.Probe()
	}
	return fmt.Errorf("fullrun has no probe for %q", name)
}

// partProvides are the properties every numbered part's runner provides: a
// part runs on a Linux schema runner of the fleet, each of which mounts the
// case-folding scratch (nova-tools github-runners, casefold.yml). A part
// launched on a runner whose probe refuses one runs the units that need it
// nowhere: each is NOT RUN, naming the need and the refusal.
var partProvides = []string{"casefold"}
