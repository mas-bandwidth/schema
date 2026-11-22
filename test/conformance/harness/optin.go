package main

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

// AN OPT-IN UNIT IS ANSWERED BY THE LEGS THAT REGISTER IT. A compiled leg
// registers each unit's codecs by hand, and its driver stops at a row whose
// root it has no codec for, which would fail every surface the row sits on.
// So a unit the manifest marks `opt-in <unit>` reaches a leg only when the
// leg's `list` names `unit <key>`. For any other leg the harness hands the
// driver a manifest without those rows, and scores each of them ABSENT: a
// missing feature, never a pass (and on the reference leg, a failure).

// refKind is what a derived manifest line names in its reference column.
type refKind int

const (
	refNone     refKind = iota // the row reads no unit (a forgery)
	refUnit                    // a unit key
	refConn                    // a connection key: the row reads the connection's unit
	refInstance                // an instance name: the row reads the instance's unit
)

// derivedRow is one line kind of the derived manifest (deriveManifest, run.go):
// which column holds what the row reads, and the cases the row carries. It is
// the one place that says how a line is read, so the filter and the check
// below never guess a column. A cook-write's third column is a file name and a
// forgery's a kind; neither is ever read as a unit.
type derivedRow struct {
	ref   refKind
	col   int
	cases func(f []string) []caseKey
}

func sameName(surfaces ...string) func(f []string) []caseKey {
	return func(f []string) []caseKey {
		out := make([]caseKey, len(surfaces))
		for i, s := range surfaces {
			out[i] = caseKey{s, f[1]}
		}
		return out
	}
}

func ownName(f []string) []caseKey { return []caseKey{{f[0], f[1]}} }

var derivedRows = map[string]derivedRow{
	"unit":       {ref: refUnit, col: 1},
	"connection": {ref: refUnit, col: 2},
	// a prior-writer drift row (run.go, md.Prior) rides its own pair of
	// keywords, its connection read as a connection is and its message
	// scored on the message-prior surface (claims.go)
	"prior-connection": {ref: refUnit, col: 2},
	"prior-message":    {ref: refConn, col: 2, cases: func(f []string) []caseKey { return []caseKey{{"message-prior", f[1]}} }},
	"instance": {ref: refUnit, col: 2, cases: func(f []string) []caseKey {
		// a no-text instance has a wire case alone (§16.7, claims.go)
		if len(f) > 5 && slices.Contains(f[5:], "no-text") {
			return []caseKey{{"wire", f[1]}}
		}
		return []caseKey{{"wire", f[1]}, {"json-read", f[1]}, {"json-write", f[1] + ".json"}}
	}},
	"json-write-refusal":    {ref: refUnit, col: 2, cases: func(f []string) []caseKey { return []caseKey{{"json-write", f[1] + ".json"}} }},
	"message":               {ref: refConn, col: 2, cases: ownName},
	"message-refusal":       {ref: refConn, col: 2, cases: ownName},
	"write-refusal-message": {ref: refConn, col: 2, cases: ownName},
	"write-refusal":         {ref: refUnit, col: 2, cases: ownName},
	"write-refusal-fixed":   {ref: refUnit, col: 2, cases: ownName},
	"write-refusal-json":    {ref: refUnit, col: 2, cases: ownName},
	"retain":                {ref: refUnit, col: 2, cases: sameName("retain", "retain-save")},
	"report":                {ref: refUnit, col: 2, cases: ownName},
	"framing":               {ref: refUnit, col: 2, cases: ownName},
	"json-hostile":          {ref: refUnit, col: 2, cases: ownName},
	"cook":                  {ref: refUnit, col: 2, cases: sameName("cook", "cook-foreign")},
	"block":                 {ref: refUnit, col: 2, cases: sameName("block", "block-foreign", "block-dump")},
	"cook-write": {ref: refInstance, col: 1, cases: func(f []string) []caseKey {
		return []caseKey{{"cook-write", f[1]}, {"cook-write", f[1] + "-be"}}
	}},
	"forgery": {ref: refNone},
}

// derivedLine reads one line of a derived manifest: its fields and its kind,
// or nil fields for a blank line or a comment. A kind the table does not
// know, or a line too short for its reference column, is refused.
func derivedLine(line string) ([]string, derivedRow, error) {
	f := strings.Fields(line)
	if len(f) == 0 || strings.HasPrefix(f[0], "#") {
		return nil, derivedRow{}, nil
	}
	row, ok := derivedRows[f[0]]
	if !ok {
		return nil, derivedRow{}, fmt.Errorf("`%s`: %q is not a derived manifest line kind", line, f[0])
	}
	if len(f) <= row.col || len(f) < 2 {
		return nil, derivedRow{}, fmt.Errorf("`%s`: too short for a %s line", line, f[0])
	}
	return f, row, nil
}

// optInLines splits a derived manifest's text for one leg: kept is the text
// without the rows on units the leg has not opted into, and dropped names
// every row those rows carried.
func optInLines(text string, skip map[string]bool) (kept string, dropped map[string]bool, err error) {
	dropped = map[string]bool{}
	if len(skip) == 0 {
		return text, dropped, nil
	}
	// a pass over the connections and instances first, so a row that reads
	// one goes with it
	conns := map[string]bool{}
	instances := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		f, row, err := derivedLine(line)
		if err != nil {
			return "", nil, err
		}
		if f == nil || row.ref != refUnit || !skip[f[row.col]] {
			continue
		}
		switch f[0] {
		case "connection", "prior-connection":
			conns[f[1]] = true
		case "instance":
			instances[f[1]] = true
		}
	}
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		f, row, _ := derivedLine(line)
		drop := false
		if f != nil {
			switch row.ref {
			case refUnit:
				drop = skip[f[row.col]]
			case refConn:
				drop = conns[f[row.col]]
			case refInstance:
				drop = instances[f[row.col]]
			}
		}
		if drop {
			if row.cases != nil {
				dropped[f[1]] = true
			}
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n"), dropped, nil
}

// checkLegRows holds the filtered manifest to the cases the leg is asked: no
// row it keeps belongs to a case the leg is not asked, and no row it keeps
// names a connection or an instance it dropped. Either would hand a driver a
// row it can answer only by failing its whole surface, so the leg's manifest
// is refused here instead, naming the row.
func checkLegRows(kept string, cases notAsked) error {
	conns, instances := map[string]bool{}, map[string]bool{}
	for line := range strings.SplitSeq(kept, "\n") {
		f, _, err := derivedLine(line)
		if err != nil {
			return err
		}
		if f == nil {
			continue
		}
		switch f[0] {
		case "connection", "prior-connection":
			conns[f[1]] = true
		case "instance":
			instances[f[1]] = true
		}
	}
	for line := range strings.SplitSeq(kept, "\n") {
		f, row, _ := derivedLine(line)
		if f == nil {
			continue
		}
		if row.cases != nil {
			for _, key := range row.cases(f) {
				if unit, ok := cases[key]; ok {
					return fmt.Errorf("`%s` is a row of case %q on the %s surface, which reads the omitted unit %s",
						strings.Join(f, " "), key.name, key.surface, unit)
				}
			}
		}
		switch row.ref {
		case refConn:
			if !conns[f[row.col]] {
				return fmt.Errorf("`%s` names connection %s, which the leg is not handed", strings.Join(f, " "), f[row.col])
			}
		case refInstance:
			if !instances[f[row.col]] {
				return fmt.Errorf("`%s` names instance %s, which the leg is not handed", strings.Join(f, " "), f[row.col])
			}
		}
	}
	return nil
}

// caseKey is a case as a driver answers it: its surface and its name there.
type caseKey struct {
	surface, name string
}

// caseOwners maps every case to the unit its row reads. A CASE'S IDENTITY IS
// (unit, surface, name), and a name alone says nothing: the manifest lets an
// instance and a write-refusal row on two units share a name, and each is a
// case of its own surface. What it may not do is put two units' rows under ONE
// name on ONE surface, because a driver answers a case by surface and name and
// the answer would belong to both; that manifest is refused here.
func caseOwners(m *Manifest) (map[caseKey]string, error) {
	owners := map[caseKey]string{}
	err := walkCases(m, func(surface, name, unit, _ string) error {
		key := caseKey{surface, name}
		if had, ok := owners[key]; ok && had != unit {
			return fmt.Errorf("the manifest is ambiguous: case %q of the %s surface belongs to a row on unit %s and a row on unit %s; "+
				"a case is answered by surface and name, so one name on one surface is one unit's", name, surface, had, unit)
		}
		owners[key] = unit
		return nil
	})
	if err != nil {
		return nil, err
	}
	return owners, nil
}

// notAsked is the cases a leg is not handed, each with the omitted opt-in
// unit its row reads: the whole identity, so a case of another unit that
// shares the name, on this surface's neighbour, is still asked.
type notAsked map[caseKey]string

// has says whether the case of this surface under this name is one the leg
// was not handed.
func (n notAsked) has(surface, name string) bool {
	_, ok := n[caseKey{surface, name}]
	return ok
}

// casesNotAsked is every case whose row reads a unit in skip.
func casesNotAsked(m *Manifest, skip map[string]bool) (notAsked, error) {
	owners, err := caseOwners(m)
	if err != nil {
		return nil, err
	}
	out := notAsked{}
	for key, unit := range owners {
		if skip[unit] {
			out[key] = unit
		}
	}
	return out, nil
}

// legManifest is the derived manifest a leg is handed, and the cases it is
// not asked: derived itself when the leg opted into every opt-in unit, and
// otherwise a filtered copy beside it.
func legManifest(m *Manifest, derived, lang string, listed map[string]bool) (string, notAsked, []string, error) {
	skip := map[string]bool{}
	var missing []string
	for key := range m.OptIn {
		if !listed["unit "+key] {
			skip[key] = true
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(skip) == 0 {
		return derived, notAsked{}, nil, nil
	}
	cases, err := casesNotAsked(m, skip)
	if err != nil {
		return "", nil, nil, err
	}
	text, err := os.ReadFile(derived)
	if err != nil {
		return "", nil, nil, err
	}
	kept, _, err := optInLines(string(text), skip)
	if err != nil {
		return "", nil, nil, fmt.Errorf("the manifest for %s: %w", lang, err)
	}
	if err := checkLegRows(kept, cases); err != nil {
		return "", nil, nil, fmt.Errorf("the manifest for %s: %w", lang, err)
	}
	path := strings.TrimSuffix(derived, ".txt") + "-" + lang + ".txt"
	if err := os.WriteFile(path, []byte(kept), 0o644); err != nil {
		return "", nil, nil, err
	}
	return path, cases, missing, nil
}
