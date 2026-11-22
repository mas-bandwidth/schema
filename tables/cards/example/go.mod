module cardsexample

go 1.26

require (
	cards v0.0.0
	// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)
	github.com/mas-bandwidth/schema/v3 v3.0.0-00010101000000-000000000000
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
)

// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)
replace github.com/mas-bandwidth/schema/v3 => ../../..
