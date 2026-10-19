// Command report turns the pinned mutation tool's JSON output into the
// nightly report: one SURVIVED line per mutant no test killed, one STALE line
// per equivalent-mutant row whose source line no longer holds its original
// text, and one summary line.
//
// The tool is gremlins (see README.md); its JSON records a mutant's file, line,
// column, mutator and status, but not the substituted text, so a SURVIVED line
// names the mutator in place of the mutant token that the tool does not print.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type mutation struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type fileResult struct {
	FileName  string     `json:"file_name"`
	Mutations []mutation `json:"mutations"`
}

type toolReport struct {
	Files []fileResult `json:"files"`
}

type target struct {
	File    string
	Command string
	Timeout string
}

type equivalent struct {
	File     string
	Line     int
	Mutator  string
	Original string
	Why      string
}

type summary struct {
	targets    map[string]bool
	mutants    int
	killed     int
	survived   int
	equivalent int
	timedOut   int
	notCovered int
}

func main() {
	root := flag.String("root", ".", "repository root")
	targetsPath := flag.String("targets", "tools/mutation/targets.tsv", "targets.tsv")
	equivDir := flag.String("equivalent", "tools/mutation/equivalent", "equivalent-mutant directory")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: report [-root dir] [-targets file] [-equivalent dir] tool.json...")
		os.Exit(2)
	}
	if err := run(*root, *targetsPath, *equivDir, flag.Args(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
		os.Exit(1)
	}
}

func run(root, targetsPath, equivDir string, jsonPaths []string, out io.Writer) error {
	targets, err := readTargets(targetsPath)
	if err != nil {
		return err
	}
	byBase := map[string]string{}
	for _, t := range targets {
		byBase[filepath.Base(t.File)] = t.File
	}

	equivalents, err := readEquivalents(equivDir)
	if err != nil {
		return err
	}

	var reports []toolReport
	for _, path := range jsonPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r toolReport
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		reports = append(reports, r)
	}

	s := summary{targets: map[string]bool{}}
	lines := map[string][]string{}
	for _, r := range reports {
		for _, f := range r.Files {
			file := f.FileName
			if !strings.ContainsRune(file, filepath.Separator) {
				if full, ok := byBase[file]; ok {
					file = full
				}
			}
			s.targets[file] = true
			for _, m := range f.Mutations {
				s.mutants++
				switch strings.ToUpper(m.Status) {
				case "KILLED":
					s.killed++
				case "LIVED":
					if equivalentFor(root, equivalents, file, m) {
						s.equivalent++
						continue
					}
					s.survived++
					original := sourceToken(root, file, m.Line, m.Column)
					if original == "" {
						original = "?"
					}
					fmt.Fprintf(out, "SURVIVED %s:%d:%d %s %s -> <%s>\n", file, m.Line, m.Column, m.Type, original, m.Type)
				case "TIMED OUT", "TIMEOUT", "TIMED-OUT":
					s.timedOut++
				case "NOT COVERED", "NOT_COVERED":
					s.notCovered++
				}
			}
		}
	}

	lines["stale"] = staleRows(root, equivalents)
	sort.Strings(lines["stale"])
	for _, line := range lines["stale"] {
		fmt.Fprintln(out, line)
	}

	score := 0.0
	if scored := s.killed + s.survived + s.timedOut + s.notCovered; scored > 0 {
		score = 100 * float64(s.killed) / float64(scored)
	}
	fmt.Fprintf(out, "mutation targets=%d mutants=%d killed=%d survived=%d equivalent=%d timed_out=%d not_covered=%d score=%.2f\n",
		len(s.targets), s.mutants, s.killed, s.survived, s.equivalent, s.timedOut, s.notCovered, score)
	return nil
}

func readTargets(path string) ([]target, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []target
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			return nil, fmt.Errorf("%s: target row has %d columns, want file, command, timeout", path, len(fields))
		}
		out = append(out, target{File: fields[0], Command: fields[1], Timeout: fields[2]})
	}
	return out, sc.Err()
}

func readEquivalents(dir string) ([]equivalent, error) {
	var out []equivalent
	matches, err := filepath.Glob(filepath.Join(dir, "*.tsv"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 5 {
				f.Close()
				return nil, fmt.Errorf("%s:%d: equivalent row has %d columns, want file, line, mutator, original, why", path, lineNo, len(fields))
			}
			n, err := strconv.Atoi(strings.TrimSpace(fields[1]))
			if err != nil {
				f.Close()
				return nil, fmt.Errorf("%s:%d: bad line %q: %w", path, lineNo, fields[1], err)
			}
			out = append(out, equivalent{File: fields[0], Line: n, Mutator: fields[2], Original: fields[3], Why: fields[4]})
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func equivalentFor(root string, rows []equivalent, file string, m mutation) bool {
	line := sourceLine(root, file, m.Line)
	for _, row := range rows {
		if row.File == file && row.Line == m.Line && row.Mutator == m.Type &&
			line != "" && strings.Contains(line, row.Original) {
			return true
		}
	}
	return false
}

func staleRows(root string, rows []equivalent) []string {
	var out []string
	seen := map[string]bool{}
	for _, row := range rows {
		line := sourceLine(root, row.File, row.Line)
		if line != "" && strings.Contains(line, row.Original) {
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", row.File, row.Line, row.Mutator)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, fmt.Sprintf("STALE %s:%d %s %s", row.File, row.Line, row.Mutator, row.Original))
	}
	return out
}

func sourceLine(root, file string, line int) string {
	if line < 1 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

func sourceToken(root, file string, line, column int) string {
	text := sourceLine(root, file, line)
	start := column - 1
	if start < 0 || start >= len(text) {
		return ""
	}
	isWord := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	end := start
	if isWord(text[start]) {
		for end < len(text) && isWord(text[end]) {
			end++
		}
	} else {
		end = start + 1
		if strings.ContainsRune("+-*/%<>=!&|^~:", rune(text[start])) && end < len(text) &&
			strings.ContainsRune("=<>", rune(text[end])) {
			end++
		}
	}
	return text[start:end]
}
