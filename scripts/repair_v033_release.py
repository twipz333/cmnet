#!/usr/bin/env python3
"""One-time, idempotent recovery of the split v0.3.3 GitHub release.

The tag-push workflow accidentally created two releases for the same tag:
428456669 has guest.yml and the guest installer; 428456671 has latest.yml
and the host installer. A tag URL may resolve to only one of them, returning
404 for the other updater manifest.

Run only in GitHub Actions on master with GH_TOKEN (contents: write).
No tags, release asset contents, or unrelated releases are changed.
"""
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

REPOSITORY = "twipz333/cmnet"
TAG = "v0.3.3"
KEEP_ID = 428456669
REMOVE_ID = 428456671
API = f"https://api.github.com/repos/{REPOSITORY}"
EXPECTED = {
    "cmnet-setup-0.3.3.exe",
    "cmnet-setup-0.3.3.exe.blockmap",
    "latest.yml",
    "cmnet-guest-setup-0.3.3.exe",
    "cmnet-guest-setup-0.3.3.exe.blockmap",
    "guest.yml",
}
TOKEN = os.environ["GH_TOKEN"]


def call(url, method="GET", data=None, accept="application/vnd.github+json"):
    headers = {
        "Authorization": f"Bearer {TOKEN}",
        "Accept": accept,
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "cmnet-release-repair",
    }
    if data is not None:
        headers["Content-Type"] = "application/octet-stream"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=180) as response:
        return response.read()


def get_release(release_id):
    try:
        return json.loads(call(f"{API}/releases/{release_id}"))
    except urllib.error.HTTPError as err:
        if err.code == 404:
            return None
        raise


def names(release):
    return {asset["name"]: asset for asset in release["assets"]}


def main():
    keep = get_release(KEEP_ID)
    old = get_release(REMOVE_ID)
    if not keep or keep["tag_name"] != TAG:
        raise RuntimeError(f"expected canonical release {KEEP_ID} for {TAG}")
    if old is None:
        if not EXPECTED.issubset(names(keep)):
            raise RuntimeError("duplicate deleted but canonical release is incomplete")
        print("Already reconciled; all six release assets are present.")
        return
    if old["tag_name"] != TAG:
        raise RuntimeError(f"refusing to delete release {REMOVE_ID} for a different tag")
    src = names(old)
    dest = names(keep)
    if not EXPECTED.issubset(set(src) | set(dest)):
        raise RuntimeError(f"missing required files: {EXPECTED - (set(src) | set(dest))}")
    for name, asset in src.items():
        if name in dest:
            if dest[name]["size"] != asset["size"]:
                raise RuntimeError(f"asset conflict: {name}")
            continue
        if name not in EXPECTED:
            raise RuntimeError(f"unexpected asset in duplicate: {name}")
        print(f"Copying {name}: {asset['size']} bytes", flush=True)
        payload = call(f"{API}/releases/assets/{asset['id']}", accept="application/octet-stream")
        if len(payload) != asset["size"]:
            raise RuntimeError(f"{name}: downloaded {len(payload)}, expected {asset['size']}")
        upload = (
            f"https://uploads.github.com/repos/{REPOSITORY}/releases/{KEEP_ID}/assets?"
            + urllib.parse.urlencode({"name": name})
        )
        added = json.loads(call(upload, method="POST", data=payload))
        if added["name"] != name or added["size"] != asset["size"]:
            raise RuntimeError(f"{name}: invalid uploaded asset response")
        print(f"Uploaded {name}", flush=True)

    keep = get_release(KEEP_ID)
    assets = names(keep)
    if not EXPECTED.issubset(assets):
        raise RuntimeError(f"canonical release still incomplete: {EXPECTED - set(assets)}")
    for name in EXPECTED:
        if assets[name]["state"] != "uploaded" or assets[name]["size"] == 0:
            raise RuntimeError(f"asset {name} is not completely uploaded")
    print("Canonical release contains every installer, blockmap and updater manifest.")
    # Remove ONLY the empty/redundant release object. Git tag stays untouched.
    call(f"{API}/releases/{REMOVE_ID}", method="DELETE")
    remaining = get_release(KEEP_ID)
    if not remaining or not EXPECTED.issubset(names(remaining)):
        raise RuntimeError("post-deletion validation failed")
    print(f"Fixed {TAG}: one complete release {KEEP_ID}.", flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"REPAIR FAILED: {exc}", file=sys.stderr)
        raise
