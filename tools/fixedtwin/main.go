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
	leftover = append(leftover, diffLines("runtime", canonicalize(cRun), canonicalize(cppRun))...)

	plan, err := compareArraySection(cSrc, cppSrc, "plan", "fixed_table_fixed_plan", "FixedTableFixedPlan")
	if err != nil {
		return nil, err
	}
	leftover = append(leftover, plan...)

	layout, err := compareArraySection(cSrc, cppSrc, "layout", "fixed_table_fixed_layout", "FixedTableFixedLayout")
	if err != nil {
		return nil, err
	}
	leftover = append(leftover, layout...)

	return leftover, nil
}

// compareArraySection diffs one identity array section. A header that
// embeds the twin runtime without a plan or layout of its own (the
// paired BenchTable.h) has the section on neither leg, and is compared
// on its runtime alone. A section present on only one leg is a
// disagreement, reported as an error exactly as a missing array was
// when every compared header carried one.
func compareArraySection(cSrc, cppSrc, section, cName, cppName string) ([]string, error) {
	cArr, cErr := extractArray(cSrc, cName, cppName)
	cppArr, cppErr := extractArray(cppSrc, cName, cppName)
	switch {
	case cErr != nil && cppErr != nil:
		return nil, nil
	case cErr != nil:
		return nil, fmt.Errorf("c %s: %w", section, cErr)
	case cppErr != nil:
		return nil, fmt.Errorf("cpp %s: %w", section, cppErr)
	}
	return diffLines(section, canonicalize(cArr), canonicalize(cppArr)), nil
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
