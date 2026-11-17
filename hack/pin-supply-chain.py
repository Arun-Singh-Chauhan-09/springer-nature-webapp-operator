#!/usr/bin/env python3
"""Pin the build's supply chain. Safe to re-run; Dependabot keeps the pins fresh afterwards.

* GitHub Actions   uses: owner/repo@v5      ->  uses: owner/repo@<commit-sha> # v5
  A tag can be moved to malicious code (tj-actions/changed-files, 2025); a commit SHA cannot.
* Container images FROM golang:1.24         ->  FROM golang:1.24@sha256:<digest>
  in the Dockerfile and the *_IMG images (Go, Terraform, tflint, checkov) in the Makefile; checkov:latest is first
  resolved to its real version so the pin is readable.

Needs git and docker (run it from WSL). Usage: make pin-supply-chain
"""
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SHA40 = re.compile(r"[0-9a-f]{40}")


def run(*args, check=True):
    return subprocess.run(args, check=check, capture_output=True, text=True)


# ---------- GitHub Actions ----------
def action_sha(repo, ref):
    out = run("git", "ls-remote", f"https://github.com/{repo}",
              f"refs/tags/{ref}^{{}}", f"refs/tags/{ref}", f"refs/heads/{ref}").stdout
    rows = [line.split("\t") for line in out.splitlines() if line]
    peeled = [sha for sha, name in rows if name.endswith("^{}")]  # annotated tag -> its commit
    sha = (peeled or [sha for sha, _ in rows] or [""])[0]
    if not SHA40.fullmatch(sha):
        sys.exit(f"cannot resolve {repo}@{ref}")
    return sha


USES = re.compile(r"^(\s*(?:-\s+)?uses:\s+)([\w.-]+/[\w./-]+)@([\w.-]+)(?:\s+#\s*(\S+))?\s*$")


def pin_actions():
    for wf in sorted((ROOT / ".github" / "workflows").glob("*.y*ml")):
        lines = wf.read_text().splitlines(keepends=True)
        changed = False
        for i, line in enumerate(lines):
            m = USES.match(line.rstrip("\n"))
            if not m:
                continue
            prefix, action, ref, comment = m.groups()
            tag = comment if SHA40.fullmatch(ref) and comment else ref
            if SHA40.fullmatch(tag):
                continue  # pinned with no tag to refresh from
            repo = "/".join(action.split("/")[:2])  # owner/repo/sub/path -> owner/repo
            new = f"{prefix}{action}@{action_sha(repo, tag)} # {tag}\n"
            if new != line:
                lines[i], changed = new, True
                print(f"  {wf.name}: {action}@{tag} -> {new.split('@')[1][:12]}")
        if changed:
            wf.write_text("".join(lines))


# ---------- container images ----------
def digest(image):
    run("docker", "pull", "-q", image)
    repo_digests = json.loads(run("docker", "inspect", "--format", "{{json .RepoDigests}}", image).stdout)
    if not repo_digests:
        sys.exit(f"no digest for {image}")
    return repo_digests[0].split("@", 1)[1]


def readable_tag(image):
    """checkov:latest -> checkov:<version>, so the pin says what it is."""
    name, tag = image.rsplit(":", 1)
    if tag != "latest" or "checkov" not in name:
        return image
    version = run("docker", "run", "--rm", image, "--version").stdout.strip().splitlines()[-1]
    candidate = f"{name}:{version}"
    return candidate if run("docker", "pull", "-q", candidate, check=False).returncode == 0 else image


def pin_image(image):
    base = readable_tag(image.split("@", 1)[0])
    pinned = f"{base}@{digest(base)}"
    if pinned != image:
        print(f"  {image} -> {pinned}")
    return pinned


def pin_images():
    df = ROOT / "Dockerfile"
    text = re.sub(r"(?im)^(FROM\s+)(\S+)(\s+AS\s+\w+)?$",
                  lambda m: m.group(1) + pin_image(m.group(2)) + (m.group(3) or ""), df.read_text())
    df.write_text(text)
    mk = ROOT / "Makefile"
    text = re.sub(r"(?m)^((?:GO|TF|TFLINT|CHECKOV)_IMG\s*\?=\s*)(\S+)",
                  lambda m: m.group(1) + pin_image(m.group(2)), mk.read_text())
    mk.write_text(text)


if __name__ == "__main__":
    print("GitHub Actions -> commit SHAs")
    pin_actions()
    print("Images -> digests")
    pin_images()
    print("Done. Review with: git diff")
