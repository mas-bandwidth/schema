package main

func init() {
	sabotages["js-cook-absent-defaults"] = []edit{{
		old: `        if (view !== null) { view.setUint8(base + fd.presentAt, 0); }`,
		new: `        if (view !== null) { view.setUint8(base + fd.presentAt, 0); }
        if (fd.k === "rec") {
          return writeStruct(view, base + fd.at, layouts[fd.t], layouts[fd.t].make(), le, ctx); // SABOTAGED
        }`,
	}}
}
