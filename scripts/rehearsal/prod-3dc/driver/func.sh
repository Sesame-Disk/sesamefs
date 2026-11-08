#!/bin/sh
# Cross-region functional matrix through central nginx -> frontend nginx -> Go.
NA=http://na.files.rehearsal.test; EU=http://eu.files.rehearsal.test; AS=http://asia.files.rehearsal.test
PASS=0; FAIL=0
ok(){ PASS=$((PASS+1)); echo "PASS  $1"; }
ko(){ FAIL=$((FAIL+1)); echo "FAIL  $1 :: $2"; }
code(){ curl -s -m 60 -o /tmp/body -w '%{http_code}' "$@"; }
expect(){ n="$1"; want="$2"; shift 2; c=$(code "$@"); case " $want " in *" $c "*) ok "$n ($c)";; *) ko "$n" "got $c want $want body=$(head -c 220 /tmp/body)";; esac; }
loginurl(){ curl -s -G --data-urlencode "redirect_uri=$1/sso/" "$1/api/v2.1/auth/oidc/login" | jq -r .authorization_url; }
login(){ # host
  a=$(loginurl "$1"); r=$(curl -s -o /dev/null -w '%{redirect_url}' "$a&login_hint=admin@rehearsal.test")
  cd=$(echo "$r" | sed -E 's/.*[?&]code=([^&]*).*/\1/'); st=$(echo "$r" | sed -E 's/.*[?&]state=([^&]*).*/\1/')
  curl -s -X POST -H 'Content-Type: application/json' -d "{\"code\":\"$cd\",\"state\":\"$st\",\"redirect_uri\":\"$1/sso/\"}" "$1/api/v2.1/auth/oidc/callback"
}
# wait_read host repo path sha label : polls until content matches, reports lag
wait_read(){
  h=$1; id=$2; p=$3; want=$4; l=$5; t0=$(date +%s); got=""
  while [ $(( $(date +%s)-t0 )) -lt 30 ]; do
    u=$(curl -s -m 30 -H "$A" "$h/api2/repos/$id/file/?p=$p" | tr -d '"')
    case "$u" in http*) curl -s -m 60 -o /tmp/r.bin "$u"; got=$(sha256sum /tmp/r.bin | cut -d' ' -f1);; esac
    if [ "$got" = "$want" ]; then ok "$l (visible after $(( $(date +%s)-t0 ))s)"; return 0; fi
    sleep 1
  done
  ko "$l" "not readable within 30s; last url=$(echo "$u" | head -c 120)"
}
upload(){ # host repo localfile name
  u=$(curl -s -m 30 -H "$A" "$1/api2/repos/$2/upload-link/?p=/" | tr -d '"')
  code -F parent_dir=/ -F "file=@$3;filename=$4" "$u?ret-json=1"
}

echo "== login per region"
R=$(login $NA); T=$(echo "$R" | jq -r .token); echo "INFO  na: $(echo "$R" | jq -c '{role,email,error}')"
for h in $NA $EU $AS; do r=$(login $h); t=$(echo "$r" | jq -r .token); if [ -n "$t" ] && [ "$t" != null ]; then ok "OIDC login at $h"; else ko "OIDC login at $h" "$r"; fi; done
A="Authorization: Token $T"
echo "$T" > /tmp/token
for h in $EU $AS; do expect "NA session accepted at $h" 200 -H "$A" $h/api2/account/info/; done

echo "== login started on one region, finished on another (ISSUE-OIDC-PKCE-STATE-NODE-LOCAL-01)"
a=$(curl -s -G --data-urlencode "redirect_uri=$NA/sso/" $NA/api/v2.1/auth/oidc/login | jq -r .authorization_url)
r=$(curl -s -o /dev/null -w '%{redirect_url}' "$a&login_hint=admin@rehearsal.test"); cd=$(echo "$r" | sed -E 's/.*[?&]code=([^&]*).*/\1/'); st=$(echo "$r" | sed -E 's/.*[?&]state=([^&]*).*/\1/')
c=$(code -X POST -H 'Content-Type: application/json' -d "{\"code\":\"$cd\",\"state\":\"$st\",\"redirect_uri\":\"$NA/sso/\"}" $EU/api/v2.1/auth/oidc/callback)
echo "INFO  callback at EU for a login started at NA: $c $(head -c 140 /tmp/body)"

echo "== plaintext library across regions"
head -c 3000000 /dev/urandom > /tmp/f.bin; S1=$(sha256sum /tmp/f.bin | cut -d' ' -f1)
head -c 500000 /dev/urandom > /tmp/g.bin; S2=$(sha256sum /tmp/g.bin | cut -d' ' -f1)
c=$(code -X POST -H "$A" -H 'Content-Type: application/json' -d '{"repo_name":"xregion"}' $NA/api/v2.1/repos/); ID=$(jq -r '.repo_id // .id // empty' /tmp/body)
if [ -n "$ID" ]; then ok "create library at NA"; else ko "create library at NA" "$c $(head -c 200 /tmp/body)"; fi
echo "INFO  id=$ID"
c=$(upload $NA $ID /tmp/f.bin a.bin); if [ "$c" = 200 ]; then ok "upload at NA"; else ko "upload at NA" "$c $(head -c 200 /tmp/body)"; fi
wait_read $NA $ID /a.bin $S1 "read at NA"
wait_read $EU $ID /a.bin $S1 "read at EU"
wait_read $AS $ID /a.bin $S1 "read at Asia"
c=$(upload $EU $ID /tmp/g.bin b.bin); if [ "$c" = 200 ]; then ok "upload at EU into NA library"; else ko "upload at EU" "$c $(head -c 200 /tmp/body)"; fi
wait_read $AS $ID /b.bin $S2 "EU upload read at Asia"
wait_read $NA $ID /b.bin $S2 "EU upload read at NA"
c=$(upload $AS $ID /tmp/g.bin a.bin); if [ "$c" = 200 ]; then ok "same name again at Asia"; else ko "same name again at Asia" "$c $(head -c 200 /tmp/body)"; fi
wait_read $NA $ID "/a%20(1).bin" $S2 "upload link autorenamed it, read at NA"
wait_read $NA $ID /a.bin $S1 "original unchanged at NA"
c=$(code -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_name\":\"eu-placed\"}" $EU/api/v2.1/repos/); ID2=$(jq -r '.repo_id // .id // empty' /tmp/body); c=$(upload $EU $ID2 /tmp/f.bin p.bin); [ "$c" = 200 ] && ok "library created at EU" || ko "library created at EU" "$c"
c=$(code -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_name\":\"asia-placed\"}" $AS/api/v2.1/repos/); ID3=$(jq -r '.repo_id // .id // empty' /tmp/body); c=$(upload $AS $ID3 /tmp/g.bin p.bin); [ "$c" = 200 ] && ok "library created at Asia" || ko "library created at Asia" "$c"
wait_read $NA $ID3 /p.bin $S2 "Asia-placed library read at NA"

echo "== encrypted library across regions"
c=$(code -X POST -H "$A" -H 'Content-Type: application/json' -d '{"repo_name":"xregion-enc","passwd":"s3cret-pass","encrypted":true}' $EU/api/v2.1/repos/); E=$(jq -r '.repo_id // .id // empty' /tmp/body)
if [ -n "$E" ]; then ok "create encrypted at EU"; else ko "create encrypted" "$c $(head -c 200 /tmp/body)"; fi
expect "unlock at EU" 200 -X POST -H "$A" -d 'password=s3cret-pass' "$EU/api/v2.1/repos/$E/set-password/"
c=$(upload $EU $E /tmp/f.bin e.bin); if [ "$c" = 200 ]; then ok "encrypted upload at EU"; else ko "encrypted upload" "$c $(head -c 200 /tmp/body)"; fi
c=$(code -H "$A" "$AS/api2/repos/$E/dir/?p=/"); echo "INFO  encrypted dir at Asia WITHOUT unlocking there: $c $(head -c 120 /tmp/body)"
expect "unlock at Asia" 200 -X POST -H "$A" -d 'password=s3cret-pass' "$AS/api/v2.1/repos/$E/set-password/"
wait_read $AS $E /e.bin $S1 "encrypted read at Asia"
c=$(code -X POST -H "$A" -d 'password=wrong-pass' "$NA/api/v2.1/repos/$E/set-password/"); case "$c" in 400|403) ok "wrong password refused at NA ($c)";; *) ko "wrong password at NA" "$c";; esac

echo "== share link across regions"
c=$(code -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"repo_id\":\"$ID\",\"path\":\"/b.bin\"}" $NA/api/v2.1/share-links/); TK=$(jq -r '.token // empty' /tmp/body); L=$(jq -r '.link // empty' /tmp/body)
echo "INFO  link: $L"
expect "share page at Asia" 200 "$AS/d/$TK"
curl -s -L -m 60 -o /tmp/s.bin "$AS/d/$TK?dl=1"; if [ "$(sha256sum /tmp/s.bin | cut -d' ' -f1)" = "$S2" ]; then ok "anonymous download at Asia of NA link"; else ko "anonymous download" "$(head -c 120 /tmp/s.bin)"; fi

echo "== trash and restore across regions"
expect "delete at Asia" 200 -X DELETE -H "$A" "$AS/api2/repos/$ID/file/?p=/b.bin"
sleep 2
expect "trash at NA" 200 -H "$A" "$NA/api/v2.1/repos/$ID/trash/"; if grep -q b.bin /tmp/body; then ok "deleted file in NA trash"; else ko "NA trash" "$(head -c 200 /tmp/body)"; fi
CID=$(jq -r '.data[0].commit_id // empty' /tmp/body)
expect "restore at EU" 200 -X POST -H "$A" -H 'Content-Type: application/json' -d "{\"commit_id\":\"$CID\",\"p\":\"/b.bin\"}" "$EU/api/v2.1/repos/$ID/file/restore/"
wait_read $NA $ID /b.bin $S2 "restored file read at NA"

echo "== library lifecycle across regions"
expect "soft delete at NA" 200 -X DELETE -H "$A" "$NA/api/v2.1/repos/$ID3/"
sleep 2
expect "restore at EU" 200 -X PUT -H "$A" "$EU/api/v2.1/repos/deleted/$ID3/"
expect "soft delete again at Asia" 200 -X DELETE -H "$A" "$AS/api/v2.1/repos/$ID3/"
sleep 2
expect "permanent delete at NA" "200 204" -X DELETE -H "$A" "$NA/api/v2.1/repos/deleted/$ID3/"
echo "$ID" > /tmp/keep-id
echo "RESULT pass=$PASS fail=$FAIL"
