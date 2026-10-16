# C# Fixed-Class Table Body Inlining Measurement (`#405`, M4)

- **Host:** `b1` (`Linux 7.0.0-38-generic x86_64`, Ubuntu 26.04)
- **CPU:** AMD Ryzen 7 255 w/ Radeon 780M Graphics (1 socket, 8 cores, 16 threads)
- **Toolchain:** `.NET SDK 10.0.401` (`.NET Runtime 10.0.12`, RyuJIT `linux-x64`), `go1.26.6 linux/amd64`, `gcc/g++ 15.2.0`
- **Window:** `2026-10-16T02:06:26Z` – `2026-10-16T02:18:10Z` (one interleaved sitting `A1, B1, A2, B2`)
- **Load:** `0.34, 2.75, 4.98` before `A1`; `1.08, 1.37, 2.57` after `B2` (quiet bench; zero competing compiler or benchmark jobs; 0 noise warnings in `B1`, `A2`, `B2`)
- **Base (`A1`, `A2`):** `d713c7d6663dfa265b800df0870fb9e621f40f97` (`origin/sprint/schema-cs`; clean tree `0cd0aee6bd5dd86067ec787f0c419eb3f00477f4`, `dirty: false`) — `[MethodImpl(MethodImplOptions.AggressiveInlining)]` on primitive helpers in `internal/codegen/cstable/wire.go:463-659` (`Ids.Reference`, `Ids.Ref`, `Ids.RefAt`, `Writer.Byte`, `Writer.Raw`, `Writer.Fixed`, `Writer.Header`, `Writer.HeaderAt`, `Writer.Var`, `Reader.Has`, `Reader.Byte`, `Reader.Fixed`, `Reader.Var`, `Reader.Ref`) and `internal/codegen/cstable/cstable.go:623-641` (`TableBitsToFloat`, `TableFloatToBits`, `TableBitsToDouble`, `TableDoubleToBits`) only
- **Candidate (`B1`, `B2`):** base plus `[MethodImpl(MethodImplOptions.AggressiveInlining)]` on Fixed-class (`!ir.VariableTables(g.unit)[st.Name]`) table body methods in `internal/codegen/cstable/{wire.go,codecs.go,fixedform.go}` (`<Name>CollectTyped`, `<Name>BodySizeTyped`, `<Name>WriteBodyTyped`, `<Name>SaveTyped`, `<Name>Reset`, and `<Name>FixedWriteBody`) while keeping Variable-class table bodies and all `<Name>Measure` methods unattributed (clean tree `03ca64025665d4f5d315f0e9f1e763f67580cbfd`, `dirty: false`)
- **Runtime Checkouts:** `serialize` `fd258c003b51cb9a0455c74b6a44d5ba643b1f46`, `serialize.c` `4c3d881f86ee4a3f4c1291dcf0af31d67b16cbd9`, `serialize.cs` `8d0201b80583f0b1298b76bfb926a4913736009e` (with `(Int128Value, System.Int128)` `<`/`>` disambiguation overloads for C# 14 `CS9342` in `bench/paired/cs`), `serialize.go` `995b8e9ad7e01119c683d6f4be3f8c7829efd53c`
- **Corpus IDs:**
  - `cmd/schema-paired`: `fd0748942dc6ae09` (`packet`, `438 B/op`), `066b2822b3e5d487` (`table` `BenchFixedTable`, `1264.30 B/op`)
  - `make bench-table`: `b51387f36d9b59c4` (`table` `BenchTable`, `2147 B/op`)

## Verdict (`reverted`)

Adding `[MethodImpl(MethodImplOptions.AggressiveInlining)]` to Fixed-class table body methods is **reverted** because it does not move `BenchFixedTable` `write` (`0.132 / 0.130 µs/op` base vs `0.130 / 0.130 µs/op` candidate) or `round_trip` (`3.493 / 3.504 µs/op` base vs `3.494 / 3.523 µs/op` candidate) outside spread while regressing `BenchTable` (`make bench-table`) `write` by `-10.5%` to `-12.8%` (`219,905 / 226,417 msg/s` base vs `191,662 / 202,579 msg/s` candidate) and `round_trip` by `-5.7%` to `-5.8%` (`101,827 / 102,230 msg/s` base vs `96,022 / 96,266 msg/s` candidate) due to inlining Fixed-class `BenchSmall` bodies into enclosing Variable-class `BenchTable` methods; `[MethodImpl(MethodImplOptions.AggressiveInlining)]` remains on the primitive wire helpers only.

## 1. Paired Fixed-Table Benchmark (`bin/schema-paired -mode fast -langs cpp,c,go,cs`)

Corpus: `066b2822b3e5d487` (`BenchFixedTable`, `1264.30 B/op`, `3` adequate rounds per sitting after warmup; `1,917,568`–`1,930,240` table operations per sample, `2,000,000` packet operations per sample).

### C# Summary Across Interleaved Sittings (`A1, B1, A2, B2`)

| Sitting | Arm | Wire | Path | Median µs/op | Range µs/op | Round 0 msg/s | Round 1 msg/s | Round 2 msg/s | Spread % | Fixed Table % | vs Packet Wire % |
|---|---|---|---|---:|---|---:|---:|---:|---:|---:|---:|
| `A1` | Base (primitives only) | `packet` | `write` | `0.250` | `0.250–0.252` | `3,971,398` | `4,000,729` | `3,997,846` | `0.73%` | — | — |
| `A1` | Base (primitives only) | `packet` | `round_trip` | `0.556` | `0.555–0.604` | `1,801,132` | `1,798,490` | `1,654,921` | `8.13%` | — | — |
| `A1` | Base (primitives only) | `table` | `write` | `0.132` | `0.131–0.136` | `7,337,745` | `7,574,196` | `7,612,254` | `3.62%` | — | — |
| `A1` | Base (primitives only) | `table` | `round_trip` | `3.493` | `3.381–4.022` | `248,658` | `286,273` | `295,787` | `16.46%` | `883.7%` | `628.2%` |
| `B1` | Candidate (+Fixed bodies) | `packet` | `write` | `0.253` | `0.251–0.255` | `3,954,233` | `3,924,681` | `3,988,141` | `1.60%` | — | — |
| `B1` | Candidate (+Fixed bodies) | `packet` | `round_trip` | `0.558` | `0.556–0.561` | `1,800,066` | `1,781,508` | `1,793,553` | `1.04%` | — | — |
| `B1` | Candidate (+Fixed bodies) | `table` | `write` | `0.130` | `0.130–0.131` | `7,658,184` | `7,674,757` | `7,684,815` | `0.35%` | — | — |
| `B1` | Candidate (+Fixed bodies) | `table` | `round_trip` | `3.494` | `3.422–4.122` | `292,255` | `242,583` | `286,214` | `17.35%` | `883.4%` | `626.6%` |
| `A2` | Base (primitives only) | `packet` | `write` | `0.249` | `0.249–0.254` | `3,930,123` | `4,009,621` | `4,017,265` | `2.17%` | — | — |
| `A2` | Base (primitives only) | `packet` | `round_trip` | `0.560` | `0.557–0.563` | `1,794,999` | `1,776,854` | `1,785,490` | `1.02%` | — | — |
| `A2` | Base (primitives only) | `table` | `write` | `0.130` | `0.130–0.130` | `7,711,232` | `7,718,221` | `7,693,774` | `0.32%` | — | — |
| `A2` | Base (primitives only) | `table` | `round_trip` | `3.504` | `3.348–3.584` | `285,366` | `278,995` | `298,690` | `6.90%` | `881.1%` | `625.7%` |
| `B2` | Candidate (+Fixed bodies) | `packet` | `write` | `0.252` | `0.248–0.254` | `3,967,971` | `4,027,943` | `3,944,565` | `2.10%` | — | — |
| `B2` | Candidate (+Fixed bodies) | `packet` | `round_trip` | `0.558` | `0.555–0.560` | `1,800,785` | `1,786,363` | `1,793,172` | `0.80%` | — | — |
| `B2` | Candidate (+Fixed bodies) | `table` | `write` | `0.130` | `0.130–0.131` | `7,699,665` | `7,616,531` | `7,663,394` | `1.08%` | — | — |
| `B2` | Candidate (+Fixed bodies) | `table` | `round_trip` | `3.523` | `3.485–4.201` | `283,831` | `238,053` | `286,909` | `17.21%` | `889.2%` | `631.8%` |

### Full Four-Language Boards (`A1, B1, A2, B2`)

| Language | Wire | Path | `A1` Median µs/op (Range) | `B1` Median µs/op (Range) | `A2` Median µs/op (Range) | `B2` Median µs/op (Range) |
|---|---|---|---|---|---|---|
| C++ | `packet` | `write` | `0.157` (`0.156–0.304`) | `0.157` (`0.156–0.157`) | `0.157` (`0.156–0.157`) | `0.157` (`0.157–0.157`) |
| C++ | `packet` | `round_trip` | `0.322` (`0.322–0.413`) | `0.322` (`0.321–0.322`) | `0.322` (`0.321–0.323`) | `0.321` (`0.321–0.322`) |
| C++ | `table` | `write` | `0.145` (`0.142–0.147`) | `0.143` (`0.142–0.145`) | `0.142` (`0.142–0.142`) | `0.142` (`0.142–0.144`) |
| C++ | `table` | `round_trip` | `0.395` (`0.395–0.450`) | `0.395` (`0.394–0.396`) | `0.398` (`0.396–0.402`) | `0.396` (`0.394–0.397`) |
| C | `packet` | `write` | `0.136` (`0.136–0.170`) | `0.136` (`0.136–0.138`) | `0.137` (`0.137–0.138`) | `0.136` (`0.135–0.136`) |
| C | `packet` | `round_trip` | `0.322` (`0.322–0.365`) | `0.324` (`0.321–0.324`) | `0.321` (`0.321–0.321`) | `0.322` (`0.321–0.331`) |
| C | `table` | `write` | `0.145` (`0.144–0.148`) | `0.145` (`0.144–0.145`) | `0.144` (`0.144–0.145`) | `0.144` (`0.144–0.144`) |
| C | `table` | `round_trip` | `0.407` (`0.404–0.574`) | `0.403` (`0.402–0.404`) | `0.404` (`0.402–0.406`) | `0.403` (`0.402–0.404`) |
| Go | `packet` | `write` | `0.291` (`0.291–0.293`) | `0.290` (`0.290–0.290`) | `0.290` (`0.290–0.292`) | `0.290` (`0.290–0.290`) |
| Go | `packet` | `round_trip` | `0.708` (`0.707–0.709`) | `0.708` (`0.707–0.713`) | `0.709` (`0.708–0.712`) | `0.712` (`0.709–0.712`) |
| Go | `table` | `write` | `0.199` (`0.198–0.262`) | `0.198` (`0.197–0.198`) | `0.198` (`0.197–0.200`) | `0.197` (`0.197–0.197`) |
| Go | `table` | `round_trip` | `0.755` (`0.747–0.865`) | `0.746` (`0.745–0.748`) | `0.749` (`0.743–0.750`) | `0.747` (`0.746–0.751`) |
| C# | `packet` | `write` | `0.250` (`0.250–0.252`) | `0.253` (`0.251–0.255`) | `0.249` (`0.249–0.254`) | `0.252` (`0.248–0.254`) |
| C# | `packet` | `round_trip` | `0.556` (`0.555–0.604`) | `0.558` (`0.556–0.561`) | `0.560` (`0.557–0.563`) | `0.558` (`0.555–0.560`) |
| C# | `table` | `write` | `0.132` (`0.131–0.136`) | `0.130` (`0.130–0.131`) | `0.130` (`0.130–0.130`) | `0.130` (`0.130–0.131`) |
| C# | `table` | `round_trip` | `3.493` (`3.381–4.022`) | `3.494` (`3.422–4.122`) | `3.504` (`3.348–3.584`) | `3.523` (`3.485–4.201`) |

## 2. Variable-Class Table Benchmark (`bench/run_table.sh` / `make bench-table`)

Corpus: `b51387f36d9b59c4` (`BenchTable`, `2147 B/op`, `400,000` iterations, `7` measured runs per row). `BenchTable` is a Variable-class table that embeds `BenchSmall` (a Fixed-class subtable) as a scalar field, a 64-element fixed array, and a variable-length list.

| Sitting | Arm | Path | Median msg/s | Min msg/s | Max msg/s | Median MB/s | Spread % | Delta vs Preceding `A` |
|---|---|---|---:|---:|---:|---:|---:|---:|
| `A1` | Base (primitives only) | `write` | `219,905` | `219,485` | `220,159` | `450.26` | `0.31%` | — |
| `B1` | Candidate (+Fixed bodies) | `write` | `191,662` | `190,348` | `202,564` | `392.44` | `6.37%` | `-12.84%` |
| `A2` | Base (primitives only) | `write` | `226,417` | `214,545` | `227,255` | `463.60` | `5.61%` | — |
| `B2` | Candidate (+Fixed bodies) | `write` | `202,579` | `191,848` | `203,454` | `414.79` | `5.73%` | `-10.53%` |
| `A1` | Base (primitives only) | `round_trip` | `101,827` | `101,226` | `102,156` | `208.50` | `0.91%` | — |
| `B1` | Candidate (+Fixed bodies) | `round_trip` | `96,022` | `94,268` | `96,967` | `196.61` | `2.81%` | `-5.70%` |
| `A2` | Base (primitives only) | `round_trip` | `102,230` | `101,773` | `102,431` | `209.32` | `0.64%` | — |
| `B2` | Candidate (+Fixed bodies) | `round_trip` | `96,266` | `95,269` | `98,955` | `197.11` | `3.83%` | `-5.83%` |

## 3. Analysis

1. **`BenchFixedTable` (`schema-paired` `table` wire):** On `BenchFixedTable`, `write` runs through `BenchFixedTableFixedSave` -> `BenchFixedTableFixedWriteBody` (`0.130–0.132 µs/op`), which RyuJIT already optimizes at the default inline budget (`A2` at `7,711,232 msg/s` vs `B1` `7,674,757 msg/s` and `B2` `7,663,394 msg/s`). `round_trip` (`3.493–3.504 µs/op` base vs `3.494–3.523 µs/op` candidate) is dominated by `BenchFixedTableFixedLoad` (`TableFixedRuntime.Load` + `BenchMixedClamp`), which is the plan-driven reader path (`32` header/field checks plus subtable reads) and is not a Fixed-class body candidate.
2. **`BenchTable` (`make bench-table`):** `BenchTable` is a Fixed-class table in `BenchTable.schema` (`!ir.VariableTables(g.unit)["BenchTable"]`) that nests `BenchSmall` (also Fixed-class) as a scalar field and a 64-element fixed array. Marking Fixed-class `BenchSmallCollectTyped`, `BenchSmallBodySizeTyped`, `BenchSmallWriteBodyTyped`, `BenchTableCollectTyped`, `BenchTableBodySizeTyped`, `BenchTableWriteBodyTyped`, and `BenchTableSaveTyped` with `[MethodImpl(MethodImplOptions.AggressiveInlining)]` forces RyuJIT to inline all of those subtable and root table bodies across `BenchSmall[64]` loops and `BenchTableSave`, increasing register pressure and code size and regressing `BenchTable` `write` by `10.5%–12.8%` and `round_trip` by `5.7%–5.8%` across both interleaved pairs.
3. **Decision:** Revert the Fixed-class table body `[MethodImpl(MethodImplOptions.AggressiveInlining)]` additions and keep `[MethodImpl(MethodImplOptions.AggressiveInlining)]` strictly on the primitive wire helpers in `internal/codegen/cstable/wire.go:463-659` and `internal/codegen/cstable/cstable.go:623-641`.
