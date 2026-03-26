.PHONY: build test clean run install deps

BINARY=judgejudy
VERSION=0.1.0
BUILD_DIR=./build

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/judgejudy

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/judgejudy

test:
	go test ./... -v -race

deps:
	go mod tidy
	pip install -r python/requirements.txt

clean:
	rm -rf $(BUILD_DIR)
	rm -f report_*.html

lint:
	golangci-lint run ./...

# Run an example eval
example-text:
	$(BUILD_DIR)/$(BINARY) run examples/text_eval.yaml --report text_eval_report.html

example-image:
	$(BUILD_DIR)/$(BINARY) run examples/image_eval.yaml --report image_eval_report.html
