package main

func init() {
	// Removes the packet writer's bits(N) width assert (docs/SPEC.md §5.1
	// row 2) from the per-field path; the flat run's guard is removed by the
	// sibling edit. The read path is untouched, so only the wide-bits case
	// goes red.
	sabotages["packet-write-refusal-cs"] = []edit{{
		old: `					" // a bits(N) value wider than N cannot ride (SPEC §5.1 row 2)")
			}
		}
		return p, true`,
		new: `					" // SABOTAGED")
			}
		}
		p.guard = nil // SABOTAGED: no bits(N) width check on the packet write
		return p, true`,
	}}
}
