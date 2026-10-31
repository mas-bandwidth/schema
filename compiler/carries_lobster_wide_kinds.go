package compiler

// The Lobster target carries the wide table kinds (docs/PORT-LOBSTER.md, THE
// CARD MAP; docs/SPEC-TABLES.md §2.11, §4.3): the `timestamp` kind rides the
// descriptor as kind 36 and data-runtime/wide_kinds.lobster reads it, and a
// one-sided integer range reaches the descriptor as the two-sided range the
// checker filled, which the scalar shape clamps against. Registering them
// here takes them off every other non-Go target's refusal only for lobster;
// compiler/carries.go and the refusal files are untouched.
var _ = carryLobster(carryTimestamp)
var _ = carryLobster(carryOneSidedRange)
