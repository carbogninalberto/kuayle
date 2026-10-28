"""Audio intake: recognise the container, decode with ffmpeg to 16 kHz mono
PCM and enforce the duration limit on the decoded audio itself (browser
recordings often carry no duration in their headers)."""

from __future__ import annotations

import os
import subprocess
import tempfile

SAMPLE_RATE = 16000
BYTES_PER_SECOND = SAMPLE_RATE * 2  # s16le mono

# Demuxers ffmpeg may use. Anything else (playlists, images, devices, ...) is
# refused before decoding even if the first bytes look acceptable.
DEMUXERS = "ogg,matroska,webm,mov,mp4,m4a,3gp,mp3,wav,aac,flac"


class AudioError(Exception):
    """The upload is not audio we accept."""


class TooLong(Exception):
    """The decoded audio is longer than the limit."""


def sniff(data: bytes) -> str | None:
    """Name the container from its first bytes, or None when unsupported.

    Telegram voice notes are OGG/Opus, Chrome/Firefox record WebM/Opus and iOS
    Safari records MP4/AAC; Telegram audio messages are usually MP3/M4A."""
    if len(data) < 12:
        return None
    if data.startswith(b"OggS"):
        return "ogg"
    if data.startswith(b"\x1a\x45\xdf\xa3"):
        return "webm"
    if data[4:8] == b"ftyp":
        return "mp4"
    if data.startswith(b"ID3") or (data[0] == 0xFF and data[1] & 0xE0 == 0xE0 and data[1] & 0x06 != 0):
        # MPEG audio frame sync with a layer set (ADTS AAC has layer bits 00).
        return "mp3"
    if data[0] == 0xFF and data[1] & 0xF6 == 0xF0:
        return "aac"
    if data.startswith(b"RIFF") and data[8:12] == b"WAVE":
        return "wav"
    if data.startswith(b"fLaC"):
        return "flac"
    return None


def decode(data: bytes, max_seconds: float, tolerance: float = 1.0, timeout: float = 120.0, ffmpeg: str = "ffmpeg") -> bytes:
    """Decode to s16le mono 16 kHz. Raise TooLong past max_seconds (plus a
    small tolerance for encoder padding) and AudioError for undecodable or
    silent-empty input. The temporary file is always removed."""
    if sniff(data) is None:
        raise AudioError("unsupported audio format")
    limit = max_seconds + tolerance
    fd, path = tempfile.mkstemp(prefix="kuayle-voice-", suffix=".audio")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
        # Decode a little past the limit so an over-long file is detected
        # without decoding all of it.
        cmd = [
            ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
            "-protocol_whitelist", "file", "-format_whitelist", DEMUXERS,
            "-i", path, "-t", f"{limit + 0.5:.3f}", "-vn", "-sn", "-dn",
            "-ac", "1", "-ar", str(SAMPLE_RATE), "-f", "s16le", "pipe:1",
        ]
        try:
            result = subprocess.run(cmd, capture_output=True, timeout=timeout, check=False)
        except subprocess.TimeoutExpired as exc:
            raise AudioError("audio took too long to decode") from exc
        if result.returncode != 0:
            raise AudioError("audio could not be decoded")
        pcm = result.stdout
    finally:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
    check_duration(pcm, max_seconds, tolerance)
    return pcm


def check_duration(pcm: bytes, max_seconds: float, tolerance: float = 1.0) -> float:
    seconds = len(pcm) / BYTES_PER_SECOND
    if seconds <= 0:
        raise AudioError("the recording contains no audio")
    if seconds > max_seconds + tolerance:
        raise TooLong(f"audio is longer than {max_seconds:g} seconds")
    return seconds
