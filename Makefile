.PHONY: fmt clean test generate

# Formatting runs through golangci-lint so the formatters and their section
# order come from .golangci.yml: sweeping the tree with bare goimports/gci
# instead both disagrees with the lint job and rewrites the generated files,
# whose emitted formatting is the contract CI diff-checks.
fmt:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	golangci-lint fmt ./...

clean:
	go clean -testcache

test:
	go test -cover ./...

generate:
	cd examples/petstore/gen && go run .
