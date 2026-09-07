# `test/tables/enumbound` — the enum-bound refusal's corpus

One unit a SHAPE, for the rule docs/SPEC-TABLES.md §2.4 and §11 state on the
bound's PROVENANCE: a positional array whose bound folds from an enum is
refused in a TABLE BODY and in a UNION ARM, and `[E]T` is the table form.

`Body*.schema` and `Arm*.schema` are the refused shapes, one file for each
spelling the bound has in each of the two scopes. `Control*.schema` are the
units the rule leaves alone: the packet wire, a bound that folds from no enum,
and the `type`-held case schema#606 rules on.

`internal/check/tables_test.go` reads this directory and asserts each file's
answer, so a shape is red the moment it compiles or the diagnostic stops
naming the field, the enum and the fix. Nothing here is swept by `make check`
or `make fmt`: the refused units are the point, and a corpus `make check`
sweeps must compile.
