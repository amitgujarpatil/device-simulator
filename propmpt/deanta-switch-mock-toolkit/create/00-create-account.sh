#!/usr/bin/env bash
# Create the mock (target) account. WRITES TO PROD — there is no account delete route.
#   INTANGLES_TOKEN=... MOCK_NAME=intangles-switch-test-deanta bash create/00-create-account.sh          # prints payload only
#   INTANGLES_TOKEN=... MOCK_NAME=intangles-switch-test-deanta APPLY=1 bash create/00-create-account.sh  # creates
# Profile fields are copied from DATA_DIR/accountV2.json (the source); stage is forced to poc.
# Deliberately NOT copied: account_managers, sales_users, address (decided for the Deanta clone).
set -uo pipefail
: "${INTANGLES_TOKEN:?set INTANGLES_TOKEN}"
: "${MOCK_NAME:?set MOCK_NAME (lowercase; must not already exist)}"
API="${INTANGLES_API:-https://apis.intangles-aws-eu-north-1.eu.intangles.com}"
D="${DATA_DIR:-$(cd "$(dirname "$0")/.." && pwd)/data/source-dump}"
H=(-H 'Intangles-Client: intangles_app' -H 'Intangles-Session-Type: web' -H "Intangles-User-Token: $INTANGLES_TOKEN" -H 'Accept: application/json')

BODY=$(MOCK_NAME="$MOCK_NAME" node -e '
const fs=require("fs");const a=JSON.parse(fs.readFileSync(process.argv[1]+"/accountV2.json","utf8")).result;
const pick=["type","payment_mode","region","country","timezone","currency","imperial_units","imperial_mileage","gallon_unit","operation_types","intangles_level","intangles_zone"];
const b={name:process.env.MOCK_NAME,display_name:process.env.MOCK_NAME,stage:"poc",fleet_size:Number(process.env.FLEET_SIZE||a.fleet_size||0)};
for(const k of pick) if(a[k]!==undefined&&a[k]!==null) b[k]=a[k];
console.log(JSON.stringify(b,null,1));' "$D")
echo "$BODY"

N=$(curl -s -m 40 "${H[@]}" "$API/account/listV2?&psize=50&pnum=1&status=*&query=$MOCK_NAME&lang=en" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const j=JSON.parse(s);console.log((j.accounts||[]).filter(a=>a.name===process.argv[1]).length)})' "$MOCK_NAME")
if [ "$N" != "0" ]; then echo "account named $MOCK_NAME already exists — not creating"; exit 1; fi
[ "${APPLY:-0}" = "1" ] || { echo "dry run — set APPLY=1 to create"; exit 0; }
curl -s -m 60 "${H[@]}" -H 'Content-Type: application/json' -X POST "$API/account/createV2" -d "$BODY" \
  | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const j=JSON.parse(s);console.log(JSON.stringify(j.status),"new account id:",j.result&&j.result.id)})'
# Note: the new account gets platform-default alert config, including default Intangles user lists on
# algo_output / def_chori / dtc / fuel_chori. Read-back via /accountV2 does not show country/timezone/currency (normal).
