PROJECT_ROOT := $(shell git rev-parse --show-toplevel)
GO_FILES := $(shell git ls-files '*.go' '*.sum')
EMBED_FILES := cli/wrap_dockerd.sh
IMAGE_FILES := $(shell find deploy)
ARCH ?= linux/$(shell go env GOARCH)
SYSBOX_SHA ?= $(shell ./scripts/sysbox_sha.sh $(ARCH))
GOOS := $(word 1,$(subst /, ,$(ARCH)))
GOARCH := $(word 2,$(subst /, ,$(ARCH)))

.PHONY: clean
clean:
	rm -rf build

.PHONY: build/envbox
build/envbox: $(GO_FILES) $(EMBED_FILES)
	mkdir -p $(@D)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o build/envbox ./cmd/envbox

.PHONY: build/image/envbox
build/image/envbox: build/image/envbox/$(GOOS)_$(GOARCH)/.ctx

build/image/envbox/$(GOOS)_$(GOARCH)/.ctx: build/envbox $(IMAGE_FILES) scripts/sysbox_sha.sh
	rm -rf $(@D)
	mkdir -p $(@D)
	cp -r build/envbox deploy/. $(@D)
	docker buildx build --build-arg SYSBOX_SHA=$(SYSBOX_SHA) --load -t envbox --platform $(ARCH) $(@D)
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
