"""Profiles YAML: valid files load, mistakes become one readable line."""

from pathlib import Path

import pytest

from app.profiles import ConfigError, Profile, load_profiles, pick


def test_a_valid_file_loads(tmp_path: Path) -> None:
    path = tmp_path / "profiles.yaml"
    path.write_text("profiles:\n  fast:\n    every: 5\n    labels: [car]\n")
    assert load_profiles(path) == {"fast": Profile(every=5, labels=("car",))}


def test_an_unknown_field_names_where_it_is(tmp_path: Path) -> None:
    path = tmp_path / "profiles.yaml"
    path.write_text("profiles:\n  fast:\n    evry: 5\n")
    with pytest.raises(ConfigError, match="evry"):
        load_profiles(path)


def test_a_missing_file_is_a_config_error(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match="no such file"):
        load_profiles(tmp_path / "missing.yaml")


def test_pick_lists_the_profiles_there_are() -> None:
    with pytest.raises(ConfigError, match="have: fast"):
        pick({"fast": Profile()}, "slow", Path("profiles.yaml"))
