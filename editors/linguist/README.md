# GitHub Linguist kit

Everything a pull request to [github-linguist/linguist](https://github.com/github-linguist/linguist)
needs so that GitHub recognises and highlights `.schema` files. Nothing here
has been submitted. Linguist takes a new extension only once it is in wide
use: at least 2000 files indexed by GitHub code search in the last year,
excluding forks, across many repositories. A search on 2026-11-21 found
about 716. Until then the pull request would be closed; this kit keeps it
ready.

## The languages.yml entry

Insert it in alphabetical order in `lib/linguist/languages.yml`. No language
in Linguist used the `.schema` extension or the `source.schema` scope on
2026-11-21, so no heuristic is needed. Omit `language_id`: Linguist's
`script/update-ids` assigns it.

```yaml
Schema:
  type: data
  color: "#3d7bd9"
  extensions:
  - ".schema"
  tm_scope: source.schema
  ace_mode: text
```

## The grammar

[`editors/vscode/syntaxes/schema.tmLanguage.json`](../vscode/syntaxes/schema.tmLanguage.json),
scope `source.schema`, tested over the corpus by `go test ./editors/...`.
Add it with:

```
script/add-grammar https://github.com/mas-bandwidth/schema
```

Linguist vendors the grammar's repository as a submodule and runs its
grammar compiler over it. This repository is large; if Linguist's
maintainers ask for a smaller grammar repository, the grammar file is the
only file it needs. The grammar must carry a licence Linguist accepts; this
repository's AGPL-3.0 is on its list.

## Samples

Linguist wants at least two real files per extension under
`samples/Schema/`. These cover the language between them:

- `examples/Types.schema`: types, ranged integers, quantized vectors, `if`
  branches, `cpp_native` qualifications, typed constants.
- `examples/Wire.schema`: enums, flags with a qualification, a union.
- `tables/examples/Keyed.schema`: enums, `fixed table`, defaults, optional
  sections, enum-keyed arrays.

The pull request states the licence that covers each sample. These files are
under this repository's licence unless the owner says otherwise.
