# Atlas schema vs. mas-bandwidth/schema — evaluation and direction

An evaluation of Atlas's existing schema machinery against the mas-bandwidth
`schema` spec (draft 4, 2026-08-05), read together with
`~/atlas_schema_vision.md`, and a take on how Loom (`lib/compiler`) fits.
Working document, not `docs/` material.

## 1. What Atlas has today

### 1a. `core/data/schema` — the working reflection substrate

`lib/core/include/core/data/schema.h`: runtime type descriptions
(`SchemaStruct` / `SchemaField`) with member offsets and sizes, inheritance
chains, enum vocabularies, repeatable `Array<T>` fields with type-erased
append/clear, deferred shared defaults, `Ptr` conversion hooks, asset
references. Registration is code-form (`SchemaBuilder<T>`), and the substrate
already drives multiple encodings and consumers:

- **ADN text, both directions** — schema-driven parse and canonical format,
  with the node-shape mapping (positional arg vs. property vs. child) carried
  per field, local `:key` reference forms, emit-only-when-differs-from-defaults.
- **A schema-interpreted binary codec** — `asset/build/params_codec`:
  canonical bytes, bounds-checked decode through the same schema, plus a
  channel-filtered projection encoding (content-only, for node keys).
- **The asset layer** (`asset/data`) — `AssetType` with lifecycle fns, the
  `AssetValue` generic value model, per-field UI hints and Config/Content/Both
  channels; the asset db, server, wire protocol, layered composition, and
  rename tooling all operate schema-driven.
- **The generic inspector** — `studio/asset` renders and edits registered
  types from schema + hints, no per-type code.
- **Open subtype registration in practice** — shading models and every asset
  kind register their own schema'd types with systems that have never seen
  them.
- **A second consumer family** — `lib/xaml`: type registry, binding,
  templates over the same core schema.
- **Derive primitives** — `schemaFieldEquals`, `schemaLayoutHash`.

Against the vision's litmus tests: the generic editor exists for asset types;
perspectives exist as the channel pair; code-form schemas are the native form;
derives exist in embryo. The missing legs: **schemas as data** (a schema can't
yet describe itself, travel with data, or load at runtime), **codegen** (both
directions — the substrate is interpret-only today), a general perspective
mechanism, semantic hints, computed/validated behavior, table↔tree.

### 1b. `net/sync` — bespoke by choice, ready for re-evaluation

The replication schema (`SyncKindDesc`) binds each kind to hand-written
serialize/deserialize/interpolate functions plus replication policy (priority,
despawn/TTL, coherence, smoothing prefix). The wire codec is fixed-width
little-endian with sticky-failure bounds checks and step-quantized floats;
each game hand-writes ~200 lines of codecs.

This bypassed `core/data/schema` deliberately: net was immature, and bespoke
code is easier to evolve while the shape settles. That call was right — and
with two shipped consumers to look at, now is the time to re-evaluate. What
the examples show the hand-written form costs, in rough order of real pain:

- **Boilerplate that is per-field metadata pretending to be code** — the
  serialize/deserialize/interpolate triples are mechanical projections of
  per-field facts (quantization step, lerp vs. snap, smoothed prefix).
- **Symmetric-pair drift** — serialize/deserialize must mirror field-for-field
  with matching step constants; nothing checks it.
- **Layout-by-comment** — `correctionFloats` requires smoothed floats to lead
  the struct, enforced by prose.
- Minor, at current scale: the hand-bumped protocol id (fine at current
  sizes; `schemaLayoutHash` is the natural derived answer when wanted — it
  moves on layout/vocabulary change, not on whitespace), and
  `maxEncodedBytes`, which only sizes shadow memory anyway (and pointer-stable
  growth via `VirtualArena` could remove even that need).

The practicality constraint, stated plainly: **net/sync's per-entity encode is
a hot per-tick path; declaring kinds over the schema substrate is not
practical with an interpreted walk — codegen is the enabler.** Until Atlas has
schema→code generation, bespoke codecs remain the right answer here.

## 2. The mas-bandwidth spec, read against Atlas

### What it is

A small Go-compiled DSL generating straight-line bitpacked read/write
functions for C++/C#/Go/Rust over the serialize runtime family, with uniform
generated read validation, a hashed protocol id, and no wire versioning. Its
recorded horizon: object definitions with view markers generating
deep/shallow/interpolate structs and quantize pairs, delta encoding,
config/asset tables — "everything in schema."

### The honest accounting

Most of what the spec presents as its contribution is either already the
Atlas core-schema model or serves needs Atlas doesn't have:

- *One declaration, many artifacts* and *"the minimal representation of the
  true thing, boilerplate generated"* — already the Atlas model; the spec's
  horizon is converging on it from below.
- *Set extraction* (declare messages, derive the discriminant enum and
  dispatch) — just codegen, nothing structural.
- *Cross-language byte identity, uniform multi-reader validation, the
  conformance matrix* — machinery for four independent runtimes agreeing.
  Atlas has one language and one reader; this buys nothing here. (The
  "golden bytes" discipline is test-pinning each construct's generated
  encoding so a generator change can't silently move the wire — the shaderc
  version-gate idea; relevant only once codegen exists, and even then Atlas
  already knows how to run that gate.)
- *Bitpacking* — not a divergence from Atlas's scheme at all: bitpacked
  canonical bytes are just as `memcmp`-friendly and fit the existing
  wire-form-shadow model. It's an unoptimized dimension of the current codec,
  available whenever the bytes matter.
- *The hashed protocol id* — the one discipline worth noting, and Atlas's
  answer is better than the spec's: file hashing moves the id on a whitespace
  edit; `schemaLayoutHash` moves it on layout/vocabulary change. The manual
  id is of little concern at current sizes anyway.

What the spec is actually useful for, when the codegen pass comes:

- **A worked catalog of wire-encoding constructs** for quantized game state —
  ranged ints (minimal bits, reject out-of-range), compressed floats
  (min/max/resolution), bounded arrays/strings, const/reserved/align framing,
  branch-on-prior-field — with the read-validation rules each needs. This is
  reference material for Atlas's per-field wire-attribute vocabulary.
- **Its horizon as convergent validation**: the object-view design
  (`[interpolated]`/`[local]` markers generating sim/wire/interpolate structs
  and quantize pairs) independently arrives at exactly the artifacts
  net/sync's examples hand-write. Two codebases deriving the same shape is
  decent evidence the declaration-plus-codegen form is right.
- A field report on grammar-first sequencing: four protocol-id designs in two
  days, retrofitted horizons — the cost of designing the language before the
  model.

## 3. How Loom fits

`lib/compiler` is why the codegen leg — the actual missing piece — is an
increment rather than a project:

- **The compiler stack exists.** `compiler_base` diagnostics and interned
  types; `compiler_lang` with nominal structs, branded-`u32` enums, composable
  constants, attributes, `AssetId(K)`; `scanModuleDeclarations` — parse and
  register a module's types with no function lowering, the exact
  declaration-reader shape a schema tool needs, already shipped for host
  slot-filling.
- **The deployment shape exists.** shaderc already generates C++ record twins
  (`.gpu.h`) from Loom declarations through the build graph with version-gated
  caching. A schema generator is the same tool shape with a second output
  family.
- **The type domain is in scope to grow.** Loom's current types (32-bit
  scalars/vectors + u64) reflect what's been needed, not a design boundary —
  strings, small integer storage, dynamic arrays are all in scope. So the
  vision's lean (ADN for data-form schemas, Loom for code-form and computed
  behavior) is a genuinely open declaration-surface choice, not blocked on
  Loom's domain.
- **The VM is the behavior story.** Computed fields, validation predicates,
  and derived views as Loom functions carried by the schema, baked to
  `validate`d, total-by-construction VM programs — executable by a generic
  editor that could never run game C++. This is what makes the data-driven
  surfaces complete without a constraint mini-syntax.

## 4. Direction

Not the spec, and not its language. The plan is the vision's hub-and-spokes,
recognizing the hub is already built and identifying the missing legs in
dependency order:

1. **Codegen over `core/data/schema`** — the enabling leg. Schema-described
   types gain generated C++: derives first (print/eq/hash/compare — value on
   their own, per the vision), then encodings. Tool shape and gating copy
   shaderc. This is also what makes generated-vs-interpreted a per-consumer
   choice over one model rather than two worlds.
2. **The field-metadata surface grows into the general mechanism** — fold UI
   hints, channels, and the wire refinements the net examples need
   (quantization step/range, lerp-vs-snap, smoothed markers) into the schema's
   per-field metadata; generalize channels into named perspectives with
   per-boundary policy. The spec's encoding table is the reference vocabulary
   for the wire attributes.
3. **net/sync re-evaluated on top of codegen** — kinds declared once; the
   codec pair, interpolate function, ordered decoded struct (smoothed floats
   front-loaded, `correctionFloats` derived), kind-table entry, and a
   layout-hash-derived protocol id generated from the declaration. The two
   shipped games' `net/` modules are the acceptance diff. Bitpacking slots in
   here whenever the bytes are worth it — it fits the canonical-form model
   as-is.
4. **Schemas as data** — schema self-description, the ADN-family data form
   (inline-child notation question and all), schema-with-data travel, runtime
   loading; Loom-carried validation and computed fields ride this step. This
   is what carries litmus tests 1 and 2 across library and process
   boundaries.

The one-sentence version: **Atlas already has the hub — `core/data/schema`
and its asset/inspector/xaml spokes — and the mas-bandwidth spec offers
little beyond a reference catalog for wire attributes; the missing leg is
codegen, Loom's stack and the shaderc pattern make it cheap, and net/sync is
the consumer waiting on it.**
