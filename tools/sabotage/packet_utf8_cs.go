package main

func init() {
	// Skips the string(N) content validator's loop (TextValid in
	// internal/codegen/csharp/utf8.go), so the read accepts ill-formed bytes.
	sabotages["packet-utf8-cs-read"] = []edit{{
		old: `for (int utf8Index = 0; utf8Index < bytes.Length;)`,
		new: `for (int utf8Index = int.MaxValue; utf8Index < bytes.Length;) // SABOTAGED`,
	}}
}
