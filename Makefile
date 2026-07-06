SERVICE := proxy-dataplane
IMAGE ?= dendenmushi/$(SERVICE):local
PORT ?= 8083
SESSION_MANAGER_URL ?= http://localhost:8081
SESSION_AUDIT_URL ?= http://localhost:8085
STS_URL ?= http://localhost:8082
PROXY_DATAPLANE_PUBLIC_URL ?= ws://localhost:8083
MAX_SESSIONS ?= 50
GOCACHE ?= /private/tmp/ddm-go-build-cache
GOMODCACHE ?= /private/tmp/ddm-go-mod-cache

.PHONY: run test build docker-build clean

run:
	PORT=$(PORT) SESSION_MANAGER_URL=$(SESSION_MANAGER_URL) SESSION_AUDIT_URL=$(SESSION_AUDIT_URL) STS_URL=$(STS_URL) PROXY_DATAPLANE_PUBLIC_URL=$(PROXY_DATAPLANE_PUBLIC_URL) MAX_SESSIONS=$(MAX_SESSIONS) go run ./cmd/server

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o bin/$(SERVICE) ./cmd/server

docker-build:
	docker build -t $(IMAGE) .

clean:
	rm -rf bin
