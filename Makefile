.PHONY: test build-client build_android_client build_ipad_client \
        run-client run-client-once \
        worker-check worker-dev worker-secret worker-deploy setup-env

test:
	go test ./...

build-client:
	go build -trimpath -ldflags="-s -w" -o ./sync-client ./cmd/client


build_android_client:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o ./data/android/sync-client-arm64 ./cmd/client

build_ipad_client:
	GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -ldflags="-s -w" -o ./data/ipad/sync-client-ipad ./cmd/client

run-client:
	go run ./cmd/client

run-client-once:
	go run ./cmd/client --once

setup-env:
	@test -f .env || cp .env.example .env
	@echo "Created .env from .env.example; set SYNC_TOKEN before running the client."

worker-check:
	cargo fmt --manifest-path rust_server/Cargo.toml --check
	cargo check --manifest-path rust_server/Cargo.toml

worker-dev:
	cd rust_server && wrangler dev --local --port 8787

worker-secret:
	cd rust_server && wrangler secret put SYNC_TOKEN

worker-deploy:j
	cd rust_server && wrangler deploy
