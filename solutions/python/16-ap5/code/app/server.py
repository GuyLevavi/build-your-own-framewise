"""The HTTP API: upload videos, submit jobs, read results. A worker thread runs the jobs."""

import logging
import queue
import shutil
import threading
import uuid
from pathlib import Path

from fastapi import FastAPI, HTTPException, UploadFile
from pydantic import BaseModel

from app import motion
from app.config import Config, ConfigError
from app.video import VideoError

log = logging.getLogger(__name__)


class JobRequest(BaseModel):
    video_id: str
    detector: str = "default"


class Jobs:
    """Job records shared by request handlers and the worker thread, behind one lock."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._jobs: dict[str, dict[str, object]] = {}

    def add(self, job: dict[str, object]) -> None:
        with self._lock:
            self._jobs[str(job["id"])] = job

    def get(self, job_id: str) -> dict[str, object] | None:
        with self._lock:
            job = self._jobs.get(job_id)
            return dict(job) if job else None

    def update(self, job_id: str, **changes: object) -> None:
        with self._lock:
            self._jobs[job_id].update(changes)


class Worker:
    """Runs jobs one at a time on a background thread, so requests never wait for analysis."""

    def __init__(self, jobs: Jobs, videos: dict[str, dict[str, object]], config: Config) -> None:
        self.jobs = jobs
        self.videos = videos
        self.config = config
        self.queue: queue.Queue[str] = queue.Queue()
        threading.Thread(target=self._run, daemon=True).start()

    def submit(self, job_id: str) -> None:
        self.queue.put(job_id)

    def _run(self) -> None:
        while True:
            self._process(self.queue.get())

    def _process(self, job_id: str) -> None:
        job = self.jobs.get(job_id)
        if job is None:
            return
        self.jobs.update(job_id, status="RUNNING")
        try:
            path = Path(str(self.videos[str(job["video_id"])]["path"]))
            detector = self.config.detector(str(job["detector"]))
            segments = motion.analyze(path, detector, self.config.max_gap_s)
        except (VideoError, ConfigError) as e:
            self.jobs.update(job_id, status="FAILED", error=str(e))
        except Exception as e:
            log.exception("job %s crashed", job_id)
            self.jobs.update(job_id, status="FAILED", error=f"internal error: {e}")
        else:
            self.jobs.update(job_id, status="DONE", segments=segments)


def create_app(data_dir: Path, config: Config) -> FastAPI:
    videos_dir = data_dir / "videos"
    videos_dir.mkdir(parents=True, exist_ok=True)
    videos: dict[str, dict[str, object]] = {}
    jobs = Jobs()
    worker = Worker(jobs, videos, config)

    app = FastAPI(title="Framewise")

    @app.get("/health")
    def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.post("/videos", status_code=201)
    def upload(file: UploadFile) -> dict[str, object]:
        filename = Path(file.filename or "upload").name
        video_id = uuid.uuid4().hex
        path = videos_dir / f"{video_id}{Path(filename).suffix.lower()}"
        with path.open("wb") as out:
            shutil.copyfileobj(file.file, out)
        videos[video_id] = {
            "video_id": video_id,
            "filename": filename,
            "size_bytes": path.stat().st_size,
            "path": path,
        }
        return {k: v for k, v in videos[video_id].items() if k != "path"}

    @app.get("/videos/{video_id}")
    def get_video(video_id: str) -> dict[str, object]:
        if video_id not in videos:
            raise HTTPException(404, f"no video {video_id!r}")
        return {k: v for k, v in videos[video_id].items() if k != "path"}

    @app.post("/jobs", status_code=202)
    def submit(body: JobRequest) -> dict[str, object]:
        if body.video_id not in videos:
            raise HTTPException(404, f"no video {body.video_id!r}")
        if body.detector not in config.detectors:
            raise HTTPException(422, f"unknown detector {body.detector!r}")
        job_id = uuid.uuid4().hex
        job: dict[str, object] = {
            "id": job_id,
            "video_id": body.video_id,
            "detector": body.detector,
            "status": "PENDING",
            "error": None,
            "segments": None,
        }
        jobs.add(job)
        worker.submit(job_id)
        return job

    @app.get("/jobs/{job_id}")
    def get_job(job_id: str) -> dict[str, object]:
        job = jobs.get(job_id)
        if job is None:
            raise HTTPException(404, f"no job {job_id!r}")
        return job

    return app
