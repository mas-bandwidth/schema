module cardsexample

go 1.26

require (
	cards v0.0.0
	// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)
	github.com/mas-bandwidth/schema/v2 v2.0.0
	github.com/mas-bandwidth/serialize.go v0.0.0
)

// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)
replace github.com/mas-bandwidth/schema/v2 => ../../..
