#!/bin/sh
set -eu

curl --noproxy '*' -fsS http://127.0.0.1:3000/ >/dev/null
curl --noproxy '*' -fsS http://127.0.0.1:9222/json/version >/dev/null
