module table-base64-test

go 1.26

require (
	base64test v0.0.0
	github.com/mas-bandwidth/schema/v2 v2.0.0
)

replace base64test => ../../../build/table-base64/go

replace github.com/mas-bandwidth/schema/v2 => ../../..
