module table-base64-test

go 1.26

require base64test v0.0.0

require github.com/mas-bandwidth/schema/v3 v3.0.0-00010101000000-000000000000 // indirect

replace (
	base64test => ../../../build/table-base64/go
	github.com/mas-bandwidth/schema/v3 => ../../..
)
