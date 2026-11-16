from typing import Any

try:
    from .sync_service import BlobStore, MetadataStore, StoredFile
except ImportError:
    from sync_service import BlobStore, MetadataStore, StoredFile


class D1MetadataStore(MetadataStore):
    def __init__(self, database: Any) -> None:
        self._database = database

    async def initialize(self) -> None:
        await self._database.prepare(
            """
            CREATE TABLE IF NOT EXISTS files (
                path TEXT PRIMARY KEY,
                hash TEXT NOT NULL,
                size INTEGER NOT NULL,
                deleted INTEGER NOT NULL DEFAULT 0
            )
            """
        ).run()

    async def list_files(self) -> list[StoredFile]:
        result = await self._database.prepare(
            "SELECT path, hash, size, deleted FROM files"
        ).all()
        rows = result.results
        if hasattr(rows, "to_py"):
            rows = rows.to_py()
        return [
            StoredFile(
                path=row["path"],
                hash=row["hash"],
                size=int(row["size"]),
                deleted=bool(row["deleted"]),
            )
            for row in rows
        ]

    async def upsert_file(self, file: StoredFile) -> None:
        await (
            self._database.prepare(
                """
            INSERT INTO files(path, hash, size, deleted)
            VALUES (?, ?, ?, ?)
            ON CONFLICT(path) DO UPDATE SET
                hash = excluded.hash,
                size = excluded.size,
                deleted = excluded.deleted
            """
            )
            .bind(
                file.path,
                file.hash,
                file.size,
                int(file.deleted),
            )
            .run()
        )


class R2BlobStore(BlobStore):
    def __init__(self, bucket: Any) -> None:
        self._bucket = bucket

    async def exists(self, content_hash: str) -> bool:
        return await self._bucket.head(f"files/{content_hash}") is not None
