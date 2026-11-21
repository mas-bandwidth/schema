// `run --jobs N` (runOptions.jobs, prefetchDrivers) starts a leg's surface
// drivers side by side and scores them afterwards in the serial order. These
// hold it to the serial run: the same verdict, the same matrix text and the
// same coverage rows, for a green leg and for a leg that fails by a claimed
// absence, and a surface the leg does not list is still never started. Fake
// drivers over the one-instance corpus absence_test.go builds: no language
// leg, no network, no clock.
package main

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// runJobs runs the fake corpus with the claim check on and jobs surface
// drivers at once, and hands back the matrix with the work directory's name
// taken out, the verdict and the coverage rows.
func (c fakeCorpus) runJobs(t *testing.T, drivers string, claims *claimSet, jobs int) (string, bool, []coverageRow) {
	t.Helper()
	var out bytes.Buffer
	var rows []coverageRow
	work := filepath.Join(t.TempDir(), "work")
	ok, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		work, "", "", runOptions{claims: claims, coverage: &rows, jobs: jobs})
	if err != nil {
		t.Fatalf("harness run --jobs %d: %v\n%s", jobs, err, out.String())
	}
	return strings.ReplaceAll(out.String(), work, "WORK"), ok, rows
}

func TestJobsRunIsTheSerialRun(t *testing.T) {
	needShell(t)
	for _, tc := range []struct {
		name   string
		leg    fakeLeg
		wantOK bool
	}{
		{"every surface answers", fakeLeg{}, true},
		{"a claimed case is absent", fakeLeg{caseGone: true}, false},
		{"a surface is not listed", fakeLeg{unlisted: "json-write"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeCorpus(t)
			drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": tc.leg})
			claims := fakeClaims([]string{"timestamp"}, map[string][]string{"zz": {"timestamp"}})
			serialOut, serialOK, serialRows := c.runJobs(t, drivers, claims, 1)
			if serialOK != tc.wantOK {
				t.Fatalf("the serial run's verdict is %v, want %v:\n%s", serialOK, tc.wantOK, serialOut)
			}
			for _, jobs := range []int{2, 8} {
				out, ok, rows := c.runJobs(t, drivers, claims, jobs)
				if ok != serialOK {
					t.Errorf("--jobs %d verdict %v, the serial run's is %v:\n%s", jobs, ok, serialOK, out)
				}
				if out != serialOut {
					t.Errorf("--jobs %d prints a different matrix.\nserial:\n%s\n--jobs %d:\n%s", jobs, serialOut, jobs, out)
				}
				if !reflect.DeepEqual(rows, serialRows) {
					t.Errorf("--jobs %d coverage rows differ.\nserial: %+v\n--jobs:  %+v", jobs, serialRows, rows)
				}
			}
		})
	}
}
