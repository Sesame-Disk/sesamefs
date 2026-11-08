#!/bin/sh
# usage: conc.sh <label> <files-per-region> <parallelism>
# Concurrent uploads from three regions to ONE library, with concurrent mkdirs for HEAD
# contention. Compares what the client was told against what is actually listed.
LABEL=$1; N=${2:-15}; PAR=${3:-6}
NA=http://na.files.rehearsal.test; EU=http://eu.files.rehearsal.test; AS=http://asia.files.rehearsal.test
T=$(cat /tmp/token); A="Authorization: Token $T"
ID=$(curl -s -m 60 -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_name\":\"conc-$LABEL\"}" $NA/api/v2.1/repos/ | jq -r '.repo_id // .id')
sleep ${SETTLE:-8}
echo "library=$ID label=$LABEL files_per_region=$N parallelism=$PAR"
[ -f /tmp/c.bin ] || head -c 64000 /dev/urandom > /tmp/c.bin
export A ID
cat > /tmp/one.sh <<'EOS'
#!/bin/sh
h=$1; n=$2; head -c 64000 /dev/urandom > /tmp/u-$n.bin
s=$(date +%s%N); u=$(curl -s -m 60 -H "$A" "$h/api2/repos/$ID/upload-link/?p=/" | tr -d '"')
c=$(curl -s -m 120 -o /tmp/ub-$n -w '%{http_code}' -F parent_dir=/ -F "file=@/tmp/u-$n.bin;filename=$n.bin" "$u?ret-json=1")
e=$(date +%s%N); echo "$c $(( (e-s)/1000000 )) $h $n $(head -c 110 /tmp/ub-$n | tr '\n' ' ')"
rm -f /tmp/u-$n.bin /tmp/ub-$n
EOS
: > /tmp/jobs
i=0; while [ $i -lt $N ]; do i=$((i+1)); echo "$NA n$i" >> /tmp/jobs; echo "$EU e$i" >> /tmp/jobs; echo "$AS a$i" >> /tmp/jobs; done
touch /tmp/run-$LABEL; : > /tmp/mk-$LABEL
# background HEAD contention from every region while uploads run
( j=0; while [ -f /tmp/run-$LABEL ]; do j=$((j+1)); for h in $NA $EU $AS; do curl -s -m 60 -o /dev/null -w '%{http_code}\n' -X POST -H "$A" -d 'operation=mkdir' "$h/api2/repos/$ID/dir/?p=/d-$(basename $h | cut -d. -f1)-$j" >> /tmp/mk-$LABEL & done; wait; sleep 1; done ) &
xargs -P "$PAR" -L 1 sh /tmp/one.sh < /tmp/jobs > /tmp/res-$LABEL
rm -f /tmp/run-$LABEL; wait
echo "-- upload status by code"; cut -d' ' -f1 /tmp/res-$LABEL | sort | uniq -c
echo "-- mkdir status by code"; sort /tmp/mk-$LABEL | uniq -c
echo "-- latency ms 200s: $(grep '^200 ' /tmp/res-$LABEL | cut -d' ' -f2 | sort -n | awk '{a[NR]=$1} END{if(NR)printf "n=%d p50=%d p95=%d max=%d",NR,a[int(NR*0.5)+1],a[int(NR*0.95)+1],a[NR]}')"
echo "-- non-200 samples"; grep -v '^200 ' /tmp/res-$LABEL | cut -c1-170 | sed -E 's/http:\/\/([a-z]+)\.files\.rehearsal\.test/\1/' | head -8
sleep ${LISTWAIT:-3}
for h in $NA $EU $AS; do curl -s -m 60 -H "$A" "$h/api2/repos/$ID/dir/?p=/" | jq -r '.[] | select(.type=="file") | .name' | sort > /tmp/l-$(basename $h | cut -d. -f1); echo "listed at $(basename $h | cut -d. -f1): $(wc -l < /tmp/l-$(basename $h | cut -d. -f1))"; done
grep '^200 ' /tmp/res-$LABEL | awk '{print $4".bin"}' | sort > /tmp/acked; grep -v '^200 ' /tmp/res-$LABEL | awk '{print $4".bin"}' | sort > /tmp/nack
echo "acked=$(wc -l < /tmp/acked) failed=$(wc -l < /tmp/nack)"
echo "LOST (acked, not listed at NA): $(comm -23 /tmp/acked /tmp/l-na | wc -l) $(comm -23 /tmp/acked /tmp/l-na | head -5 | tr '\n' ' ')"
echo "PUBLISHED DESPITE ERROR (failed, listed at NA): $(comm -12 /tmp/nack /tmp/l-na | wc -l)"
echo "Region views differ: na-eu=$(comm -3 /tmp/l-na /tmp/l-eu | wc -l) na-asia=$(comm -3 /tmp/l-na /tmp/l-asia | wc -l)"
