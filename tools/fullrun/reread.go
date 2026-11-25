package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// rereadBackup is where reread keeps a shard's results as the shard wrote
// them.
const rereadBackup = resultsFile + ".before-reread"

// reread reads the logs of every unit a finished shard ran again with this
// build's verdicts and rewrites its results.jsonl, so a verdict or a first
// line the launcher misread is corrected without running anything. The
// results as the shard wrote them are kept in results.jsonl.before-reread,
// and a shard reread before is read again from them, never from a reread.
// A shard that has not finished, one a shard still holds, and results that leave a name of the manifest without one are refused. A log that cannot be read as a whole
// run (no exit line) is an error naming it, never a verdict.
func reread(out string, log io.Writer) error {
	mb, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		return err
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return fmt.Errorf("%s: %w", filepath.Join(out, "manifest.json"), err)
	}
	if _, err := os.Stat(filepath.Join(out, doneFile)); err != nil {
		return fmt.Errorf("%s: the shard has not finished (no %s): not reread", out, doneFile)
	}
	unlock, err := lockOut(out)
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(out, resultsFile)
	backup := filepath.Join(out, rereadBackup)
	from := path
	kept := false
	if _, err := os.Stat(backup); err == nil {
		from, kept = backup, true
	}
	orig, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	res, err := loadResults(from)
	if err != nil {
		return err
	}
	// a line a shard's death cut short is dropped, as the shard dropped it
	// when it ran that unit again; a name with no result is refused
	units := map[string]Unit{}
	have := map[string]bool{}
	for _, r := range res {
		have[r.Unit+"\x00"+r.Name] = true
	}
	for _, u := range m.Units {
		units[u.ID] = u
		for _, n := range u.Names {
			if u.Runs() && !have[u.ID+"\x00"+n.Name] {
				return fmt.Errorf("%s: %s of unit %s has no result: not reread", from, n.Name, u.ID)
			}
		}
	}
	type key struct{ unit, log string }
	verdicts := map[key]map[string]Result{}
	read, toGreen, toRed, changed := 0, 0, 0, 0
	for i, r := range res {
		u, ok := units[r.Unit]
		if !ok {
			return fmt.Errorf("%s: result %s names unit %s, which the manifest does not hold", out, r.Name, r.Unit)
		}
		if !u.Runs() {
			continue
		}
		k := key{r.Unit, r.Log}
		v, ok := verdicts[k]
		if !ok {
			data, err := os.ReadFile(filepath.Join(out, r.Log))
			if err != nil {
				return err
			}
			body, runErr, err := splitLog(data)
			if err != nil {
				return fmt.Errorf("%s: %w", filepath.Join(out, r.Log), err)
			}
			v = unitVerdicts(u, body, runErr)
			verdicts[k] = v
		}
		arg := ""
		for j, n := range u.Names {
			if n.Name == r.Name {
				arg = u.Args[j]
			}
		}
		if arg == "" {
			return fmt.Errorf("%s: result %s is not a name of unit %s", out, r.Name, r.Unit)
		}
		nv := v[arg]
		read++
		if nv.Verdict != r.Verdict || nv.First != r.First {
			changed++
			switch {
			case r.Verdict != Pass && nv.Verdict == Pass:
				toGreen++
			case r.Verdict == Pass && nv.Verdict != Pass:
				toRed++
			}
			res[i].Verdict, res[i].First = nv.Verdict, nv.First
		}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, r := range res {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if !kept {
		if err := os.WriteFile(backup, orig, 0o644); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	_, err = fmt.Fprintf(log, "reread %s: %d results read again, %d changed from the results as written: %d red to green, %d green to red\n", out, read, changed, toGreen, toRed)
	return err
}

// splitLog takes a unit's log apart: what the command wrote, between the
// two header lines and the exit line runUnit adds, and the error it exited
// with.
func splitLog(data []byte) ([]byte, error, error) {
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "# fullrun ") || !strings.HasPrefix(lines[1], "# ") {
		return nil, nil, errors.New("not a unit log: no header")
	}
	exit := -1
	for i := len(lines) - 1; i >= 2; i-- {
		if strings.HasPrefix(lines[i], "# exit: ") {
			exit = i
			break
		}
	}
	if exit < 0 {
		return nil, nil, errors.New("no exit line: the unit did not finish")
	}
	how := strings.TrimPrefix(lines[exit], "# exit: ")
	cut := strings.LastIndex(how, ", ")
	if cut < 0 {
		return nil, nil, fmt.Errorf("exit line %q has no time", lines[exit])
	}
	var runErr error
	if e := how[:cut]; e != "<nil>" {
		runErr = errors.New(e)
	}
	// runUnit writes "\n# exit:", so the line before it is the blank it added
	return []byte(strings.Join(lines[2:exit], "\n")), runErr, nil
}
