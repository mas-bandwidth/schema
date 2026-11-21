package a

import "testing"

func TestA(t *testing.T)    {}
func TestB(t *testing.T)    {}
func FuzzC(f *testing.F)    {}
func TestMain(m *testing.M) {}

// not a test: the name after Test starts in lower case
func Testhelper(t *testing.T) {}

const src = `
func TestInAString(t *testing.T) {}
`
