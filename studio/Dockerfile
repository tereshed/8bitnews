#syntax=docker/dockerfile:1
FROM python:3.12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt requirements-espeak.txt ./
RUN pip install --no-cache-dir -r requirements.txt \
    && pip install --no-cache-dir --require-hashes -r requirements-espeak.txt

# Kokoro model and voices: fixed URLs, sha256-checked, fetched once here so
# a build of a video needs no network (studio/models.json).
COPY models.json scripts/fetch_models.py /tmp/models/
RUN python /tmp/models/fetch_models.py /tmp/models/models.json /opt/kokoro && rm -rf /tmp/models
ENV STUDIO_KOKORO_DIR=/opt/kokoro

COPY . .

ENTRYPOINT ["python", "-m", "studio"]
