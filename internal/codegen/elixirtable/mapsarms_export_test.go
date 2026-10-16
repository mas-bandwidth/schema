package elixirtable

import "testing"

// The functional-test helpers, exported to the external test package so a
// test that drives the COMPILER (which imports this package) can run the leg's
// own toolchain the way the in-package tests do (mapsarms_test.go).

// ElixirBinaryForTest is elixirBinary: the pinned Elixir, or a skip that
// SCHEMA_REQUIRE_CORPUS turns into a failure.
func ElixirBinaryForTest(t *testing.T) string { return elixirBinary(t) }

// WriteModuleDirForTest is writeModuleDir: the generated modules in load order.
func WriteModuleDirForTest(t *testing.T, dir string, files map[string][]byte) []string {
	return writeModuleDir(t, dir, files)
}

// RunProbeForTest is runProbe: probe.exs beside the modules, run once.
func RunProbeForTest(t *testing.T, elixirBin, dir string, names []string, probe string) string {
	return runProbe(t, elixirBin, dir, names, probe)
}
