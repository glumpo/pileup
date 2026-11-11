#!/bin/bash
set -uo pipefail
name=$1
shift
mkdir -p "$CI_LOGS"
start=$SECONDS
"$@" 2>&1 | tee "$CI_LOGS/$name.log"
code=${PIPESTATUS[0]}
echo "$name exit=$code seconds=$((SECONDS - start))" | tee -a "$CI_LOGS/steps"
exit "$code"
