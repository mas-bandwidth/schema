package mootable

import (
	"sync"
	"testing"
)

// SweepControl holds a cell negative control's two requirements with the least
// work: every row goes red under at least one plant, and every plant turns at
// least one row red, so no row's gate and no plant is green by holding
// nothing. red answers whether row r is red under plant p, and its first
// finding; it stops at that first finding, since the clean full corpus is the
// positive test's to run.
//
// A row stops at the first plant that turns it red. A plant no row's sweep
// reached red is then tried row by row and stops at its first red row.
func SweepControl(t *testing.T, rows, plants []string, red func(t *testing.T, r, p int) (bool, string)) {
	t.Helper()
	var mu sync.Mutex
	reddened := make([]bool, len(plants))
	t.Run("rows", func(t *testing.T) {
		for r := range rows {
			t.Run(rows[r], func(t *testing.T) {
				t.Parallel()
				for p := range plants {
					if ok, first := red(t, r, p); ok {
						mu.Lock()
						reddened[p] = true
						mu.Unlock()
						t.Logf("%s: first finding %s", plants[p], first)
						return
					}
				}
				t.Fatalf("the %s row stayed green under every plant", rows[r])
			})
		}
	})
	t.Run("plants", func(t *testing.T) {
		for p := range plants {
			if reddened[p] {
				continue
			}
			t.Run(plants[p], func(t *testing.T) {
				t.Parallel()
				for r := range rows {
					if ok, first := red(t, r, p); ok {
						t.Logf("%s row: first finding %s", rows[r], first)
						return
					}
				}
				t.Errorf("the %s plant turned no row red", plants[p])
			})
		}
	})
}
