# THE FUZZER'S FINDINGS, AS REGRESSION INPUTS

Every file here is an input `test/tables/fixedform_fuzz.cpp` aborted on,
minimized with `-minimize_crash=1`, and committed so the finite gate replays it
forever: `make tables-fixedform-fuzz` copies this directory into the seed corpus
and runs `-runs=0` over it, so a fix that regresses is red in a second and not in
an hour.

A file stays here after its finding is fixed. That is the point of it.

## `i4-fx2-refusal-moved-a-counter.bin` — OPEN, and a CONTRACT QUESTION

389 bytes, a form-3 file carrying FX1's layout, read by **FX2's** reader — the
plan path. Storage is ONE record, so this is not the multi-record case the
harness already set aside. The reader **refuses**, and the report has already
**moved a counter**.

`test/tables/fixedform_main.cpp`'s `refuses()` states the opposite in as many
words — "NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read a
record would be the damage the refusal exists to prevent" — and asserts
`unknown == 0 && kind_mismatch == 0 && widened == 0 && clamped == 0`. But every
file it states that over is a file broken in its **LAYOUT**, where the refusal
happens before a single body byte is touched. This input refuses from somewhere
inside the **body walk**, after a field was already widened or skipped.

So the question is Glenn's and not a port's: **does a refusal from inside the
body promise zero counters too, or does the refusal verdict ride on top of the
counters the walk had already moved?** One of the two is a reader fix and the
other is a sentence in §3.4 and a narrower assertion in `refuses()`. Nothing in
the spec answers it today, which is why this is a finding and not a patch.

## `i4-fx2-layout-refusal-moved-a-counter.bin` — OPEN, and the stronger half

317 bytes, again one record and again FX2's reader, and this time the refusal
carries a **layout-level** reason — the form byte, the layout's own validation or
the hash lineage. Those refusals are decided before any body byte is read, which
is precisely the case `refuses()` asserts `unknown == 0 && kind_mismatch == 0 &&
widened == 0 && clamped == 0` over. A counter is set anyway.

The two files are probably one cause: a counter moved somewhere the refusal path
does not clear, or a report handed to a second decision without being reset. The
reader's own gate cannot see it because every file that gate hands to `refuses()`
is broken in its layout and read by a reader whose walk never starts.

### Why the fuzzer counts these instead of aborting

`test/tables/fixedform_fuzz.cpp` tallies I4 and names it in the exit line. A
fuzz target that aborts on a finding nobody has ruled on yet stops searching for
the next one, and the count being non-zero is what keeps the finding visible.
When the ruling lands and a reader fix takes the count to zero, I4 goes back to
being an abort — the same way `fixedform_properties.cpp` deletes a `known_red[]`
entry as part of landing a fix.
