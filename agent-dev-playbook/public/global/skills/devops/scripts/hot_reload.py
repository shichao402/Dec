#!/usr/bin/env python3
"""Forward to the skill-root hot_reload.py so scripts/ stays the usual cwd."""
from pathlib import Path
import runpy

runpy.run_path(str(Path(__file__).resolve().parent.parent / "hot_reload.py"), run_name="__main__")
