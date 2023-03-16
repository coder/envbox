PROJECT_ROOT := $(shell git rev-parse --show-toplevel)
GO_FILES := $(shell git ls-files '*.go' '*.sum')
IMAGE_FILES := $(shell find deploy)

.PHONY: clean
clean:
	rm -rf build

build/envbox: $(GO_FILES)
	go build -o build/envbox ./cmd/envbox

.PHONY: build/image/envbox
build/image/envbox: build/image/envbox/.ctx

build/image/envbox/.ctx: build/envbox $(IMAGE_FILES)
	mkdir -p $(@D)
	cp -r build/envbox deploy/. $(@D)
	docker build -t envbox $(@D)
	touch $@

fmt:
	# VS Code users should check out
	# https://github.com/mvdan/gofumpt#visual-studio-code
	go run mvdan.cc/gofumpt@v0.4.0 -w -l .