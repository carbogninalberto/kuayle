import shutil
import subprocess

import pytest

from kuayle_voice import audio


def _header(prefix: bytes) -> bytes:
    return prefix + b"\x00" * 32


@pytest.mark.parametrize(
    "data, expected",
    [
        (_header(b"OggS"), "ogg"),  # Telegram voice notes
        (_header(b"\x1a\x45\xdf\xa3"), "webm"),  # Chrome / Firefox MediaRecorder
        (b"\x00\x00\x00\x1cftypM4A " + b"\x00" * 16, "mp4"),  # iOS Safari / M4A
        (b"\x00\x00\x00\x18ftypiso5" + b"\x00" * 16, "mp4"),
        (_header(b"ID3\x04"), "mp3"),
        (_header(b"\xff\xfb\x90"), "mp3"),
        (_header(b"\xff\xf1\x50"), "aac"),
        (b"RIFF\x24\x00\x00\x00WAVEfmt " + b"\x00" * 16, "wav"),
        (_header(b"fLaC"), "flac"),
    ],
)
def test_sniff_accepts_recorder_and_messenger_containers(data, expected):
    assert audio.sniff(data) == expected


@pytest.mark.parametrize(
    "data",
    [
        b"",
        b"OggS",  # too short to be a file
        _header(b"<html><script>"),
        _header(b"\x89PNG\r\n\x1a\n"),
        _header(b"#EXTM3U\n"),  # playlists could make ffmpeg open other inputs
        _header(b"FORM\x00\x00\x00\x00AIFF"),
    ],
)
def test_sniff_refuses_everything_else(data):
    assert audio.sniff(data) is None


def test_duration_is_measured_on_decoded_audio():
    second = b"\x00\x00" * audio.SAMPLE_RATE
    assert audio.check_duration(second * 300, 300) == 300
    # Encoder padding just past the limit is tolerated...
    assert audio.check_duration(second * 300 + b"\x00\x00" * 8000, 300) == pytest.approx(300.5)
    # ...but not a recording that runs longer.
    with pytest.raises(audio.TooLong):
        audio.check_duration(second * 302, 300)
    with pytest.raises(audio.AudioError):
        audio.check_duration(b"", 300)


def test_decode_refuses_unsniffable_input_without_running_ffmpeg():
    with pytest.raises(audio.AudioError):
        audio.decode(_header(b"#EXTM3U\n"), 300, ffmpeg="/nonexistent/ffmpeg")


needs_ffmpeg = pytest.mark.skipif(shutil.which("ffmpeg") is None, reason="ffmpeg is not installed")


def _encode(tmp_path, seconds: float, codec: list[str], suffix: str) -> bytes:
    out = tmp_path / f"sample{suffix}"
    subprocess.run(
        ["ffmpeg", "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", f"sine=frequency=440:duration={seconds}", *codec, str(out)],
        check=True,
    )
    return out.read_bytes()


@needs_ffmpeg
@pytest.mark.parametrize(
    "codec, suffix",
    [
        (["-c:a", "libopus"], ".ogg"),
        (["-c:a", "libopus"], ".webm"),
        (["-c:a", "aac"], ".m4a"),
    ],
)
def test_decode_turns_supported_containers_into_16k_mono(tmp_path, codec, suffix):
    data = _encode(tmp_path, 2, codec, suffix)
    pcm = audio.decode(data, 300)
    assert len(pcm) / audio.BYTES_PER_SECOND == pytest.approx(2, abs=0.1)
    assert not list(tmp_path.glob("kuayle-voice-*")), "the temporary file is removed"


@needs_ffmpeg
def test_decode_refuses_audio_over_the_limit(tmp_path):
    data = _encode(tmp_path, 8, ["-c:a", "libopus", "-b:a", "8k"], ".ogg")
    with pytest.raises(audio.TooLong):
        audio.decode(data, 5)


@needs_ffmpeg
def test_decode_refuses_a_corrupt_file():
    with pytest.raises(audio.AudioError):
        audio.decode(_header(b"OggS") + b"garbage" * 100, 300)
