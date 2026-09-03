.PHONY: proto test build_server build_desktop_client build_android_client \
        run-server run-client run-client-once check-client-env

# Regenerate gRPC/protobuf Go code from proto/sync/sync.proto
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       proto/sync/sync.proto

build_server:
	go build -o ./sync-server ./cmd/server

build_desktop_client:
	go build -o ./sync-client-desktop ./cmd/client

build_android_client:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o ./data/android/sync-client-arm64 ./cmd/client

# iSH is i686 (x86 32-bit)
build_ipad_client:
	GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -ldflags="-s -w" -o ./data/ipad/sync-client-ipad ./cmd/client

test:
	go test ./...

# ------------------------------------------------------------------
# Run targets.
#
# Server reads its config from the gitignored .env (Turso + R2).
# The client reads its per-device settings from a gitignored .env.client
# so each machine can use a different folder/address without a shared
# .env. The recipe injects those vars into the shell before launching.
# ------------------------------------------------------------------

# Fail with a hint if .env.client is missing.
check-client-env:
	@test -f ./.env.client || (echo "Missing ./.env.client. Create it (copy .env.client.example), then fill in SYNC_FOLDER/SYNC_ADDR/DEVICE_NAME for THIS device." && exit 1)

# Start the gRPC server (reads Turso + R2 from .env).
run-server:
	@test -f ./.env && echo "starting sync server..." || (echo "Missing ./.env with TURSO_URL/R2_* config" && exit 1)
	set -a; . ./.env; set +a; ./sync-server

# Run the client in daemon mode (watcher + periodic sync).
# Uses $(CLIENT); build it first with `make build_desktop_client`, or ship
# the arm64 build on a phone.
run-client: check-client-env
	set -a; . ./.env.client; set +a; $(CLIENT)

# Client binary to run. Defaults to the desktop build; override on a phone
# with: make run-client-once CLIENT=./sync-client-arm64
CLIENT ?= ./sync-client-desktop

# Run the client once (single sync cycle, then exit) - for mobile/manual.
run-client-once: check-client-env
	set -a; . ./.env.client; set +a; $(CLIENT) --once
