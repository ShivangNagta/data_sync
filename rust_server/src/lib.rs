use serde::{Deserialize, Serialize};
use wasm_bindgen::JsValue;
use worker::*;

#[event(fetch)]
async fn fetch(req: Request, env: Env, _ctx: Context) -> Result<Response> {
    let router = Router::new();

    router
        .get_async("/health", |_req, _ctx| async { Response::ok("ok") })
        .put_async("/v2/files/:hash", upload)
        .get_async("/v2/files/:hash", download)
        .delete_async("/v2/files/:hash", delete)
        .get_async("/v2/events", events)
        .post_async("/v2/sync/plan", sync_plan)
        .post_async("/v2/sync/commit", sync_commit)
        .on_async("/v2/*path", |req, ctx| async move {
            let token = ctx.secret("SYNC_TOKEN")?.to_string();
            if !authorized(&req, &token)? {
                return Response::error("unauthorized", 401);
            }

            Response::error("not implemented", 501)
        })
        .run(req, env)
        .await
}

async fn sync_plan(mut req: Request, ctx: RouteContext<()>) -> Result<Response> {
    if !authorize(&req, &ctx)? {
        return Response::error("unauthorized", 401);
    }
    forward_to_namespace(&mut req, &ctx, "/v2/sync/plan").await
}

async fn events(req: Request, ctx: RouteContext<()>) -> Result<Response> {
    if !authorize(&req, &ctx)? {
        return Response::error("unauthorized", 401);
    }
    let namespace = ctx.durable_object("SYNC_NAMESPACE")?;
    let stub = namespace.id_from_name("default")?.get_stub()?;
    stub.fetch_with_request(req).await
}

async fn sync_commit(mut req: Request, ctx: RouteContext<()>) -> Result<Response> {
    if !authorize(&req, &ctx)? {
        return Response::error("unauthorized", 401);
    }
    forward_to_namespace(&mut req, &ctx, "/v2/sync/commit").await
}

async fn forward_to_namespace(
    req: &mut Request,
    ctx: &RouteContext<()>,
    path: &str,
) -> Result<Response> {
    let body = req.bytes().await?;
    let mut init = RequestInit::new();
    init.with_method(Method::Post)
        .with_body(Some(JsValue::from(js_sys::Uint8Array::from(
            body.as_slice(),
        ))));
    let internal = Request::new_with_init(&format!("https://sync.internal{path}"), &init)?;
    let namespace = ctx.durable_object("SYNC_NAMESPACE")?;
    let stub = namespace.id_from_name("default")?.get_stub()?;
    stub.fetch_with_request(internal).await
}

async fn upload(mut req: Request, ctx: RouteContext<()>) -> Result<Response> {
    if !authorize(&req, &ctx)? {
        return Response::error("unauthorized", 401);
    }
    let hash = path_param(&ctx, "hash")?;
    let body = req.bytes().await?;
    ctx.bucket("FILES")?
        .put(format!("files/{hash}"), Data::Bytes(body))
        .execute()
        .await?;
    Response::ok("stored")
}

async fn download(req: Request, ctx: RouteContext<()>) -> Result<Response> {
    if !authorize(&req, &ctx)? {
        return Response::error("unauthorized", 401);
    }
    let hash = path_param(&ctx, "hash")?;
    let Some(object) = ctx
        .bucket("FILES")?
        .get(format!("files/{hash}"))
        .execute()
        .await?
    else {
        return Response::error("not found", 404);
    };
    let body = object
        .body()
        .ok_or_else(|| Error::RustError("R2 object has no body".into()))?
        .bytes()
        .await?;
    Response::from_bytes(body)
}

async fn delete(req: Request, ctx: RouteContext<()>) -> Result<Response> {
    if !authorize(&req, &ctx)? {
        return Response::error("unauthorized", 401);
    }
    let hash = path_param(&ctx, "hash")?;
    ctx.bucket("FILES")?.delete(format!("files/{hash}")).await?;
    Response::ok("deleted")
}

fn authorize(req: &Request, ctx: &RouteContext<()>) -> Result<bool> {
    let token = ctx.secret("SYNC_TOKEN")?.to_string();
    authorized(req, &token)
}

fn path_param(ctx: &RouteContext<()>, name: &str) -> Result<String> {
    ctx.param(name)
        .map(|value| value.to_owned())
        .ok_or_else(|| Error::RustError(format!("missing path parameter: {name}")))
}

fn authorized(req: &Request, expected: &str) -> Result<bool> {
    let Some(value) = req.headers().get("Authorization")? else {
        return Ok(false);
    };

    Ok(value == format!("Bearer {expected}"))
}

#[durable_object]
pub struct SyncNamespace {
    state: State,
}

impl DurableObject for SyncNamespace {
    fn new(state: State, _env: Env) -> Self {
        Self { state }
    }

    async fn fetch(&self, mut req: Request) -> Result<Response> {
        let sql = self.state.storage().sql();
        sql.exec(
            "CREATE TABLE IF NOT EXISTS files (
                path TEXT PRIMARY KEY,
                hash TEXT NOT NULL,
                size INTEGER NOT NULL,
                deleted INTEGER NOT NULL DEFAULT 0
            )",
            None,
        )?;

        match req.path().as_str() {
            "/v2/events" => {
                let pair = WebSocketPair::new()?;
                self.state.accept_web_socket(&pair.server);
                Response::from_websocket(pair.client)
            }
            "/v2/sync/plan" => plan(&sql, &mut req).await,
            "/v2/sync/commit" => commit(&self.state, &sql, &mut req).await,
            _ => Response::error("not found", 404),
        }
    }
}

#[derive(Debug, Deserialize)]
struct Manifest {
    local_files: Vec<LocalFile>,
}

#[derive(Debug, Deserialize)]
struct LocalFile {
    path: String,
    hash: String,
    size: i64,
    #[serde(default)]
    last_seen_hash: String,
}

#[derive(Debug, Deserialize)]
struct StoredFile {
    path: String,
    hash: String,
    size: i64,
    deleted: i64,
}

#[derive(Debug, Deserialize)]
struct CommitRequest {
    operation: String,
    path: String,
    hash: String,
    size: i64,
    #[serde(default)]
    last_seen_hash: String,
}

#[derive(Debug, Serialize)]
struct PlanResponse {
    actions: Vec<SyncAction>,
}

#[derive(Debug, Serialize)]
struct SyncAction {
    path: String,
    action: &'static str,
    hash: String,
    size: i64,
}

#[derive(Debug, Serialize)]
struct CommitResponse {
    accepted: bool,
    conflict: bool,
    current_hash: String,
}

async fn plan(sql: &SqlStorage, req: &mut Request) -> Result<Response> {
    let manifest: Manifest = req.json().await?;
    let rows: Vec<StoredFile> = sql
        .exec("SELECT path, hash, size, deleted FROM files", None)?
        .to_array()?;
    let mut actions = Vec::new();

    for local in &manifest.local_files {
        let remote = rows.iter().find(|file| file.path == local.path);
        match remote {
            None if local.hash.is_empty() => {}
            None if !local.hash.is_empty() => actions.push(SyncAction {
                path: local.path.clone(),
                action: "upload",
                hash: local.hash.clone(),
                size: local.size,
            }),
            Some(remote) if remote.deleted != 0 => {
                if !local.hash.is_empty() {
                    actions.push(SyncAction {
                        path: local.path.clone(),
                        action: "upload",
                        hash: local.hash.clone(),
                        size: local.size,
                    });
                }
            }
            Some(remote) if remote.hash == local.hash => {}
            Some(remote)
                if !local.last_seen_hash.is_empty() && local.last_seen_hash == remote.hash =>
            {
                actions.push(SyncAction {
                    path: local.path.clone(),
                    action: "upload",
                    hash: local.hash.clone(),
                    size: local.size,
                });
            }
            Some(remote) if local.hash.is_empty() => actions.push(SyncAction {
                path: local.path.clone(),
                action: "delete",
                hash: remote.hash.clone(),
                size: 0,
            }),
            Some(remote) => actions.push(SyncAction {
                path: local.path.clone(),
                action: "conflict",
                hash: remote.hash.clone(),
                size: remote.size,
            }),
            None => {}
        }
    }

    for remote in rows.iter().filter(|file| file.deleted == 0) {
        if !manifest
            .local_files
            .iter()
            .any(|local| local.path == remote.path)
        {
            actions.push(SyncAction {
                path: remote.path.clone(),
                action: "download",
                hash: remote.hash.clone(),
                size: remote.size,
            });
        }
    }

    Response::from_json(&PlanResponse { actions })
}

async fn commit(state: &State, sql: &SqlStorage, req: &mut Request) -> Result<Response> {
    let input: CommitRequest = req.json().await?;
    let current: Option<StoredFile> = sql
        .exec(
            "SELECT path, hash, size, deleted FROM files WHERE path = ?",
            vec![input.path.clone().into()],
        )?
        .to_array()?
        .into_iter()
        .next();

    if input.operation == "delete" {
        if let Some(current) = current {
            if !input.last_seen_hash.is_empty() && current.hash != input.last_seen_hash {
                return Response::from_json(&CommitResponse {
                    accepted: false,
                    conflict: true,
                    current_hash: current.hash,
                });
            }
        }
        sql.exec(
            "INSERT INTO files(path, hash, size, deleted) VALUES (?, ?, 0, 1)
             ON CONFLICT(path) DO UPDATE SET hash = excluded.hash, size = 0, deleted = 1",
            vec![input.path.clone().into(), input.hash.into()],
        )?;
        broadcast(state, &input.path, "delete");
        return Response::from_json(&CommitResponse {
            accepted: true,
            conflict: false,
            current_hash: String::new(),
        });
    }

    if let Some(current) = current {
        if current.deleted == 0
            && !input.last_seen_hash.is_empty()
            && current.hash != input.last_seen_hash
        {
            return Response::from_json(&CommitResponse {
                accepted: false,
                conflict: true,
                current_hash: current.hash,
            });
        }
    }

    sql.exec(
        "INSERT INTO files(path, hash, size, deleted) VALUES (?, ?, ?, 0)
         ON CONFLICT(path) DO UPDATE SET hash = excluded.hash, size = excluded.size, deleted = 0",
        vec![
            input.path.clone().into(),
            input.hash.clone().into(),
            input.size.into(),
        ],
    )?;
    broadcast(state, &input.path, "change");
    Response::from_json(&CommitResponse {
        accepted: true,
        conflict: false,
        current_hash: input.hash,
    })
}

fn broadcast(state: &State, path: &str, event_type: &str) {
    let event = serde_json::json!({ "path": path, "type": event_type }).to_string();
    for socket in state.get_websockets() {
        let _ = socket.send_with_str(&event);
    }
}
