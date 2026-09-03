// THE CODEC TABLE: one row per (unit, root) the corpus names.
//
// Nothing here is an expectation — the rows only say which generated functions
// answer for which manifest key. The expectations all live in the DATA.
package main

import (
	"tabledemo"
	"tblp1"
	"tblp3"
	"tblv1"
	"tblv2"
)

// each unit's TableReport is its OWN type, so each gets a narrowing of four
// lines. A row that stopped copying a counter would be caught by the first
// case that counts it.
func snapDemo(r *tabledemo.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed}
}

func snapV1(r *tblv1.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed}
}

func snapV2(r *tblv2.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed}
}

func snapP1(r *tblp1.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed}
}

func snapP3(r *tblp3.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed}
}

var codecTable = []codec{
	row("tabledemo", "RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoad,
		tabledemo.RootConfigMeasure, tabledemo.RootConfigSave, snapDemo),
	row("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoad,
		tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave, snapDemo),
	row("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoad,
		tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave, snapDemo),
	row("tabledemo", "WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoad,
		tabledemo.WideBlobMeasure, tabledemo.WideBlobSave, snapDemo),
	row("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoad,
		tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave, snapDemo),
	row("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoad,
		tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave, snapDemo),
	row("tblv1", "Cfg", tblv1.CfgReset, tblv1.CfgLoad, tblv1.CfgMeasure, tblv1.CfgSave, snapV1),
	row("tblv2", "Cfg", tblv2.CfgReset, tblv2.CfgLoad, tblv2.CfgMeasure, tblv2.CfgSave, snapV2),
	row("tblp1", "Chain", tblp1.ChainReset, tblp1.ChainLoad, tblp1.ChainMeasure, tblp1.ChainSave, snapP1),
	row("tblp3", "Chain", tblp3.ChainReset, tblp3.ChainLoad, tblp3.ChainMeasure, tblp3.ChainSave, snapP3),
}

// surfaces is what this backend implements. A surface not listed prints as
// ABSENT in the matrix, which is a missing FEATURE and not a failing test.
func surfaces() []string {
	return []string{"wire", "report", "block", "forgery"}
}
