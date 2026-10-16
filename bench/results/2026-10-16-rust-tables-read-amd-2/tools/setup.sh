#!/bin/bash
# setup.sh: clone schema (the carried branch) and the runtimes, build every leg, gate it.
set -euo pipefail
export PATH=/home/nova/sdk/go1.26.6/bin:/home/nova/sdk/bin:$PATH GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1
J=~/rowan-jobs/rust-tables-read-w3e21; mkdir -p $J; cd $J
[ -d schema ] || git clone -q https://github.com/mas-bandwidth/schema.git schema
[ -d serialize ] || git clone -q https://github.com/mas-bandwidth/serialize.git serialize
[ -d serialize.rs ] || git clone -q https://github.com/mas-bandwidth/serialize.rs.git serialize.rs
git -C serialize checkout -q v1.16.2
git -C serialize.rs checkout -q 5be639fd3ca3f7417c202b98acd7ba063881f716
cd schema
git config user.name "Rowan Claude"; git config user.email rowan@mas-bandwidth.com
git fetch -q origin sprint/schema-rust
if ! git rev-parse -q --verify sprint/rust-tables-read-profile-finds-the-flat-decode-gap.w3.g1.e21 >/dev/null; then
  git checkout -q -b sprint/rust-tables-read-profile-finds-the-flat-decode-gap.w3.g1.e21 origin/sprint/schema-rust
  git cherry-pick 67f73768447483815bb2dda9f97a8ba560bfc7df >/dev/null
fi
git log --oneline -2
go run ./bench/paired -mode build -langs cpp,rust > ../build.log 2>&1 || { tail -30 ../build.log; exit 1; }
B=build/paired/sit; mkdir -p $B
cp build/paired/table-cpp $B/table-cpp-gcc; cp build/paired/table-rust $B/table-rust; cp build/paired/packet-cpp $B/packet-cpp-control
F=(-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG '-DBENCH_OPT="O3"' -DBENCH_MATCHED)
clang++ "${F[@]}" -Igenerated/bench/paired/cpp -I../serialize bench/tables/cpp/table_main.cpp -o $B/table-cpp-clang
R=$B/cpp-restrict; rm -rf $R; cp -r generated/bench/paired/cpp $R
sed -i -E 's/(FixedClampBody\( [A-Za-z]+ & value, )int32_t & clamped, int32_t & damaged \)/\1int32_t \& __restrict clamped, int32_t \& __restrict damaged )/' $R/*.h
echo "restrict declarations: $(diff -r generated/bench/paired/cpp $R | grep -c '^>')"
clang++ "${F[@]}" -I$R -I../serialize bench/tables/cpp/table_main.cpp -o $B/exp-cpp-clang-restrict
g++ "${F[@]}" -I$R -I../serialize bench/tables/cpp/table_main.cpp -o $B/exp-cpp-gcc-restrict
for b in table-rust table-cpp-clang table-cpp-gcc exp-cpp-clang-restrict exp-cpp-gcc-restrict; do
  echo "$b: $($B/$b --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --gate 2>&1 | tail -1)"
done
echo "packet-cpp-control: $($B/packet-cpp-control --gate --wire-dir testdata/wire --variant-dir bench/corpus/variants 2>&1 | tail -1)"
(cd $B && sha256sum exp-* packet-cpp-control table-*)
