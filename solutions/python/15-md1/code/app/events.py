"""Event rules: two labels whose boxes overlap, for long enough."""

from pathlib import Path

import yaml
from pydantic import BaseModel, ConfigDict, Field, ValidationError, field_validator

from app import model
from app.errors import FramewiseError


class EventRule(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    labels: tuple[str, str]
    max_gap_s: float = Field(default=0.5, ge=0)
    min_duration_s: float = Field(default=0.0, ge=0)

    @field_validator("labels")
    @classmethod
    def _detector_has_them(cls, labels: tuple[str, str]) -> tuple[str, str]:
        for label in labels:
            if label not in model.labels():
                raise ValueError(f"the detector has no label {label!r} (models/yolo26n.labels.txt)")
        return labels


class EventsFile(BaseModel):
    model_config = ConfigDict(extra="forbid")

    events: dict[str, EventRule]


def load_events(path: Path) -> dict[str, EventRule]:
    try:
        raw = yaml.safe_load(path.read_text())
    except FileNotFoundError:
        raise FramewiseError(f"{path}: no such file") from None
    except yaml.YAMLError as e:
        raise FramewiseError(f"{path}: not valid YAML ({e.__class__.__name__})") from None
    try:
        return EventsFile.model_validate(raw).events
    except ValidationError as e:
        first = e.errors()[0]
        where = ".".join(str(part) for part in first["loc"] if not isinstance(part, int))
        raise FramewiseError(f"{path}: {where}: {first['msg']}") from None
