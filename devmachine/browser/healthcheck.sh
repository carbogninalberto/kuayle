#!/bin/sh
set -eu

curl --noproxy '*' -fsS http://127.0.0.1:3000/ >/dev/null
set -- $(hostname -i)
container_ip=$1
curl --noproxy '*' -fsS "http://$container_ip:9222/json/version" >/dev/null
