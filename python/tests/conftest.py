from __future__ import annotations

import json
from pathlib import Path
from typing import Any

VECTORS = Path(__file__).resolve().parent.parent / ".." / "spec" / "vectors"


def load_vectors(name: str) -> Any:
    with open(VECTORS / f"{name}.json", encoding="utf-8") as f:
        return json.load(f)
