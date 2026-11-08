"""Create a source/evidence ZIP with a blockchain/ root and SHA-256 manifest."""

import argparse
import hashlib
import json
from pathlib import Path
import sys
import zipfile

REPO = Path(__file__).resolve().parent.parent
ROOT_FILES = ("README.md", "AGENTS.md", "go.mod", "go.sum")
DIRECTORIES = ("src", "tests", "logs", "config", "report")
ALLOWED_SUFFIXES = {".go", ".proto", ".py", ".md", ".json", ".jsonl", ".log"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=REPO / "submission" / "blockchain.zip")
    args = parser.parse_args()
    output = args.output.resolve()
    if output.exists():
        raise ValueError(f"Refusing to replace existing archive: {output}")
    files = [REPO / name for name in ROOT_FILES]
    for directory in DIRECTORIES:
        children = sorted(path for path in (REPO / directory).rglob("*")
                          if path.is_file() and path.suffix in ALLOWED_SUFFIXES and "__pycache__" not in path.parts)
        if not children:
            raise ValueError(f"Required directory is empty: {directory}")
        files.extend(children)
    # Deliberate allowlist: no binaries, Git metadata, local runs or key-*.hex.
    manifest = {path.relative_to(REPO).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
                for path in files}
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "x", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in files:
            archive.write(path, "blockchain/" + path.relative_to(REPO).as_posix())
        archive.writestr("blockchain/MANIFEST.json", json.dumps(manifest, indent=2) + "\n")
    with zipfile.ZipFile(output) as archive:
        if archive.testzip() is not None:
            raise RuntimeError("Archive integrity check failed")
        for name, expected in manifest.items():
            if hashlib.sha256(archive.read("blockchain/" + name)).hexdigest() != expected:
                raise RuntimeError(f"Archived content differs: {name}")
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    output.with_suffix(".zip.sha256").write_text(f"{digest}  {output.name}\n", encoding="utf-8")
    print(f"Created {output}: {len(files)} files; manifest and ZIP integrity verified.")
    print(f"SHA-256: {digest}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, zipfile.BadZipFile) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)
