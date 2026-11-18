//go:build !schema_leg_python

package main

// pythonTagged is whether this build carries the schema_leg_python tag; its
// tagged twin is implementation_python.go, which also admits the language.
const pythonTagged = false
