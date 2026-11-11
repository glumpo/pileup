#!/bin/bash
set -euo pipefail
app=$1
sim=$2
out=$3
mkdir -p "$out"
id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Info.plist")
xcrun simctl boot "$sim" || true
xcrun simctl bootstatus "$sim" -b
xcrun simctl install "$sim" "$app"

core_port() {
  for _ in $(seq 60); do
    port=$(lsof -nP -a -p "$1" -iTCP@127.0.0.1 -sTCP:LISTEN -Fn | sed -n 's/^n127\.0\.0\.1://p' | head -1)
    if [ -n "$port" ]; then
      echo "$port"
      return
    fi
    sleep 1
  done
  return 1
}

health() {
  curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/health"
}

for run in 1 2; do
  pid=$(xcrun simctl launch "$sim" "$id" | awk '{print $NF}')
  port=$(core_port "$pid")
  code=$(health "$port")
  echo "launch $run: pid $pid, core port $port, /health without token: $code"
  [ "$code" = 401 ]
  sleep 10
  kill -0 "$pid"
  xcrun simctl io "$sim" screenshot "$out/launch-$run.png"
  xcrun simctl terminate "$sim" "$id"
  sleep 2
  if code=$(health "$port"); then
    echo "port $port still answers after terminate: $code"
    exit 1
  fi
done
echo smoke ok
