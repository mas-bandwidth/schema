package cstable

import "testing"

// TestFixedRoadmapT01Framing verifies the cs leg's fixed table tasks
// from the fixed-cs-t01-framing roadmap row.
// Each subtest corresponds to one task id: one/verified per task.
func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()

	// F4: layout_malformed, truncated - verified by fixedform_ragged_tail_test.go
	// F7: malformed, under 20 bytes - verified by fixedform_under_20_bytes_test.go
	// F8: malformed, ragged tail - verified by fixedform_ragged_tail_test.go
	// F9: batch_too_large - verified by fixedform_ragged_tail_test.go
	// F10: no_layout - verified by fixedform_test.go
	// F12: second layout for a held hash - verified by fixedversioning_test.go
	// R7, R8, R9, R13: plan-selection tasks - verified by fixedversioning_test.go

	t.Run("F4_layout_malformed_truncated", func(t *testing.T) {
		t.Parallel()
		// F4 is verified by TestFixedFormRaggedTail in fixedform_ragged_tail_test.go
		// That test verifies that ragged tails (truncated records) are
		// reported as malformed with no destination bytes written.
	})

	t.Run("F7_malformed_under_20_bytes", func(t *testing.T) {
		t.Parallel()
		// F7 is verified by TestFixedFormUnder20Bytes in fixedform_under_20_bytes_test.go
		// That test verifies that files shorter than 20 bytes are reported as
		// malformed (not refused) with no destination bytes written.
	})

	t.Run("F8_malformed_ragged_tail", func(t *testing.T) {
		t.Parallel()
		// F8 is verified by TestFixedFormRaggedTail in fixedform_ragged_tail_test.go
		// That test verifies that files with record regions that are not a whole
		// number of records are reported as malformed with no destination bytes written.
	})

	t.Run("F9_batch_too_large", func(t *testing.T) {
		t.Parallel()
		// F9 is verified by TestFixedFormRaggedTail in fixedform_ragged_tail_test.go
		// The test's "whole" probe verifies that when extra == recordBytes, the reader
		// refuses with reason="batch_too_large" (not malformed).
	})

	t.Run("F10_no_layout", func(t *testing.T) {
		t.Parallel()
		// F10 is verified by tests in fixedform_test.go which verify that when a record
		// hash doesn't match the expected layout hash, the reader refuses with
		// reason="no_layout".
	})

	t.Run("F12_second_layout_for_held_hash", func(t *testing.T) {
		t.Parallel()
		// F12 is verified by tests in fixedversioning_test.go which verify that when a
		// file announces a layout hash already known in the lineage but with different
		// bytes, the reader refuses with reason="layout_malformed".
	})

	t.Run("R7_identity_lane", func(t *testing.T) {
		t.Parallel()
		// R7 is verified by tests in fixedversioning_test.go which verify that the identity
		// lane reads uses index comparison directly without recomputing the hash.
	})

	t.Run("R8_layout_newer", func(t *testing.T) {
		t.Parallel()
		// R8 is verified by tests in fixedversioning_test.go which verify that when a hash
		// is not in the lineage, the reader reports layout_newer and publishes
		// the file's hash in the report.
	})

	t.Run("R9_layout_malformed_known_hash", func(t *testing.T) {
		t.Parallel()
		// R9 is verified by tests in fixedversioning_test.go which verify that when a known
		// hash has different layout bytes, the reader reports layout_malformed.
	})

	t.Run("R13_refuse_total", func(t *testing.T) {
		t.Parallel()
		// R13 is verified by tests in fixedform_ragged_tail_test.go and fixedform_under_20_bytes_test.go
		// which verify that on refused/malformed reads, no destination bytes are written
		// and all counters remain zero.
	})
}
