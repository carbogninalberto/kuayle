import hashlib
import io
import zipfile

import pytest

from kuayle_voice import catalog, models
from kuayle_voice.catalog import ModelSpec, PinnedFile


class FakeResponse(io.BytesIO):
    status = 200


def git_sha1(data: bytes) -> str:
    return hashlib.sha1(b"blob %d\0" % len(data) + data).hexdigest()


def whisper_spec(config: bytes, weights: bytes, weights_digest: str | None = None) -> ModelSpec:
    return ModelSpec(
        id="fake-whisper",
        engine="faster-whisper",
        label="Fake",
        repo="Systran/fake",
        revision="0" * 40,
        files=(
            PinnedFile("config.json", len(config), "git-sha1", git_sha1(config)),
            PinnedFile("model.bin", len(weights), "sha256", weights_digest or hashlib.sha256(weights).hexdigest()),
        ),
    )


def vosk_spec(archive: bytes, root="vosk-model-fake") -> ModelSpec:
    return ModelSpec(
        id="fake-vosk",
        engine="vosk",
        label="Fake Vosk",
        archive_url="https://alphacephei.com/vosk/models/fake.zip",
        archive_root=root,
        files=(PinnedFile("fake.zip", len(archive), "sha256", hashlib.sha256(archive).hexdigest()),),
    )


def manager_for(tmp_path, monkeypatch, spec: ModelSpec, files: dict[str, bytes], engine_factory=None):
    monkeypatch.setattr(models, "MODELS", {spec.id: spec})
    requested = []

    def opener(url):
        requested.append(url)
        return FakeResponse(files[url])

    manager = models.ModelManager(tmp_path, engine_factory or (lambda spec, path: object()), opener=opener)
    return manager, requested


def test_pinned_catalog_covers_the_offered_models():
    assert set(catalog.MODELS) == {
        "faster-whisper-base",
        "faster-whisper-small",
        "faster-whisper-medium",
        "faster-whisper-large-v3-turbo",
        "vosk-small-en-us-0.15",
        "vosk-small-it-0.22",
    }
    for spec in catalog.MODELS.values():
        for file in spec.files:
            url = spec.url(file)
            assert url.startswith("https://")
            assert catalog.host_allowed(url.split("/")[2])
            assert file.algorithm in ("sha256", "git-sha1")
            assert len(file.digest) == (64 if file.algorithm == "sha256" else 40)
        if spec.engine == "faster-whisper":
            assert len(spec.revision) == 40, "Hugging Face downloads are pinned to a commit"
            assert {"config.json", "model.bin", "tokenizer.json"} <= {f.name for f in spec.files}


def test_only_publisher_hosts_are_allowed():
    assert catalog.host_allowed("huggingface.co")
    assert catalog.host_allowed("us.aws.cdn.hf.co")
    assert catalog.host_allowed("alphacephei.com")
    assert not catalog.host_allowed("huggingface.co.evil.test")
    assert not catalog.host_allowed("evilhf.co")
    with pytest.raises(models.DownloadError):
        models.default_opener("http://huggingface.co/x")
    with pytest.raises(models.DownloadError):
        models.default_opener("https://example.com/model.bin")


def test_verified_whisper_download_becomes_ready_and_survives_restart(tmp_path, monkeypatch):
    config, weights = b'{"a": 1}', b"weights" * 1000
    spec = whisper_spec(config, weights)
    files = {spec.url(f): data for f, data in zip(spec.files, (config, weights))}
    manager, requested = manager_for(tmp_path, monkeypatch, spec, files)
    assert manager.status()["models"][0]["state"] == "absent"
    manager.request(spec.id)
    manager.wait_idle(10)
    status = manager.status()["models"][0]
    assert status["state"] == "ready"
    assert status["downloaded_bytes"] == status["total_bytes"] == len(config) + len(weights)
    assert (tmp_path / spec.id / "model.bin").read_bytes() == weights
    assert requested == [f"https://huggingface.co/Systran/fake/resolve/{'0' * 40}/config.json", f"https://huggingface.co/Systran/fake/resolve/{'0' * 40}/model.bin"]
    # Asking again does not download again; a restart sees the model as ready.
    manager.request(spec.id)
    manager.wait_idle(10)
    assert len(requested) == 2
    restarted = models.ModelManager(tmp_path, lambda spec, path: object(), opener=lambda url: pytest.fail("no download"))
    assert restarted.state(spec.id) == "ready"


def test_checksum_mismatch_fails_and_leaves_nothing_usable(tmp_path, monkeypatch):
    config, weights = b"{}", b"tampered"
    spec = whisper_spec(config, weights, weights_digest=hashlib.sha256(b"original").hexdigest())
    files = {spec.url(f): data for f, data in zip(spec.files, (config, weights))}
    manager, _ = manager_for(tmp_path, monkeypatch, spec, files)
    manager.request(spec.id)
    manager.wait_idle(10)
    status = manager.status()["models"][0]
    assert status["state"] == "failed"
    assert "checksum" in status["error"]
    assert not (tmp_path / spec.id).exists()
    assert not list(tmp_path.glob(".partial-*"))
    with pytest.raises(LookupError):
        manager.engine(spec.id)


def test_oversized_download_is_refused(tmp_path, monkeypatch):
    config = b"{}"
    spec = whisper_spec(config, b"1234")
    files = {spec.url(spec.files[0]): config, spec.url(spec.files[1]): b"12345678"}
    manager, _ = manager_for(tmp_path, monkeypatch, spec, files)
    manager.request(spec.id)
    manager.wait_idle(10)
    assert manager.state(spec.id) == "failed"


def _zip(entries: dict[str, bytes]) -> bytes:
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w") as zf:
        for name, data in entries.items():
            zf.writestr(name, data)
    return buffer.getvalue()


def test_vosk_archive_is_verified_and_extracted(tmp_path, monkeypatch):
    archive = _zip({"vosk-model-fake/am/final.mdl": b"am", "vosk-model-fake/conf/model.conf": b"conf"})
    spec = vosk_spec(archive)
    manager, _ = manager_for(tmp_path, monkeypatch, spec, {spec.archive_url: archive})
    manager.request(spec.id)
    manager.wait_idle(10)
    assert manager.state(spec.id) == "ready"
    assert (tmp_path / spec.id / "am" / "final.mdl").read_bytes() == b"am"
    assert not (tmp_path / spec.id / "fake.zip").exists(), "the archive is removed after extraction"


@pytest.mark.parametrize(
    "entries",
    [
        {"vosk-model-fake/../../escape": b"x"},
        {"other-root/am/final.mdl": b"x"},
        {"/etc/passwd": b"x"},
    ],
)
def test_vosk_archive_with_unexpected_paths_is_refused(tmp_path, monkeypatch, entries):
    archive = _zip(entries)
    spec = vosk_spec(archive)
    manager, _ = manager_for(tmp_path, monkeypatch, spec, {spec.archive_url: archive})
    manager.request(spec.id)
    manager.wait_idle(10)
    assert manager.state(spec.id) == "failed"
    assert not (tmp_path.parent / "escape").exists()


def test_one_engine_is_loaded_at_a_time(tmp_path, monkeypatch):
    first, second = whisper_spec(b"{}", b"a"), whisper_spec(b"{}", b"b")
    second = ModelSpec(**{**second.__dict__, "id": "fake-whisper-2", "repo": "Systran/fake2"})
    monkeypatch.setattr(models, "MODELS", {first.id: first, second.id: second})
    loads = []
    files = {first.url(f): d for f, d in zip(first.files, (b"{}", b"a"))}
    manager = models.ModelManager(tmp_path, lambda spec, path: loads.append(spec.id) or spec.id, opener=lambda url: FakeResponse(files[url]))
    files.update({second.url(f): d for f, d in zip(second.files, (b"{}", b"b"))})
    manager.request(first.id)
    manager.request(second.id)
    manager.wait_idle(10)
    assert manager.engine(first.id) == first.id
    assert manager.engine(first.id) == first.id
    assert manager.engine(second.id) == second.id
    assert loads == [first.id, second.id]
    assert manager.status()["loaded"] == second.id
