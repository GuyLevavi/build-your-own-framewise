"""The HTTP API."""

import os
from dataclasses import dataclass
from pathlib import Path

from fastapi import FastAPI, HTTPException, UploadFile
from pydantic import BaseModel

from app.profiles import Profile
from app.store import Store, Video, new_id
from app.worker import Worker

VIDEO_SUFFIXES = {".avi", ".mp4", ".mkv", ".mov"}
CHUNK = 1 << 20


@dataclass(frozen=True)
class Settings:
    data_dir: Path
    max_upload_bytes: int

    @classmethod
    def from_env(cls, data_dir: Path) -> "Settings":
        return cls(data_dir, int(os.environ.get("FRAMEWISE_MAX_UPLOAD_MB", "1024")) * 1024 * 1024)


class JobRequest(BaseModel):
    video_id: str
    profile: str = "default"


def create_app(settings: Settings, store: Store, profiles: dict[str, Profile]) -> FastAPI:
    app = FastAPI(title="Framewise")
    videos_dir = settings.data_dir / "videos"
    videos_dir.mkdir(parents=True, exist_ok=True)
    worker = Worker(store, profiles)

    @app.on_event("startup")
    def start_worker() -> None:
        worker.start()

    @app.get("/health")
    def health() -> dict[str, str]:
        return {"status": "ok"}

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
        job = store.add_job(request.video_id, request.profile)
        worker.submit(job.id)
        return job.public()

    @app.get("/jobs/{job_id}")
    def get_job(job_id: str) -> dict:
        job = store.job(job_id)
        if job is None:
            raise HTTPException(404, "no such job")
        return job.public()

    return app
