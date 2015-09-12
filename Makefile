VERSION := $(shell git describe --tags)

build:
	go build -o dashing -ldflags "-X main.version=${VERSION}" dashing.go

install: build
	install -d ${DESTDIR}/usr/local/bin/
	install -m 755 ./dashing ${DESTDIR}/usr/local/bin/dashing

test:
	go test ./...

clean:
	rm -f ./dashing
	rm -f ./dashing.test

.PHONY: build test install clean
