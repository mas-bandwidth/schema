These fifteen literal cases cover repeated inline table fields, union arms, fixed positional table/union slots, and enum-keyed table slots. Each directory is an independent `probe` unit with root `Root`.

The inputs are constructed from field framing independently of the emitters. Saved bytes are pinned against both the neutral implementation and actual generated C++ retaining readers/writers. Dart tests consume these fixtures and compare JIT and AOT results, including repeated loads and saves.

Compatible replacement supersedes the earlier body's retained records without increasing either retention counter. An incompatible outer kind preserves the previous body and its retained records. A compatible fixed positional array occurrence discards earlier retention across the whole array, including untouched known-value suffix slots; a keyed occurrence discards only slots it replaces. A union's explicit None replaces its earlier arm.

`unknown`, `kind_mismatch`, `retained`, and `load_lost` describe the read; `save_lost` is the additional loss reported by a fresh report during save. `wire` and `saved` are lowercase hexadecimal bytes. No fixture requires enum-keyed arrays of unions, which remain a named language follow-on.
