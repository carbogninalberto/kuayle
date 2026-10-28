import http.client
import json
import threading

import pytest

from kuayle_voice import audio, models, server
from kuayle_voice.catalog import ModelSpec, PinnedFile

TOKEN = "t" * 40
OGG = b"OggS" + b"\x00" * 60


class FakeManager:
    """Stands in for downloads and engines: no model files are needed."""

    def __init__(self, ready=("faster-whisper-base",), engine=None):
        self.ready = set(ready)
        self.requested = []
        self.engine_impl = engine or FakeEngine()

    def status(self):
        return {"loaded": None, "models": [{"id": m, "state": "ready" if m in self.ready else "absent"} for m in ("faster-whisper-base", "vosk-small-it-0.22")]}

    def state(self, model_id):
        return "ready" if model_id in self.ready else "absent"

    def request(self, model_id):
        self.requested.append(model_id)
        return self.status()

    def engine(self, model_id):
        if model_id not in self.ready:
            raise LookupError(model_id)
        return self.engine_impl


class FakeEngine:
    def __init__(self, text="hello world", gate=None):
        self.text, self.gate, self.calls = text, gate, []

    def transcribe(self, pcm, sample_rate):
        self.calls.append((len(pcm), sample_rate))
        if self.gate:
            self.gate.wait(5)
        return self.text


def fake_decoder(seconds=3.0):
    def decode(data, max_seconds):
        if seconds > max_seconds + 1:
            raise audio.TooLong()
        return b"\x00\x00" * int(audio.SAMPLE_RATE * seconds)

    return decode


@pytest.fixture
def running():
    servers = []

    def start(manager=None, decoder=None, **settings):
        app = server.App(server.Settings(token=TOKEN, models_dir=None, **settings), manager or FakeManager(), decoder or fake_decoder())
        srv = server.serve(app, host="127.0.0.1", port=0)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        servers.append(srv)
        return app, srv.server_address[1]

    yield start
    for srv in servers:
        srv.shutdown()
        srv.server_close()


def call(port, method, path, body=None, token=TOKEN, headers=None):
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    all_headers = {"Authorization": f"Bearer {token}"} if token else {}
    all_headers.update(headers or {})
    conn.request(method, path, body=body, headers=all_headers)
    response = conn.getresponse()
    data = response.read()
    conn.close()
    return response.status, (json.loads(data) if data else None)


def test_health_needs_no_token_and_reveals_nothing(running):
    _, port = running()
    assert call(port, "GET", "/health", token=None) == (200, {"ok": True})


@pytest.mark.parametrize("token", [None, "wrong", TOKEN + "x"])
def test_every_api_call_needs_the_internal_token(running, token):
    _, port = running()
    for method, path, body in (("GET", "/v1/status", None), ("POST", "/v1/models/faster-whisper-base", b""), ("POST", "/v1/transcribe?model=faster-whisper-base", OGG)):
        status, payload = call(port, method, path, body, token=token)
        assert status == 401
        assert payload["error"]["code"] == "unauthorized"


def test_status_reports_models_queue_and_limits(running):
    _, port = running()
    status, payload = call(port, "GET", "/v1/status")
    assert status == 200
    assert payload["max_seconds"] == 300
    assert payload["busy"] is False and payload["queued"] == 0
    assert {m["id"] for m in payload["models"]} >= {"faster-whisper-base"}


def test_model_download_request(running):
    manager = FakeManager()
    _, port = running(manager)
    assert call(port, "POST", "/v1/models/vosk-small-it-0.22", b"")[0] == 202
    assert manager.requested == ["vosk-small-it-0.22"]
    assert call(port, "POST", "/v1/models/../etc", b"")[0] == 404
    assert call(port, "POST", "/v1/models/whisper-huge", b"")[0] == 404


def test_transcribe_returns_text(running):
    engine = FakeEngine("ciao a tutti")
    _, port = running(FakeManager(engine=engine))
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG, headers={"Content-Type": "audio/ogg"})
    assert status == 200
    assert payload == {"text": "ciao a tutti", "duration_seconds": 3.0, "model": "faster-whisper-base"}
    assert engine.calls == [(audio.SAMPLE_RATE * 2 * 3, audio.SAMPLE_RATE)]


def test_transcribe_refuses_models_that_are_not_ready_or_unknown(running):
    _, port = running()
    status, payload = call(port, "POST", "/v1/transcribe?model=vosk-small-it-0.22", OGG)
    assert (status, payload["error"]["code"]) == (409, "model_not_ready")
    assert call(port, "POST", "/v1/transcribe?model=nope", OGG)[0] == 404
    assert call(port, "POST", "/v1/transcribe", OGG)[0] == 404


def test_transcribe_enforces_size_format_and_duration(running):
    _, port = running(max_bytes=1000)
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG + b"\x00" * 1000)
    assert (status, payload["error"]["code"]) == (413, "too_large")
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", b"<html>" + b"\x00" * 40)
    assert (status, payload["error"]["code"]) == (415, "unsupported_audio")

    _, long_port = running(decoder=fake_decoder(seconds=302))
    status, payload = call(long_port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)
    assert (status, payload["error"]["code"]) == (422, "too_long")


def test_undecodable_audio_is_unsupported(running):
    def broken(data, max_seconds):
        raise audio.AudioError("bad")

    _, port = running(decoder=broken)
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)
    assert (status, payload["error"]["code"]) == (415, "unsupported_audio")


def test_engine_failures_do_not_leak_details(running):
    class Exploding:
        def transcribe(self, pcm, rate):
            raise RuntimeError("/models/secret/path exploded")

    _, port = running(FakeManager(engine=Exploding()))
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)
    assert status == 500
    assert "secret" not in json.dumps(payload)


def test_one_transcription_at_a_time_with_a_bounded_queue(running):
    release = threading.Event()
    engine = FakeEngine(gate=release)
    app, port = running(FakeManager(engine=engine), queue=1, queue_timeout=10)
    results = []

    def submit():
        results.append(call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)[0])

    first = threading.Thread(target=submit)
    first.start()
    _wait(lambda: app.gate.active == 1)
    second = threading.Thread(target=submit)
    second.start()
    _wait(lambda: app.gate.queued == 1)
    # Running one and queueing one fills the gate: the next caller is told to retry.
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)
    assert (status, payload["error"]["code"]) == (503, "busy")
    assert len(engine.calls) == 1, "the queued request waits for the running one"
    release.set()
    first.join(10)
    second.join(10)
    assert results == [200, 200]
    assert app.gate.active == 0 and app.gate.queued == 0


def test_settings_require_a_long_token(monkeypatch):
    monkeypatch.setenv("VOICE_INTERNAL_TOKEN", "short")
    with pytest.raises(SystemExit):
        server.Settings.from_env()
    monkeypatch.setenv("VOICE_INTERNAL_TOKEN", TOKEN)
    monkeypatch.setenv("VOICE_MAX_SECONDS", "900")
    assert server.Settings.from_env().max_seconds == 300, "the limit can be lowered but never raised past 5 minutes"


def test_real_manager_and_server_together(running, tmp_path, monkeypatch):
    """The real ModelManager behind the HTTP API, with a fake engine."""
    spec = ModelSpec(id="faster-whisper-base", engine="faster-whisper", label="x", repo="Systran/x", revision="0" * 40, files=(PinnedFile("config.json", 2, "sha256", __import__("hashlib").sha256(b"{}").hexdigest()),))
    monkeypatch.setattr(models, "MODELS", {spec.id: spec})
    monkeypatch.setattr(server, "MODELS", {spec.id: spec})

    class Response(__import__("io").BytesIO):
        status = 200

    manager = models.ModelManager(tmp_path, lambda spec, path: FakeEngine("from disk"), opener=lambda url: Response(b"{}"))
    _, port = running(manager)
    assert call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)[0] == 409
    assert call(port, "POST", "/v1/models/faster-whisper-base", b"")[0] == 202
    manager.wait_idle(10)
    status, payload = call(port, "POST", "/v1/transcribe?model=faster-whisper-base", OGG)
    assert (status, payload["text"]) == (200, "from disk")


def _wait(predicate, timeout=5.0):
    import time

    deadline = time.monotonic() + timeout
    while not predicate():
        if time.monotonic() > deadline:
            raise AssertionError("condition not reached")
        time.sleep(0.01)
