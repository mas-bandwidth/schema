# Unity (`--lang unity`) wire-fuzz qualification, 2026-11-18

Source: `96df2141b4319b79eaef096f3ca3ac0752abb2f9` (`emma/unity-u2-consumers` / `emma/unity-u2-vt`). Linux x86-64 (`vision`), .NET 10 SDK compiling generated Unity units strictly under `test/unity/UnityDialect.props` (`<LangVersion>9.0</LangVersion>`, `<TargetFramework>netstandard2.1</TargetFramework>`, `<AllowUnsafeBlocks>false</AllowUnsafeBlocks>`, warnings as errors) with pinned `serialize.cs` `v2.1.1`.

Plain rows cover all 34 conformance units (`testdata/conformance/tables/MANIFEST.txt` plus `tables/cards/Card.schema`) across 63 roots (`224,114` enumerated mutations + `100,000` random mutations = `324,114` total mutations per seed). Retain rows cover all 29 variable-table roots (`135,354` enumerated mutations + `100,000` random mutations = `235,354` total mutations per seed). All 6 release-tier arms (`make -j 6 tables-wire-fuzz-release-unity`, `1,678,404` total mutants) passed with zero divergences and zero absent roots; `make tables-unity-wire-fuzz-negative-control` sabotaged the 4096 ID-table cap and exited nonzero by name.

| Row | Arm | Seed | Random mutations | Total mutations | Wall time | Verdict |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| `release-plain-1` | `plain` | 1 | 100000 | 324114 | 56 s | `pass` |
| `release-plain-2` | `plain` | 2 | 100000 | 324114 | 57 s | `pass` |
| `release-plain-3` | `plain` | 3 | 100000 | 324114 | 57 s | `pass` |
| `release-retain-1` | `retain` | 1 | 100000 | 235354 | 44 s | `pass` |
| `release-retain-2` | `retain` | 2 | 100000 | 235354 | 44 s | `pass` |
| `release-retain-3` | `retain` | 3 | 100000 | 235354 | 44 s | `pass` |
