package main

import "writebounds"

func init() {
	codecTable = append(codecTable,
		regionRow("writebounds", "TortureVarWref", writebounds.TortureVarWrefLoadMeasure, writebounds.TortureVarWrefLoad, writebounds.TortureVarWrefMeasure, writebounds.TortureVarWrefSave, func(text []byte, r *writebounds.TableReport) (*writebounds.TortureVarWref, []byte, bool) {
			var b writebounds.TortureVarWrefBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := writebounds.TortureVarWrefFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, writebounds.TortureVarWrefToJSONMeasure, writebounds.TortureVarWrefToJSON, writebounds.TortureVarWrefCookMeasure, writebounds.TortureVarWrefCookFrom, func(r *writebounds.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	)
}
