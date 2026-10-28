"""Model storage: verified downloads, a sequential download queue and the
single loaded engine."""

from __future__ import annotations

import hashlib
import json
import logging
import shutil
import stat
import threading
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from collections import deque
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Iterable, Protocol

from .catalog import MODELS, ModelSpec, PinnedFile, host_allowed

log = logging.getLogger("kuayle_voice.models")

READY_MARKER = ".kuayle-ready.json"
CHUNK = 1 << 20


class DownloadError(Exception):
    pass


class Engine(Protocol):
    def transcribe(self, pcm: bytes, sample_rate: int) -> str: ...


EngineFactory = Callable[[ModelSpec, Path], Engine]


class _PublisherRedirects(urllib.request.HTTPRedirectHandler):
    """Follow redirects only to HTTPS URLs on the publishers' hosts."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        target = urllib.parse.urlsplit(urllib.parse.urljoin(req.full_url, newurl))
        if target.scheme != "https" or not host_allowed(target.hostname or ""):
            raise DownloadError("download redirected to an unexpected host")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def default_opener(url: str):
    parts = urllib.parse.urlsplit(url)
    if parts.scheme != "https" or not host_allowed(parts.hostname or ""):
        raise DownloadError("download URL is not an allowed publisher")
    opener = urllib.request.build_opener(_PublisherRedirects(), urllib.request.ProxyHandler({}))
    request = urllib.request.Request(url, headers={"User-Agent": "kuayle-voice"})
    return opener.open(request, timeout=60)


def _hasher(file: PinnedFile):
    if file.algorithm == "sha256":
        return hashlib.sha256()
    if file.algorithm == "git-sha1":
        h = hashlib.sha1()
        h.update(b"blob %d\0" % file.size)
        return h
    raise DownloadError(f"unknown digest algorithm {file.algorithm}")


@dataclass
class ModelState:
    state: str = "absent"  # absent | queued | downloading | ready | failed
    downloaded_bytes: int = 0
    error: str = ""


@dataclass
class ModelManager:
    root: Path
    engine_factory: EngineFactory
    opener: Callable[[str], object] = default_opener
    _states: dict[str, ModelState] = field(default_factory=dict)
    _queue: deque = field(default_factory=deque)
    _lock: threading.Lock = field(default_factory=threading.Lock)
    _wake: threading.Condition = field(init=False)
    _loaded_id: str | None = None
    _loaded: Engine | None = None
    _worker: threading.Thread | None = None

    def __post_init__(self) -> None:
        self.root = Path(self.root)
        self.root.mkdir(parents=True, exist_ok=True)
        self._wake = threading.Condition(self._lock)
        for spec in MODELS.values():
            ready = self._ready_marker_matches(spec)
            self._states[spec.id] = ModelState(state="ready" if ready else "absent", downloaded_bytes=spec.total_bytes if ready else 0)
        # Leftovers of interrupted downloads are never used.
        for leftover in self.root.glob(".partial-*"):
            shutil.rmtree(leftover, ignore_errors=True)

    # --- status -----------------------------------------------------------

    def status(self) -> dict:
        with self._lock:
            return {
                "loaded": self._loaded_id,
                "models": [
                    {
                        "id": spec.id,
                        "engine": spec.engine,
                        "label": spec.label,
                        "state": self._states[spec.id].state,
                        "downloaded_bytes": self._states[spec.id].downloaded_bytes,
                        "total_bytes": spec.total_bytes,
                        "error": self._states[spec.id].error,
                    }
                    for spec in MODELS.values()
                ],
            }

    def state(self, model_id: str) -> str:
        with self._lock:
            return self._states[model_id].state

    # --- downloads --------------------------------------------------------

    def request(self, model_id: str) -> dict:
        """Queue a model for download; ready, queued or downloading models are
        left as they are. A failed model is retried."""
        if model_id not in MODELS:
            raise KeyError(model_id)
        with self._lock:
            current = self._states[model_id]
            if current.state in ("absent", "failed"):
                self._states[model_id] = ModelState(state="queued")
                self._queue.append(model_id)
                self._ensure_worker()
                self._wake.notify_all()
        return self.status()

    def _ensure_worker(self) -> None:
        if self._worker is None or not self._worker.is_alive():
            self._worker = threading.Thread(target=self._run, name="model-downloads", daemon=True)
            self._worker.start()

    def _run(self) -> None:
        while True:
            with self._lock:
                if not self._queue:
                    self._worker = None
                    return
                model_id = self._queue.popleft()
                self._states[model_id] = ModelState(state="downloading")
            spec = MODELS[model_id]
            try:
                self._download(spec)
            except Exception as exc:  # noqa: BLE001 - reported in status
                message = str(exc) if isinstance(exc, DownloadError) else "download failed"
                log.warning("model %s download failed: %s", model_id, message)
                with self._lock:
                    self._states[model_id] = ModelState(state="failed", error=message)
            else:
                with self._lock:
                    self._states[model_id] = ModelState(state="ready", downloaded_bytes=spec.total_bytes)
                log.info("model %s ready", model_id)

    def wait_idle(self, timeout: float | None = None) -> None:
        """Test helper: wait for the download worker to finish."""
        worker = self._worker
        if worker is not None:
            worker.join(timeout)

    def _progress(self, model_id: str, done: int) -> None:
        with self._lock:
            self._states[model_id].downloaded_bytes = done

    def _download(self, spec: ModelSpec) -> None:
        partial = self.root / f".partial-{spec.id}"
        shutil.rmtree(partial, ignore_errors=True)
        partial.mkdir(parents=True)
        try:
            done = 0
            for file in spec.files:
                target = partial / file.name
                done = self._fetch(spec, file, target, done)
            if spec.engine == "vosk":
                archive = partial / spec.files[0].name
                extracted = partial / "model"
                _extract_zip(archive, spec.archive_root, extracted)
                archive.unlink()
                for child in extracted.iterdir():
                    child.rename(partial / child.name)
                extracted.rmdir()
            (partial / READY_MARKER).write_text(json.dumps(_marker(spec)))
            final = self.root / spec.id
            shutil.rmtree(final, ignore_errors=True)
            partial.rename(final)
        finally:
            shutil.rmtree(partial, ignore_errors=True)

    def _fetch(self, spec: ModelSpec, file: PinnedFile, target: Path, done: int) -> int:
        hasher = _hasher(file)
        size = 0
        try:
            response = self.opener(spec.url(file))
        except DownloadError:
            raise
        except (urllib.error.URLError, OSError) as exc:
            raise DownloadError(f"could not reach the model publisher for {file.name}") from exc
        with response, open(target, "wb") as out:
            status = getattr(response, "status", 200)
            if status != 200:
                raise DownloadError(f"the publisher answered {status} for {file.name}")
            while True:
                chunk = response.read(CHUNK)
                if not chunk:
                    break
                size += len(chunk)
                if size > file.size:
                    raise DownloadError(f"{file.name} is larger than its pinned size")
                hasher.update(chunk)
                out.write(chunk)
                self._progress(spec.id, done + size)
        if size != file.size:
            raise DownloadError(f"{file.name} has an unexpected size")
        if hasher.hexdigest() != file.digest:
            raise DownloadError(f"{file.name} failed its checksum")
        return done + size

    def _ready_marker_matches(self, spec: ModelSpec) -> bool:
        try:
            marker = json.loads((self.root / spec.id / READY_MARKER).read_text())
        except (OSError, ValueError):
            return False
        return marker == _marker(spec)

    # --- engines ----------------------------------------------------------

    def engine(self, model_id: str) -> Engine:
        """Return the engine for a ready model, loading it (and unloading the
        previous one) when needed. Callers serialize transcriptions."""
        with self._lock:
            if self._states.get(model_id, ModelState()).state != "ready":
                raise LookupError(model_id)
            if self._loaded_id == model_id and self._loaded is not None:
                return self._loaded
            self._loaded, self._loaded_id = None, None
        engine = self.engine_factory(MODELS[model_id], self.root / model_id)
        with self._lock:
            self._loaded, self._loaded_id = engine, model_id
        return engine


def _marker(spec: ModelSpec) -> dict:
    return {"id": spec.id, "files": [[f.name, f.size, f.algorithm, f.digest] for f in spec.files]}


def _extract_zip(archive: Path, root: str, destination: Path) -> None:
    """Extract regular files under the expected top-level directory only:
    no absolute paths, parent references, links or other special files."""
    destination.mkdir(parents=True)
    base = destination.resolve()
    with zipfile.ZipFile(archive) as zf:
        for info in zf.infolist():
            name = info.filename
            parts = Path(name).parts
            if not parts or parts[0] != root or name.startswith("/") or ".." in parts or "\\" in name:
                raise DownloadError("model archive has an unexpected layout")
            mode = info.external_attr >> 16
            if mode and stat.S_ISLNK(mode):
                raise DownloadError("model archive contains a link")
            relative = Path(*parts[1:]) if len(parts) > 1 else Path()
            target = (destination / relative).resolve()
            if target != base and base not in target.parents:
                raise DownloadError("model archive has an unexpected layout")
            if info.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            with zf.open(info) as src, open(target, "wb") as out:
                shutil.copyfileobj(src, out, CHUNK)


def model_ids() -> Iterable[str]:
    return MODELS.keys()


__all__ = ["ModelManager", "DownloadError", "Engine", "default_opener", "model_ids", "READY_MARKER"]

