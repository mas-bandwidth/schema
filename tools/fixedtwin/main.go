// fixedtwin is the C / C++ fixed-form runtime twin gate.
//
// It extracts the emitted fixed runtimes (and the identity plan / layout
// bytes they run) from the paired Fixed Table headers, normalises them
// through the token map in bench/paired/TWIN.md, and diffs the rest.
// Any leftover line that is not one of the three named remaining
// differences is a failure. C++-only algorithm §5 rows are OWED by the
// C leg and stripped by the map; they are not a fourth named remaining.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: fixedtwin <c-header> <cpp-header>\n")
		os.Exit(2)
	}
	cPath, cppPath := os.Args[1], os.Args[2]
	cSrc, err := os.ReadFile(cPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixedtwin: %v\n", err)
		os.Exit(1)
	}
	cppSrc, err := os.ReadFile(cppPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixedtwin: %v\n", err)
		os.Exit(1)
	}
	cPlain, err := dequalifyC(string(cSrc), ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixedtwin: %v\n", err)
		os.Exit(1)
	}
	leftover, err := compareHeaders(cPlain, string(cppSrc))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixedtwin: %v\n", err)
		os.Exit(1)
	}
	if len(leftover) > 0 {
		for _, d := range nameDisagreements(leftover) {
			fmt.Fprintln(os.Stderr, "fixedtwin: "+d)
		}
		fmt.Fprintf(os.Stderr, "fixedtwin: leftover lines after the twin map (not one of the three named differences):\n")
		for _, line := range leftover {
			fmt.Fprintln(os.Stderr, line)
		}
		os.Exit(1)
	}
	fmt.Println("tables-fixed-twin: C and C++ fixed runtimes agree after the named twin map")
}

func compareHeaders(cSrc, cppSrc string) ([]string, error) {
	cRun, err := extractRuntime(cSrc)
	if err != nil {
		return nil, fmt.Errorf("c: %w", err)
	}
	cppRun, err := extractRuntime(cppSrc)
	if err != nil {
		return nil, fmt.Errorf("cpp: %w", err)
	}
	var leftover []string
	if !hasStrictCountTextScan(cppRun) {
		leftover = append(leftover, "runtime:cpp: the strict count/text pre-scan (TableFixedRefuseScan) is missing or weakened")
	}
	if !hasStrictCountTextScanCall(cppSrc) {
		leftover = append(leftover, "runtime:cpp: the strict count/text pre-scan (TableFixedRefuseScan) is defined but never invoked as the whole-read refusal")
	}
	leftover = append(leftover, diffLines("runtime", canonicalize(cRun), canonicalize(cppRun))...)

	cPlan, err := extractArray(cSrc, "fixed_table_fixed_plan", "FixedTableFixedPlan")
	if err != nil {
		return nil, fmt.Errorf("c plan: %w", err)
	}
	cppPlan, err := extractArray(cppSrc, "fixed_table_fixed_plan", "FixedTableFixedPlan")
	if err != nil {
		return nil, fmt.Errorf("cpp plan: %w", err)
	}
	leftover = append(leftover, diffLines("plan", canonicalize(cPlan), canonicalize(cppPlan))...)

	cLay, err := extractArray(cSrc, "fixed_table_fixed_layout", "FixedTableFixedLayout")
	if err != nil {
		return nil, fmt.Errorf("c layout: %w", err)
	}
	cppLay, err := extractArray(cppSrc, "fixed_table_fixed_layout", "FixedTableFixedLayout")
	if err != nil {
		return nil, fmt.Errorf("cpp layout: %w", err)
	}
	leftover = append(leftover, diffLines("layout", canonicalize(cLay), canonicalize(cppLay))...)

	return leftover, nil
}

// A disagreement is a rule one leg carries and the other lacks. The twin
// map never strips one: its lines stay in the leftover, and this list only
// names the rule so a red says which rule is open, from which spec line, and
// which leg is behind. A disagreement leaves this list when the lagging leg
// carries the rule and the gate goes green over it.
type disagreement struct {
	name    string
	markers []string
}

var openDisagreements = []disagreement{
	{
		name: "ABSENT PAYLOAD, AWAITS THE DEAD-BYTES RULE: under a clear present flag (or an inactive union arm) the legs " +
			"land different values. C++ lands the declared defaults (kTableFixedPresentDefaults); the Go engine and the C leg " +
			"land the bytes the record carries. docs/SPEC-TABLES.md §3.4 says the payload is \"IGNORED on read, never a " +
			"refusal\", which neither spelling settles. The pending standard change on dead bytes in a fixed record decides " +
			"which leg moves; this is not yet a defect in either leg",
		markers: []string{"kTableFixedPresentDefaults", "defaults = nullptr", ", defaults )", "payload_dst", "payload_size", "second_half"},
	},
}

// nameDisagreements returns the open disagreements the leftover shows, each
// with the count of leftover lines that carry its markers.
func nameDisagreements(left []string) []string {
	var out []string
	for _, d := range openDisagreements {
		n := 0
		for _, line := range left {
			for _, m := range d.markers {
				if strings.Contains(line, m) {
					n++
					break
				}
			}
		}
		if n > 0 {
			out = append(out, fmt.Sprintf("OPEN DISAGREEMENT (%d leftover line(s)): %s", n, d.name))
		}
	}
	return out
}

func diffLines(section string, c, cpp []string) []string {
	// LCS: a hoisted C declaration must not shift every later line into a
	// leftover. Only statements that appear on one side are leftovers.
	n, m := len(c), len(cpp)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case c[i] == cpp[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case c[i] == cpp[j]:
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, fmt.Sprintf("%s:c:%d + %s", section, i+1, c[i]))
			i++
		default:
			out = append(out, fmt.Sprintf("%s:cpp:%d - %s", section, j+1, cpp[j]))
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, fmt.Sprintf("%s:c:%d + %s", section, i+1, c[i]))
	}
	for ; j < m; j++ {
		out = append(out, fmt.Sprintf("%s:cpp:%d - %s", section, j+1, cpp[j]))
	}
	return out
}
