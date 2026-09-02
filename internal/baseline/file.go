// The FILE side: where the baseline lives, when the check runs, and how
// `--update` moves it (SPEC-TABLES.md §16).
package baseline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Locate finds the unit's baseline file from the unit's source paths. A unit
// is one directory (SPEC §3.2), so the answer is normally that directory's
// tables.baseline.
//
// Two honest answers besides a path. When the paths span several directories
// and NONE of them holds a baseline, there is nothing to check and ok is
// false. When several of them do, the unit has no single baseline to diff
// against and that is an error rather than a silent skip — a check that
// quietly picks one would be a check that lies.
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
		return "", false, fmt.Errorf("this unit's files span %d directories and %d of them hold a %s (%s) — a unit has one baseline; compile the unit as one directory (SPEC §3.2)",
			len(dirs), len(found), FileName, strings.Join(found, ", "))
	}
}

// Check diffs a checked unit against its committed baseline, when it has one.
// No file means no check: the baseline is opt-in, and a unit that never
// committed one is unaffected by any of this.
//
// Refusals come back as errors — they fail the compile — and warnings as text
// for the caller's own reporting.
func Check(u *ir.Unit, paths []string) (warnings []string, errs []error) {
	path, ok, err := Locate(paths)
	if err != nil {
		return nil, []error{err}
	}
	if !ok {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	base, err := Parse(path, data)
	if err != nil {
		return nil, []error{err}
	}
	if base.Package != u.Package {
		return nil, []error{fmt.Errorf("%s: baseline is for package %s, this unit is package %s — the baseline belongs to the unit it sits beside", path, base.Package, u.Package)}
	}

	refusals, warns := Split(Diff(base, Render(u), DefaultTokenPolicy))
	for _, w := range warns {
		warnings = append(warnings, fmt.Sprintf("%s: %s", path, w))
	}
	for _, r := range refusals {
		errs = append(errs, fmt.Errorf("%s: %w — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason \"...\" (SPEC-TABLES.md §16)", path, r))
	}
	return warnings, errs
}

// Update rewrites the unit's baseline and appends a dated entry to its history
// naming every edit it recorded and the reason for them. The reason is
// MANDATORY — the owner's ruling: an intentional break is declared, never
// slipped in — and Update refuses without one.
//
// It is idempotent: when the projection has not moved, the file is left
// exactly as it sits and no history entry is written.
func Update(u *ir.Unit, paths []string, reason string) (path string, rewrote bool, err error) {
	return update(u, paths, reason, time.Now().UTC().Format("2006-01-02"))
}

// update is Update with the date supplied, so tests are not dated by the clock.
func update(u *ir.Unit, paths []string, reason, date string) (string, bool, error) {
	if strings.TrimSpace(reason) == "" {
		return "", false, fmt.Errorf("--update needs --reason: moving the baseline declares an intentional break with data already written, and the reason is what a person reads years later when an old file refuses (SPEC-TABLES.md §16)")
	}
	dirs := map[string]bool{}
	var dir string
	for _, p := range paths {
		dir = filepath.Dir(p)
		dirs[dir] = true
	}
	if len(dirs) != 1 {
		return "", false, fmt.Errorf("this unit's files span %d directories — a baseline belongs to one unit directory (SPEC §3.2); compile the unit as one directory", len(dirs))
	}
	path := filepath.Join(dir, FileName)

	live := Render(u)
	data, readErr := os.ReadFile(path)
	var entry []string
	switch {
	case readErr == nil:
		base, err := Parse(path, data)
		if err != nil {
			return "", false, err
		}
		live.History = base.History
		if live.Text() == string(data) {
			return path, false, nil
		}
		entry = historyEntry(date, reason, Diff(base, live, DefaultTokenPolicy))
	case os.IsNotExist(readErr):
		noun := "tables"
		if len(live.Tables) == 1 {
			noun = "table"
		}
		entry = []string{
			fmt.Sprintf("### %s — %s", date, reason),
			fmt.Sprintf("- baseline created over %d %s — data written BEFORE this point is not covered by it", len(live.Tables), noun),
		}
	default:
		return "", false, readErr
	}

	if len(live.History) > 0 {
		live.History = append(live.History, "")
	}
	live.History = append(live.History, entry...)
	if err := os.WriteFile(path, []byte(live.Text()), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// historyEntry is the intentional-break log's one entry: the date, the reason,
// and one line per edit that changed what stored data means or loses. Edits the
// wire absorbs are not listed — the projection above already records them, and
// a history of them would bury the breaks it exists to show.
func historyEntry(date, reason string, findings []Finding) []string {
	entry := []string{fmt.Sprintf("### %s — %s", date, reason)}
	refusals, warns := Split(findings)
	for _, f := range append(refusals, warns...) {
		entry = append(entry, fmt.Sprintf("- %s: %s [%s]", f.Where, f.What, f.Verdict))
	}
	if len(refusals) == 0 && len(warns) == 0 {
		entry = append(entry, "- no compatibility-affecting edits; the wire absorbs the rest")
	}
	return entry
}
