package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// THE RECORD CORPUS. Each directory of testdata/records is one lane record as
// a recorder leaves it: the .jsonl files of real `go test -json` runs
// (testdata/recordgen writes them from testdata/recordgen/ledgertest), or the
// empty and budget-only files a cut recorder leaves, and case.txt:
//
//	# what the case shows
//	args: <flags>        the lane's flags, after -slowest 0 -skips <the shipped ledger>
//	exit: <0|1>          the verdict
//	want: <text>         a line of the report (any number of them)
//
// The report runs on each exactly as ci-fast.yml's ledger step runs it, read
// against .github/skip-ledger.txt. A defect case is beside its control: the
// one that is red on the defect and the one that was always right.
const recordCases = 14

type recordCase struct {
	args []string
	exit int
	want []string
}

func readRecordCase(path string) (recordCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return recordCase{}, err
	}
	defer func() { _ = f.Close() }()
	c := recordCase{exit: -1}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		switch {
		case !ok:
			return c, fmt.Errorf("%s:%d: not a case line: %q", path, n, line)
		case key == "args":
			c.args = strings.Fields(val)
		case key == "exit":
			if c.exit, err = strconv.Atoi(val); err != nil {
				return c, fmt.Errorf("%s:%d: %w", path, n, err)
			}
		case key == "want":
			c.want = append(c.want, val)
		default:
			return c, fmt.Errorf("%s:%d: unknown key %q", path, n, key)
		}
	}
	if c.exit < 0 || len(c.want) == 0 {
		return c, fmt.Errorf("%s: a case names its exit and at least one want line", path)
	}
	return c, sc.Err()
}

func TestTheRecordCorpus(t *testing.T) {
	t.Parallel()
	root := filepath.Join("testdata", "records")
	ledger := filepath.Join("..", "..", ".github", "skip-ledger.txt")
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, d := range dirs {
		if !d.IsDir() {
			t.Errorf("%s: the corpus holds case directories only", d.Name())
			continue
		}
		dir := filepath.Join(root, d.Name())
		c, err := readRecordCase(filepath.Join(dir, "case.txt"))
		if err != nil {
			t.Errorf("%s: %v", d.Name(), err)
			continue
		}
		ran++
		args := append(append([]string{"-slowest", "0", "-skips", ledger}, c.args...), dir)
		var stdout, stderr bytes.Buffer
		if got := run(args, &stdout, &stderr, ""); got != c.exit {
			t.Errorf("%s: exit %d, want %d\n%s%s", d.Name(), got, c.exit, stdout.String(), stderr.String())
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(stdout.String(), w) {
				t.Errorf("%s: the report lacks %q:\n%s", d.Name(), w, stdout.String())
			}
		}
	}
	if ran != recordCases {
		t.Errorf("the corpus ran %d case(s), want %d: a case added or lost changes recordCases", ran, recordCases)
	}
}
