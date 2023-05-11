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

.PHONY: fmt
fmt: fmt/go fmt/tf fmt/md

.PHONY: fmt/go
fmt/go:
	# VS Code users should check out
	# https://github.com/mvdan/gofumpt#visual-studio-code
	go run mvdan.cc/gofumpt@v0.4.0 -w -l .

.PHONY: fmt/tf
fmt/tf:
	# VS Code users should check out
	# https://github.com/mvdan/gofumpt#visual-studio-code
	terraform fmt ./template.tf

.PHONY: fmt/md
fmt/md:
	go run github.com/shurcooL/markdownfmt@v0.0.0-20210117162146-75134924a9fd -w ./README.md
