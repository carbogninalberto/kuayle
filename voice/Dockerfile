# Kuayle voice transcription (faster-whisper int8 and Vosk, CPU only).
# Python packages are pinned exactly in requirements.txt; ffmpeg comes from
# Debian 12 and follows its security updates. Models are not baked in: they
# are downloaded at runtime from their pinned official sources into /models.
FROM python:3.12.11-slim-bookworm

ARG SECURITY_REFRESH=initial
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    HF_HUB_OFFLINE=1 \
    VOICE_MODELS_DIR=/models \
    VOICE_PORT=8000

RUN apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ffmpeg libatomic1 \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --no-deps -r /tmp/requirements.txt \
    && pip check \
    && python -c "import ctranslate2, faster_whisper, vosk" \
    && rm /tmp/requirements.txt

RUN useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin --user-group kuayle \
    && mkdir -p /models \
    && chown kuayle:kuayle /models

WORKDIR /app
COPY kuayle_voice ./kuayle_voice

USER kuayle
VOLUME ["/models"]
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["python", "-c", "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8000/health', timeout=4).status == 200 else 1)"]
CMD ["python", "-m", "kuayle_voice.server"]
