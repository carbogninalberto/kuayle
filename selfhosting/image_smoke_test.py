#!/usr/bin/env python3
"""Exercise the built release image, using disposable containers only.

Usage: python3 selfhosting/image_smoke_test.py IMAGE {all-in-one,api,web}
The web-only image uses a Caddy upstream fixture; API and all-in-one images
run their real server and migrations against PostgreSQL 17 and Redis 7.
"""

import json
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


def request(base, path, data=None):
    payload = None if data is None else json.dumps(data).encode()
    req = urllib.request.Request(
        base + path, data=payload, headers={"Content-Type": "application/json"}
    )
    try:
        response = urllib.request.urlopen(req, timeout=5)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        return response.status, response.headers, response.read()


def main():
    image, kind = sys.argv[1:]
    assert kind in ("all-in-one", "api", "web"), kind
    name = "kuayle-smoke-" + uuid.uuid4().hex[:12]
    containers = []
    network_created = False
    with tempfile.TemporaryDirectory(prefix=name) as directory:
        def run(suffix, *args):
            container = name + "-" + suffix
            containers.append(container)
            docker("run", "-d", "--name", container, "--network", name, *args)
            return container

        try:
            docker("network", "create", name)
            network_created = True
            if kind == "web":
                fixture = Path(directory) / "Caddyfile"
                fixture.write_text('''{
    admin off
}
:8080 {
    header Content-Type application/json
    respond /health `{"status":"ok"}`
    respond /ready `{"status":"ready"}`
    respond `{"upstream":true,"path":"{uri}","method":"{method}"}`
}
''')
                run("upstream", "--network-alias", "upstream", "-v",
                    f"{fixture}:/etc/caddy/fixture:ro", "--entrypoint", "caddy",
                    image, "run", "--config", "/etc/caddy/fixture", "--adapter", "caddyfile")
                server = run("web", "-p", "127.0.0.1::3000", "-e",
                             "API_URL=http://upstream:8080", image)
            else:
                database = run("db", "--network-alias", "db",
                               "-e", "POSTGRES_PASSWORD=smoke", "-e", "POSTGRES_DB=smoke",
                               "postgres:17-alpine")
                run("redis", "--network-alias", "redis", "redis:7-alpine")
                for _ in range(90):
                    ready = subprocess.run(
                        # The entrypoint's temporary init server uses only a Unix
                        # socket. Wait for TCP so migrations cannot race initdb.
                        ["docker", "exec", database, "pg_isready", "-h", "127.0.0.1",
                         "-U", "postgres", "-d", "smoke"],
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
                    )
                    if ready.returncode == 0:
                        break
                    time.sleep(1)
                else:
                    raise AssertionError("PostgreSQL did not become ready")
                env = ["-e", "DATABASE_URL=postgres://postgres:smoke@db:5432/smoke?sslmode=disable",
                       "-e", "REDIS_URL=redis://redis:6379",
                       "-e", "JWT_SECRET=disposable-image-smoke-secret-32-characters",
                       "-e", "ENVIRONMENT=production"]
                print(docker("run", "--rm", "--network", name, *env,
                             "--entrypoint", "/app/server", image, "migrate", "up"))
                schema = docker("exec", database, "psql", "-U", "postgres", "-d", "smoke", "-Atc",
                                "select version || ':' || dirty from schema_migrations")
                migrations = Path(__file__).resolve().parents[1] / "BE" / "migrations"
                expected_version = max(int(p.name.split("_", 1)[0]) for p in migrations.glob("*.up.sql"))
                assert schema == f"{expected_version}:false", schema
                print(f"PASS packaged migrations reach {expected_version}, clean")
                port = "8080" if kind == "api" else "3000"
                server = run("server", "-p", f"127.0.0.1::{port}", *env, image)

            port = "8080" if kind == "api" else "3000"
            address = docker("port", server, port + "/tcp").splitlines()[0]
            base = "http://" + address
            for _ in range(120):
                try:
                    status, headers, body = request(base, "/ready")
                    if status == 200:
                        assert "application/json" in headers.get("Content-Type", ""), (status, body[:100])
                        assert json.loads(body) == {"status": "ready"}, body
                        break
                except (OSError, ValueError):
                    pass
                time.sleep(1)
            else:
                raise AssertionError("Image never served JSON readiness through its public port")
            for route, expected in (("/health", "ok"), ("/ready", "ready")):
                status, headers, body = request(base, route)
                assert status == 200 and json.loads(body) == {"status": expected}, (route, status, body)
                assert "application/json" in headers.get("Content-Type", ""), headers
                print("PASS", route, "JSON from backend")

            if kind == "web":
                for route, data in (("/api/auth/register?probe=1", {}), ("/uploads/missing.png", None)):
                    status, _, body = request(base, route, data)
                    assert status == 200 and json.loads(body) == {
                        "upstream": True, "path": route, "method": "POST" if data is not None else "GET"
                    }, (status, body)
                    print("PASS upstream receives original path and method:", route)
            else:
                status, _, body = request(base, "/api/auth/register", {
                    "name": "Image Smoke", "email": "image-smoke@example.test",
                    "password": "Disposable-test-password-61!"
                })
                assert status == 201 and json.loads(body).get("id"), (status, body)
                print("PASS API POST registration reaches real backend")
                status, headers, body = request(base, "/uploads/missing.png")
                assert status in (401, 403, 404) and "text/html" not in headers.get("Content-Type", ""), (status, body)
                print("PASS uploads do not fall through to SPA")

            if kind != "api":
                for route in ("/", "/smoke-workspace/issues"):
                    status, headers, body = request(base, route)
                    assert status == 200 and "text/html" in headers.get("Content-Type", ""), (route, status)
                    assert b"<!doctype html>" in body.lower(), body[:100]
                    assert headers.get("X-Content-Type-Options") == "nosniff", headers
                    print("PASS SPA root/deep-link and security header:", route)
                status, headers, body = request(base, "/favicon.svg")
                assert status == 200 and "image/svg+xml" in headers.get("Content-Type", ""), (status, headers)
                print("PASS static asset is served without SPA rewrite")
            print("PASS", kind, docker("image", "inspect", "--format", "{{.Id}}", image))
        except BaseException:
            for container in containers:
                subprocess.run(["docker", "logs", "--tail", "60", container], check=False)
            raise
        finally:
            for container in reversed(containers):
                subprocess.run(["docker", "rm", "-f", "-v", container],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            if network_created:
                subprocess.run(["docker", "network", "rm", name],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)


if __name__ == "__main__":
    main()
