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
	python3 -m unittest discover -s python_worker -p 'test_*.py'

worker-dev:
	cd python_worker && uv run pywrangler dev --local --port 8787 --var SYNC_TOKEN:dev-token

worker-secret:
	cd python_worker && uv run pywrangler secret put SYNC_TOKEN

worker-deploy:
	cd python_worker && uv run pywrangler deploy
