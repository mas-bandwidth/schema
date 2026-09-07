package main

func init() {
	sabotages["packet-utf8-dart-read"] = []edit{{
		old: `  var index = 0;`,
		new: `  var index = length; // SABOTAGED: skip the UTF-8 scan`,
	}}
}
