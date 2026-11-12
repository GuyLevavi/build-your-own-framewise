"""The HTTP API."""

import os
from dataclasses import dataclass, field
from pathlib import Path

import sqlalchemy as sa
from fastapi import FastAPI, HTTPException, Request, UploadFile
from fastapi.responses import JSONResponse
from pydantic import BaseModel

from app.auth import require_api_key
from app.events import EventRule
from app.profiles import Profile
from app.store import Job, Store, Video, new_id

VIDEO_SUFFIXES = {".avi", ".mp4", ".mkv", ".mov"}
CHUNK = 1 << 20


@dataclass(frozen=True)
class Settings:
    data_dir: Path
    max_upload_bytes: int
    api_key: str | None = field(default=None, repr=False)  # keep it out of logs and tracebacks

    @classmethod
    def from_env(cls, data_dir: Path) -> "Settings":
        return cls(
            data_dir,
            int(os.environ.get("FRAMEWISE_MAX_UPLOAD_MB", "1024")) * 1024 * 1024,
            os.environ.get("FRAMEWISE_API_KEY") or None,
        )


class JobRequest(BaseModel):
    video_id: str
    profile: str = "default"


def create_app(
    settings: Settings, store: Store, profiles: dict[str, Profile], events: dict[str, EventRule]
) -> FastAPI:
    """The API records videos and jobs; `work` processes run the jobs."""
    app = FastAPI(title="Framewise")
    require_api_key(app, settings.api_key)
    videos_dir = settings.data_dir / "videos"
    videos_dir.mkdir(parents=True, exist_ok=True)

    def find_job(job_id: str) -> Job:
        job = store.job(job_id)
        if job is None:
            raise HTTPException(404, "no such job")
        return job

    @app.exception_handler(sa.exc.DBAPIError)
    def database_unavailable(request: Request, exc: sa.exc.DBAPIError) -> JSONResponse:
        # Not our bug and not the client's: the database is away. 503 tells
        # clients and load balancers to come back later.
        return JSONResponse({"detail": "database unavailable"}, status_code=503)

    @app.get("/health")
    def health() -> dict[str, str]:
        """Liveness: the process answers. Never depends on the database, or an
        outage would make Kubernetes restart every healthy replica."""
        return {"status": "ok"}

    @app.get("/ready")
    def ready() -> dict[str, str]:
        """Readiness: this replica can serve requests right now."""
        store.ping()
        return {"status": "ready"}

    @app.post("/videos", status_code=201)
    def upload(file: UploadFile) -> dict:
        filename = file.filename or ""
        suffix = Path(filename).suffix.lower()
        if suffix not in VIDEO_SUFFIXES:
            raise HTTPException(415, f"expected a video file ({', '.join(sorted(VIDEO_SUFFIXES))})")
        video_id = new_id()
        path = videos_dir / f"{video_id}{suffix}"
        size = 0
        try:
            with path.open("wb") as out:
                while chunk := file.file.read(CHUNK):
                    size += len(chunk)
                    if size > settings.max_upload_bytes:
                        raise HTTPException(413, f"larger than {settings.max_upload_bytes} bytes")
                    out.write(chunk)
        except BaseException:
            path.unlink(missing_ok=True)
            raise
        video = Video(video_id, filename, size, str(path))
        store.add_video(video)
        return video.public()

    @app.get("/videos/{video_id}")
    def get_video(video_id: str) -> dict:
        video = store.video(video_id)
        if video is None:
            raise HTTPException(404, "no such video")
        return video.public()

    @app.post("/jobs", status_code=202)
    def submit(request: JobRequest) -> dict:
        if store.video(request.video_id) is None:
            raise HTTPException(404, "no such video")
        if request.profile not in profiles:
            raise HTTPException(422, f"unknown profile {request.profile!r}")
        return store.add_job(request.video_id, request.profile).public()

    @app.get("/jobs/{job_id}")
    def get_job(job_id: str) -> dict:
        return find_job(job_id).public()

    @app.get("/jobs/{job_id}/detections")
    def get_detections(job_id: str, from_s: float | None = None, to_s: float | None = None) -> dict:
        find_job(job_id)
        return {"detections": [d.public() for d in store.detections(job_id, from_s, to_s)]}

    @app.get("/videos/{video_id}/events")
    def get_video_events(video_id: str) -> dict:
        if store.video(video_id) is None:
            raise HTTPException(404, "no such video")
        return {"events": store.video_events(video_id)}

    @app.get("/jobs/{job_id}/events")
    def get_events(job_id: str) -> dict:
        find_job(job_id)
        return {"events": store.events(job_id, events)}

    return app
