This is just an attempt to build a cli based tool to syncronize my files across my devices. It isn't that there are no existing solutions, there are giants like Google Drive, iCloud or Dropbox, then there are some P2P tools like Syncthing. There are also self hostable cloud options like NextCLoud and OwnCloud (I am not sure if they provide sync functionality though). The project is intended to be both - a fun learning project and serve my usecase of syncing files with my custom centralised storage backend (I have opted for client-server based architecture for now instead of P2P for simplicity). I am heavily utilizing OpenCode for building this project. I am trying it for the first time, it provides some decent coding models along with the options to connect with external APIs. Cloudflare R2 provides 10GB of free storage in their free tier, so that was the reason for going towards it. As for Turso for my metadata storage, I just find the project interesting, it is a fork of SQLite written in Rust (I mean libsql, on top of which Turso build their SaaS). They also have a pretty generous free tier(atleast for now).

The actual README for the project is here - [MAIN.md](./MAIN.md)


## Architecture

```mermaid
flowchart TB
    subgraph Client["Client Daemon (Go)"]
        Watcher["fsnotify watcher<br/>(top-level dir only)"]
        Reconcile["Startup reconcile<br/>(one-shot, recursive disk-vs-DB)"]
        LocalDB["SQLite DB<br/>local_files + pending_operations"]
        Manifest["DB-driven manifest<br/>(re-hash pending files only)"]
        SyncEngine["Sync Engine<br/>(ticker, every SYNC_INTERVAL)"]

        Watcher -->|"RecordChange"| LocalDB
        Reconcile -->|"record create/modify/delete"| LocalDB
        LocalDB --> Manifest
        Manifest --> SyncEngine
    end

    subgraph Server["Go Sync Server"]
        Auth["AuthInterceptor<br/>(bearer token, RegisterDevice exempt)"]
        Transport["Service (gRPC transport)"]
        App["SyncService (business)<br/>ComputeSyncPlan / ApplyUpload / FetchFile"]
        Repos["FileRepository<br/>VersionRepository, DeviceRepository"]
        R2Client["R2Client"]

        Auth --> Transport
        Transport --> App
        App --> Repos
        App --> R2Client
    end

    Turso["Turso metadata DB"]
    R2["Cloudflare R2<br/>uploads/<file_id>/<version_id>"]

    SyncEngine <-->|"GetSyncPlan /<br/>UploadFile / DownloadFile"| Auth
    Repos <-->|"metadata"| Turso
    R2Client <-->|"bytes"| R2
```
