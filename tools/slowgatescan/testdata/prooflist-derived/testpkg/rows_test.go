package testpkg

import "testing"

// slowtest.Gate( is named here only so the fixture package is treated as gated.
func TestRowOne(t *testing.T) { t.Parallel() }

func TestRowTwo(t *testing.T) { t.Parallel() }
