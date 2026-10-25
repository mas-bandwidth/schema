package baseline

// UpdateAt is update with the history date given, for the external test
// package (foreignunit_test.go), which imports internal/check and so cannot be
// package baseline: check imports baseline for the retired-name ledger.
var UpdateAt = update
