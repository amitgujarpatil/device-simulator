#!/usr/bin/env bash
# Read-only: dump everything about a SOURCE account that a mock clone needs.
#   INTANGLES_TOKEN=... SOURCE_ACCOUNT=1482046680973967360 bash discovery/collect-account.sh
# Env: INTANGLES_API (default EU), SOURCE_ACCOUNT (default Deanta), DATA_DIR (default ./data/source-dump)
# Output contains customer data (users, emails, plates, IMEIs) — keep it internal, never commit it.
set -uo pipefail
: "${INTANGLES_TOKEN:?set INTANGLES_TOKEN (user token; sensitive, never commit)}"
API="${INTANGLES_API:-https://apis.intangles-aws-eu-north-1.eu.intangles.com}"
ACC="${SOURCE_ACCOUNT:-1482046680973967360}"
D="${DATA_DIR:-$(cd "$(dirname "$0")/.." && pwd)/data/source-dump}"
mkdir -p "$D"
H=(-H 'Intangles-Client: intangles_app' -H 'Intangles-User-Lang: en' -H 'Intangles-Session-Type: web' -H "Intangles-User-Token: $INTANGLES_TOKEN" -H 'Accept: application/json')
g() { code=$(curl -s -m 60 -w '%{http_code}' "${H[@]}" "$API$1" -o "$D/$2"); printf "%s %8s bytes  %s\n" "$code" "$(wc -c < "$D/$2" | tr -d ' ')" "$2"; }

g "/accountV2/$ACC"                                        accountV2.json            # account record incl. alert_config
g "/config/all/accountV2/$ACC"                             config-all-accountV2.json # UI config (pages, units, fleet_health ...)
g "/alertconfigV2/account/$ACC/all"                        alertconfigV2-account.json
g "/vehicle/getlist?&pnum=1&psize=500&lastloc=true&acc_id=$ACC&lang=en" vehicles.json   # vehicles in .v[], imei = t_imei
g "/idevice/listV2?acc_id=$ACC&psize=500&pnum=1"           idevices.json             # devices in .idevices[]
g "/geofence/listV2?acc_id=$ACC&pnum=1&psize=2000"         geofences.json            # geofences in .gf[] (not .result)
g "/v2/user/list?acc_id=$ACC&psize=200&pnum=1"             users.json
g "/group/listV2?acc_id=$ACC&psize=200&pnum=1"             groups.json
g "/driver/get?account_id=$ACC&psize=500&pnum=1"           drivers.json              # must be account_id= (other params return every account)
g "/routes/get?acc_id=$ACC&psize=200&pnum=1"               routes.json
g "/account/$ACC/specsV2?psize=200&pnum=1"                 account-specs.json
g "/schedule/definition?acc_id=$ACC&psize=200&pnum=1"      schedule-defs.json
g "/integration-connector/get-all/$ACC"                    integrations.json
# Known traps: /account/<id> and /alertconfig/account/<id>/all (v1) return 503 "apis.appacitive.com" in EU — use the V2 routes.
# /reminder/list and /subscription/list/v2 ignore the account filter — filter the results yourself.
echo "dump written to $D"
