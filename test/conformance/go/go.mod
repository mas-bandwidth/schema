module schemaconformance

go 1.23

require (
	blockdemo v0.0.0
	graphdemo v0.0.0
	tabledemo v0.0.0
	tblp1 v0.0.0
	tblp3 v0.0.0
	tblv1 v0.0.0
	tblv2 v0.0.0
)

require github.com/mas-bandwidth/serialize.go v0.0.0

replace tabledemo => ../../../build/tables-generated-go/examples

replace tblv1 => ../../../build/tables-generated-go/v1

replace tblv2 => ../../../build/tables-generated-go/v2

replace tblp1 => ../../../build/tables-generated-go/p1

replace tblp3 => ../../../build/tables-generated-go/p3

replace blockdemo => ../../../build/tables-generated-go/block

replace graphdemo => ../../../build/tables-generated-go/pointers

replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
