// THE TWO-PARENT LOCK RUN, at a merge commit (docs/FIXED-FORM-BILL-READS-BACKWARD.md
// §8a.1 and §11.3, docs/FIXED-FORM-ALGORITHM.md §5.1).
//
// WHY THIS FILE EXISTS AND WHAT IT IS NOT. §8a.1's first break is two branches
// that both append: a hotfix appends A while main appends B, each commit passes
// its own lock, and the merge takes both. Neither parent's field order is a
// PREFIX of the merged one — a merge that took both branches' appends cannot be
// a prefix of either side — so diff.go's strict prefix rule refuses a merge that
// is correct, and a law that refuses the correct answer is a law nobody keeps.
//
// Rowan's ruling (final): the name-subset clause of §8a.1/§11.3 is IMPLEMENTED
// AS THIS RUN AND NEVER AS A RUN-TIME RULE. Reads already pair fields BY WIRE ID
// (algorithm §5.9 #41), so a merged layout reads both parents' files through the
// plans COMPILE lays down from the lineage; nothing on any read path needs to
// learn about merges. What a merge needs is a LOCK RUN that knows it has two
// `old`s:
//
//	schema lock --parent a/schema.lock --parent b/schema.lock
//
// and the law it runs is §5.1's WIDENS with ONE clause changed: a parent's
// fields must all be PRESENT in the merged record and each must widen, and the
// merged record's NEW fields may sit ANYWHERE in the order. Position is
// POSITION-FREE AT A MERGE and nowhere else — WITHIN ONE BRANCH the strict
// prefix rule of diff.go stays exactly as it is, because within one branch a
// layout that is not a prefix is a reorder, which is the thing the prefix rule
// is for.
//
// The record the run writes: the merged lock's lineage is BOTH parents' entries
// concatenated in commit order, deduped by wire hash, followed by the merged
// layout's own entry. Retirements from both parents stand — a layout either
// branch stopped serving is not served by the merge — which makes the merged
// floor the MAX of the two parents' floors, because the floor is one past the
// highest retired entry and a mark that survives the dedup survives at its
// merged index.
package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A Parent is one side of a merge: the NAME every refusal against it calls it by
// — the `--parent` path, which is what the person typed and what they can open —
// and the lock that path holds.
type Parent struct {
	Name string
	Lock *Unit
}

// Merge is `schema lock --parent A --parent B`: the lock run AT A MERGE COMMIT.
//
// It takes the place of [Update] there and runs the same shape of comparison
// against EACH parent — so a break against either side is a refusal, and the
// command that writes the merged file cannot be the one that breaks the rule —
// then writes the unit's lock with the composed lineage. It is idempotent for
// the same reason [Update] is: the file it writes is a function of the parents
// and the declaration, so a second run over the same three finds the file
// already current and rewrote is false.
//
// Two parents are the case the bill names and two is the minimum; an octopus
// merge hands more, and every one of them is checked the same way.
func Merge(u *ir.Unit, paths []string, parentPaths []string) (path string, rewrote bool, err error) {
	if len(parentPaths) < 2 {
		return "", false, fmt.Errorf("the two-parent lock run wants a lock per parent of the merge and was given %d — pass `--parent <lock>` once per parent (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.3); within ONE branch the ordinary `schema lock` is the run, and its prefix rule is the law there", len(parentPaths))
	}
	dir, err := unitDir(paths)
	if err != nil {
		return "", false, err
	}
	path = filepath.Join(dir, FileName)

	live := Render(u)
	parents := make([]Parent, 0, len(parentPaths))
	for _, p := range parentPaths {
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return "", false, fmt.Errorf("--parent %s: %w — a merge is judged against the lock EACH PARENT COMMITTED, so the file has to be there; read it out of the parent commit (`git show <parent>:%s > <path>`)", p, readErr, FileName)
		}
		locked, perr := Parse(p, data)
		if perr != nil {
			return "", false, fmt.Errorf("--parent %s: %w — this command will not write a merged lock over a parent it cannot read, because it cannot tell an append from a break without one (docs/SPEC-TABLES.md §2.10)", p, perr)
		}
		if locked.Package != u.Package {
			return "", false, fmt.Errorf("--parent %s: lock is for package %s, this unit is package %s — a parent of this merge is a commit of THIS unit", p, locked.Package, u.Package)
		}
		// THE SALVAGE FIRST (§11.8), per parent: a parent from before the
		// lineage holds one layout and no history, and the one layout it holds
		// is the layout that side shipped.
		salvageLineage(locked, live)
		parents = append(parents, Parent{Name: p, Lock: locked})
	}

	if errs := diffAgainstParents(parents, live); len(errs) > 0 {
		return "", false, fmt.Errorf("%s: %w — a merge widens EACH parent, and this is not a widening of one of them; the lock is not a way to make the refusal go away (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.3, docs/SPEC-TABLES.md §2.10)", path, errs[0])
	}
	mergeLineageParents(parents, live)
	carryRetiredParents(parents, live)

	if old, readErr := os.ReadFile(path); readErr == nil && string(old) == live.Text() {
		return path, false, nil
	}
	if err := os.WriteFile(path, []byte(live.Text()), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// unitDir is the one directory a unit's lock belongs to (SPEC §3.2).
func unitDir(paths []string) (string, error) {
	dirs := map[string]bool{}
	var dir string
	for _, p := range paths {
		dir = filepath.Dir(p)
		dirs[dir] = true
	}
	if len(dirs) != 1 {
		return "", fmt.Errorf("this unit's files span %d directories — a lock belongs to one unit directory (SPEC §3.2); compile the unit as one directory", len(dirs))
	}
	return dir, nil
}

// diffAgainstParents is the law AT A MERGE: the merged rendering is held to
// EVERY parent, and every parent's findings are reported. One refusal per block
// per parent, the way [Diff] reports one per block.
func diffAgainstParents(parents []Parent, live *Unit) []error {
	var errs []error
	for _, p := range parents {
		errs = append(errs, diffParent(p, live)...)
	}
	return errs
}

// diffParent is [Diff] under [Appendable] with ONE clause replaced — the field
// order, which [diffTableAtMerge] reads by wire id rather than by position.
//
// Everything else is the same law read from the same file, and it is here rather
// than behind a policy flag in [Diff] because the two runs differ in what they
// ARE and not only in what they allow: [Diff] compares a declaration to THE
// record that precedes it, and a merge has two records before it and no
// single `old`. Two findings of [Diff] are therefore absent here by
// construction: the [Current] reading, which has no meaning when the thing being
// written is a merge, and diffLineage's "the declaration's layout is the
// lineage's last entry", which the composition below makes true.
func diffParent(p Parent, live *Unit) []error {
	var errs []error
	locked := p.Lock
	// THE RETIRED TABLE'S BLOCKS (bill §11.5), per parent: a side that retired a
	// table and dropped its declaration hands a lock whose block the merged
	// declaration does not answer, and that is the record of something that
	// shipped rather than a removal.
	orphans := retiredOrphans(locked, live)
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		// THE SELF-CONSISTENCY CHECK FIRST, as in [Diff]: a parent lock whose
		// halves disagree was edited by a hand, and nothing below it can be
		// trusted to mean what it says.
		if got := LayoutHash(lk.Entries); got != lk.Layout {
			errs = append(errs, parentErrf(p, "%s %s: the parent lock records layout=0x%016x over entries that hash to 0x%016x — the lock is written by the compiler and a hand-edit does not hold; restore the parent's file from version control (docs/SPEC-TABLES.md §2.10)",
				lk.Decl, lk.Name, lk.Layout, got))
			continue
		}
		if lk.Decl == DeclFixedTable && lk.rollupWritten && locked.Version == Version {
			if err := diffRollup(lk); err != nil {
				errs = append(errs, parentErr(p, err))
				continue
			}
		}
		lv := live.Table(lk.Name)
		if lv == nil || lv.Decl != lk.Decl {
			if !orphans[lk.Name] {
				errs = append(errs, parentErr(p, gone(lk, lv)))
			}
			continue
		}
		// THE FORM'S CEILING (lineage.go): a merge that grows a bound across it
		// changes the WIRE on both sides at once, and the refusal is the same
		// one a single branch takes.
		if lk.Decl == DeclFixedTable {
			if err := diffCeiling(lk, lv); err != nil {
				errs = append(errs, parentErr(p, err))
				continue
			}
		}
		if err := diffTableAtMerge(lk, lv); err != nil {
			errs = append(errs, parentErr(p, err))
			continue
		}
	}
	for i := range locked.Values {
		lk := &locked.Values[i]
		if got := ValuesHash(lk); got != lk.Hash {
			errs = append(errs, parentErrf(p, "%s %s: the parent lock records values=0x%016x over %ss that hash to 0x%016x — the lock is written by the compiler and a hand-edit does not hold; restore the parent's file from version control (docs/SPEC-TABLES.md §2.10)",
				lk.Decl, lk.Name, lk.Hash, lk.Child(), got))
			continue
		}
		lv := live.List(lk.Name)
		if lv == nil || lv.Decl != lk.Decl {
			if orphans[lk.Name] {
				continue
			}
			errs = append(errs, parentErrf(p, "%s %s is in the parent lock and this unit's fixed tables no longer reach it — the lock holds every type a fixed record is made of, so a type leaving the closure is a field that changed what it holds; restore it, or deprecate that field and append a new one (docs/SPEC-TABLES.md §2.10)",
				lk.Decl, lk.Name))
			continue
		}
		// A VALUE LIST IS STILL A NUMBERING, AT A MERGE TOO, and this run does
		// NOT loosen it. A field's identity is its wire id and the read pairs by
		// it; an enum's variant, a flags bit and a union's arm have no id on the
		// wire at all — the stored number IS the position — so two branches that
		// both append to one enum have written two different meanings for one
		// ordinal, and there is no order the merge can choose that reads both
		// sides' files. §5.1's prefix sentence stands for these, and the
		// remedy is the bill's: one branch's variants go at the end of the
		// other's, which is a rebase and not a merge of the lists.
		if err := diffValues(lk, lv, Appendable); err != nil {
			errs = append(errs, parentErr(p, err))
		}
	}
	return errs
}

// diffTableAtMerge is THE ONE CLAUSE THIS RUN CHANGES (bill §8a.1, §11.3;
// algorithm §5.1's "a SUBSET by NAME ... any interleaving after a merge").
//
// The parent's fields are paired with the merged record's BY WIRE ID — the
// field's identity (§5) — and not by position: a merge that took both branches'
// appends has the two sides' new fields interleaved in SOME order, and neither
// parent's order is a prefix of it. What the parent is owed is that every field
// it locked is STILL THERE and still widens, which is exactly what a reader of
// that parent's files needs: COMPILE lays a plan down per lineage entry and the
// plan pairs the parent's layout to the merged one by id, so a field that is
// present and widened is read, wherever it now sits.
//
// Position is free only for the merged record's NEW fields. Everything else is
// monotone.go's law, field for field, unchanged — including that deprecation
// stays one way: a slot the parent deprecated may not come back to life in a
// merge any more than in a commit.
func diffTableAtMerge(lk, lv *Table) error {
	at := make(map[uint64]int, len(lv.Entries))
	for i, e := range lv.Entries {
		at[e.Id] = i
	}
	for i, want := range lk.Entries {
		where := fmt.Sprintf("%s %s: entry %d, field %s (id=0x%016x)", lk.Decl, lk.Name, i+1, want.Name, want.Id)
		j, ok := at[want.Id]
		if !ok {
			return fmt.Errorf("%s, is in this parent's lock and not in the merged declaration: field removed — a merge WIDENS EACH PARENT: a field either side locked is still in the record, deprecated in place where it is done with, because every file that parent's build wrote holds it and the merged reader pairs the two layouts by id (docs/FIXED-FORM-BILL-READS-BACKWARD.md §8a.1, §11.3, docs/SPEC-TABLES.md §2.10); restore it — anywhere in the order — and mark it `| deprecated` if it is finished",
				where)
		}
		if _, _, err := monotone(where, want, lv.Entries[j]); err != nil {
			return err
		}
	}
	return nil
}

// parentErr names the side of the merge a refusal is against. A merge has two
// `old`s and a person fixing one has to know WHICH, so the parent leads: it is
// the path they typed, and the file they can open.
func parentErr(p Parent, err error) error {
	return fmt.Errorf("against the parent lock %s: %w", p.Name, err)
}

func parentErrf(p Parent, format string, args ...any) error {
	return parentErr(p, fmt.Errorf(format, args...))
}

// mergeLineageParents composes the merged lineage (bill §11.3): BOTH parents'
// entries concatenated IN COMMIT ORDER — the order `--parent` was given, which
// is the order the person's merge names them in — DEDUPED BY WIRE HASH, then the
// merged layout's own entry.
//
// Dedup keeps the FIRST occurrence's place, because the shared history before
// the branch point is the oldest part of both lists and its order is the same on
// both sides; what differs is only what each branch appended after it. A layout
// EITHER parent retired is retired in the merge — a side that stopped serving a
// layout did not start again by merging — and that is what makes the merged
// floor the max of the two: [Floor] is one past the highest retired entry, and a
// surviving mark survives at its merged index.
//
// Nothing is dropped and nothing already written is rewritten, exactly as
// [mergeLineage] promises within one branch.
func mergeLineageParents(parents []Parent, live *Unit) {
	for i := range live.Tables {
		lv := &live.Tables[i]
		if lv.Decl != DeclFixedTable || len(lv.Lineage) != 1 {
			continue
		}
		var history []LineageEntry
		seen := map[uint64]int{}
		for _, p := range parents {
			lk := p.Lock.Table(lv.Name)
			if lk == nil || lk.Decl != DeclFixedTable {
				continue
			}
			for _, e := range lk.Lineage {
				if at, ok := seen[e.Wire]; ok {
					// RETIREMENTS FROM BOTH PARENTS STAND, and a reason
					// already written is not overwritten by a side that wrote
					// none.
					if e.Retired && !history[at].Retired {
						history[at].Retired = true
						if history[at].Reason == "" {
							history[at].Reason = e.Reason
						}
					}
					continue
				}
				seen[e.Wire] = len(history)
				history = append(history, e)
			}
		}
		if len(history) == 0 {
			// a table NEITHER parent ever locked: its own layout is the first
			// entry of its lineage (§11.9), which the rendering already is
			continue
		}
		cur := lv.Lineage[0]
		if at, ok := seen[cur.Wire]; ok {
			// THE MERGE THAT IS ONE PARENT'S LAYOUT: a merge that took no new
			// field for this table has a layout one side already shipped, so the
			// record already ends where it should and nothing is appended. It
			// keeps its retired mark, because the entry is the one that shipped.
			_ = at
			lv.Lineage = history
			continue
		}
		// THE DIGEST LANDING, as [mergeLineage] has it: a parent written with an
		// empty digest recorded the layout-bytes-only hash, and filling the §13
		// digest moves the wire hash without moving a layout byte.
		if last := history[len(history)-1]; len(last.Digest) == 0 && bytes.Equal(last.Layout, cur.Layout) {
			history[len(history)-1] = cur
			lv.Lineage = history
			continue
		}
		lv.Lineage = append(history, cur)
	}
}

// carryRetiredParents is [carryRetired] over both sides: a table EITHER parent
// retired and dropped the declaration of keeps its block, its lineage and its
// reason in the merged file (bill §11.5).
func carryRetiredParents(parents []Parent, live *Unit) {
	for _, p := range parents {
		carryRetired(p.Lock, live)
	}
}
