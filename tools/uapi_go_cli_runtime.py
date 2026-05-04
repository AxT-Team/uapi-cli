#!/usr/bin/env python3
from __future__ import annotations

import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Mapping, Sequence


ROOT = Path(__file__).resolve().parents[1]
CLI_ROOT = ROOT / "uapi-cli"

PLATFORM_BINARIES: Mapping[tuple[str, str], Path] = {
    ("win32", "amd64"): ROOT / "uapi-cli-win32-x64" / "bin" / "uapi.exe",
    ("linux", "x86_64"): ROOT / "uapi-cli-linux-x64" / "bin" / "uapi",
    ("linux", "aarch64"): ROOT / "uapi-cli-linux-arm64" / "bin" / "uapi",
    ("darwin", "x86_64"): ROOT / "uapi-cli-darwin-x64" / "bin" / "uapi",
    ("darwin", "arm64"): ROOT / "uapi-cli-darwin-arm64" / "bin" / "uapi",
}


def normalized_platform_key() -> tuple[str, str]:
    return sys.platform, platform.machine().lower()


def resolve_binary_path() -> Path:
    env_override = os.getenv("UAPI_CLI_BINARY")
    if env_override:
        candidate = Path(env_override).expanduser()
        if candidate.is_file():
            return candidate
        raise FileNotFoundError(f"UAPI_CLI_BINARY does not exist: {candidate}")

    key = normalized_platform_key()
    if key not in PLATFORM_BINARIES:
        supported = ", ".join(f"{os_name}/{arch}" for os_name, arch in sorted(PLATFORM_BINARIES))
        raise RuntimeError(f"Unsupported platform {key[0]}/{key[1]}. Supported targets: {supported}")
    return PLATFORM_BINARIES[key]


def ensure_cli_binary(binary_path: Path) -> Path:
    if binary_path.is_file():
        return binary_path
    if not CLI_ROOT.is_dir():
        raise FileNotFoundError(f"Go CLI source directory not found: {CLI_ROOT}")

    go_binary = shutil.which("go")
    if not go_binary:
        raise RuntimeError(
            "Go toolchain not found. Install Go or prebuild the platform binary with `npm run build:local` in `uapi-cli-npm`."
        )

    binary_path.parent.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.setdefault("CGO_ENABLED", "0")
    result = subprocess.run(
        [go_binary, "build", "-trimpath", "-ldflags", "-s -w", "-o", str(binary_path), "."],
        cwd=str(CLI_ROOT),
        env=env,
    )
    if result.returncode != 0:
        raise RuntimeError(f"Failed to build Go CLI binary with exit code {result.returncode}")
    return binary_path


def run_go_cli(args: Sequence[str]) -> int:
    binary = ensure_cli_binary(resolve_binary_path())
    result = subprocess.run([str(binary), *inject_llm_format(list(args))])
    return result.returncode


def inject_llm_format(args: list[str]) -> list[str]:
    if not args:
        return args
    if any(arg == "--format" or arg.startswith("--format=") for arg in args):
        return args
    if args[0] not in {"discover", "tags", "schema", "call"}:
        return args
    return [*args, "--format", "llm"]


def main(argv: Sequence[str] | None = None) -> int:
    return run_go_cli(list(sys.argv[1:] if argv is None else argv))


if __name__ == "__main__":
    sys.exit(main())
