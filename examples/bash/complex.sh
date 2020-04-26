#!/usr/bin/env bash
set -euo pipefail

_term() {
  echo "BASH received SIGTERM, exit 0"
  exit 0
}
trap "_term" TERM

echo "BASH example starting"
while true; do
  now="$(date)"
  echo "It's $now - try saving this file or adding a new file to this directory."

  sleep 1
done
