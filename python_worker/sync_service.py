from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class LocalFile:
    path: str
    hash: str
    size: int
    last_seen_hash: str = ""

    def has_local_change(self) -> bool:
        return not self.last_seen_hash or self.hash != self.last_seen_hash


@dataclass(frozen=True)
class StoredFile:
    path: str
    hash: str
    size: int
    deleted: bool = False


@dataclass(frozen=True)
class SyncAction:
    path: str
    action: str
    hash: str = ""
    size: int = 0


@dataclass(frozen=True)
class CommitRequest:
    operation: str
    path: str
    hash: str = ""
    size: int = 0


@dataclass(frozen=True)
class CommitResponse:
    accepted: bool
    conflict: bool
    current_hash: str


class MetadataStore(Protocol):
    async def list_files(self) -> list[StoredFile]:
        ...

    async def upsert_file(self, file: StoredFile) -> None:
        ...


class BlobStore(Protocol):
    async def exists(self, content_hash: str) -> bool:
        ...


class EventPublisher(Protocol):
    async def publish(self, path: str, event_type: str) -> None:
        ...


def build_plan(
    local_files: list[LocalFile], remote_files: list[StoredFile]
) -> list[SyncAction]:
    """Build a reconciliation plan without changing either side."""
    remote_by_path = {file.path: file for file in remote_files}
    local_by_path = {file.path: file for file in local_files}
    actions: list[SyncAction] = []

    for local in local_files:
        remote = remote_by_path.get(local.path)
        if remote is None:
            if local.hash:
                actions.append(
                    SyncAction(local.path, "upload", local.hash, local.size)
                )
            continue

        if remote.deleted:
            if not local.hash:
                continue
            if local.has_local_change():
                actions.append(
                    SyncAction(local.path, "upload", local.hash, local.size)
                )
            else:
                actions.append(SyncAction(local.path, "delete"))
            continue

        if remote.hash == local.hash:
            continue
        if local.has_local_change():
            actions.append(
                SyncAction(local.path, "upload", local.hash, local.size)
            )
        else:
            actions.append(
                SyncAction(local.path, "download", remote.hash, remote.size)
            )

    for remote in remote_files:
        if remote.deleted or remote.path in local_by_path:
            continue
        actions.append(
            SyncAction(remote.path, "download", remote.hash, remote.size)
        )

    return actions


class SyncService:
    def __init__(
        self,
        metadata: MetadataStore,
        blobs: BlobStore,
        events: EventPublisher,
    ) -> None:
        self._metadata = metadata
        self._blobs = blobs
        self._events = events

    async def plan(self, local_files: list[LocalFile]) -> list[SyncAction]:
        remote_files = await self._metadata.list_files()
        return build_plan(local_files, remote_files)

    async def commit(self, request: CommitRequest) -> CommitResponse:
        if request.operation == "upload":
            if not request.hash or not await self._blobs.exists(request.hash):
                raise ValueError("uploaded blob is missing")
            await self._metadata.upsert_file(
                StoredFile(request.path, request.hash, request.size)
            )
            await self._events.publish(request.path, "change")
            return CommitResponse(True, False, request.hash)

        if request.operation == "delete":
            await self._metadata.upsert_file(
                StoredFile(request.path, request.hash, 0, deleted=True)
            )
            await self._events.publish(request.path, "delete")
            return CommitResponse(True, False, "")

        raise ValueError(f"unsupported operation: {request.operation}")
