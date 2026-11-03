# Authored optional-map cook states

These four cases pin SPEC-TABLES §2.3 and §7 storage behavior. Each instance is built directly in the language's value model, so an absent field can retain backing entries that wire/JSON decoding would normally omit.

Absent-empty, present-empty and present-populated cook successfully with the declared presence byte in both byte orders. Absent-stale must refuse cooking while its wire and JSON output remain identical to absent-empty. Clear its entries before cooking. The native tests use neutral cook bytes only for the three valid states; the explicit absent-stale refusal is the specification oracle.

The schema uses signed int32 keys and values. `Entries` is a sequence of `Key`/`Value` pairs; `Present` is the authored presence flag and `Refuse` is the expected cook refusal. No wire/cook pin is redefined by this corpus.
