PROJECT_ROOT := $(shell git rev-parse --show-toplevel)
GO_FILES := $(shell git ls-files '*.go' '*.sum')
IMAGE_FILES := $(shell find deploy)
ARCH ?= linux/amd64
SYSBOX_SHA ?= b7ac389e5a19592cadf16e0ca30e40919516128f6e1b7f99e1cb4ff64554172e

.PHONY: clean
clean:
	rm -rf build

build/envbox: $(GO_FILES)
	CGO_ENABLED=0 go build -o build/envbox ./cmd/envbox

.PHONY: build/image/envbox
build/image/envbox: build/image/envbox/.ctx

build/image/envbox/.ctx: build/envbox $(IMAGE_FILES)
	mkdir -p $(@D)
	cp -r build/envbox deploy/. $(@D)
	docker buildx build --build-arg SYSBOX_SHA=$(SYSBOX_SHA) -t envbox --platform $(ARCH) $(@D)
	touch $@

.PHONY: fmt
fmt: fmt/go fmt/md

.PHONY: fmt/go
fmt/go:
	# VS Code users should check out
	# https://github.com/mvdan/gofumpt#visual-studio-code
	go run mvdan.cc/gofumpt@v0.4.0 -w -l .

.PHONY: fmt/md
fmt/md:
	go run github.com/Kunde21/markdownfmt/v3/cmd/markdownfmt@v3.1.0 -w ./README.md

.PHONY: test
test:
	go test -v -count=1 ./...

.PHONY: test-integration
test-integration:
	CODER_TEST_INTEGRATION=1 go test -v -count=1 ./integration/
