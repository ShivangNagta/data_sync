import asyncio
import hashlib
import json
import os
import shutil
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

import flet as ft


STATE_FILENAME = ".data-sync-state.json"
IGNORED_NAMES = {".ds_store", STATE_FILENAME}


def load_dotenv() -> None:
    env_path = Path(__file__).resolve().parent.parent / ".env"
    if not env_path.exists():
        return
    for raw_line in env_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip().strip("\"'"))


def normalize_url(value: str) -> str:
    return value.strip().rstrip("/")


def is_android_storage_available() -> bool:
    return Path("/storage/emulated/0").is_dir()


async def default_sync_folder(storage_paths: ft.StoragePaths) -> Path | None:
    if is_android_storage_available():
        documents = await storage_paths.get_application_documents_directory()
        return Path(documents) / "sync"
    configured = os.getenv("SYNC_FOLDER", "").strip()
    return Path(configured).expanduser() if configured else None


def relative_path(root: Path, path: Path) -> str:
    return path.relative_to(root).as_posix()


def sha256_file(path: Path) -> tuple[str, int]:
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
            size += len(chunk)
    return digest.hexdigest(), size


def install_download(path: Path, content: bytes) -> None:
    if str(path).startswith("/storage/"):
        with tempfile.NamedTemporaryFile(prefix=".sync-tmp-", delete=False) as temporary:
            temporary.write(content)
            temporary_path = Path(temporary.name)
        try:
            shutil.copyfile(temporary_path, path)
        finally:
            temporary_path.unlink(missing_ok=True)
        return

    with tempfile.NamedTemporaryFile(
        dir=path.parent, prefix=".sync-tmp-", delete=False
    ) as temporary:
        temporary.write(content)
        temporary_path = Path(temporary.name)
    try:
        temporary_path.replace(path)
    finally:
        temporary_path.unlink(missing_ok=True)


def state_path(root: Path) -> Path:
    if str(root).startswith("/storage/"):
        root_id = hashlib.sha256(str(root).encode()).hexdigest()[:16]
        return Path(tempfile.gettempdir()) / f"data-sync-state-{root_id}.json"
    return root / STATE_FILENAME


def load_state(root: Path) -> dict[str, dict[str, str | int]]:
    path = state_path(root)
    if not path.exists():
        return {}
    with path.open(encoding="utf-8") as source:
        value = json.load(source)
    return value if isinstance(value, dict) else {}


def save_state(root: Path, state: dict[str, dict[str, str | int]]) -> None:
    path = state_path(root)
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(state, indent=2, sort_keys=True), encoding="utf-8")
    temporary.replace(path)


def scan_directory(
    root: Path, state: dict[str, dict[str, str | int]]
) -> tuple[list[dict[str, str | int]], list[tuple[str, str]]]:
    manifest: list[dict[str, str | int]] = []
    current_paths: set[str] = set()

    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.name.lower() in IGNORED_NAMES:
            continue
        relative = relative_path(root, path)
        file_hash, size = sha256_file(path)
        current_paths.add(relative)
        previous = state.get(relative, {})
        manifest.append(
            {
                "path": relative,
                "hash": file_hash,
                "size": size,
                "last_seen_hash": previous.get("last_seen_hash", ""),
            }
        )

    deletes = [
        (path, str(metadata.get("last_seen_hash", "")))
        for path, metadata in state.items()
        if path not in current_paths
    ]
    return manifest, deletes


class SyncClient:
    def __init__(self, base_url: str, token: str):
        self.base_url = normalize_url(base_url)
        self.token = token

    def request(
        self,
        method: str,
        path: str,
        body: bytes | None = None,
        content_type: str = "application/json",
    ) -> bytes:
        request = urllib.request.Request(
            f"{self.base_url}/v2{path}",
            data=body,
            method=method,
            headers={
                "Authorization": f"Bearer {self.token}",
                "Content-Type": content_type,
                "User-Agent": "DataSync-Flet/0.1",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                return response.read()
        except urllib.error.HTTPError as error:
            detail = error.read().decode("utf-8", errors="replace").strip()
            raise RuntimeError(f"HTTP {error.code}: {detail}") from error

    def plan(self, manifest: list[dict[str, str | int]]) -> list[dict[str, str | int]]:
        body = json.dumps({"local_files": manifest}).encode()
        response = self.request("POST", "/sync/plan", body)
        value = json.loads(response)
        return value.get("actions", [])

    def commit(self, operation: dict[str, str | int]) -> dict:
        body = json.dumps(operation).encode()
        response = self.request("POST", "/sync/commit", body)
        value = json.loads(response)
        if value.get("conflict"):
            raise RuntimeError(
                f"conflict for {operation['path']}: server has "
                f"{value.get('current_hash', 'another version')}"
            )
        return value

    def upload(self, file_hash: str, content: bytes) -> None:
        self.request(
            "PUT",
            f"/files/{file_hash}",
            content,
            content_type="application/octet-stream",
        )

    def download(self, file_hash: str) -> bytes:
        return self.request("GET", f"/files/{file_hash}", None, "application/octet-stream")


def apply_sync(
    root: Path,
    client: SyncClient,
    state: dict[str, dict[str, str | int]],
    log: callable,
) -> int:
    manifest, deletes = scan_directory(root, state)
    for path, last_seen_hash in deletes:
        log(f"Deleting remote: {path}")
        client.commit(
            {
                "operation": "delete",
                "path": path,
                "hash": "",
                "size": 0,
                "last_seen_hash": last_seen_hash,
            }
        )
        state.pop(path, None)

    actions = client.plan(manifest)
    for action in actions:
        path = str(action["path"])
        action_type = action["action"]
        if action_type == "upload":
            local_path = root / Path(path)
            file_hash, size = sha256_file(local_path)
            log(f"Uploading: {path}")
            client.upload(file_hash, local_path.read_bytes())
            client.commit(
                {
                    "operation": "upload",
                    "path": path,
                    "hash": file_hash,
                    "size": size,
                    "last_seen_hash": str(
                        state.get(path, {}).get("last_seen_hash", "")
                    ),
                }
            )
            state[path] = {
                "hash": file_hash,
                "size": size,
                "last_seen_hash": file_hash,
            }
        elif action_type == "download":
            local_path = root / Path(path)
            log(f"Downloading: {path}")
            content = client.download(str(action["hash"]))
            local_path.parent.mkdir(parents=True, exist_ok=True)
            install_download(local_path, content)
            state[path] = {
                "hash": str(action["hash"]),
                "size": int(action["size"]),
                "last_seen_hash": str(action["hash"]),
            }
        elif action_type == "delete":
            local_path = root / Path(path)
            log(f"Deleting local: {path}")
            local_path.unlink(missing_ok=True)
            state.pop(path, None)
        elif action_type == "conflict":
            raise RuntimeError(f"conflict for {path}: manual resolution required")

    save_state(root, state)
    return len(actions) + len(deletes)


async def main(page: ft.Page) -> None:
    load_dotenv()
    page.title = "Data Sync"
    page.padding = 20
    page.scroll = ft.ScrollMode.AUTO

    storage_paths = ft.StoragePaths()
    page.services.append(storage_paths)
    android_managed_folder = is_android_storage_available()
    initial_folder = await default_sync_folder(storage_paths)
    if android_managed_folder:
        initial_folder.mkdir(parents=True, exist_ok=True)

    directory = ft.TextField(
        label="Directory",
        value=str(initial_folder) if initial_folder else "",
        expand=True,
        read_only=True,
    )
    sync_url = ft.TextField(
        label="Sync URL",
        value=os.getenv("SYNC_HTTP", "http://localhost:8787"),
        expand=True,
    )
    token = ft.TextField(
        label="Sync token",
        value=os.getenv("SYNC_TOKEN", ""),
        password=True,
        can_reveal_password=True,
        expand=True,
    )
    status = ft.Text("Choose a directory, then press Sync.")
    output = ft.TextField(
        label="Activity",
        multiline=True,
        min_lines=8,
        max_lines=12,
        read_only=True,
    )
    sync_button = ft.Button(
        "Sync", disabled=initial_folder is None or not initial_folder.is_dir()
    )
    choose_button = ft.Button("Choose directory")

    async def choose_directory(_event: ft.ControlEvent) -> None:
        selected = await directory_picker.get_directory_path()
        if selected:
            directory.value = selected
            sync_button.disabled = False
            status.value = "Ready to sync."
            page.update()

    async def sync(_event: ft.ControlEvent) -> None:
        sync_button.disabled = True
        output.value = ""
        status.value = "Syncing..."
        page.update()

        def log(message: str) -> None:
            output.value += message + "\n"
            page.update()

        try:
            root = Path(directory.value).expanduser()
            if not root.is_dir():
                raise RuntimeError("Selected directory is not accessible.")
            if not sync_url.value.strip() or not token.value.strip():
                raise RuntimeError("Sync URL and token are required.")
            state = load_state(root)
            count = await asyncio.to_thread(
                apply_sync,
                root,
                SyncClient(sync_url.value, token.value),
                state,
                log,
            )
            status.value = f"Sync complete. Processed {count} action(s)."
        except Exception as error:
            status.value = f"Sync failed: {error}"
        finally:
            sync_button.disabled = False
            page.update()

    directory_picker = ft.FilePicker()
    if not android_managed_folder:
        page.services.remove(storage_paths)
        page.services.append(directory_picker)
        choose_button.on_click = choose_directory
    else:
        choose_button.visible = False
    page.add(
        ft.Text("Data Sync", size=28, weight=ft.FontWeight.BOLD),
        ft.Text("Manual folder synchronization prototype"),
        ft.Row(
            [
                directory,
                choose_button,
            ]
        ),
        ft.Row([sync_url, token]),
        sync_button,
        status,
        output,
    )
    sync_button.on_click = sync


if __name__ == "__main__":
    ft.run(main)
