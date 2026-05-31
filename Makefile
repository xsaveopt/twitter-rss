BINARY := twitter-rss
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build run tidy docker docker-run clean

build:
	go build $(LDFLAGS) -o bin/$(BINARY) .

run:
	go run $(LDFLAGS) .

tidy:
	go mod tidy

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) .

docker-run:
	docker run --rm -p 8080:8080 -e TWITTER_RSS_NITTER $(BINARY):$(VERSION)

clean:
	rm -rf bin
