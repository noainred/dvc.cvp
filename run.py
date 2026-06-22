#!/usr/bin/env python3
"""HomeLab Monitor entrypoint.

Usage:
    python run.py
Configuration is read from config.yaml (see config.example.yaml) and/or
environment variables (HOMELAB_HOST, HOMELAB_PORT, HOMELAB_DB, ...).
"""

import uvicorn

from backend.app.config import config


def main() -> None:
    uvicorn.run(
        "backend.app.main:app",
        host=config.host,
        port=config.port,
        log_level=config.log_level.lower(),
    )


if __name__ == "__main__":
    main()
