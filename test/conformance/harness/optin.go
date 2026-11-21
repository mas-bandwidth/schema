package main

import (
	"fmt"
	"os"
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

// optInLines splits a derived manifest's text for one leg: kept is the text
// without the rows on units the leg has not opted into, and dropped names
// every case those rows carried.
func optInLines(text string, skip map[string]bool) (kept string, dropped map[string]bool) {
	dropped = map[string]bool{}
	if len(skip) == 0 {
		return text, dropped
	}
	// a pass over the connections first, so a message row on one goes too
	conns := map[string]bool{}
	instances := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) > 2 && skip[f[2]] {
			switch f[0] {
			case "connection":
				conns[f[1]] = true
			case "instance":
				instances[f[1]] = true
			}
		}
	}
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		drop := false
		switch {
		case len(f) == 0 || strings.HasPrefix(f[0], "#"):
		case f[0] == "unit" && len(f) > 1 && skip[f[1]]:
			drop = true
		case f[0] == "connection" && len(f) > 1 && conns[f[1]]:
			drop = true
		case onConnection[f[0]]:
			// its third field is a connection key, never a unit, even where
			// a key happens to spell an omitted unit's name
			if len(f) > 2 && conns[f[2]] {
				drop, dropped[f[1]] = true, true
			}
		case f[0] == "cook-write" && len(f) > 1 && instances[f[1]]:
			drop, dropped[f[1]] = true, true
		case len(f) > 2 && skip[f[2]]:
			drop, dropped[f[1]] = true, true
		}
		if !drop {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), dropped
}

// onConnection is the derived line kinds whose third field is a connection
// key rather than a unit: the row reads the connection's unit.
var onConnection = map[string]bool{"message": true, "message-refusal": true, "write-refusal-message": true}

// checkLegRows holds the filtered manifest to the cases the leg is asked: no
// row it keeps belongs to a case the leg is not asked, and no row it keeps
// names a connection or an instance it dropped. Either would hand a driver a
// row it can answer only by failing its whole surface, so the leg's manifest
// is refused here instead, naming the row.
func checkLegRows(kept string, cases notAsked) error {
	conns, instances := map[string]bool{}, map[string]bool{}
	for line := range strings.SplitSeq(kept, "\n") {
		if f := strings.Fields(line); len(f) > 1 {
			switch f[0] {
			case "connection":
				conns[f[1]] = true
			case "instance":
				instances[f[1]] = true
			}
		}
	}
	for line := range strings.SplitSeq(kept, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || strings.HasPrefix(f[0], "#") {
			continue
		}
		var surfaces []caseKey
		switch f[0] {
		case "unit", "opt-in", "connection", "forgery":
		case "instance":
			surfaces = []caseKey{{"wire", f[1]}, {"json-read", f[1]}, {"json-write", f[1] + ".json"}}
		case "json-write-refusal":
			surfaces = []caseKey{{"json-write", f[1] + ".json"}}
		default:
			surfaces = []caseKey{{f[0], f[1]}}
		}
		for _, key := range surfaces {
			if unit, ok := cases[key]; ok {
				return fmt.Errorf("`%s` is a row of case %q on the %s surface, which reads the omitted unit %s",
					strings.Join(f, " "), key.name, key.surface, unit)
			}
		}
		if onConnection[f[0]] && len(f) > 2 && !conns[f[2]] {
			return fmt.Errorf("`%s` names connection %s, which the leg is not handed", strings.Join(f, " "), f[2])
		}
		if f[0] == "cook-write" && !instances[f[1]] {
			return fmt.Errorf("`%s` names instance %s, which the leg is not handed", strings.Join(f, " "), f[1])
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
	kept, _ := optInLines(string(text), skip)
	if err := checkLegRows(kept, cases); err != nil {
		return "", nil, nil, fmt.Errorf("the manifest for %s: %w", lang, err)
	}
	path := strings.TrimSuffix(derived, ".txt") + "-" + lang + ".txt"
	if err := os.WriteFile(path, []byte(kept), 0o644); err != nil {
		return "", nil, nil, err
	}
	return path, cases, missing, nil
}
