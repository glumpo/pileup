#!/bin/bash
set -euo pipefail
job=$1
mkdir -p "$CI_LOGS"
echo "$2" > "$CI_LOGS/status"
echo "$GITHUB_REF_NAME $GITHUB_SHA run $GITHUB_RUN_ID attempt $GITHUB_RUN_ATTEMPT" > "$CI_LOGS/run"
dest=$GITHUB_SHA/$GITHUB_RUN_NUMBER.$GITHUB_RUN_ATTEMPT/$job
header=$(git config --get http.https://github.com/.extraheader)
work=$(mktemp -d)
cd "$work"
git init -q
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"
git config http.https://github.com/.extraheader "$header"
git remote add origin "https://github.com/$GITHUB_REPOSITORY"
for try in $(seq 10); do
  if git fetch -q --depth=1 origin ci-logs; then
    git reset -q --hard FETCH_HEAD
  fi
  rm -rf "$dest"
  mkdir -p "$dest"
  cp -R "$CI_LOGS"/. "$dest"
  find "$dest" -name '*.log' -size +1000k -exec sh -c 'tail -c 1000000 "$1" > "$1.tail" && mv "$1.tail" "$1"' _ {} \;
  git add -A
  git commit -qm "$dest $2"
  if git push -q origin HEAD:ci-logs; then
    exit 0
  fi
  sleep $((try * 3 + RANDOM % 5))
done
exit 1
