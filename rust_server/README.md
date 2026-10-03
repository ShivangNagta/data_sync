# Cloudflare sync v2

This is the isolated Rust/WASM Cloudflare Worker implementation. The working
Go implementation remains the v1 reference and is not modified by this project.

## Current slice

- `GET /health` returns `ok`.
- `/v2/*` requires `Authorization: Bearer <SYNC_TOKEN>`.
- `PUT`, `GET`, and `DELETE /v2/files/:hash` provide content-addressed R2
  operations under the `files/` prefix.
- `SyncNamespace` is the first Durable Object binding; sync metadata and
  compare-and-swap behavior will be added in the next slice.

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
