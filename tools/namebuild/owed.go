package namebuild

import (
	"fmt"
	"os"
	"path/filepath"
)

// RetiredOwedFile names the ledger (testdata/name-buildability/owed.txt) that
// once counted a failing (target, row) job as owed instead of failed. It is
// retired under the no-skips ruling (docs/rulings.json): every builds row
// builds in every target or the nightly is red on that job. RefuseRetiredOwed
// fails while a file by this name sits in the corpus, so the list cannot come
// back without someone deleting the check first.
const RetiredOwedFile = "owed.txt"

// RefuseRetiredOwed fails when dir holds the retired ledger.
func RefuseRetiredOwed(dir string) error {
	path := filepath.Join(dir, RetiredOwedFile)
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("%s is present: the owed ledger is retired (docs/rulings.json, no-skips); every job runs and a failure is red, so delete the file", path)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return nil
}
