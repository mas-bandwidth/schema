; The serialize.rb roadmap: the data ROADMAP.md is generated from.
; Data, never evaluated. Edit this file, then regenerate ROADMAP.md:
;   make roadmap
; Never edit ROADMAP.md by hand: test/test_docs.rb holds it to what
; test/roadmap.rb writes. Ideas and wanted features go here, never in issues.
; Landed work is in git, so its items leave this file when it lands.
(roadmap "v1"
 :title "serialize.rb roadmap"
 :text "Planned work for serialize.rb. The port keeps STANDARD.md format 2.0 and
        the uniform rules U1 to U16 of IMPLEMENTING.md as vendored from serialize
        v2.1.1: every corpus vector passes with its stated refusal reason and every
        sabotage control fails exactly its list (test/test_conformance.rb). The
        items below are what the design write-up (DESIGN.md) found open. Landed
        work is in git."

 :groups
 ((group "speed"
   :title "Speed"
   :text "Changes to the hot path that keep the wire byte for byte and are
          measured by make bench and make bench-choices.")
  (group "surface"
   :title "Public surface"
   :text "What a caller can reach, held to IMPLEMENTING.md U15."))

 :items
 ((item "writer-words-of-32-bits"
   :group "speed"
   :title "The writer empties its accumulator every 32 bits"
   :text "make bench-choices measures the writer's inner loop emptying its
          accumulator every 32 bits at 98 ns per 32-bit field with YJIT off and
          59 ns with it on, against 133 ns and 89 ns for the whole 64-bit words
          the writer appends today, with the same bytes (the bench compares
          them before it measures). At 5-bit fields the
          gain is smaller (56 to 50 ns off, 34 to 27 ns on). Change
          WriteStream#put and #put_aligned_bytes, then re-measure make bench
          and update the README's table."
   :exists "WriteStream appends whole 64-bit words (lib/serialize/core.rb,
            WriteStream#put); test_every_operation_from_every_start_offset and the
            corpus pin the bytes any change must keep."
   :why "Found by the design write-up; a change to the hot path lands on its own,
         behind the round-trip tests and the corpus."
   :date "2026-11-17")
  (item "hide-internal-names"
   :group "surface"
   :title "Hide the names that are not the public surface"
   :text "IMPLEMENTING.md U15: where the language can hide a name, it is hidden.
          Serialize::Encode (marked @api private), the declaration checks
          Serialize.number?, int_bounds?, fixed_params, precomputed_ok? and
          utf8_text?, and the constants INT32_MIN to UINT128_LIMIT, M64,
          RELATIVE_MAX, LAST_BOUNDED_MAX, TIERS, COMPRESSED_FLOAT_VALUES_MAX,
          FLOAT32_MAX, FLOAT32_OVERFLOW and TEXT_ENCODINGS are reachable today. Make them
          private with private_constant and private_class_method, keeping the
          names DESIGN.md lists as public."
   :exists "DESIGN.md, The public surface, lists which names are public and says
            the rest are internal and promise nothing."
   :origin "IMPLEMENTING.md U15, Exports beyond the reference operations"
   :date "2026-11-17")))
