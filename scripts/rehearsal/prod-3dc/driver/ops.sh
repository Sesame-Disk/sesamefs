#!/bin/sh
# One sequential probe of the main operations per region, for DC-outage checks.
# usage: ops.sh prepare            create a library with one seed file
#        ops.sh check <region>...  account, listing, download, upload, library, share link
#        ops.sh list               files of the prepared library, as each region sees them
PHASE=$1; shift
T=$(cat /tmp/token); A="Authorization: Token $T"
u(){ echo "http://$1.files.rehearsal.test"; }
t(){ curl -s -m 40 -o /tmp/b -w '%{http_code} %{time_total}s' "$@"; }
BAD=0
# step <region> <label> <curl args...>: prints code and time, counts anything but 2xx.
step(){ r=$1; l=$2; shift 2; o=$(t "$@"); printf "  [%s] %s %s %s\n" "$r" "$l" "$o" "$(grep -o '"error":"[^"]*"' /tmp/b 2>/dev/null)"
  case "$o" in 2??\ *) ;; *) BAD=$((BAD+1));; esac; }

if [ "$PHASE" = prepare ]; then
  ID=$(curl -s -m 60 -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_name\":\"ops-$$\"}" "$(u na)/api/v2.1/repos/" | jq -r '.repo_id // .id // empty')
  [ -n "$ID" ] || { echo "prepare: library creation failed" >&2; exit 1; }
  sleep 6
  head -c 500000 /dev/urandom > /tmp/seed.bin
  ul=$(curl -s -m 40 -H "$A" "$(u na)/api2/repos/$ID/upload-link/?p=/" | tr -d '"')
  c=$(curl -s -m 60 -o /dev/null -w '%{http_code}' -F parent_dir=/ -F "file=@/tmp/seed.bin;filename=seed.bin" "$ul?ret-json=1")
  echo "seed upload $c"
  [ "$c" = 200 ] || { echo "prepare: seed upload failed" >&2; exit 1; }
  sha256sum /tmp/seed.bin | cut -d' ' -f1 > /tmp/seed.sha; echo "$ID" > /tmp/ops-id
  exit 0
fi

ID=$(cat /tmp/ops-id); SHA=$(cat /tmp/seed.sha)
case "$PHASE" in
  list)
    for r in na eu asia; do
      printf "  [%s] files: %s\n" "$r" "$(curl -s -m 40 -H "$A" "$(u $r)/api2/repos/$ID/dir/?p=/" | jq -r '.[] | select(.type=="file") | .name' | sort | tr '\n' ' ')"
    done ;;
  check)
    for r in "$@"; do
      h=$(u $r); n="ops-$(date +%s)-$r"
      step "$r" account -H "$A" "$h/api2/account/info/"
      step "$r" "list dir" -H "$A" "$h/api2/repos/$ID/dir/?p=/"
      du=$(curl -s -m 40 -H "$A" "$h/api2/repos/$ID/file/?p=/seed.bin" | tr -d '"')
      case "$du" in
        http*) curl -s -m 40 -o /tmp/d.bin "$du"
               if [ "$(sha256sum /tmp/d.bin | cut -d' ' -f1)" = "$SHA" ]; then echo "  [$r] download OK"; else echo "  [$r] download BAD"; BAD=$((BAD+1)); fi ;;
        *) echo "  [$r] download link FAILED: $(echo "$du" | head -c 90)"; BAD=$((BAD+1)) ;;
      esac
      head -c 64000 /dev/urandom > /tmp/x.bin
      ul=$(curl -s -m 40 -H "$A" "$h/api2/repos/$ID/upload-link/?p=/" | tr -d '"')
      case "$ul" in
        http*) step "$r" upload -F parent_dir=/ -F "file=@/tmp/x.bin;filename=$n.bin" "$ul?ret-json=1" ;;
        *) echo "  [$r] upload link FAILED: $(echo "$ul" | head -c 90)"; BAD=$((BAD+1)) ;;
      esac
      step "$r" "create library" -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_name\":\"$n\"}" "$h/api/v2.1/repos/"
      step "$r" "share link" -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_id\":\"$ID\",\"path\":\"/seed.bin\"}" "$h/api/v2.1/share-links/"
    done
    echo "RESULT failed_steps=$BAD"
    [ "$BAD" -eq 0 ] ;;
  *) echo "usage: ops.sh prepare | check <region>... | list" >&2; exit 2 ;;
esac
