#!/usr/bin/env python3
from __future__ import annotations

import sys

from tools.uapi_go_cli_runtime import run_go_cli


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if not args or args[0] != "llm":
        sys.stderr.write("This standalone CLI repo supports `uapi_sdk_cli.py llm ...` only.\n")
        return 2
    return run_go_cli(args[1:])


if __name__ == "__main__":
    sys.exit(main())

