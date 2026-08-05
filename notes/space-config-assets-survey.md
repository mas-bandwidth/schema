# Space game flatbuffers config/assets estate — survey synthesis (design input for "schema")

Synthesized 2026-08-05 from five survey reports over /Users/glenn/space: **[GO]** Go pipeline
(cmd/update_schemas, update_config, upload_config), **[CFG]** config data (game/Schemas + game/Config),
**[AST]** assets data (game/Schemas + game/Assets), **[CPP]** C++ managers (game/Source), **[DEP]**
deployment/runtime-update path. Governing scope rule from the owner: capture the SUBSET of flatbuffers
actually used — minimal representation of the true thing, never a 1:1 flatbuffers port.
Citations are `report → file:line`. Where reports disagree or were unsure, it is said so inline.

---

## 1. The estate at a glance

| Piece | What/where | Size | Job |
|---|---|---|---|
| .fbs schemas (all) | game/Schemas/*.fbs | 19 files [GO update_schemas.go:88-90] | Whole protocol: config, assets, events, server, user_settings, definitions |
| — config schemas | config.fbs + 8 kind files + definitions.fbs | config.fbs 36 lines; kind files 10-49 lines; definitions.fbs 223 lines [CFG §4] | 8 config tables (71 fields total), shared enums/tables |
| — asset schemas | assets.fbs + 5 kind files (+ definitions.fbs) | 5 asset tables + ~10 shared tables/structs + 1 union + ~16 enums [AST §5] | Colliders, hardpoints, level capacities |
| Config JSON | game/Config/<Kind>/*.json + Global.json | 27 files, 4,356 bytes total [CFG §4] | Source of truth for tunables |
| Asset JSON | game/Assets/<Kind>/*.json, Levels/<N>/<N>.json | 17 files, < 8 KB total [AST §5] | Source of truth for spatial/structural data |
| json2flat | game/Tools/json2flat/json2flat.cpp | 81 lines [GO §2] ([AST] says "~80") | flatbuffers::Parser: schema + JSON → .flat |
| Go pipeline | cmd/update_schemas (132 ln), cmd/update_config (928 ln), cmd/upload_config (65 ln [GO]; [DEP] says 64) | ~1,125 lines | codegen + hash stamp; JSON→bins collation; authenticated upload |
| C++ managers | Source/ConfigManager.h (546 ln), Source/AssetManager.h (305 ln), header-only [CPP §1] | 851 lines | load/verify/index/hot-swap; 20 getters |
| Built blobs | game/Config.bin, game/Assets.bin | 2,112 B / 3,696 B [AST §4] | Runtime payloads (caps: 250 KB / 1 MiB, Constants.h:176,180) |
| Deployment | backend.go (/admin_upload_config, /server_update, /server_config), BackendManager.h, Messages.h | 3 config routes of 15 [DEP §4] | Upload → backend memory → server poll → client push |

The whole data corpus is ~12 KB of JSON producing ~6 KB of binary. The machinery around it is
~2,000 lines of Go/C++ plus flatc-generated code in three languages.

## 2. The used flatbuffers subset

This list scopes Config.schema/Assets.schema. Union of [CFG §2] and [AST §2], with side noted.

**USED — must be expressible (or deliberately replaced):**
- Tables, `root_type`, `include`, single `namespace generated` (both sides).
- Scalar fields `float`, `int`, `bool` with **non-zero defaults, including `bool = true`**
  [CFG §2]. Defaults are load-bearing: Beam.json and Flak.json are `{ "type": "Beam" }`-style
  pure-default instances [CFG §1 LaserConfig].
- **Int enums as field types** (7 semantic type enums + Team), implicit sequential values,
  `None = 0` first, JSON writes them by name string (`"type": "Fighter"`) [CFG §2].
- Nested table fields, max 2 deep: ShipConfig/TurretConfig → GunnerSettings → `[FiringGroup]`
  [CFG §1, definitions.fbs:141-153].
- Vectors: of tables (`[ArmorConfig]`, `[FiringGroup]`, `[Collider]`, hardpoint lists, the 13
  `[XxxBuffer]` wrappers), of structs (`[Vec3]` — schema-declared in MeshCollider only), of
  scalars (`[int]` hardpoints, `[float]` damage_multiplier, `[ubyte]`) [CFG §2, AST §2].
- **Structs**: Vec3/Quat of double (definitions.fbs:62-73) — asset side only [AST §1].
- **One union**: ColliderUnion = Box|Sphere|Capsule|Hull|Mesh (definitions.fbs:207-213), JSON
  spelled as `"collider_type"` + `"collider"` pair — asset side only; every on-disk collider is
  Box or Sphere [AST §2].
- **`string` with `(required)`**: hardpoint `name`, LevelAsset `name` — asset side only; **zero
  string fields in config tables** [CFG §2, AST §1].
- `(nested_flatbuffer)` on `buffer:[ubyte]` (config.fbs:14-21, assets.fbs:11-25) — pure packaging
  so independently compiled .flat files can be concatenated; read via `buffer_nested_root()`
  [CFG §2, CPP §6].
- **Enum-as-constant hack**: 13 single-value enums (`enum MaxPlayers : int { Default = 1000 }`,
  definitions.fbs:5-55) because flatbuffers has no constants; LevelAsset fields typed as these
  enums with `= Default`, JSON freely overrides with non-member ints (`"max_players": 100`)
  [CFG §2, AST §1,§5].
- flatc's **relaxed JSON**: trailing commas throughout (Fighter.json:44, Global.json:8,
  Sphere.json), omitted fields → defaults [CFG §2, AST §2].
- Load-time: `flatbuffers::Verifier` + generated `Verify*Buffer` [CPP §1]; zero-copy in-place
  field access — **zero uses of the object API** (`grep UnPack|Pack(` empty outside generated
  headers) [CPP §6].

**NOT USED — explicitly out of scope:**
- `file_identifier`; `key`/sorted vectors; `deprecated`/`id` attributes; optional scalars; fixed
  arrays; rpc; 64-bit offsets [CFG §2, AST §2].
- Forward/backward compatibility machinery generally: lookups are positional with hard asserts
  `type() == i+1`, `size() == ENUM_MAX` — "the true thing is fixed, enum-indexed arrays" [DEP §3].
- Unions, structs, strings on the config side [CFG §2].
- `(bit_flags)` exists (ShipFlags, definitions.fbs:129) but is netcode runtime state, not
  config/asset data [AST §2].
- Non-zero enum defaults on the config side [CFG §2] (the asset-side `= Default` constant hack
  above is the one enum-default use).
- Binary payloads in practice: no mesh paths, textures, or geometry anywhere; MeshCollider/
  HullCollider vertex data is schema-declared but unused on disk, and MeshCollider is
  `fatal("mesh collider is not supported yet")` (PhysicsManager.h:311) [AST §2,§4].

## 3. The pipeline, end to end

Every step below is a chore the schema compiler absorbs (or a transport that survives unchanged).

**Schema change path (today: `update_schemas`):**
1. Clean generated outputs across three trees (update_schemas.go:53-67, per-OS duplicated) [GO §1].
2. `flatc --cpp/--csharp/--go *.fbs`, three passes; per-platform binary shim (PATH flatc on unix,
   vendored `bin\flatc_<platform>\flatc.exe` on Windows, GOOS/GOARCH id) [GO update_schemas.go:16-28,88-90].
3. Copy-out to consumers: *.cs → unity/Assets/CodeGen/Gamedata, *.go → modules/generated
   (update_schemas.go:124-125); tools/update_config/generated is mkdir'd but never populated —
   vestigial [GO §1].
4. schema_hash.h: SHA-256 over all 19 .fbs concatenated **in unsorted Readdir order**
   (update_schemas.go:30-49; modules/common/files.go:14-35 — common.FindFiles is
   dir.Readdir(-1) with no sort, re-verified at gather 2026-08-05), emitted as a C
   byte-array header [GO §1].
   Downstream: `GetProtocolId() = fnv64a(schema_hash ‖ PROTOCOL_VERSION)` (Protocol.h:7-20) gates
   netcode connect via tokens (backend.go:281) [DEP §2].
5. `git add` of all generated artifacts (update_schemas.go:110-129) — build tool mutates git index [GO §1].

**Data change path (today: `run update-config` → `update_config`, run.go:191-193 [DEP §1]):**
6. Author edits JSON under game/Config/** or game/Assets/**.
7. Per file: spawn `./dist/json2flat schema.fbs in.json out.flat` (update_config.go:49) — 13
   groups: 8 config (global, teams, lasers, missiles, explosions, turrets, ships, props) + 5
   asset (levels, missiles, turrets, ships, props) [DEP §1]. .flat intermediates strewn beside
   sources [GO §4].
8. Validation in Go: file count == enum count − 1 (update_config.go:66-68), `Type()==0` fatal
   ("missing type property", :88-90), every enum value 1..N−1 present (:98-103), config↔asset
   pairing by shared base filename with missing-asset fatal (:503-505) [GO §2, AST §3].
9. Slotting: enum-indexed slices, drop sentinel slot 0, `reverse()` to compensate the
   prepend-only builder (update_config.go:29-37; 11 reverse() calls per [DEP §4]) [GO §2].
10. Wrap each .flat as `XBuffer{buffer:[ubyte]}` (13 hand-written wrapper tables), assemble roots:
    Config.bin (global + 7 vectors, update_config.go:615-795) and Assets.bin (5 vectors,
    :800-927) — ~400 lines of hand-rolled builder calls [GO §2].
11. FNV-1a-64 hash of each bin, printed only (:788-792, :922-926) [GO §2].
12. Levels are the exception: `Levels/<Name>/<Name>.json` convention (update_config.go:127-129),
    parsed root discarded — **no validation** (:140), ordering = unsorted Readdir [GO §2].

**Deploy path (config only):**
13. `run upload-config` → upload_config.go: read Config.bin, base64, FNV-1a-64 hash, PUT
    `{config_hash, config_data}` to `/admin_upload_config`, Bearer key from env or
    ~/space-secrets/admin-api-key.txt read raw (trailing-newline hazard) [GO §3].
14. Backend stores hash+blob in process globals under mutex — **not persisted, not
    flatbuffer-verified, client-supplied hash trusted** (backend.go:1109-1141); restart reverts
    to Config.bin baked into the artifact (build_artifact.go:41-46) [DEP §1].
15. Server polls `/server_update` ~1s with its config_hash (BackendManager.h:342-381); on
    mismatch (:477-483) PUTs `/server_config`, base64-decodes, main thread runs ValidateConfig
    (Verifier + 8 presence checks, ConfigManager.h:239-337) then UpdateConfig: memcpy swap,
    `m_configSequence++`, re-root, re-index 7 pointer tables + derived armor table
    (ConfigManager.h:339-524) [DEP §1, CPP §3].
16. Server broadcasts UpdateConfigMessage (full ~blob, raw bytes, Messages.h:129-149) to all
    players except on first load; every joiner gets it in PlayerJoined (ConfigManager.h:152-155,
    354-371). Delivery via reliable chunk stream, ~4s for a full 250 KB config [DEP §1].
17. Client applies **unconditionally** — no ValidateConfig, no hash recompute
    (ConfigManager.h:164-171; Client.cpp:899-905) [CPP §3, DEP §1].
18. Unity FFI: `client_config(size, hash, sequence)` export; uint16 sequence lets C# detect a
    hot swap and re-read pointers (Client.cpp:1743-1762) [DEP §1, CPP §2].
19. **Assets.bin has no upload/hot-reload path**: [GO §5] found no upload path and called
    distribution unclear; [DEP §2] resolves it — assets_hash is reported by client (client_init)
    and server (server_update, "reloading this requires restarting the server", api.go:85) but
    the backend only stores/echoes it; observability only. The client can serve its own copy back
    (Client.cpp:1728-1729) [AST §3].

**Missing today, explicitly wanted:** post-build buffer verification — update_config.go:794
comment verbatim: "IMPORTANT: Golang has no way to validate the flatbuffer. It would be super
smart if we write a C++ tool to check over the flatbuffer before we upload it..." [GO §2].

## 4. The derived-enum evidence

Today's reality, verified exhaustively by [CFG §3] and [AST §3]:
- Exactly **one JSON per non-None enum variant, filename == variant name**, across all 7 kinds:
  Ships 5/5, Lasers 3/3, Missiles 3/3, Explosions 5/5, Props 6/6, Teams 2/2, Turrets 2/2 —
  26 instance JSONs = 26 non-None variants [CFG §3].
- Each variant is stated **three times**: enum declaration (definitions.fbs), filename, and the
  JSON `type` field — with two enforcement layers whose only job is checking the three agree:
  Go fatals (count, `type!=0`, completeness — update_config.go:66-103) and C++ asserts
  (`ships->size() == ShipType_MAX`, `type() == i+1` — ConfigManager.h:421-431,
  AssetManager.h:236-244) [CFG §3, AST §3].
- Asset JSONs carry **no type field at all** — identity is purely filename ↔ same-basename config
  ↔ enum slot (update_config.go:501) [AST §3]. "Rename a file, get a different ship" [AST §5].
- Churn evidence: stale `Props/Moon.flat` and `Turrets/Turbolaser.flat` exist with no .json and
  no enum variant (deleted variants leave orphaned artifacts); `Props/DysonPanel.json` has no
  committed .flat [CFG §3, AST §5].

**What deriving the enum from instances means concretely:** the compiler owning both sides makes
`ShipType` = the listing of the Ships instance set. This deletes: the hand-maintained enum in
definitions.fbs, the `type:`/`team:` field in every config JSON, the Go count/coverage/pairing
fatals, and the C++ re-assertion pass — correct by construction [CFG §5.1]. It also collapses
consumers: the enum is today consumed by generated C++ AND generated Go
(`generated.EnumValuesShipType`, update_config.go:12,468) [AST §3].

**The catch, flagged by both [CFG §5.1] and implied by [DEP §3]:** enum values ride the wire
(config blobs are positionally indexed; type ids appear in netcode state). Derived enums need a
defined, stable ordering — directory sort is not stable under rename/insert, and today's level
ordering is already filesystem-Readdir-dependent [GO §5]. Ordering/stability must come from
somewhere explicit.

## 5. Config.schema / Assets.schema — the design surface

Requirements (what must be expressible), not syntax.

**Config.schema must express:**
- R1. Named record types with typed fields: the vocabulary is exactly **float, int, bool, enum,
  nested record, and arrays of {record, int, float}** — nothing else on the config side [CFG §5.6].
- R2. **Per-field defaults as a first-class feature**, including true-by-default bools
  (`allow_laser_dumbfire:bool = true`, definitions.fbs) — the single most load-bearing feature:
  Beam/Flak are 100% default, and roughly half of the 71 config fields are never set by any JSON
  [CFG §2,§4,§5.5]. Design should stop instances restating defaults verbatim (Fighter
  `health:100`, PewPew `penetration:1` equal their defaults today) [CFG §5.5].
- R3. Nested records to 2 levels with record arrays inside (GunnerSettings → [FiringGroup] →
  [int] hardpoints) [CFG §1].
- R4. A real 2-D matrix type or equivalent: ArmorConfig is 6×6 floats expressed today as
  vector-of-single-field-tables (Global.json; definitions.fbs:220-222) [CFG §5.6].
- R5. **Real named constants**, replacing the 13 single-value-enum hacks (MaxPlayers etc.,
  definitions.fbs:5-55); note MaxBounds/MaxCollidersPerShip/MaxArmor are referenced by no table
  field — pure code constants [CFG §5.4, AST §5].
- R6. **Instance collation**: one instance file per type, collated into one blob in enum order,
  replacing the .flat + XBuffer + reverse() machinery entirely [CFG §5.2, GO §4].
- R7. Derived type enums per §4, with an explicit ordering/stability mechanism.
- R8. Blob identity (content hash) consistent across Go and C++ (today FNV-1a-64, misc.go:81-85
  ≡ core_hash.h:38-44) and a schema-version stamp replacing schema_hash.h/protocol-id [DEP §4].
- R9. Generated validation usable at all three trust boundaries (backend ingest, server apply,
  client apply) — today only the server validates [DEP §4].
- R10. Size-budget enforcement at compile time (blob ≤ 250 KB < 256 KB network message cap)
  [DEP §4].
- R11. Relaxed source ergonomics the owner already relies on (trailing commas, omitted fields)
  [CFG §5.7].

**Assets.schema must additionally express:**
- R12. Small fixed structs: Vec3/Quat of double [AST §1].
- R13. A collider variant type — today a 5-arm union of which only **Box and Sphere** carry any
  on-disk data; Hull is implemented in code, Capsule declared-only, Mesh fatals [AST §2].
- R14. Required string fields (hardpoint `name`, level `name`) [AST §1].
- R15. Name-keyed lookup for levels (strcasecmp scan, AssetManager.h:48-74) vs enum-keyed for
  everything else; levels also live one-subdir-per-level — "presumably to grow per-level payloads
  later" [AST §1,§5].
- R16. **No binary-blob story is needed today** — Assets.bin is 3,696 bytes total; the only
  schema capacity for bulk data (hull points, mesh vertices) is unused [AST §4].
- R17. Loading semantics the generated code must honor: fixed static storage, zero-parse/in-place
  or one-shot unpack both acceptable — "nothing depends on flatbuffers' lazy/offset
  representation itself"; the only representation-sensitive consumers treat the buffer as opaque
  bytes (network resend, hash identity, DLL exports) [CPP §6]. Hot-swap semantics: staging
  buffer, validate-before-commit, re-index, sequence bump, broadcast hook [CPP §5].

**Open design questions, sharpest first:**
1. **Enum value stability for derived enums** — values ride the wire and index blobs; directory
   sort vs explicit ordering manifest vs append-only log? (raised by [CFG §5.1]; sharpened by
   the Readdir nondeterminism already biting schema_hash and level order [GO §5]).
2. **Does config compatibility stay coupled to the whole protocol?** schema_hash covers all 19
   .fbs, so an events.fbs edit invalidates config compatibility today [GO §5]. Should the new
   config/asset schema hash be independent of the netcode protocol id?
3. **Keep or kill the 0 sentinel?** Enum value 0 is burned in every type enum as
   "field absent", and flatbuffers default-elision makes explicit 0 indistinguishable from
   missing [GO §5]. Derived enums with no type field make the sentinel unnecessary — but 0 vs
   1-based indexing leaks into wire state and the NULL-slot-0 pointer tables [CPP §2].
4. **Collider variant scope**: minimal (Box|Sphere) per the used-subset rule, or keep
   Hull/Capsule/Mesh headroom given MeshCollider's fatal() reads intended-but-deferred? Where do
   future point clouds/triangle soups live — this data layer or external? ([AST §4] explicitly
   left unclear.)
5. **Default-only fields — tuned-and-settled or vestigial?** `thrust_rate_up/down`, `roll_power`,
   `roll_sensitivity`, `MissileConfig.penetration` are read via defaults but overridden by no
   instance [CFG §5.8]; hardpoint `rotation:Quat` and turret yaw/pitch_range likewise unset in
   every asset [AST §5]. Owner call needed per field; `TeamConfig.something` is confirmed unread
   (drop) [CFG §1].
6. **Zero-copy views vs unpack-to-native at load** — both viable per [CPP §6]; unpack would also
   subsume the hand-rolled LevelInfo copy-with-clamping (LevelInfo.h:44-95) and the derived armor
   float table (ConfigManager.h:385-416).
7. **Levels**: regularize into the enum-keyed pattern, or bless name-keyed as a second collation
   mode? (Also fixes their unvalidated, order-nondeterministic packing [GO §2,§5].)
8. **Does the compiler keep the side-effects**: git add of generated artifacts [GO §4], and the
   printed-only bin hashes?

## 6. Hazards and surprises

Migration-relevant fragility, each with source:
- **schema_hash is order-nondeterministic**: unsorted Readdir → identical .fbs bytes can yield
  different protocol ids across filesystems/OSes; two machines can disagree on protocol version
  with identical sources [GO §5]. Level ordering in Assets.bin is Readdir-dependent the same way.
- **Three trust boundaries, one validator**: backend stores uploads unverified and trusts the
  client-supplied hash (backend.go:1136-1139); the game client applies pushed config with no
  validation or hash recompute (ConfigManager.h:164-171); only the server validates [DEP §1,§4].
- **Bad-blob livelock**: on server-side validation failure, `m_backendConfigBytes` is only
  cleared on success, so a bad blob re-applies and re-warns every frame [DEP §1].
- **Backend config is not persisted**: restart reverts to the Config.bin baked into the artifact
  — a hot-uploaded config silently disappears [DEP §1].
- **Windows flatc errors silently ignored**: `cmd.Run()` return unchecked
  (update_schemas.go:74,79,84); unix path exits on failure [GO §5].
- **Duplicate type ids across two JSONs silently overwrite**; the eventual "missing X" fatal
  blames the wrong name [GO §5].
- **Dangling-pointer hazard on hot swap**: every cached config pointer is invalidated by the
  UpdateConfig memcpy; code copes by re-fetching per use — except ExplosionManager.h:475, whose
  retention window [CPP §4] did not trace (unclear). Whether DLL exports are called from another
  thread is also unclear [CPP §4].
- **Zero-parse is load-bearing for the in-place design**: managers are memset-zeroed static
  objects (~2 MB) holding the raw buffer; all typed pointers point into it
  [CPP §1, AST §5 AssetManager.h:19].
- **Stale artifacts accumulate**: orphaned Moon.flat/Turbolaser.flat, missing DysonPanel.flat —
  the *.json scan ignores strays, so nothing flags them [CFG §3, AST §5].
- **Two unrelated hash schemes** (SHA-256 for schemas, FNV-1a-64 for blobs) [GO §5]; FNV impls
  duplicated in Go and C++ and must stay identical [DEP §2].
- **Aspirational client hash reporting**: `ClientUpdateRequest.ConfigHash` exists in the API but
  Unity never sends it (AuthenticationManager.cs:235) [DEP §1].
- **Dead surface**: no GetTurretConfig getter exists though m_turretConfig is indexed (only
  would-be caller commented out, Turret.h:212-222); GetTeamConfig has zero call sites [CPP §2].
- **Admin key file read raw** — a trailing newline lands in the Authorization header [GO §3].
- **No config version numbers anywhere**: compatibility is all-or-nothing via protocol id;
  backend holds exactly one blob, last-write-wins; full ~250 KB reship on every join and reload
  (~4s over the chunk stream) [DEP §3].
