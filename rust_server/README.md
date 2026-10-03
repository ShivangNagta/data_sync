# Cloudflare sync v2

This is the isolated Rust/WASM Cloudflare Worker implementation. The working
Go implementation remains the v1 reference and is not modified by this project.

## Current slice

- `GET /health` returns `ok`.
- `/v2/*` requires `Authorization: Bearer <SYNC_TOKEN>`.
- `PUT`, `GET`, and `DELETE /v2/files/:hash` provide content-addressed R2
  operations under the `files/` prefix.
- `POST /v2/sync/plan` accepts `{ "local_files": [...] }` and returns upload,
  download, delete, or conflict actions.
- `POST /v2/sync/commit` accepts an upload or delete mutation and applies
  first-writer-wins compare-and-swap using `last_seen_hash`.
- `SyncNamespace` stores file metadata in SQLite. The current namespace is
  named `default`; namespace partitioning can be added when authentication
  supports multiple users.

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
