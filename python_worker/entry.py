import json
from http import HTTPMethod
from urllib.parse import urlparse

import js
from js import WebSocketPair
from pyodide.ffi import create_proxy, to_js
from workers import DurableObject, Request, Response, WorkerEntrypoint

try:
    from .cloudflare import D1MetadataStore, R2BlobStore
    from .sync_service import (
        CommitRequest,
        EventPublisher,
        LocalFile,
        SyncService,
    )
except ImportError:
    from cloudflare import D1MetadataStore, R2BlobStore
    from sync_service import CommitRequest, EventPublisher, LocalFile, SyncService


class DurableObjectEventPublisher(EventPublisher):
    def __init__(self, namespace) -> None:
        self._namespace = namespace

    async def publish(self, path: str, event_type: str) -> None:
        stub = self._namespace.getByName("default")
        await stub.publish(path, event_type)


class EventHub(DurableObject):
    async def fetch(self, request):
        client, server = WebSocketPair.new().object_values()
        self.ctx.acceptWebSocket(server)
        return Response(None, status=101, web_socket=client)

    def webSocketMessage(self, websocket, message):
        return None

    def webSocketClose(self, websocket, code, reason, was_clean):
        websocket.close(code, reason)

    async def publish(self, path: str, event_type: str) -> None:
        payload = json.dumps({"path": path, "type": event_type})
        for websocket in self.ctx.getWebSockets():
            websocket.send(payload)


class Default(WorkerEntrypoint):
    async def fetch(self, request: Request) -> Response:
        if request.method == HTTPMethod.GET and self._path(request) == "/health":
            return Response("ok")

        if not self._authorized(request):
            return Response("unauthorized", status=401)

        path = self._path(request)
        if path == "/v2/events":
            return await self._events(request)

        metadata = D1MetadataStore(self.env.DB)
        await metadata.initialize()
        service = SyncService(
            metadata=metadata,
            blobs=R2BlobStore(self.env.FILES),
            events=DurableObjectEventPublisher(self.env.EVENTS),
        )

        if path.startswith("/v2/files/"):
            return await self._file_request(request, path)
        if path == "/v2/sync/plan" and request.method == HTTPMethod.POST:
            return await self._plan(request, service)
        if path == "/v2/sync/commit" and request.method == HTTPMethod.POST:
            return await self._commit(request, service)
        return Response("not found", status=404)

    async def _events(self, request: Request) -> Response:
        stub = self.env.EVENTS.getByName("default")
        internal_request = Request(
            "https://events.internal",
            method="GET",
            headers={"Upgrade": "websocket"},
        )
        websocket_response = await stub.fetch(internal_request)
        websocket = websocket_response.js_object.webSocket
        websocket.accept()

        def enqueue(controller, payload: str) -> None:
            data = f"data: {payload}\n\n".encode()
            array = js.Uint8Array.new(len(data))
            array.set(to_js(list(data)))
            controller.enqueue(array)

        def start(controller) -> None:
            enqueue(controller, "connected")

            def on_message(event) -> None:
                enqueue(controller, str(event.data))

            def on_close(_event) -> None:
                controller.close()

            websocket.addEventListener("message", create_proxy(on_message))
            websocket.addEventListener("close", create_proxy(on_close))

        stream = js.ReadableStream.new(
            to_js({"start": create_proxy(start)}, dict_converter=js.Object.fromEntries)
        )
        return Response(
            stream,
            headers={
                "Content-Type": "text/event-stream",
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
            },
        )

    async def _file_request(self, request: Request, path: str) -> Response:
        content_hash = path.removeprefix("/v2/files/")
        if not content_hash or "/" in content_hash:
            return Response("invalid hash", status=400)

        if request.method == HTTPMethod.PUT:
            await self.env.FILES.put(f"files/{content_hash}", await request.bytes())
            return Response("stored")

        if request.method == HTTPMethod.GET:
            obj = await self.env.FILES.get(f"files/{content_hash}")
            if obj is None:
                return Response("not found", status=404)
            return Response(obj.body)

        if request.method == HTTPMethod.DELETE:
            await self.env.FILES.delete(f"files/{content_hash}")
            return Response("deleted")

        return Response("method not allowed", status=405)

    async def _plan(self, request: Request, service: SyncService) -> Response:
        body = await request.json()
        local_files = [
            LocalFile(
                path=item["path"],
                hash=item["hash"],
                size=int(item["size"]),
                last_seen_hash=item.get("last_seen_hash", ""),
            )
            for item in body.get("local_files", [])
        ]
        actions = await service.plan(local_files)
        return Response.from_json(
            {
                "actions": [
                    {
                        "path": action.path,
                        "action": action.action,
                        "hash": action.hash,
                        "size": action.size,
                    }
                    for action in actions
                ]
            }
        )

    async def _commit(self, request: Request, service: SyncService) -> Response:
        body = await request.json()
        try:
            result = await service.commit(
                CommitRequest(
                    operation=body["operation"],
                    path=body["path"],
                    hash=body.get("hash", ""),
                    size=int(body.get("size", 0)),
                )
            )
        except (KeyError, ValueError) as error:
            return Response(str(error), status=400)
        return Response.from_json(
            {
                "accepted": result.accepted,
                "conflict": result.conflict,
                "current_hash": result.current_hash,
            }
        )

    def _authorized(self, request: Request) -> bool:
        return request.headers.get("Authorization") == (f"Bearer {self.env.SYNC_TOKEN}")

    @staticmethod
    def _path(request: Request) -> str:
        return urlparse(request.url).path
