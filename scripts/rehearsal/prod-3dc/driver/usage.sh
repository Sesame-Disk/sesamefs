#!/bin/sh
# Realistic usage shapes, each on fresh libraries, allowing them to settle first.
T=$(cat /tmp/token); A="Authorization: Token $T"
u(){ echo "http://$1.files.rehearsal.test"; }
# Library names carry the run time, so a label can be reused on the same stack.
mklib(){ curl -s -m 60 -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_name\":\"$1-$(date +%s)\"}" "$(u na)/api/v2.1/repos/" | jq -r '.repo_id // .id // empty'; }
up(){ # region lib name size -> "code ms"
  h=$(u $1); head -c $4 /dev/urandom > /tmp/up-$3.bin
  s=$(date +%s%N); l=$(curl -s -m 60 -H "$A" "$h/api2/repos/$2/upload-link/?p=/" | tr -d '"')
  c=$(curl -s -m 120 -o /dev/null -w '%{http_code}' -F parent_dir=/ -F "file=@/tmp/up-$3.bin;filename=$3.bin" "$l?ret-json=1"); e=$(date +%s%N)
  echo "$c $(( (e-s)/1000000 ))"; rm -f /tmp/up-$3.bin
}
summ(){ # file label
  codes=$(cut -d' ' -f1 "$1" | sort | uniq -c | awk '{printf "%sx%s ",$1,$2}')
  lat=$(grep '^200 ' "$1" | cut -d' ' -f2 | sort -n | awk '{a[NR]=$1} END{if(NR)printf "p50=%dms p95=%dms max=%dms",a[int(NR*0.5)+1],a[int(NR*0.95)+1],a[NR]}')
  echo "$2: $codes $lat"
}
L1=$(mklib seq-$1); L2=$(mklib par-$1); LA=$(mklib dl-$1); LB=$(mklib dl2-$1); LC=$(mklib dl3-$1)
sleep 10
# warm each library so first-use cost is reported separately
: > /tmp/s1; echo "first upload per library: L1=$(up na $L1 w1 64000) L2=$(up na $L2 w2 64000) A=$(up na $LA w3 64000) B=$(up eu $LB w4 64000) C=$(up asia $LC w5 64000)"
: > /tmp/s1; i=0; while [ $i -lt 12 ]; do i=$((i+1)); up na $L1 s$i 64000 >> /tmp/s1; done; summ /tmp/s1 "A sequential, one region (NA), 64 KB"
: > /tmp/s2; i=0; while [ $i -lt 12 ]; do i=$((i+1)); up asia $L1 r$i 64000 >> /tmp/s2; done; summ /tmp/s2 "B sequential, library's other region (Asia), 64 KB"
: > /tmp/s3; i=0; while [ $i -lt 4 ]; do i=$((i+1)); up na $L1 b$i 8000000 >> /tmp/s3; done; summ /tmp/s3 "C sequential, one region, 8 MB"
: > /tmp/s4; export A; i=0; while [ $i -lt 15 ]; do i=$((i+1)); ( up na $L2 p$i 64000 >> /tmp/s4 ) & [ $((i%3)) = 0 ] && wait; done; wait; summ /tmp/s4 "D one region, 3 parallel, same library"
: > /tmp/s5; i=0; while [ $i -lt 12 ]; do i=$((i+1)); ( up na $LA x$i 64000 >> /tmp/s5 ) & ( up eu $LB y$i 64000 >> /tmp/s5 ) & ( up asia $LC z$i 64000 >> /tmp/s5 ) & wait; done; summ /tmp/s5 "E three regions at once, DIFFERENT libraries"
: > /tmp/s6; i=0; while [ $i -lt 10 ]; do i=$((i+1)); for r in na eu asia; do s=$(date +%s%N); c=$(curl -s -m 60 -o /dev/null -w '%{http_code}' -H "$A" "$(u $r)/api2/repos/$L1/dir/?p=/"); echo "$c $(( ($(date +%s%N)-s)/1000000 ))" >> /tmp/s6; done; done; summ /tmp/s6 "F list directory, any region"
s=$(date +%s%N); d=$(curl -s -m 60 -H "$A" "$(u eu)/api2/repos/$L1/file/?p=/b1.bin" | tr -d '"'); c=$(curl -s -m 120 -o /tmp/d.bin -w '%{http_code} %{size_download}' "$d"); echo "G 8 MB download at EU: $c $(( ($(date +%s%N)-s)/1000000 ))ms"
