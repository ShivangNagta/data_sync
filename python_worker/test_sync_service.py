import asyncio
import unittest

from python_worker.sync_service import (
    BlobStore,
    CommitRequest,
    EventPublisher,
    MetadataStore,
    LocalFile,
    StoredFile,
    SyncService,
    build_plan,
)


class MemoryMetadata(MetadataStore):
    def __init__(self, files=()):
        self.files = {file.path: file for file in files}

    async def list_files(self):
        return list(self.files.values())

    async def upsert_file(self, file):
        self.files[file.path] = file


class MemoryBlobs(BlobStore):
    def __init__(self, hashes=()):
        self.hashes = set(hashes)

    async def exists(self, content_hash):
        return content_hash in self.hashes


class MemoryEvents(EventPublisher):
    def __init__(self):
        self.events = []

    async def publish(self, path, event_type):
        self.events.append((path, event_type))


class SyncServiceTests(unittest.TestCase):
    def test_remote_change_downloads_when_local_is_unchanged(self):
        actions = build_plan(
            [LocalFile("a.txt", "old", 3, last_seen_hash="old")],
            [StoredFile("a.txt", "new", 3)],
        )
        self.assertEqual(actions[0].action, "download")
        self.assertEqual(actions[0].hash, "new")

    def test_local_change_uploads_when_remote_is_unchanged(self):
        actions = build_plan(
            [LocalFile("a.txt", "new", 3, last_seen_hash="old")],
            [StoredFile("a.txt", "old", 3)],
        )
        self.assertEqual(actions[0].action, "upload")
        self.assertEqual(actions[0].hash, "new")

    def test_both_changed_prefers_local_pending_change(self):
        actions = build_plan(
            [
                LocalFile(
                    "a.txt",
                    "local",
                    5,
                    last_seen_hash="old",
                )
            ],
            [StoredFile("a.txt", "remote", 6)],
        )
        self.assertEqual(actions[0].action, "upload")

    def test_tombstone_deletes_unchanged_local_file(self):
        actions = build_plan(
            [LocalFile("a.txt", "old", 3, last_seen_hash="old")],
            [StoredFile("a.txt", "old", 3, deleted=True)],
        )
        self.assertEqual(len(actions), 1)
        self.assertEqual(actions[0].action, "delete")

    def test_tombstone_can_be_replaced_by_local_change(self):
        actions = build_plan(
            [LocalFile("a.txt", "new", 3, last_seen_hash="old")],
            [StoredFile("a.txt", "old", 3, deleted=True)],
        )
        self.assertEqual(actions[0].action, "upload")

    def test_new_empty_client_does_not_recreate_tombstone(self):
        actions = build_plan(
            [],
            [StoredFile("a.txt", "old", 3, deleted=True)],
        )
        self.assertEqual(actions, [])

    def test_equal_hash_stops_notification_reconciliation_loop(self):
        actions = build_plan(
            [LocalFile("a.txt", "same", 4, last_seen_hash="same")],
            [StoredFile("a.txt", "same", 4)],
        )
        self.assertEqual(actions, [])

    def test_commit_is_last_writer_wins_and_publishes_event(self):
        metadata = MemoryMetadata()
        blobs = MemoryBlobs(["first", "second"])
        events = MemoryEvents()
        service = SyncService(metadata, blobs, events)

        async def run():
            await service.commit(CommitRequest("upload", "a.txt", "first", 5))
            await service.commit(CommitRequest("upload", "a.txt", "second", 6))

        asyncio.run(run())
        self.assertEqual(metadata.files["a.txt"].hash, "second")
        self.assertEqual(events.events, [("a.txt", "change"), ("a.txt", "change")])

    def test_commit_rejects_missing_blob_before_metadata_write(self):
        metadata = MemoryMetadata()
        service = SyncService(metadata, MemoryBlobs(), MemoryEvents())

        async def run():
            with self.assertRaises(ValueError):
                await service.commit(CommitRequest("upload", "a.txt", "missing", 1))

        asyncio.run(run())
        self.assertNotIn("a.txt", metadata.files)


if __name__ == "__main__":
    unittest.main()
