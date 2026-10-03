use worker::*;

#[event(fetch)]
async fn fetch(req: Request, env: Env, _ctx: Context) -> Result<Response> {
    let router = Router::new();

    router
        .get_async("/health", |_req, _ctx| async { Response::ok("ok") })
        .put_async("/v2/files/:hash", upload)
        .get_async("/v2/files/:hash", download)
        .delete_async("/v2/files/:hash", delete)
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

    async fn fetch(&self, _req: Request) -> Result<Response> {
        self.state.storage().put("ready", true).await?;
        Response::ok("ready")
    }
}
