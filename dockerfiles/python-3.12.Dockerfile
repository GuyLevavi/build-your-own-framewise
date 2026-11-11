# syntax=docker/dockerfile:1.7-labs
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

# Ensures the container is re-built if dependency files change
ENV CODECRAFTERS_DEPENDENCY_FILE_PATHS="pyproject.toml,uv.lock"

# Prevents Python from buffering stdout and stderr
ENV PYTHONUNBUFFERED=1

ENV PYTHONPATH=/app:$PYTHONPATH

# Keep the virtualenv outside /app, so it survives the code directory being mounted over /app
ENV UV_PROJECT_ENVIRONMENT=/opt/venv

WORKDIR /app

# Install dependencies first, so code changes reuse this layer
COPY pyproject.toml uv.lock /app/
RUN uv sync --locked --no-install-project

# .git & README.md are unique per-repository. We ignore them on first copy to prevent cache misses
COPY --exclude=.git --exclude=README.md . /app
