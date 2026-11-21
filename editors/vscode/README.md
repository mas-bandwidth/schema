# Schema for VS Code

Syntax highlighting for `.schema` files: keywords, scalar types, declaration
names, enum and flags variants, qualification names after `|`, numbers,
strings, and `//`, `///` and `/* */` comments. The grammar is a TextMate
grammar, so editors that read TextMate grammars (VS Code, Cursor, VSCodium,
Sublime Text, TextMate) can use `syntaxes/schema.tmLanguage.json` as it is.

The extension is not on a marketplace yet. Install it from a local build:

```
cd editors/vscode
npx @vscode/vsce package --skip-license
code --install-extension schema-language-0.1.0.vsix
```

Or copy this directory to `~/.vscode/extensions/schema-language` and restart
the editor.

## The test

```
go test ./editors/...
```

`grammar_test.go` runs the grammar over every `.schema` file in `examples/`,
`examples128/` and `tables/`, and checks each token's scope against the
compiler's own scanner (`internal/scanner`): every keyword, type, comment,
string, number, declaration name, qualification name and variant gets its
scope, and no other identifier gets one it should not have.

The test runs the grammar with a small TextMate engine in Go. Every regular
expression in the grammar stays inside the subset RE2 and Oniguruma share, so
Go runs it as VS Code does; a rule outside that subset fails
`TestGrammarCompiles`. When the grammar changes, keep to that subset, and
add a line to `TestGrammarSamples` for any new construct.
