//go:build schema_leg_luau

// The Luau target carries the TIMESTAMP scalar kind (docs/SPEC-TABLES.md
// §2.11): a 64-bit signed integer count of nanoseconds since the Unix epoch,
// stored as a Luau integer (0i default), written as kind 36 (L = 1, 8 bytes
// two's-complement little-endian) on the variable form and as an 8-byte
// signed slot (layout tag 36) on the fixed and block forms. Registered here in
// the target's own build-tagged file so every other non-carrying target keeps
// refusing it by name.
package compiler

func init() {
	registerCarried("luau", carryTimestamp)
}
