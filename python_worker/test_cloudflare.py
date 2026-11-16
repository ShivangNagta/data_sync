import asyncio
import unittest

from python_worker.cloudflare import D1MetadataStore, R2BlobStore
from python_worker.sync_service import StoredFile


class FakeResult:
    def __init__(self, rows):
        self.results = self
        self._rows = rows

    def to_py(self):
        return self._rows


class FakeStatement:
    def __init__(self, database, query):
        self.database = database
        self.query = query
        self.parameters = ()

    def bind(self, *parameters):
        self.parameters = parameters
        return self

    async def all(self):
        return FakeResult(self.database.rows)

    async def run(self):
        self.database.writes.append((self.query, self.parameters))


class FakeD1:
    def __init__(self, rows):
        self.rows = rows
        self.writes = []

    def prepare(self, query):
        return FakeStatement(self, query)


class FakeBucket:
    def __init__(self, hashes):
        self.hashes = set(hashes)

    async def head(self, key):
        return object() if key in self.hashes else None


class CloudflareAdapterTests(unittest.TestCase):
    def test_d1_reads_metadata_rows(self):
        store = D1MetadataStore(
            FakeD1(
                [
                    {"path": "a.txt", "hash": "abc", "size": 3, "deleted": 0},
                    {"path": "gone.txt", "hash": "old", "size": 0, "deleted": 1},
                ]
            )
        )

        files = asyncio.run(store.list_files())

        self.assertEqual(
            files,
            [
                StoredFile("a.txt", "abc", 3),
                StoredFile("gone.txt", "old", 0, deleted=True),
            ],
        )

    def test_d1_initializes_metadata_table(self):
        database = FakeD1([])
        store = D1MetadataStore(database)

        asyncio.run(store.initialize())

        self.assertIn("CREATE TABLE IF NOT EXISTS files", database.writes[0][0])

    def test_d1_writes_metadata(self):
        database = FakeD1([])
        store = D1MetadataStore(database)

        asyncio.run(store.upsert_file(StoredFile("a.txt", "abc", 3)))

        self.assertEqual(len(database.writes), 1)
        self.assertEqual(database.writes[0][1], ("a.txt", "abc", 3, 0))

    def test_r2_checks_content_addressed_key(self):
        store = R2BlobStore(FakeBucket(["files/abc"]))

        self.assertTrue(asyncio.run(store.exists("abc")))
        self.assertFalse(asyncio.run(store.exists("missing")))


if __name__ == "__main__":
    unittest.main()
