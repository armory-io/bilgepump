## Simple makefile for goreleaser to build locally

all: compile lint test

clean:
	rm -rf dist/


compile:  ## Compile for the local architecture ⚙
	@echo "Compiling..."
	goreleaser build  --snapshot --clean


.PHONY: lint
lint: ## Runs the linter
	golangci-lint run --timeout 60s

test: ## 🤓 Run go tests
	@echo "Testing..."
	go test -v ./...

format: ## Format the code using gofmt
	@echo "Formatting..."
	@gofmt -s -w $(shell find . -name '*.go' -not -path "./vendor/*")

check-format: ## Used by CI to check if code is formatted
	@gofmt -l $(shell find . -name '*.go' -not -path "./vendor/*") | grep ".*" ; if [ $$? -eq 0 ]; then exit 1; fi
