.PHONY: test
test:
	go test ./...

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

.PHONY: tidy
tidy:
	go mod tidy -compat=1.17

.PHONY: tools
	go install -u google.golang.org/protobuf/cmd/protoc-gen-go
	go install -u google.golang.org/grpc/cmd/protoc-gen-go-grpc

.PHONY: proto
proto: tools
	buf generate
