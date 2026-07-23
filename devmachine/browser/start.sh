#!/bin/sh
set -eu

mkdir -p /tmp/.X11-unix "$HOME/browser-profile"

Xvnc :1 \
  -geometry 1440x900 -depth 24 \
  -interface 0.0.0.0 -websocketPort 3000 \
  -httpd /usr/share/kasmvnc/www \
  -SecurityTypes None -disableBasicAuth -sslOnly 0 \
  -PublicIP 127.0.0.1 -FrameRate 30 -RectThreads 0 -FreeKeyMappings &
xvnc_pid=$!

attempt=0
until xdpyinfo -display :1 >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 100 ] || { echo "KasmVNC display did not start" >&2; exit 1; }
  sleep 0.1
done

openbox &
openbox_pid=$!

if command -v google-chrome-stable >/dev/null 2>&1; then
  browser=google-chrome-stable
else
  browser=chromium
fi

"$browser" --no-sandbox --disable-background-networking --no-first-run --no-default-browser-check \
  --remote-debugging-address=127.0.0.1 --remote-debugging-port=9222 \
  --user-data-dir="$HOME/browser-profile" --start-maximized about:blank &
browser_pid=$!

attempt=0
until curl --noproxy '*' -fsS http://127.0.0.1:9222/json/version >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 300 ] || { echo "Browser debugging endpoint did not start" >&2; exit 1; }
  sleep 0.1
done

set -- $(hostname -i)
container_ip=$1
KUAYLE_BROWSER_CDP_LISTEN="$container_ip:9222" kuayle-browser-cdp-proxy &
cdp_proxy_pid=$!

cleanup() {
  kill "$xvnc_pid" "$openbox_pid" "$browser_pid" "$cdp_proxy_pid" 2>/dev/null || true
  wait "$xvnc_pid" "$openbox_pid" "$browser_pid" "$cdp_proxy_pid" 2>/dev/null || true
}
trap cleanup TERM INT EXIT

while kill -0 "$xvnc_pid" 2>/dev/null \
  && kill -0 "$openbox_pid" 2>/dev/null \
  && kill -0 "$browser_pid" 2>/dev/null \
  && kill -0 "$cdp_proxy_pid" 2>/dev/null; do
  sleep 1
done

exit 1
