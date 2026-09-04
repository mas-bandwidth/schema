// The FILE side: where the baseline lives, when the check runs, and how
// `--update` moves it (docs/SPEC-TABLES.md §18.1, §18.2, §18.4).
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
		// EVERY parse refusal names the remedy, in one place, and the remedy
		// works: Update parses leniently and salvages the history section.
		return nil, []error{fmt.Errorf("%w — regenerate it with: schema tables-baseline --update --reason \"...\", which preserves the %s section (docs/SPEC-TABLES.md §18.4)", err, HistoryHeading)}
	}
	if base.Package != u.Package {
		return nil, []error{fmt.Errorf("%s: baseline is for package %s, this unit is package %s — the baseline belongs to the unit it sits beside", path, base.Package, u.Package)}
	}

	refusals, warns := Split(Diff(base, Render(u), DefaultTokenPolicy))
	for _, w := range warns {
		warnings = append(warnings, fmt.Sprintf("%s: %s", path, w))
	}
	for _, r := range refusals {
		errs = append(errs, fmt.Errorf("%s: %w — this edit changes what data already written MEANS, and no reader can report it; if you mean it, record it: schema tables-baseline --update --reason \"...\" (docs/SPEC-TABLES.md §18)", path, r))
	}
	return warnings, errs
}

// Nudge is the one line `schema check` prints for a unit that declares tables
// and has committed no baseline (docs/SPEC-TABLES.md §18.1). The baseline is
// opt-in — no file, no check — so the DEFAULT posture of a save-game unit is
// unguarded against every edit §4.1 marks silent, and nothing said so. This
// says it: a notice, never a block, and committing a baseline silences it.
//
// It returns "" when there is nothing to say — a unit with no tables has no
// table wire to guard, and a unit with a baseline is already guarded.
func Nudge(u *ir.Unit, paths []string) string {
	if len(u.Tables) == 0 {
		return ""
	}
	path, ok, err := Locate(paths)
	if err != nil {
		// the unit's files span directories that hold several baselines;
		// Check reports that as the error it is, and a nudge on top would
		// only bury it
		return ""
	}
	// a unit is one directory (SPEC §3.2), so the ordinary answer names it;
	// the sentence still stands for the unit whose files span several, where
	// there is no one directory to name
	where, arg := "this unit's directory", "<unit dir>"
	if ok {
		if _, statErr := os.Stat(path); statErr == nil {
			return ""
		}
		where, arg = filepath.Dir(path), filepath.Dir(path)
	}
	noun := "tables"
	if len(u.Tables) == 1 {
		noun = "table"
	}
	return fmt.Sprintf("%s declares %d %s and %s holds no %s — save-game evolution is unguarded (docs/SPEC-TABLES.md §18); commit one with: schema tables-baseline --update --reason \"first baseline\" %s",
		u.Package, len(u.Tables), noun, where, FileName, arg)
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
		return "", false, fmt.Errorf("--update needs --reason: moving the baseline declares an intentional break with data already written, and the reason is what a person reads years later when an old file refuses (docs/SPEC-TABLES.md §18.4)")
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
		// UPDATE PARSES LENIENTLY, and it has to: every parse refusal names
		// this command as the remedy, so a baseline the parser rejects — a
		// corrupt file, another tool's file, or one written by a compiler
		// whose rendering version has since moved — must be repairable
		// WITHOUT deleting the one artifact that cannot be regenerated. The
		// projection is regenerated from the unit either way; the history is
		// salvaged verbatim.
		base, err := Parse(path, data)
		if err != nil {
			live.History = salvageHistory(data)
			// the committed file carries no machine paths: the reader of this
			// history is on another machine, years later
			entry = []string{
				fmt.Sprintf("### %s — %s", date, reason),
				fmt.Sprintf("- baseline REGENERATED over an unreadable one (%s); the projection is this unit as it stands and the history above is preserved, but the previous projection could not be diffed, so no per-edit lines follow",
					strings.TrimPrefix(err.Error(), path+": ")),
			}
			break
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

// salvageHistory lifts the `## history` lines out of a baseline the parser
// cannot read. It looks for the heading and nothing else, which is exactly why
// it works on a file whose projection is unreadable: the history is prose the
// compiler never interprets.
func salvageHistory(data []byte) []string {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		if line != HistoryHeading {
			continue
		}
		history := lines[i+1:]
		for len(history) > 0 && strings.TrimSpace(history[0]) == "" {
			history = history[1:]
		}
		for len(history) > 0 && strings.TrimSpace(history[len(history)-1]) == "" {
			history = history[:len(history)-1]
		}
		return history
	}
	return nil
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
