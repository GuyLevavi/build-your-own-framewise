"""The HTTP API."""

from pathlib import Path

from fastapi import FastAPI


def create_app(data_dir: Path) -> FastAPI:
    app = FastAPI(title="Framewise")

    @app.get("/health")
    def health() -> dict[str, str]:
        return {"status": "ok"}

    return app
