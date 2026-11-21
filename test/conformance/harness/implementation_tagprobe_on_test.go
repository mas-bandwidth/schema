//go:build schema_leg_tagprobe

package main

// tagprobe is a made-up leg, no shipped name, standing in for a leg
// `schema new-leg` is still writing: built with its tag, the harness admits
// it, as a new leg's own implementation_<name>.go would.
func init() {
	taggedImplementationLanguages = append(taggedImplementationLanguages, "tagprobe")
}

const tagprobeTagged = true
