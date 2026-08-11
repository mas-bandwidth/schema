# The table layer — the semantic model, extracted from the ground truth

*2026-08-11. Model before grammar (the banked method rule): this is what the table layer
must EXPRESS, measured from what space game actually uses — the full .fbs corpus (19
files), the JSON→bin pipeline (`update_config` 928 lines + `json2flat` + `update_schemas`),
and every C++ consumption site (`ConfigManager`/`AssetManager`/per-frame readers). Survey
run 2026-08-11, three parallel readers, all findings carry file:line citations in the
session record. NOTHING here is normative — the SPEC is; this is the design input its
grammar gets checked against. Scope rule in force: the subset space game actually uses,
never a flatbuffers equivalent.*

## 1. The shape (already DECIDED, SPEC §1) — confirmed by the survey

The collection triple holds exactly: **one source directory of JSON instances, against one
declared set of types, into one versioned hashed binary** with generated loaders, accessors
and derived enums. Two per-collection properties confirmed live in the code:

- **Reload semantics**: Config hot-swaps atomically (1s backend poll → `UPDATE_CONFIG`
  broadcast → re-index; `api.go:84` "may be hot reloaded"); Assets load once
  (`api.go:85` "reloading this requires restarting the server"). A per-collection
  property driving which generated loader the collection gets — exactly as SPEC'd.
- **Cross-collection references, one direction**: config↔asset pairing is TODAY enforced
  by same-basename filename convention + fatal checks (`update_config.go:274,416,503,590`
  — config without its asset is fatal). The declared directional expectation (config →
  assets DAG) replaces a filename convention with a checked declaration.

## 2. The type-system floor — small, closed, and mostly already in schema

**Everything the corpus uses** (exhaustive; each had a cited example site):

| needs | status in schema today |
|---|---|
| tables: all-fields-optional, per-field defaults | NEW — the defining table property (see §3) |
| defaults: float/int/bool/enum-symbol, int-literal-as-float, explicit =0 | partially new (v1 has field defaults on wire types; table defaults do double duty as absence semantics) |
| `required` (used on 3 hardpoint name strings only) | NEW, small |
| structs: Vec3/Quat/Handle fixed inline | **HAVE** — `type` (space's Vector3/Quaternion are already schema types) |
| enums (incl. non-sorted member order — see §5) | HAVE, with the ORDER question |
| vectors of scalars / structs / tables; strings | bounded arrays + string(N) HAVE; unbounded-at-authoring sizes are a table-layer question |
| sub-table fields | HAVE (nested types) |
| unions: ColliderUnion (5-way), EventUnion (8-way), union-in-vector-of-tables | NEW at type level (v1 cut it; the wire half of the same shape is the message dispatch) |
| single namespace, cross-file refs | HAVE (order-free unit resolution) |

**Everything the corpus does NOT use** — confirmed dead weight, never to build: field ids,
`deprecated`, `key`/sorted-vector lookup, `file_identifier`, size-prefixed roots, optional
scalars, mutation, object API, reflection, shared strings, FlexBuffers, RPC. Also
`nested_flatbuffer` — 13 wrapper tables exist ONLY to satisfy that mechanism (two-step
`Get(i)->buffer_nested_root()` at every access, outer Verifier blind inside); the
replacement's collections make the whole envelope layer disappear.

## 3. The versioning model — the survey's sharpest finding

**The corpus uses NONE of flatbuffers' evolution machinery.** No field ids, no
deprecations, no file_identifier. What it actually relies on: (1) append-only field order
by convention; (2) **defaults for absent fields** — JSON instances carry only overrides;
(3) **whole-corpus exact-match hashing out-of-band** (`schema_hash.h` → protocol id):
drift is DETECTED, never tolerated. And `.bin` files never outlive their schema — they are
rebuilt from JSON on every schema change.

**So "proper versioning like flatbuffers" decomposes into two different obligations at two
different times:**

- **Runtime: exact-match, schema's existing philosophy unchanged.** Two sides at the same
  hash speak; a stale `.bin` is rejected and rebuilt. No evolution machinery in the loaded
  format — the same argument the wire already won.
- **Data-compile time: sparse instances against a moving schema.** The evolution burden
  lands entirely on the JSON→bin compiler: adding a defaulted field to a table must not
  touch a single existing JSON file. Absence semantics (absent field → declared default)
  is the ONE flatbuffers property that is genuinely load-bearing, and it lives at compile
  time, where the compiler can also verify (closing `update_config.go:794`'s admitted
  no-verification hole).

This is simpler than flatbuffers by their own architecture's evidence, not by wishing.

## 4. The runtime access model — recommend DECODE-TO-STRUCTS, from the evidence

Today: zero-copy retained buffer + enum-indexed pointer tables + **per-frame lazy vtable
reads in the hot paths** (`config.speed()` per missile per tick, `RenderManager` per-laser
per-frame) + eager bakes where it hurt (`m_damageMultiplier`) + one-shot copies that
silently miss hot reloads (ship health, collider armor, missile timer — no rule marks
which fields are which).

**Recommendation: the generated loader DECODES the collection into flat arrays of
generated plain structs at load** (schema's existing storage idiom: fixed pre-allocated,
zero-init + defaults). Consequences, each answering a measured pain point:

- Per-frame reads become direct member loads — faster than vtable hops, and the accessor
  shape the game already wraps (`GetShipConfig(type)` returning a plain struct ref).
- Validation happens once at decode; the release-build gap disappears structurally (today:
  type-order/size/nested-null checks are `core_assert`, compiled out in release, with a
  live network-delivered config able to overrun index arrays — plus nested buffers and
  user settings never verified at all, and `EnumNames[type()]` OOB on corrupt data).
- The const-cast wart, the two-step nested dance and the wrapper tables all vanish.
- Hot reload = re-decode + atomic swap of the arrays — which IS the stated config
  semantics ("tweaked atomically, whole config.bin at once").
- The split-reload hazard (copied-at-init values vs live readers) becomes visible and
  eventually declarable — a field property for a later pass, noted not designed.

## 5. Derived enums — direction inverts, ORDER must be declared

The bijection already exists and is enforced backwards: JSON basenames == enum member
names, `update_config` fatals on any mismatch, and every game type carries THREE spellings
(enum member, filename, embedded type field). Deriving the enum from the files removes two
of three. **The trap the survey caught: order.** Array position is index/wire semantics,
and the current sets are NOT alphabetical (`Fighter, Corvette, Bomber, Destroyer,
Carrier`) — a directory listing would renumber them. So a derived set needs a **declared
order** (a manifest in the collection declaration, or ordering data in the instances) —
open design question for the grammar pass, alongside the stale-orphan hygiene the pipeline
never had (`Moon.flat`, `Turbolaser.flat` orphans sitting in the source dirs today).

Hand-owned sets stay hand-declared (`Team`) — the SPEC's existing rule.

## 6. The pipeline the compiler absorbs

`schema`'s data compiler replaces: `json2flat` (runtime flatbuffers parser), the 928-line
`update_config` collation (slot-by-embedded-enum, count fatals, filename pairing), and
`update_schemas`' hash generation — one tool, verification at compile, content hash
computed not trusted (today the backend stores a client-supplied hash unverified), orphan
detection, no git side effects (today `update_schemas` git-adds as a side effect). The
`UPDATE_CONFIG` transport needs nothing: it already ships opaque bytes over a message the
schema unit expresses.

**Transition hashing (flagged, not designed):** `schema_hash.h` covers `*.fbs` only, and
the schema unit has its own protocol id — during the mixed period the game's
`GetProtocolId()` should fold BOTH (today a wire change in `Messages.schema` does not move
the protocol id unless `ProtocolVersion` is bumped by hand — an easy-to-forget seam the
fold closes cheaply, and a near-term integration item independent of the table layer).

## 7. Open questions for Glenn (the grammar pass waits on none of the sections above)

1. **Derived-enum order**: manifest in the collection declaration, or per-instance order
   field? (§5 — the renumbering trap is real.)
2. **JSON dialect**: keep flatbuffers-JSON as-is (existing files untouched) or define
   schema's own (enum symbols, comments)? Existing files suggest keep-compatible first.
3. **Instance size bounds**: tables are authored unbounded; generated storage wants
   bounds. Per-collection capacity in the declaration (the `Max*` constants already exist
   and are already in `Constants.schema`) — confirm that is the intended coupling.
4. **Events**: the wire half is v1-expressible today (the message-dispatch shape);
   migrate events as messages ahead of the table layer, or with it?
