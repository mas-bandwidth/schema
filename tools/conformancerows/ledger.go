package main

import (
	"fmt"
	"sort"
	"strings"
)

// verdict holds the rows that ran: every one that failed is red, named.
// There is no ledger of rows a leg owes, so nothing excuses a failure.
func verdict(ran []string, failed map[string]bool) error {
	var red []string
	for _, r := range ran {
		if failed[r] {
			red = append(red, r)
		}
	}
	if len(red) == 0 {
		return nil
	}
	sort.Strings(red)
	return fmt.Errorf("%d rows failed (a row that cannot be answered is a failure): %s", len(red), strings.Join(red, ", "))
}

// shardOf returns the rows of shard i of n: the sorted rows, every n-th from
// i. Every row is in exactly one shard.
func shardOf(rows []string, i, n int) []string {
	sorted := append([]string(nil), rows...)
	sort.Strings(sorted)
	var out []string
	for k, r := range sorted {
		if k%n == i {
			out = append(out, r)
		}
	}
	return out
}
