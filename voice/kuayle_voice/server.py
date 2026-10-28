"""Internal HTTP API for Kuayle's backend.

    GET  /health                 liveness, no authentication, no details
    GET  /v1/status              models, download progress, queue
    POST /v1/models/<id>         download (or retry) a pinned model
    POST /v1/transcribe?model=<id>
                                 raw audio body -> {"text", "duration_seconds"}

Every /v1 call needs `Authorization: Bearer $VOICE_INTERNAL_TOKEN`. The
service is meant for a private Compose network only. Audio is held in memory
and a private temporary file just long enough to decode it; neither audio nor
transcripts are logged or stored.
"""

from __future__ import annotations

import hmac
import json
import logging
import os
import threading
import time
from dataclasses import dataclass
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

from . import audio
from .catalog import MODELS
from .models import ModelManager

log = logging.getLogger("kuayle_voice")

MAX_SECONDS = 300


class Busy(Exception):
    pass


class Gate:
    """One transcription at a time, at most `waiting` more queued behind it."""

    def __init__(self, waiting: int, wait_timeout: float):
        self._slots = threading.BoundedSemaphore(1 + waiting)
        self._work = threading.Lock()
        self._wait_timeout = wait_timeout
        self._lock = threading.Lock()
        self.active = 0
        self.queued = 0

    def __enter__(self):
        if not self._slots.acquire(blocking=False):
            raise Busy()
        with self._lock:
            self.queued += 1
        acquired = self._work.acquire(timeout=self._wait_timeout)
        with self._lock:
            self.queued -= 1
            if acquired:
                self.active += 1
        if not acquired:
            self._slots.release()
            raise Busy()
        return self

    def __exit__(self, *exc):
        with self._lock:
            self.active -= 1
        self._work.release()
        self._slots.release()
        return False


@dataclass
class Settings:
    token: str
    models_dir: Path
    max_seconds: float = MAX_SECONDS
    max_bytes: int = 20 << 20
    queue: int = 4
    queue_timeout: float = 300.0

    @classmethod
    def from_env(cls) -> "Settings":
        token = os.environ.get("VOICE_INTERNAL_TOKEN", "")
        if len(token) < 32:
            raise SystemExit("VOICE_INTERNAL_TOKEN must be set to at least 32 characters")
        max_seconds = min(float(os.environ.get("VOICE_MAX_SECONDS", MAX_SECONDS)), MAX_SECONDS)
        return cls(
            token=token,
            models_dir=Path(os.environ.get("VOICE_MODELS_DIR", "/models")),
            max_seconds=max_seconds,
            max_bytes=int(os.environ.get("VOICE_MAX_BYTES", 20 << 20)),
            queue=int(os.environ.get("VOICE_QUEUE", 4)),
        )


class App:
    def __init__(self, settings: Settings, manager: ModelManager, decoder=audio.decode):
        self.settings = settings
        self.manager = manager
        self.gate = Gate(settings.queue, settings.queue_timeout)
        self.decode = decoder

    def authorized(self, header: str) -> bool:
        expected = "Bearer " + self.settings.token
        return hmac.compare_digest(header.encode(), expected.encode())

    def status(self) -> dict:
        result = self.manager.status()
        result.update(
            busy=self.gate.active > 0,
            queued=self.gate.queued,
            max_seconds=self.settings.max_seconds,
            max_bytes=self.settings.max_bytes,
        )
        return result

    def transcribe(self, model_id: str, data: bytes) -> tuple[int, dict]:
        if model_id not in MODELS:
            return error(HTTPStatus.NOT_FOUND, "unknown_model", "Unknown model")
        if self.manager.state(model_id) != "ready":
            return error(HTTPStatus.CONFLICT, "model_not_ready", "The model is not downloaded yet")
        if audio.sniff(data) is None:
            return error(HTTPStatus.UNSUPPORTED_MEDIA_TYPE, "unsupported_audio", "Unsupported audio format")
        started = time.monotonic()
        try:
            with self.gate:
                pcm = self.decode(data, self.settings.max_seconds)
                seconds = len(pcm) / audio.BYTES_PER_SECOND
                engine = self.manager.engine(model_id)
                text = engine.transcribe(pcm, audio.SAMPLE_RATE)
        except Busy:
            return error(HTTPStatus.SERVICE_UNAVAILABLE, "busy", "Transcription queue is full")
        except audio.TooLong:
            return error(HTTPStatus.UNPROCESSABLE_ENTITY, "too_long", f"Audio can be at most {self.settings.max_seconds:g} seconds")
        except audio.AudioError:
            return error(HTTPStatus.UNSUPPORTED_MEDIA_TYPE, "unsupported_audio", "The audio could not be decoded")
        except LookupError:
            return error(HTTPStatus.CONFLICT, "model_not_ready", "The model is not downloaded yet")
        except Exception:  # noqa: BLE001 - never leak engine internals
            log.exception("transcription failed")
            return error(HTTPStatus.INTERNAL_SERVER_ERROR, "failed", "Transcription failed")
        log.info("transcribed %.1fs of audio with %s in %.1fs", seconds, model_id, time.monotonic() - started)
        return HTTPStatus.OK, {"text": text, "duration_seconds": round(seconds, 2), "model": model_id}


def error(status: HTTPStatus, code: str, message: str) -> tuple[int, dict]:
    return status, {"error": {"code": code, "message": message}}


def make_handler(app: App):
    class Handler(BaseHTTPRequestHandler):
        server_version = "kuayle-voice"
        sys_version = ""
        timeout = 60
        protocol_version = "HTTP/1.1"

        def log_message(self, format, *args):  # noqa: A002 - stdlib signature
            # Paths carry no secrets, but keep the access log minimal.
            log.debug("%s %s", self.command, urlsplit(self.path).path)

        def _send(self, status: int, body: dict) -> None:
            raw = json.dumps(body).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(raw)

        def _authorized(self) -> bool:
            if app.authorized(self.headers.get("Authorization", "")):
                return True
            self._discard_body()
            self._send(*error(HTTPStatus.UNAUTHORIZED, "unauthorized", "Unauthorized"))
            return False

        def _discard_body(self) -> None:
            try:
                length = int(self.headers.get("Content-Length") or 0)
            except ValueError:
                length = 0
            if 0 < length <= app.settings.max_bytes:
                self.rfile.read(length)
            elif length:
                self.close_connection = True

        def do_GET(self):  # noqa: N802
            path = urlsplit(self.path).path
            if path == "/health":
                return self._send(HTTPStatus.OK, {"ok": True})
            if not self._authorized():
                return
            if path == "/v1/status":
                return self._send(HTTPStatus.OK, app.status())
            self._send(*error(HTTPStatus.NOT_FOUND, "not_found", "Not found"))

        def do_POST(self):  # noqa: N802
            if not self._authorized():
                return
            url = urlsplit(self.path)
            if url.path.startswith("/v1/models/"):
                self._discard_body()
                model_id = url.path.removeprefix("/v1/models/")
                if model_id not in MODELS:
                    return self._send(*error(HTTPStatus.NOT_FOUND, "unknown_model", "Unknown model"))
                return self._send(HTTPStatus.ACCEPTED, app.manager.request(model_id))
            if url.path != "/v1/transcribe":
                self._discard_body()
                return self._send(*error(HTTPStatus.NOT_FOUND, "not_found", "Not found"))
            model_id = parse_qs(url.query).get("model", [""])[0]
            raw_length = self.headers.get("Content-Length")
            if raw_length is None or self.headers.get("Transfer-Encoding"):
                self.close_connection = True
                return self._send(*error(HTTPStatus.LENGTH_REQUIRED, "length_required", "Content-Length is required"))
            try:
                length = int(raw_length)
            except ValueError:
                length = -1
            if length < 0 or length > app.settings.max_bytes:
                self.close_connection = True
                return self._send(*error(HTTPStatus.REQUEST_ENTITY_TOO_LARGE, "too_large", "Audio is too large"))
            data = self.rfile.read(length)
            if len(data) != length:
                self.close_connection = True
                return
            self._send(*app.transcribe(model_id, data))

    return Handler


def serve(app: App, host: str = "0.0.0.0", port: int = 8000) -> ThreadingHTTPServer:
    server = ThreadingHTTPServer((host, port), make_handler(app))
    server.daemon_threads = True
    return server


def main() -> None:
    logging.basicConfig(level=os.environ.get("VOICE_LOG_LEVEL", "INFO"), format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    from .engines import load_engine

    settings = Settings.from_env()
    manager = ModelManager(settings.models_dir, load_engine)
    server = serve(App(settings, manager), port=int(os.environ.get("VOICE_PORT", 8000)))
    log.info("voice service listening on port %d", server.server_address[1])
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
