#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path

from qcloud_cos import CosConfig, CosS3Client


def required(value, name):
    if not value:
        raise RuntimeError(f"missing {name}")
    return value


def sha256sum(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def client_from_args(args):
    return CosS3Client(
        CosConfig(
            Region=required(args.region or os.environ.get("COS_REGION"), "COS region"),
            SecretId=required(os.environ.get("COS_SECRET_ID"), "COS_SECRET_ID"),
            SecretKey=required(os.environ.get("COS_SECRET_KEY"), "COS_SECRET_KEY"),
            Scheme="https",
            Timeout=900,
            AutoSwitchDomainOnRetry=True,
        )
    )


def object_root(args):
    prefix = (args.prefix or os.environ.get("COS_RELEASE_PREFIX", "releases")).strip("/")
    return f"{prefix}/{args.release_id}"


def upload(args):
    release_dir = Path(args.release_dir).resolve()
    gzip_path = release_dir / "main.gz"
    manifest_path = release_dir / "manifest.json"
    sums_path = release_dir / "SHA256SUMS"
    for path in (gzip_path, manifest_path, sums_path):
        if not path.is_file():
            raise RuntimeError(f"missing release artifact: {path}")

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("release_id") != args.release_id:
        raise RuntimeError("manifest release_id mismatch")
    actual_gzip_sha = sha256sum(gzip_path)
    if manifest.get("gzip_sha256") != actual_gzip_sha:
        raise RuntimeError("manifest gzip_sha256 mismatch")

    client = client_from_args(args)
    bucket = required(args.bucket or os.environ.get("COS_BUCKET"), "COS bucket")
    root = object_root(args)
    main_key = f"{root}/main.gz"
    client.upload_file(
        Bucket=bucket,
        LocalFilePath=str(gzip_path),
        Key=main_key,
        PartSize=args.part_size,
        MAXThread=args.threads,
        EnableMD5=True,
    )
    client.put_object(Bucket=bucket, Key=f"{root}/manifest.json", Body=manifest_path.read_bytes())
    client.put_object(Bucket=bucket, Key=f"{root}/SHA256SUMS", Body=sums_path.read_bytes())
    head = client.head_object(Bucket=bucket, Key=main_key)
    if int(head["Content-Length"]) != gzip_path.stat().st_size:
        raise RuntimeError("uploaded main.gz size mismatch")
    url = client.get_presigned_download_url(Bucket=bucket, Key=main_key, Expired=args.expires)
    print(
        json.dumps(
            {
                "release_id": args.release_id,
                "object_root": root,
                "main_key": main_key,
                "download_url": url,
                "binary_sha256": manifest["binary_sha256"],
                "gzip_sha256": actual_gzip_sha,
                "gzip_bytes": gzip_path.stat().st_size,
            },
            ensure_ascii=False,
        )
    )


def signed_url(args):
    client = client_from_args(args)
    bucket = required(args.bucket or os.environ.get("COS_BUCKET"), "COS bucket")
    key = f"{object_root(args)}/main.gz"
    print(client.get_presigned_download_url(Bucket=bucket, Key=key, Expired=args.expires))


def delete(args):
    client = client_from_args(args)
    bucket = required(args.bucket or os.environ.get("COS_BUCKET"), "COS bucket")
    prefix = f"{object_root(args)}/"
    response = client.list_objects(Bucket=bucket, Prefix=prefix, MaxKeys=1000)
    deleted = 0
    for item in response.get("Contents", []):
        client.delete_object(Bucket=bucket, Key=item["Key"])
        deleted += 1
    print(json.dumps({"release_id": args.release_id, "deleted": deleted}))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--bucket")
    parser.add_argument("--region")
    parser.add_argument("--prefix")
    subparsers = parser.add_subparsers(dest="command", required=True)

    upload_parser = subparsers.add_parser("upload")
    upload_parser.add_argument("--release-id", required=True)
    upload_parser.add_argument("--release-dir", required=True)
    upload_parser.add_argument("--part-size", type=int, default=8)
    upload_parser.add_argument("--threads", type=int, default=6)
    upload_parser.add_argument("--expires", type=int, default=3600)
    upload_parser.set_defaults(handler=upload)

    url_parser = subparsers.add_parser("url")
    url_parser.add_argument("--release-id", required=True)
    url_parser.add_argument("--expires", type=int, default=3600)
    url_parser.set_defaults(handler=signed_url)

    delete_parser = subparsers.add_parser("delete")
    delete_parser.add_argument("--release-id", required=True)
    delete_parser.set_defaults(handler=delete)

    args = parser.parse_args()
    args.handler(args)


if __name__ == "__main__":
    main()
