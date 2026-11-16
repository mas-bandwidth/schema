# make/python-vt-evolution.mk — table defaults, `was` renames and the skip walk
# on the Python table leg (docs/PORT-PYTHON.md). Included by the Makefile's
# wildcard include; the stem has a dash, so it is a support fragment and never
# probed as a --lang port.
#
# The evolution kinds' four controls, one per check: a copy of the generated
# runtime's string write is truncated (rename_table must go red), a wrong
# declared default in a copy of the generated D (defaults_at), a renamed field
# under its declared name's id (rename_field), and the skip walk without its
# text check (skip_text_ill_formed). Each control row is its own file under
# make/negative-controls.d/.
.PHONY: python-vt-evolution-negative-control python-vt-evolution-defaults-negative-control python-vt-evolution-renames-negative-control python-vt-evolution-skip-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-evolution
python-ci-vt-evolution: python-vt-evolution-negative-control python-vt-evolution-defaults-negative-control python-vt-evolution-renames-negative-control python-vt-evolution-skip-negative-control

python-vt-evolution-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtEvolutionNegativeControl$$' ./internal/codegen/pythontable/

python-vt-evolution-defaults-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtEvolutionNegativeControlDefaults$$' ./internal/codegen/pythontable/

python-vt-evolution-renames-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtEvolutionNegativeControlRenames$$' ./internal/codegen/pythontable/

python-vt-evolution-skip-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtEvolutionNegativeControlSkipCheck$$' ./internal/codegen/pythontable/
