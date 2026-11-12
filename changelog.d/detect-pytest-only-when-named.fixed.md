**A `pyproject.toml` alone no longer puts `pytest` in the detected `ready` gate.** In a repository without
`heavyq.toml`, a `pyproject.toml` kept only for ruff, mypy or packaging made the detected gate run `pytest`, which
exits 5 with no tests to collect, so `ready` could never be proven. Like `setup.cfg`, `pyproject.toml` now counts
only when it names pytest: a `[tool.pytest.ini_options]` table or a pytest dependency. `pytest.ini` still counts
on its own.
