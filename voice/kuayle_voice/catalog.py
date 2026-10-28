"""Pinned speech models: the only place their sources and hashes are listed.

Every model is downloaded from its publisher at a fixed revision and every
file is checked against the size and digest recorded here before use.

faster-whisper models come from the repositories faster-whisper itself maps
its model names to (faster_whisper/utils.py `_MODELS`). Hugging Face reports a
SHA-256 for Git LFS files and the Git blob SHA-1 for small files, so both are
verified exactly as the repository stores them. `large-v3-turbo` is listed by
faster-whisper as `mobiuslabsgmbh/faster-whisper-large-v3-turbo`; Hugging Face
now serves that repository as `dropbox-dash/faster-whisper-large-v3-turbo`
(same files, MIT licence), so the canonical id is pinned with its revision.

Vosk models come from https://alphacephei.com/vosk/models, which publishes no
checksums. The SHA-256 below was computed from the official archives when the
model was added, so a changed archive is refused rather than trusted.
"""

from __future__ import annotations

from dataclasses import dataclass

HF_BASE = "https://huggingface.co"
VOSK_BASE = "https://alphacephei.com/vosk/models"


@dataclass(frozen=True)
class PinnedFile:
    name: str
    size: int
    # "sha256" (content digest) or "git-sha1" (Git blob object id).
    algorithm: str
    digest: str


@dataclass(frozen=True)
class ModelSpec:
    id: str
    engine: str  # "faster-whisper" or "vosk"
    label: str
    files: tuple[PinnedFile, ...]
    repo: str = ""
    revision: str = ""
    archive_url: str = ""
    # For Vosk archives: the single top-level directory inside the zip.
    archive_root: str = ""

    @property
    def total_bytes(self) -> int:
        return sum(f.size for f in self.files)

    def url(self, file: PinnedFile) -> str:
        if self.engine == "vosk":
            return self.archive_url
        return f"{HF_BASE}/{self.repo}/resolve/{self.revision}/{file.name}"


def _whisper(id: str, label: str, repo: str, revision: str, files: tuple[PinnedFile, ...]) -> ModelSpec:
    return ModelSpec(id=id, engine="faster-whisper", label=label, repo=repo, revision=revision, files=files)


_TOKENIZER = PinnedFile("tokenizer.json", 2203239, "git-sha1", "7818adb6de9fa3064d3ff81226fdd675be1f6344")
_VOCABULARY = PinnedFile("vocabulary.txt", 459861, "git-sha1", "c9074644d9d1205686f16d411564729461324b75")

MODELS: dict[str, ModelSpec] = {
    spec.id: spec
    for spec in (
        _whisper(
            "faster-whisper-base",
            "faster-whisper base",
            "Systran/faster-whisper-base",
            "ebe41f70d5b6dfa9166e2c581c45c9c0cfc57b66",
            (
                PinnedFile("config.json", 2309, "git-sha1", "867cf1a0fece1394e01d55e287ba2f09a577c046"),
                PinnedFile("model.bin", 145217532, "sha256", "d01c3014881c9c6f3133c182f3d2887eb6ca1c789a7538c5c007196857a0a6a9"),
                _TOKENIZER,
                _VOCABULARY,
            ),
        ),
        _whisper(
            "faster-whisper-small",
            "faster-whisper small",
            "Systran/faster-whisper-small",
            "536b0662742c02347bc0e980a01041f333bce120",
            (
                PinnedFile("config.json", 2370, "git-sha1", "e5047537059bd8f182d9ca64c470201585015187"),
                PinnedFile("model.bin", 483546902, "sha256", "3e305921506d8872816023e4c273e75d2419fb89b24da97b4fe7bce14170d671"),
                _TOKENIZER,
                _VOCABULARY,
            ),
        ),
        _whisper(
            "faster-whisper-medium",
            "faster-whisper medium",
            "Systran/faster-whisper-medium",
            "08e178d48790749d25932bbc082711ddcfdfbc4f",
            (
                PinnedFile("config.json", 2257, "git-sha1", "242aa06a480a7b5509375c645097e87af5136774"),
                PinnedFile("model.bin", 1527906378, "sha256", "9b45e1009dcc4ab601eff815b61d80e60ce3fd8c74c1a14f4a282258286b51ae"),
                _TOKENIZER,
                _VOCABULARY,
            ),
        ),
        _whisper(
            "faster-whisper-large-v3-turbo",
            "faster-whisper large-v3-turbo",
            "dropbox-dash/faster-whisper-large-v3-turbo",
            "0a363e9161cbc7ed1431c9597a8ceaf0c4f78fcf",
            (
                PinnedFile("config.json", 2263, "git-sha1", "0351d1d6870005e865747b781b5d7c23ea0459cd"),
                PinnedFile("model.bin", 1617884929, "sha256", "e76620f83d5f5b69efd3d87e3dc180c1bd21df9fbebacfd4335e5e1efcc018da"),
                PinnedFile("preprocessor_config.json", 340, "git-sha1", "931c77a740890c46365c7ae0c9d350ba3cca908f"),
                PinnedFile("tokenizer.json", 2710337, "git-sha1", "17456db595adc78a973f97d69d8cb50bc87c0b1c"),
                PinnedFile("vocabulary.json", 1068114, "git-sha1", "0adcd01e7c237205d593b707e66dd5d7bc785d2d"),
            ),
        ),
        ModelSpec(
            id="vosk-small-en-us-0.15",
            engine="vosk",
            label="Vosk small English",
            archive_url=f"{VOSK_BASE}/vosk-model-small-en-us-0.15.zip",
            archive_root="vosk-model-small-en-us-0.15",
            files=(PinnedFile("vosk-model-small-en-us-0.15.zip", 41205931, "sha256", "30f26242c4eb449f948e42cb302dd7a686cb29a3423a8367f99ff41780942498"),),
        ),
        ModelSpec(
            id="vosk-small-it-0.22",
            engine="vosk",
            label="Vosk small Italian",
            archive_url=f"{VOSK_BASE}/vosk-model-small-it-0.22.zip",
            archive_root="vosk-model-small-it-0.22",
            files=(PinnedFile("vosk-model-small-it-0.22.zip", 49665141, "sha256", "9ec65e75861d1c6c2e457cccd932705340dcdf233f5b239f00733b4de0bf3267"),),
        ),
    )
}

# Downloads may only be served from the publishers' own hosts. Hugging Face
# redirects LFS files to its CDN/Xet storage under hf.co / huggingface.co.
ALLOWED_HOSTS = ("huggingface.co", "hf.co", "alphacephei.com")


def host_allowed(host: str) -> bool:
    host = (host or "").lower().rstrip(".")
    return any(host == allowed or host.endswith("." + allowed) for allowed in ALLOWED_HOSTS)
