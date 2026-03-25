.PHONY: build test clean run install deps

BINARY=judgejudy
VERSION=0.1.0
BUILD_DIR=./build

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/judgejudy

install:
	go install ./cmd/judgejudy

test:
	go test ./... -v -race

deps:
	go mod tidy
	pip install -r python/requirements.txt

clean:
	rm -rf $(BUILD_DIR)
	rm -f *.html

lint:
	golangci-lint run ./...

# Run an example eval
example-text:
	$(BUILD_DIR)/$(BINARY) run examples/text_eval.yaml --report

example-image:
	$(BUILD_DIR)/$(BINARY) run examples/image_eval.yaml --report
