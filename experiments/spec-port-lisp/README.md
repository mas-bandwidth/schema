# spec-port-lisp: a spec-only port in Common Lisp (SBCL)

A tracer bullet. Can a new-language port of the tables layer be written from the spec
alone? This port answers that, and nothing more. It is a record and is not meant to be
merged. What it found is in `SPEC-GAPS.md`.

It was written only from `docs/SPEC*.md`, `docs/USAGE.md`, `docs/FIXED-FORM-ALGORITHM.md`,
the driver protocol in `test/conformance/README.md` and the normative data under
`testdata/conformance/`. It was not written from `internal/`, any other language's
driver, runtime or generator, or the porting guides. A sparse checkout enforced this.

## What it covers

- **Schema intake.** It parses `.schema` text directly, since no serialized IR exists
  (SPEC-GAPS.md, gap 2).
- **Variable form (form 1).** Read and write for every kind the corpus uses: scalars,
  ranges, bits, fixed point, 128-bit integers, timestamps, enums, flags, strings, bytes,
  fixed, counted and unbounded arrays, enum-keyed arrays, maps, optionals, unions,
  pointers and blobs (the node table), plus the CRC-32C trailer. Every read either
  returns the whole value or refuses.
- **The JSON text form (§16).** The reader takes the dialect (comments, trailing
  commas, BOM) and the `&node` labels. The writer is the canonical one from §16.14
  W0-W14.

The fixed form (form 3), the message form, cook, retain, block and the hand-editable
text are not covered. Cases whose root is a `fixed table` answer `<case>.absent`.

## Running it

```
./experiments/spec-port-lisp/driver <manifest> list
./experiments/spec-port-lisp/driver <manifest> <surface> <outdir>    # the harness protocol
./experiments/spec-port-lisp/driver testdata/conformance/tables/MANIFEST.txt check [surface...]
sbcl --script experiments/spec-port-lisp/test/unit.lisp
```

Run every command from the repository root. The driver compiles itself into `build/` on
first use. `check` compares the driver's answers against the committed goldens. The
surface name `hostile-text` holds the fixed-root `json-hostile` rows to their verdict
and value text only, since no form-3 wire is in scope.

Surfaces: `wire`, `json-read`, `json-write` (with the `json-write-refusal` rows), and
`json-hostile`.
