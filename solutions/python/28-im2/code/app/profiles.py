"""Named analysis profiles, loaded from YAML and validated."""

from pathlib import Path
from typing import Literal

import yaml
from pydantic import BaseModel, ConfigDict, Field, ValidationError

from app.errors import FramewiseError

Label = Literal["person", "car", "bicycle"]


class Profile(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    every: int = Field(default=1, ge=1)
    max_gap_s: float = Field(default=0.5, ge=0)
    labels: tuple[Label, ...] = Field(default=("person", "car", "bicycle"), min_length=1)
    # Where the detector runs: fwdetect in this process, or the model server
    # at FRAMEWISE_MODEL_URL. A setting, not a subclass.
    detector: Literal["local", "remote"] = "local"


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
