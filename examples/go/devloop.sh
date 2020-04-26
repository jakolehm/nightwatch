#!/usr/bin/env bash
set -euo pipefail

_term() {
  echo "bye"

  exit 0
}

trap _term TERM INT

while true; do
  echo "loop.sh devloop starting"
  set +e
    nightwatch --debug \
      --exit-on-change 255 \
      --exit-on-success 0 \
      --exit-on-error 0 \
      go run main.go

    nightwatch_exit=$?
  set -e

  case $nightwatch_exit in
    255)
      echo "loop.sh change detected"
    ;;
    0)
      echo "loop.sh clean exit"
      exit 0
    ;;
    *)
      echo "loop.sh nightwatch exited with $nightwatch_exit"
    ;;
  esac

  sleep 1
done
