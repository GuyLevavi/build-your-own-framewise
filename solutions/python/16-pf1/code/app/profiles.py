"""Named analysis profiles, loaded from YAML and validated."""

from pathlib import Path
from typing import Literal

import yaml
from pydantic import BaseModel, ConfigDict, Field, ValidationError, ValidationInfo, field_validator

from app import model as models
from app.errors import FramewiseError


class Profile(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    every: int = Field(default=1, ge=1)
    max_gap_s: float = Field(default=0.5, ge=0)
    # Before labels: which labels exist depends on the model.
    model: Literal["yolo26n", "yolo26n-cls"] = models.DETECT
    labels: tuple[str, ...] = Field(default=("person", "car", "bicycle"), min_length=1)
    # The detector's input is input_size x input_size: smaller is faster, and finds small
    # objects less well. The model's stride is 32.
    input_size: int = Field(default=models.SIZE, ge=32, le=1280, multiple_of=32)

    @field_validator("labels")
    @classmethod
    def _model_has_them(cls, labels: tuple[str, ...], info: ValidationInfo) -> tuple[str, ...]:
        name = info.data.get("model")
        if name is None:  # the model itself was invalid, and said so
            return labels
        for label in labels:
            if label not in models.labels(name):
                raise ValueError(f"{name} has no label {label!r} (models/{name}.labels.txt)")
        return labels


class ProfilesFile(BaseModel):
    model_config = ConfigDict(extra="forbid")

    profiles: dict[str, Profile]


class ConfigError(FramewiseError):
    pass


def load_profiles(path: Path) -> dict[str, Profile]:
    try:
        raw = yaml.safe_load(path.read_text())
    except FileNotFoundError:
        raise ConfigError(f"{path}: no such file") from None
    except yaml.YAMLError as e:
        raise ConfigError(f"{path}: not valid YAML ({e.__class__.__name__})") from None
    try:
        return ProfilesFile.model_validate(raw).profiles
    except ValidationError as e:
        first = e.errors()[0]
        where = ".".join(str(part) for part in first["loc"] if not isinstance(part, int))
        raise ConfigError(f"{path}: {where}: {first['msg']}") from None


def pick(profiles: dict[str, Profile], name: str, source: Path) -> Profile:
    if name not in profiles:
        raise ConfigError(
            f"{source}: no profile named {name!r} (have: {', '.join(sorted(profiles))})"
        )
    return profiles[name]
