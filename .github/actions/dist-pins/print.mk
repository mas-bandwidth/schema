# print.mk: `make -f Makefile -f <this> print-VAR` prints a make variable
# (GNU make 3.81 has no --eval). Read by action.yml.
print-%: ; @echo $($*)
