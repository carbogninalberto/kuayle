"""Speech engines. Imports are deferred so tests run without the libraries."""

from __future__ import annotations

import json
import os
from pathlib import Path

from .catalog import ModelSpec


class FasterWhisperEngine:
    """faster-whisper on CPU with int8 weights; the language is detected."""

    def __init__(self, path: Path, threads: int = 0):
        from faster_whisper import WhisperModel

        self._model = WhisperModel(str(path), device="cpu", compute_type="int8", cpu_threads=threads, local_files_only=True)

    def transcribe(self, pcm: bytes, sample_rate: int) -> str:
        import numpy as np

        audio = np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0
        segments, _ = self._model.transcribe(audio, beam_size=5, vad_filter=True, condition_on_previous_text=False)
        return " ".join(segment.text.strip() for segment in segments).strip()


class VoskEngine:
    """Vosk (Kaldi) for one language."""

    def __init__(self, path: Path):
        import vosk

        vosk.SetLogLevel(-1)
        self._vosk = vosk
        self._model = vosk.Model(str(path))

    def transcribe(self, pcm: bytes, sample_rate: int) -> str:
        recognizer = self._vosk.KaldiRecognizer(self._model, sample_rate)
        parts: list[str] = []
        step = sample_rate * 2 // 2  # half a second of s16le audio
        for offset in range(0, len(pcm), step):
            if recognizer.AcceptWaveform(pcm[offset : offset + step]):
                parts.append(json.loads(recognizer.Result()).get("text", ""))
        parts.append(json.loads(recognizer.FinalResult()).get("text", ""))
        return " ".join(part for part in parts if part).strip()


def load_engine(spec: ModelSpec, path: Path):
    if spec.engine == "faster-whisper":
        return FasterWhisperEngine(path, threads=int(os.environ.get("VOICE_CPU_THREADS", "0") or 0))
    if spec.engine == "vosk":
        return VoskEngine(path)
    raise ValueError(f"unknown engine {spec.engine}")
