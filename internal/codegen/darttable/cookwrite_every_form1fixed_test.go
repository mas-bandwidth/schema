package darttable

// TestCookWriteEveryConstructDart's FORM-1 FIXED-CLASS instances
// (dart-cook-write-form1-fixed-instances-t).
//
// This card registers the conformance tables corpus's form-1 fixed-class
// instances (tabledemo, tblv1, tblv2, tblp1, tblp3, tblm1, tblm2) as
// `form1-<instance>` subtests of the shared
// TestCookWriteEveryConstructDart. The shared driver and its
// `cookEveryFamilies` registry live in cookwrite_every_test.go, which
// dart-cook-write-arrays-t lands. That card has not landed on
// sprint/schema-dart, so the registry does not exist at this base and this
// file cannot yet register its entries: referencing it would keep the whole
// package from compiling, which is worse than the named absence recorded
// here. The generator work this family needs is likewise not complete:
// cookUnitOf still skips every root whose closure holds a union (tabledemo's
// RootConfig, tblv1/tblv2's Cfg and tblm1/tblm2's Msg), and
// emitCookWriteFixed still gates the cook writer on the FIXED FORM's
// 65536-byte ceiling (tabledemo's WideBlob), so those roots get a comment and
// no writer. This file is the place the entries land, unchanged in shape, once
// the arrays card's driver is on the branch and the writer carries the union
// and wide-blob storages.
