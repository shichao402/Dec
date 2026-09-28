#!/bin/bash
# Backfill cronkit stable channel index on the publish host.
# cronkit clients (all versions) run channel=stable, entry raw.firoyang.com.
# Baseline note: COS old cronkit/stable index seq = 4 (verified from workstation).
# The staged tree is thin (no local artifacts); store already holds the bytes.
set -euo pipefail

CLI=/usr/local/bin/relkit
CFG=/etc/relkit-agent/products/cronkit.json
ROOT=/srv/relkit/cronkit
MAT="$ROOT/dist-backfill"
V=0.1.0+14

echo "== 1. materialize artifacts from store =="
mkdir -p "$MAT"
BASE=https://publish.firoyang.com/artifact/cronkit/$V
curl -sfm 300 -o "$MAT/cronkit-$V-win-x64-setup.exe" $BASE/cronkit-$V-win-x64-setup.exe
curl -sfm 300 -o "$MAT/cronkit-$V-windows-x64-relkit-payload.zip" $BASE/cronkit-$V-windows-x64-relkit-payload.zip
sha256sum "$MAT"/*

echo "== 2. stage onto channel stable =="
cd "$ROOT"
"$CLI" stage --config "$CFG" "$V" --channel stable --code 14 --link \
  --install "dist-backfill/cronkit-$V-win-x64-setup.exe" "os=windows,arch=x64" \
  --install "dist-backfill/cronkit-$V-windows-x64-relkit-payload.zip" "os=windows,arch=x64,apply=relkit-payload,kind=blob"

echo "== 3. dry-run =="
"$CLI" publish --config "$CFG" "$V" --to serve --dry-run

echo "== 4. publish =="
"$CLI" publish --config "$CFG" "$V" --to serve

echo "== 5. verify online =="
curl -sm 10 -o /tmp/store_cronkit_stable_after.pb -w 'GET store stable: %{http_code}\n' https://publish.firoyang.com/index/cronkit/stable.pb
"$CLI" inspect --config "$CFG" --file /tmp/store_cronkit_stable_after.pb --raw | python3 -c "import sys,json,base64; d=json.load(sys.stdin); raw=base64.b64decode(d['payload']); open('/tmp/cronkit_stable_store_inner.pb','wb').write(raw)"
"$CLI" inspect --config "$CFG" --file /tmp/cronkit_stable_store_inner.pb --raw | python3 -c "import sys,json; d=json.load(sys.stdin); print('store cronkit/stable seq:', d.get('sequence'), '| channel:', d.get('channel')); print('versions:', [(v.get('version'),v.get('code')) for v in d.get('versions',[])])"

echo "== done =="
