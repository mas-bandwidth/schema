module schemaconformance

go 1.23

require (
 mapdemo v0.0.0
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

require tblm1 v0.0.0
replace tblm1 => ../../../build/tables-generated-go/m1
require tblm2 v0.0.0
replace tblm2 => ../../../build/tables-generated-go/m2
require tbla1 v0.0.0
replace tbla1 => ../../../build/tables-generated-go/a1
require tbla2 v0.0.0
replace tbla2 => ../../../build/tables-generated-go/a2
require tblk1 v0.0.0
replace tblk1 => ../../../build/tables-generated-go/k1
require tblk2 v0.0.0
replace tblk2 => ../../../build/tables-generated-go/k2
require tblr1 v0.0.0
replace tblr1 => ../../../build/tables-generated-go/r1
require tblr2 v0.0.0
replace tblr2 => ../../../build/tables-generated-go/r2

require scalardemo v0.0.0
replace scalardemo => ../../../build/tables-generated-go/scalars
require tblscalars2 v0.0.0
replace tblscalars2 => ../../../build/tables-generated-go/scalars2

require widedemo v0.0.0
replace widedemo => ../../../build/tables-generated-go/wide

require messagedemo v0.0.0
replace messagedemo => ../../../build/tables-generated-go/messages

require (
streamdemo v0.0.0
blobdemo v0.0.0
)
replace streamdemo => ../../../build/tables-generated-go/stream
replace blobdemo => ../../../build/tables-generated-go/blobs

require listdemo v0.0.0
replace listdemo => ../../../build/tables-generated-go/lists

replace mapdemo => ../../../build/tables-generated-go/maps

require backenddemo v0.0.0
replace backenddemo => ../../../build/tables-generated-go/backend
require vocabdemo v0.0.0
replace vocabdemo => ../../../build/tables-generated-go/vocab
require vocab9demo v0.0.0
replace vocab9demo => ../../../build/tables-generated-go/vocab9

require tblrt1 v0.0.0
replace tblrt1 => ../../../build/tables-generated-go/rt1

require tblp2 v0.0.0
replace tblp2 => ../../../build/tables-generated-go/p2
require tblw1 v0.0.0
replace tblw1 => ../../../build/tables-generated-go/w1
require tblw2 v0.0.0
replace tblw2 => ../../../build/tables-generated-go/w2
require tblg1 v0.0.0
replace tblg1 => ../../../build/tables-generated-go/g1
