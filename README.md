This is just an attempt to build a cli based tool to syncronize my files across my devices. It isn't that there are no existing solutions, there are giants like Google Drive, iCloud or Dropbox, then there are some P2P tools like Syncthing. The project is intended to be both - a fun learning project and serve my usecase of syncing files with my custom centralised storage backend (I have opted for client-server based architecture for now instead of P2P for simplicity). I am heavily utilizing OpenCode for building this project. I am trying it for the first time, it provides some decent coding models along with the options to connect with external APIs. Cloudflare R2 provides 10GB of free storage in their free tier, so that was the reason for going towards it. As for Turso for my metadata storage, I just find the project interesting, it is a fork of SQLite written in Rust (I mean libsql is the actual fork, on top of which Turso build their SaaS). They also have a pretty generous free tier(atleast for now). EDIT: Things have changed, I shifted the entire backend to Cloudflare, and replaced the Go backend with a Rust one to easily use Clouflare Workers via WASM. Also Turso got acquired by Supabase :|
The actual README for the project is here - [MAIN.md](./MAIN.md)


## Architecture

```mermaid
flowchart TB
    subgraph Clients["Go CLI clients"]
        Mac["macOS client<br/>launchd user service"]
        Android["Android client<br/>Termux / --once"]
        IPad["iPad client<br/>iSH / --once"]
    end

    subgraph Client["Shared client engine"]
        Watcher["fsnotify watcher<br/>(recursive where supported)"]
        Reconcile["Startup reconcile<br/>(one-shot, recursive disk-vs-DB)"]
        LocalDB["SQLite DB<br/>local_files + pending_operations"]
        Manifest["DB-driven manifest<br/>(re-hash pending files only)"]
        SyncEngine["Event-driven sync engine"]

        Watcher -->|"record local change"| LocalDB
        Reconcile -->|"record create/modify/delete"| LocalDB
        LocalDB --> Manifest
        Manifest --> SyncEngine
    end

    subgraph Worker["Cloudflare Worker (Rust/WASM)"]
        API["Authenticated HTTP/JSON API"]
        DurableObject["Durable Object<br/>SQLite metadata + coordination"]
        R2["Cloudflare R2<br/>content-addressed file bytes"]
        Events["Authenticated SSE"]

        API --> DurableObject
        API --> R2
        DurableObject --> Events
    end

    Mac --> Client
    Android --> Client
    IPad --> Client

    SyncEngine <-->|"sync plan, commits,<br/>file bytes"| API
    SyncEngine <-->|"remote change events"| Events
```

## License

Released under the [MIT License](./LICENSE). :)
