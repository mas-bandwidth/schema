// compare.go — EXPERIMENT (issue #105). The code-identity instrument.
//
//	go run ./experiment/reflect/tools/compare.go <otool -tv output>
//
// For each (emitted, generic) pair of probe symbols it reports:
//
//   - instruction count
//   - HOT calls into the serialize runtime, counted the way BENCH-STANDARD §4.1
//     defines the universal ground truth: bl/blr, plus a tail `b` whose target
//     is ANOTHER function's symbol. Cold calls (split .cold/.cold.N targets,
//     §4.2 signal 1) are counted beside, never inside.
//   - stack frame bytes claimed by the prologue
//   - a NORMALIZED disassembly comparison: addresses and intra-function branch
//     targets are rewritten relative to the instruction that carries them, and
//     call targets are resolved to symbol names. IMMEDIATES ARE KEPT — a
//     constant that failed to propagate has to show up as a difference, so
//     normalizing them away would destroy the measurement.
package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type insn struct {
	addr uint64
	op   string
	args string
}

type sym struct {
	name  string
	start uint64
	end   uint64
	ins   []insn
}

var hexAddr = regexp.MustCompile(`0x[0-9a-f]+`)

func parse(path string) ([]*sym, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var syms []*sym
	var cur *sym
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.Contains(line, "\t") {
			name := strings.TrimSuffix(line, ":")
			if strings.HasSuffix(name, ".o") { // the file header otool prints
				continue
			}
			cur = &sym{name: name}
			syms = append(syms, cur)
			continue
		}
		if cur == nil {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		addr, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 64)
		if err != nil {
			continue
		}
		args := ""
		if len(parts) > 2 {
			args = strings.TrimSpace(strings.Join(parts[2:], " "))
		}
		if len(cur.ins) == 0 {
			cur.start = addr
		}
		cur.end = addr
		cur.ins = append(cur.ins, insn{addr: addr, op: strings.TrimSpace(parts[1]), args: args})
	}
	return syms, sc.Err()
}

// symbolAt resolves an absolute address to "<symbol>+<offset>" when it lands
// inside a known symbol, so a call target compares by name rather than address.
func symbolAt(syms []*sym, addr uint64) (string, bool) {
	for _, s := range syms {
		if len(s.ins) == 0 {
			continue
		}
		if addr >= s.start && addr <= s.end {
			return s.name, true
		}
	}
	return "", false
}

var branchOps = map[string]bool{
	"b": true, "bl": true, "blr": true, "br": true,
	"b.eq": true, "b.ne": true, "b.lt": true, "b.le": true, "b.gt": true, "b.ge": true,
	"b.hi": true, "b.ls": true, "b.hs": true, "b.lo": true, "b.mi": true, "b.pl": true,
	"b.vs": true, "b.vc": true, "b.al": true,
	"cbz": true, "cbnz": true, "tbz": true, "tbnz": true,
}

// normalize rewrites one instruction into a form that is position independent
// but immediate preserving.
func normalize(syms []*sym, self *sym, in insn) string {
	op, args := in.op, in.args
	if branchOps[op] {
		args = hexAddr.ReplaceAllStringFunc(args, func(m string) string {
			t, err := strconv.ParseUint(strings.TrimPrefix(m, "0x"), 16, 64)
			if err != nil {
				return m
			}
			if t >= self.start && t <= self.end {
				// intra-function: relative to this instruction, in instructions
				return fmt.Sprintf("<self%+d>", (int64(t)-int64(in.addr))/4)
			}
			if n, ok := symbolAt(syms, t); ok {
				return "<" + n + ">"
			}
			return "<extern>"
		})
	}
	return op + " " + args
}

// isCall reports whether the instruction transfers control into ANOTHER
// function (BENCH-STANDARD §4.1: bl/blr always; a tail `b` counts when its
// target is another function's symbol).
func isCall(syms []*sym, self *sym, in insn) (bool, string) {
	switch in.op {
	case "bl":
		m := hexAddr.FindString(in.args)
		if m == "" {
			return true, strings.TrimSpace(in.args)
		}
		t, _ := strconv.ParseUint(strings.TrimPrefix(m, "0x"), 16, 64)
		if n, ok := symbolAt(syms, t); ok {
			return true, n
		}
		return true, "extern"
	case "blr", "br":
		return true, "indirect"
	case "b":
		m := hexAddr.FindString(in.args)
		if m == "" {
			return true, strings.TrimSpace(in.args)
		}
		t, _ := strconv.ParseUint(strings.TrimPrefix(m, "0x"), 16, 64)
		if t >= self.start && t <= self.end {
			return false, "" // control flow inside the same function
		}
		if n, ok := symbolAt(syms, t); ok {
			return true, n
		}
		return true, "extern"
	}
	return false, ""
}

var stackImm = regexp.MustCompile(`#(0x[0-9a-f]+|\d+)`)

// frameBytes reads the prologue's stack claim: `sub sp, sp, #N` plus any
// pre-indexed `stp ..., [sp, #-N]!`.
func frameBytes(s *sym) int64 {
	var total int64
	for i, in := range s.ins {
		if i > 12 {
			break
		}
		if in.op == "sub" && strings.HasPrefix(in.args, "sp, sp, #") {
			if m := stackImm.FindStringSubmatch(in.args); m != nil {
				total += parseImm(m[1])
			}
		}
		if (in.op == "stp" || in.op == "str") && strings.Contains(in.args, "[sp, #-") && strings.HasSuffix(in.args, "]!") {
			if m := regexp.MustCompile(`\[sp, #-(0x[0-9a-f]+|\d+)\]!`).FindStringSubmatch(in.args); m != nil {
				total += parseImm(m[1])
			}
		}
	}
	return total
}

func parseImm(s string) int64 {
	if strings.HasPrefix(s, "0x") {
		v, _ := strconv.ParseUint(s[2:], 16, 64)
		return int64(v)
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func isCold(name string) bool {
	return strings.HasSuffix(name, ".cold") || regexp.MustCompile(`\.cold\.\d+$`).MatchString(name)
}

type report struct {
	name     string
	ins      int
	hot      map[string]int
	cold     map[string]int
	frame    int64
	normText []string
}

func analyze(syms []*sym, s *sym) report {
	r := report{name: s.name, ins: len(s.ins), hot: map[string]int{}, cold: map[string]int{}, frame: frameBytes(s)}
	for _, in := range s.ins {
		if ok, tgt := isCall(syms, s, in); ok {
			if isCold(tgt) {
				r.cold[tgt]++
			} else {
				r.hot[tgt]++
			}
		}
		r.normText = append(r.normText, normalize(syms, s, in))
	}
	return r
}

func fmtCalls(m map[string]int) string {
	if len(m) == 0 {
		return "0"
	}
	keys := make([]string, 0, len(m))
	total := 0
	for k, v := range m {
		keys = append(keys, k)
		total += v
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		short := k
		if i := strings.Index(short, "serialize"); i >= 0 {
			short = short[i:]
		}
		if len(short) > 54 {
			short = short[:54] + "…"
		}
		parts = append(parts, fmt.Sprintf("%dx %s", m[k], short))
	}
	return fmt.Sprintf("%d  [%s]", total, strings.Join(parts, ", "))
}

// diff is a plain LCS-free first-divergence + edit-count report; the pairs here
// are either identical or obviously different, and a full diff of 600
// instructions helps nobody.
func firstDiff(a, b []string) (int, string, string) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i, a[i], b[i]
		}
	}
	if len(a) != len(b) {
		return n, "<end>", "<end>"
	}
	return -1, "", ""
}

var dumpDir = ""

func dump(dir string, r report) {
	os.MkdirAll(dir, 0o755)
	os.WriteFile(dir+"/"+strings.TrimPrefix(r.name, "_")+".txt", []byte(strings.Join(r.normText, "\n")+"\n"), 0o644)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: compare <otool -tv output>")
		os.Exit(2)
	}
	if len(os.Args) > 2 {
		dumpDir = os.Args[2]
	}
	syms, err := parse(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	byName := map[string]*sym{}
	for _, s := range syms {
		byName[s.name] = s
	}

	pairs := [][2]string{
		{"_probe_emitted_write_testdata", "_probe_generic_write_testdata"},
		{"_probe_emitted_read_testdata", "_probe_generic_read_testdata"},
		{"_probe_emitted_write_ship", "_probe_generic_write_ship"},
		{"_probe_emitted_read_ship", "_probe_generic_read_ship"},
		{"_probe_emitted_write_testdata", "_probe_control_verbatim_write_testdata"},
		{"_probe_emitted_write_testdata", "_probe_control_perfield_write_testdata"},
	}

	fmt.Printf("%-30s %6s %8s %s\n", "symbol", "insns", "frame", "hot runtime calls")
	fmt.Println(strings.Repeat("-", 110))

	verdicts := []string{}
	for _, p := range pairs {
		a, ok1 := byName[p[0]]
		b, ok2 := byName[p[1]]
		if !ok1 || !ok2 {
			fmt.Printf("MISSING: %s / %s\n", p[0], p[1])
			continue
		}
		ra, rb := analyze(syms, a), analyze(syms, b)
		fmt.Printf("%-30s %6d %8d %s\n", strings.TrimPrefix(ra.name, "_probe_"), ra.ins, ra.frame, fmtCalls(ra.hot))
		fmt.Printf("%-30s %6d %8d %s\n", strings.TrimPrefix(rb.name, "_probe_"), rb.ins, rb.frame, fmtCalls(rb.hot))
		if len(ra.cold) > 0 || len(rb.cold) > 0 {
			fmt.Printf("%-30s cold: %s / %s\n", "", fmtCalls(ra.cold), fmtCalls(rb.cold))
		}

		if dumpDir != "" {
			dump(dumpDir, ra)
			dump(dumpDir, rb)
		}

		idx, la, lb := firstDiff(ra.normText, rb.normText)
		var v string
		switch {
		case idx < 0:
			v = fmt.Sprintf("%-24s INSTRUCTION-IDENTICAL (%d instructions, normalized)", pairName(p), ra.ins)
		default:
			delta := rb.ins - ra.ins
			v = fmt.Sprintf("%-24s DIFFERENT: %+d instructions (%d -> %d); first divergence at index %d:\n"+
				"%-24s   emitted: %s\n%-24s   generic: %s",
				pairName(p), delta, ra.ins, rb.ins, idx, "", la, "", lb)
		}
		verdicts = append(verdicts, v)
		fmt.Println()
	}

	fmt.Println(strings.Repeat("=", 110))
	for _, v := range verdicts {
		fmt.Println(v)
	}
}

func pairName(p [2]string) string {
	b := strings.TrimPrefix(p[1], "_probe_")
	return b
}
