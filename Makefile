.PHONY: proto build test

# Regenerate gRPC/protobuf Go code from proto/sync/sync.proto
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       proto/sync/sync.proto

build:
	go build ./...

test:
	go test ./...
