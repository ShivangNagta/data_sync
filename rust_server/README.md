# Cloudflare sync v2

This is the Rust/WASM Cloudflare Worker used by the Go client. It is the
project's only sync backend.

## Current slice

- `GET /health` returns `ok`.
- `/v2/*` requires `Authorization: Bearer <SYNC_TOKEN>`.
- `PUT`, `GET`, and `DELETE /v2/files/:hash` provide content-addressed R2
  operations under the `files/` prefix.
- `POST /v2/sync/plan` accepts `{ "local_files": [...] }` and returns upload,
  download, delete, or conflict actions.
- `POST /v2/sync/commit` accepts an upload or delete mutation and applies
  first-writer-wins compare-and-swap using `last_seen_hash`.
- `GET /v2/events` opens an authenticated Server-Sent Events stream and
  receives
  `{ "path": "...", "type": "change" }` or `{ "path": "...", "type": "delete" }`
  after a successful commit.
- `SyncNamespace` stores file metadata in SQLite. The current namespace is
  named `default`; namespace partitioning can be added when authentication
  supports multiple users.
- After each accepted upload or delete commit, the Worker removes every R2
  object under `files/` that is not referenced by an active metadata row.

Example commit:

```json
{
  "operation": "upload",
  "path": "docs/readme.txt",
  "hash": "sha256-hash",
  "size": 42,
  "last_seen_hash": "previous-sha256-hash"
}
```

## Local checks

From this directory:

```sh
cargo fmt --check
cargo check
```

Wrangler requires the Cloudflare account for local bindings and deployment:

```sh
npx wrangler dev
npx wrangler secret put SYNC_TOKEN
npx wrangler deploy
```

Create the R2 bucket before deployment:

```sh
npx wrangler r2 bucket create data-sync-files-v2
```

Never commit the token or generated `build/` output.
