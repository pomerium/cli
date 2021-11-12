.PHONY: test
test:
	go test ./...

.PHONY: lint
lint: 
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

.PHONY: tidy
tidy:
	go mod tidy -compat=1.17
