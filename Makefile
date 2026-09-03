.PHONY: proto test

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
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o ./data/sync-client-arm64 ./cmd/client

test:
	go test ./...
