package baseline

// UpdateAt is Update with the date stamp supplied, so the external test
// package can keep a fixed history date without importing the package's
// unexported update. It lives in a _test.go file and is not part of the
// package's API.
var UpdateAt = update
