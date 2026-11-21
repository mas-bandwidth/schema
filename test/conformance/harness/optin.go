package main

import (
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
		case (f[0] == "message" || f[0] == "message-refusal") && len(f) > 2 && conns[f[2]]:
			drop, dropped[f[1]] = true, true
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

// caseNotAsked says whether a surface's case sits on a row the leg was not
// handed. The json-write surface names a case after its row plus ".json"
// (the file the driver writes), so it is looked up by the row's own name too.
func caseNotAsked(notAsked map[string]bool, surface, name string) bool {
	if notAsked[name] {
		return true
	}
	if surface == "json-write" {
		if row, ok := strings.CutSuffix(name, ".json"); ok {
			return notAsked[row]
		}
	}
	return false
}

// legManifest is the derived manifest a leg is handed, and the cases it is
// not asked: derived itself when the leg opted into every opt-in unit, and
// otherwise a filtered copy beside it.
func legManifest(m *Manifest, derived, lang string, listed map[string]bool) (string, map[string]bool, []string, error) {
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
		return derived, map[string]bool{}, nil, nil
	}
	text, err := os.ReadFile(derived)
	if err != nil {
		return "", nil, nil, err
	}
	kept, dropped := optInLines(string(text), skip)
	path := strings.TrimSuffix(derived, ".txt") + "-" + lang + ".txt"
	if err := os.WriteFile(path, []byte(kept), 0o644); err != nil {
		return "", nil, nil, err
	}
	return path, dropped, missing, nil
}
