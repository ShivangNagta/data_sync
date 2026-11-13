# Flet mobile prototype

This is a manual-sync Android prototype for the Data Sync project. It is
intentionally separate from the Go daemon: the user chooses a directory and
presses **Sync**. It uses the existing HTTP API and does not provide
background synchronization or SSE notifications yet.

## Run locally

From the repository root:

```sh
python3 -m venv .venv-flet
. .venv-flet/bin/activate
pip install -r mobile_app/requirements.txt
flet run mobile_app/main.py
```

Set the Worker URL and token in the app, or provide them through the repository
`.env`. The app uses `SYNC_FOLDER` as an initial directory value when present.

## Build for Android

Install Flet's Android build tooling in the virtual environment, then run from
the repository root:

```sh
flet build apk mobile_app --arch arm64-v8a --project data-sync-mobile --yes
```

The generated debug APK is written under:

```text
mobile_app/build/apk/
```

On Android, the prototype uses an app-private documents folder returned by
Flet's `StoragePaths` service:

```text
<application documents directory>/sync/
```

This avoids arbitrary-directory scoped-storage restrictions. The files are
owned by the app and may not be visible in ordinary file managers. The desktop
version still allows selecting a directory through the directory picker.

For a release bundle, use `aab` instead of `apk` and configure signing with
Flet's Android signing options. Do not put the sync token in the APK or in
source control; enter it in the app or provide it through a secure
configuration mechanism.

The prototype stores its local synchronization metadata as
`.data-sync-state.json` inside the selected directory on desktop. On Android,
the state is stored in the app-private temporary directory because Android
scoped storage may reject hidden temporary files inside
`/storage/emulated/0/...`.

## Current scope

- Manual directory selection
- Manual sync button
- Upload, download, delete, and conflict reporting
- Atomic temporary-file replacement for downloads
- Local JSON state for `last_seen_hash`

Not included yet:

- Background sync
- SSE reconnects
- Multiple accounts
- Native Android WorkManager integration
- Large-file streaming or resumable transfers
