import io
from pathlib import Path
import tempfile
import unittest

from scripts import release_we_clay as release


class FakeBucket:
    def __init__(self):
        self.objects = {}
        self.fail_once = None

    def object_exists(self, key):
        return key in self.objects

    def get_object(self, key):
        return io.BytesIO(self.objects[key])

    def put_object_from_file(self, key, path, headers=None):
        if key == self.fail_once:
            self.fail_once = None
            raise RuntimeError("synthetic upload interruption")
        if headers.get("x-oss-forbid-overwrite") == "true" and key in self.objects:
            raise RuntimeError("immutable object overwritten")
        self.objects[key] = Path(path).read_bytes()


class ReleaseTests(unittest.TestCase):
    def test_rejects_noncanonical_version_before_publish(self):
        for value in ("latest", "v0.08.0", "v01.2.3", "v1.2.03", "v1000000000.2.3"):
            with self.subTest(version=value), self.assertRaises(ValueError):
                release.version_number(value)

    def test_partial_stable_script_publish_can_resume_without_replacing_manifest(self):
        version = "v9.8.7"
        commit = "a" * 40
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            for system, arch in release.TARGETS:
                (directory / release.asset_name(version, system, arch)).write_bytes(f"{system}/{arch}".encode())
            release.package(directory, version, commit)
            bucket = FakeBucket()
            bucket.fail_once = "we-clay/install.ps1"
            with self.assertRaisesRegex(RuntimeError, "synthetic upload interruption"):
                release.publish(bucket, directory, version, commit)
            manifest = bucket.objects[f"we-clay/{version}/manifest.json"]
            release.publish(bucket, directory, version, commit)
            self.assertEqual(bucket.objects[f"we-clay/{version}/manifest.json"], manifest)
            self.assertIn(b"v9.8.7", bucket.objects["we-clay/install.sh"])
            self.assertIn(b"v9.8.7", bucket.objects["we-clay/install.ps1"])
            self.assertIn(b"v9.8.7", bucket.objects["we-clay/current.json"])


if __name__ == "__main__":
    unittest.main()
