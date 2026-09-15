"""Runtime binding used by compatibility re-exports."""
from __future__ import annotations
from types import ModuleType

_FACADE: ModuleType | None = None

def bind(module: ModuleType) -> None:
    global _FACADE
    _FACADE = module

def facade() -> ModuleType:
    if _FACADE is None:
        raise RuntimeError("relkit_host facade is not bound")
    return _FACADE
