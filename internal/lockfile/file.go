// The FILE side: where the lock lives, when the check runs, and how `schema
// lock` moves it (docs/SPEC-TABLES.md §2.10).
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Locate finds the unit's lock file from the unit's source paths. A unit is
// one directory (SPEC §3.2), so the answer is normally that directory's
// schema.lock.
//
// Two honest answers besides a path, on the tables baseline's terms (§18.1):
// when the paths span several directories and NONE of them holds a lock there
// is nothing to check and ok is false, and when several of them do the unit
// has no single lock and that is an error rather than a silent pick.
func Locate(paths []string) (path string, ok bool, err error) {
	seen := map[string]bool{}
	var dirs []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 1 {
		return filepath.Join(dirs[0], FileName), true, nil
	}
	var found []string
	for _, d := range dirs {
		p := filepath.Join(d, FileName)
		if _, statErr := os.Stat(p); statErr == nil {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return "", false, nil
	case 1:
		return found[0], true, nil
	default:
		return "", false, fmt.Errorf("this unit's files span %d directories and %d of them hold a %s (%s) — a unit has one lock; compile the unit as one directory (SPEC §3.2)",
			len(dirs), len(found), FileName, strings.Join(found, ", "))
	}
}

// Check compares a checked unit's fixed tables — and every type they reach —
// against its committed lock, when it has one. No file means no check: a unit that has never been locked
// promises nothing, and `schema lock` is what makes the promise.
//
// A unit that HAS one is held to it exactly ([Current]): a lock the
// declaration has moved past — an append, a new fixed table, a deprecation
// not written down — is refused like any other drift, because the file in the
// tree is the record and a record is current or it is nothing.
//
// Every finding is a refusal — a fixed record has no tolerant class, so there
// is nothing here for a warning to be.
func Check(u *ir.Unit, paths []string) []error {
	path, ok, err := Locate(paths)
	if err != nil {
		return []error{err}
	}
	if !ok {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []error{err}
	}
	locked, err := Parse(path, data)
	if err != nil {
		// EVERY parse refusal names the remedy, in one place, and the remedy
		// works: `schema lock` writes the file from the unit and needs
		// nothing out of the old one.
		return []error{fmt.Errorf("%w — the compiler owns this file; write it with: schema lock (docs/SPEC-TABLES.md §2.10)", err)}
	}
	if locked.Package != u.Package {
		return []error{fmt.Errorf("%s: lock is for package %s, this unit is package %s — the lock belongs to the unit it sits beside", path, locked.Package, u.Package)}
	}
	live := Render(u)
	// A LOCK FROM AN OLDER RENDERING IS SALVAGE, NOT A DELETE (§11.8): it holds
	// one layout and no lineage, and that layout is this declaration's.
	salvageLineage(locked, live)
	var errs []error
	for _, e := range Diff(locked, live, Current) {
		errs = append(errs, fmt.Errorf("%s: %w", path, e))
	}
	return errs
}

// Update writes the unit's lock. It is the ONLY writer of this file, and it
// only ever APPENDS entries and values, adds blocks and flips `deprecated` on:
// it runs the same comparison [Check] runs, under [Appendable] rather than [Current],
// and refuses everything the check refuses, so the one command that moves the
// file cannot be the one that breaks the rule. The one thing it accepts and
// the check does not is a declaration that has moved past the lock — which is
// the whole reason this command exists.
//
// It is idempotent: when the lock is already current, the file is left
// exactly as it sits and rewrote is false.
//
// There is no `--reason` here and no history section, and the difference from
// the tables baseline (§18.4) is the point: moving a baseline DECLARES a break
// with data already written, so it wants a sentence a person reads years
// later. Writing a lock declares nothing — every write it accepts is an
// append, and an append breaks nothing.
func Update(u *ir.Unit, paths []string) (path string, rewrote bool, err error) {
	dirs := map[string]bool{}
	var dir string
	for _, p := range paths {
		dir = filepath.Dir(p)
		dirs[dir] = true
	}
	if len(dirs) != 1 {
		return "", false, fmt.Errorf("this unit's files span %d directories — a lock belongs to one unit directory (SPEC §3.2); compile the unit as one directory", len(dirs))
	}
	path = filepath.Join(dir, FileName)

	live := Render(u)
	// Validate newly rendered fixed roots before either a first write or an
	// append. Comparing only the old lock cannot validate a newly added table.
	for i := range live.Tables {
		table := &live.Tables[i]
		if table.Decl == DeclFixedTable && table.FixedEmitted {
			if err := diffLineage(table, table, Current); err != nil {
				return "", false, fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		locked, perr := Parse(path, data)
		if perr != nil {
			return "", false, fmt.Errorf("%w — repair it by hand or delete it; this command will not write over a lock it cannot read, because it cannot tell an append from a break without one (docs/SPEC-TABLES.md §2.10)", perr)
		}
		if locked.Package != u.Package {
			return "", false, fmt.Errorf("%s: lock is for package %s, this unit is package %s — the lock belongs to the unit it sits beside", path, locked.Package, u.Package)
		}
		// THE SALVAGE FIRST (§11.8): a lock written before the lineage existed
		// holds one layout and no history, and the single layout it holds is
		// this declaration's — so it becomes the first lineage entry and
		// nothing in the file is deleted.
		salvageLineage(locked, live)
		if errs := Diff(locked, live, Appendable); len(errs) > 0 {
			return "", false, fmt.Errorf("%s: %w — `schema lock` appends, and this is not an append; the lock is not a way to make the refusal go away (docs/SPEC-TABLES.md §2.10)", path, errs[0])
		}
		// THE HISTORY CARRIED FORWARD onto what is about to be written: every
		// locked layout stays, with its retired mark and its reason, and the
		// declaration's own layout is appended when the hash has MOVED (§11.6).
		mergeLineage(locked, live)
		// AND THE RETIRED BLOCKS (§11.5): a retired table's record stays in this
		// file after its declaration goes, with its lineage and its reason.
		carryRetired(locked, live)
		if live.Text() == string(data) {
			return path, false, nil
		}
	case os.IsNotExist(readErr):
		// the first lock over this unit: everything already written under
		// these tables predates the promise, and nothing here can say
		// otherwise
	default:
		return "", false, readErr
	}

	if err := os.WriteFile(path, []byte(live.Text()), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}
