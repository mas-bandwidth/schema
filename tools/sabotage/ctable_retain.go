package main

func init() {
	sabotages["retain-c-drop-field"] = []edit{{old: "table_retain_commit(keep,record,&s,r->report);return 1;", new: "return 1; /* SABOTAGED: silently drop a complete retained field */"}}
}
