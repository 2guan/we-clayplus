#!/usr/bin/env python3
"""Package six immutable binaries and publish them to the private OSS bucket."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
from urllib.parse import urlsplit

TARGETS = [(system, arch) for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]
VERSION_RE = re.compile(r"v(?:0|[1-9][0-9]{0,8})\.(?:0|[1-9][0-9]{0,8})\.(?:0|[1-9][0-9]{0,8})\Z")
SHA_RE = re.compile(r"[0-9a-f]{40}\Z")
PREFIX = "we-clay"


def version_number(value):
    if not VERSION_RE.fullmatch(value):
        raise ValueError("版本必须是 vX.Y.Z，且不能使用 latest")
    return tuple(int(x) for x in value[1:].split("."))


def asset_name(version, system, arch):
    return f"we-clay_{version}_{system}_{arch}" + (".exe" if system == "windows" else "")


def digest(path):
    sha = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            sha.update(block)
    return sha.hexdigest()


def package(directory, version, source_sha):
    version_number(version)
    if not SHA_RE.fullmatch(source_sha):
        raise ValueError("需要完整 40 位源代码提交 SHA")
    expected = {asset_name(version, *target) for target in TARGETS}
    actual = {p.name for p in directory.iterdir()}
    if actual != expected:
        raise ValueError("必须且只能包含六个平台的编译产物")
    assets = []
    for system, arch in TARGETS:
        name = asset_name(version, system, arch)
        path = directory / name
        if not path.is_file() or path.is_symlink() or path.stat().st_size <= 0:
            raise ValueError("编译产物不存在或不是普通文件")
        assets.append(dict(name=name, goos=system, goarch=arch,
                           size=path.stat().st_size, sha256=digest(path)))
    manifest = dict(schema_version=1, version=version,
                    source=dict(repository="we-clay/we-clay", commit=source_sha),
                    assets=assets)
    (directory / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    for name in ("install.sh", "install.ps1"):
        template = (Path("scripts") / name).read_text()
        if template.count("@WE_CLAY_VERSION@") != 1:
            raise ValueError(f"{name} 必须恰好有一个发布版本占位符")
        (directory / name).write_text(template.replace("@WE_CLAY_VERSION@", version))
    (directory / "LICENSE").write_bytes(Path("LICENSE").read_bytes())
    checksummed = sorted(p for p in directory.iterdir() if p.name != "SHA256SUMS")
    (directory / "SHA256SUMS").write_text("".join(f"{digest(p)}  {p.name}\n" for p in checksummed))
    verify(directory, version, source_sha)
    return manifest


def verify(directory, version, source_sha):
    manifest = json.loads((directory / "manifest.json").read_text())
    if manifest.get("schema_version") != 1 or manifest.get("version") != version:
        raise ValueError("版本清单无效")
    if manifest.get("source") != dict(repository="we-clay/we-clay", commit=source_sha):
        raise ValueError("版本清单与源码提交不一致")
    assets = manifest.get("assets")
    if not isinstance(assets, list) or len(assets) != len(TARGETS):
        raise ValueError("版本清单缺少平台附件")
    names = set()
    for item in assets:
        target = (item.get("goos"), item.get("goarch"))
        if target not in TARGETS or item.get("name") != asset_name(version, *target) or item["name"] in names:
            raise ValueError("清单平台/附件名称不匹配或重复")
        names.add(item["name"])
        path = directory / item["name"]
        if not path.is_file() or path.is_symlink() or path.stat().st_size != item.get("size") or digest(path) != item.get("sha256"):
            raise ValueError("编译产物大小或 SHA-256 与清单不一致")
    expected = names | {"manifest.json", "SHA256SUMS", "LICENSE", "install.sh", "install.ps1"}
    if {p.name for p in directory.iterdir()} != expected:
        raise ValueError("发布目录包含未授权的文件")
    for name in ("install.sh", "install.ps1"):
        text = (directory / name).read_text()
        if "@WE_CLAY_VERSION@" in text or f"{version}" not in text:
            raise ValueError("安装脚本的版本未固定")
    lines = (directory / "SHA256SUMS").read_text().splitlines()
    checks = {}
    for line in lines:
        digest_value, name = line.split("  ", 1)
        if name in checks or not re.fullmatch(r"[0-9a-f]{64}", digest_value):
            raise ValueError("SHA256SUMS 包含重复文件或无效摘要")
        checks[name] = digest_value
    if len(lines) != len(checks) or set(checks) != expected - {"SHA256SUMS"}:
        raise ValueError("SHA256SUMS 清单不完整")
    if any(digest(directory / name) != value for name, value in checks.items()):
        raise ValueError("SHA256SUMS 与发布文件不一致")
    return manifest


def oss_settings():
    bucket_name = os.getenv("WE_CLAY_OSS_BUCKET", "weclay")
    region = os.getenv("WE_CLAY_OSS_REGION", "cn-beijing")
    endpoint = os.getenv("WE_CLAY_OSS_ENDPOINT", "https://oss-cn-beijing.aliyuncs.com")
    if bucket_name != "weclay" or region != "cn-beijing":
        raise ValueError("发布目标必须是已批准的私有 weclay 北京 Bucket")
    url = urlsplit(endpoint)
    if url.scheme != "https" or url.hostname != "oss-cn-beijing.aliyuncs.com" or url.path not in ("", "/") or url.query or url.fragment:
        raise ValueError("OSS endpoint 必须是北京地域 HTTPS 官方地址")
    if not os.getenv("OSS_ACCESS_KEY_ID") or not os.getenv("OSS_ACCESS_KEY_SECRET"):
        raise ValueError("请先配置 GitHub Actions 的 OSS_ACCESS_KEY_ID 和 OSS_ACCESS_KEY_SECRET Secrets")
    return bucket_name, region, endpoint


def oss_bucket():
    import oss2
    from oss2.credentials import EnvironmentVariableCredentialsProvider
    name, region, endpoint = oss_settings()
    return oss2.Bucket(oss2.ProviderAuthV4(EnvironmentVariableCredentialsProvider()), endpoint, name, region=region, connect_timeout=60)


def current_version(bucket):
    marker = f"{PREFIX}/current.json"
    if not bucket.object_exists(marker):
        return None
    obj = bucket.get_object(marker)
    try:
        value = json.loads(obj.read(4096))
    finally:
        obj.close()
    current = value.get("version")
    version_number(current)
    return current


def preflight(bucket, version):
    version_number(version)
    current = current_version(bucket)
    if current and version_number(current) > version_number(version):
        raise ValueError("安装脚本当前版本高于本次版本，拒绝回退")
    # Existing versioned objects can only be resumed byte-for-byte. The
    # package built from this tag is checked against them before any writes.
    return bucket.object_exists(f"{PREFIX}/{version}/manifest.json")


def remote_matches(bucket, key, local):
    obj = bucket.get_object(key)
    try:
        h = hashlib.sha256()
        total = 0
        for block in obj:
            total += len(block)
            if total > local.stat().st_size:
                return False
            h.update(block)
        return total == local.stat().st_size and h.hexdigest() == digest(local)
    finally:
        obj.close()


def put_checked(bucket, key, local, *, immutable):
    if immutable and bucket.object_exists(key):
        if not remote_matches(bucket, key, local):
            raise ValueError("同版本 OSS 文件已存在且内容不同，拒绝覆盖")
        return
    headers = {"x-oss-object-acl": "private", "Content-Type": "application/json" if local.suffix == ".json" else "application/octet-stream"}
    if immutable:
        headers["x-oss-forbid-overwrite"] = "true"
        headers["Cache-Control"] = "private, max-age=31536000, immutable"
    else:
        headers["Cache-Control"] = "private, no-cache"
    bucket.put_object_from_file(key, str(local), headers=headers)
    if not remote_matches(bucket, key, local):
        raise ValueError("OSS 上传后读回校验失败")


def publish(bucket, directory, version, source_sha):
    verify(directory, version, source_sha)
    existing_manifest = preflight(bucket, version)
    manifest_key = f"{PREFIX}/{version}/manifest.json"
    if existing_manifest and not remote_matches(bucket, manifest_key, directory / "manifest.json"):
        raise ValueError("已发布版本清单与本次 tag 不同，拒绝覆盖")
    version_files = sorted(p for p in directory.iterdir() if p.name != "manifest.json")
    version_files.append(directory / "manifest.json")
    for local in version_files:
        put_checked(bucket, f"{PREFIX}/{version}/{local.name}", local, immutable=True)
    # These two stable objects are the only mutable release files. The auth
    # service redirects public bootstrap requests to their private signed URLs.
    for name in ("install.sh", "install.ps1"):
        put_checked(bucket, f"{PREFIX}/{name}", directory / name, immutable=False)
    marker = directory / "current.json"
    marker.write_text(json.dumps(dict(version=version, source_commit=source_sha)) + "\n")
    try:
        put_checked(bucket, f"{PREFIX}/current.json", marker, immutable=False)
    finally:
        marker.unlink(missing_ok=True)
    print(f"发布并读回校验成功：{version}，六个平台及两个安装脚本，OSS 对象均为 private")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("package", "preflight", "publish", "verify"))
    parser.add_argument("--directory", type=Path, default=Path("dist"))
    parser.add_argument("--version", default=os.getenv("VERSION"))
    parser.add_argument("--source-sha", default=os.getenv("SOURCE_SHA"))
    args = parser.parse_args()
    try:
        if args.action == "package":
            package(args.directory, args.version, args.source_sha)
        elif args.action == "verify":
            verify(args.directory, args.version, args.source_sha)
        elif args.action == "preflight":
            preflight(oss_bucket(), args.version)
        else:
            publish(oss_bucket(), args.directory, args.version, args.source_sha)
    except (ValueError, KeyError, TypeError) as exc:
        raise SystemExit(str(exc)) from None
    except Exception as exc:
        # SDK errors can include signed URLs and headers, which must stay out of CI logs.
        status = getattr(exc, "status", None)
        suffix = f" (HTTP {status})" if isinstance(status, int) else ""
        raise SystemExit("OSS 操作失败" + suffix + "；请检查权限、网络和私有 Bucket") from None


if __name__ == "__main__":
    main()
