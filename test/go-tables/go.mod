module schematables

go 1.24

require (
	blockdemo v0.0.0
	graphdemo v0.0.0
	tabledemo v0.0.0
)

require github.com/mas-bandwidth/serialize.go v0.0.0

replace tabledemo => ../../build/tables-generated-go/examples

replace blockdemo => ../../build/tables-generated-go/block

replace graphdemo => ../../build/tables-generated-go/pointers

replace github.com/mas-bandwidth/serialize.go => ../../../serialize.go
