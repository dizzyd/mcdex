
APPS := mcdex/cmd/mcdex

VSN := $(shell git describe --long)
GOVSNFLAG := -ldflags "-X main.version=$(VSN)"

DOCKER_ARGS := -v $(shell pwd)/builds:/builds -w /mcdex mcdex

deps:
	go get -u ./cmd/mcdex

all:
	go build $(GOVSNFLAG) $(APPS)
	echo $(VSN) > mcdex.latest

clean:
	rm -rf bin builds

publish: release
	aws --profile mcdex s3 cp builds/mcdex.darwin.x64 s3://files.mcdex.net/releases/osx/mcdex
	aws --profile mcdex s3 cp builds/mcdex.linux.x64 s3://files.mcdex.net/releases/linux/mcdex
	aws --profile mcdex s3 cp builds/mcdex.exe s3://files.mcdex.net/releases/win32/mcdex.exe

release: clean
	mkdir builds
	$(MAKE) darwin.x64 docker.windows docker.linux

windows:
	$(shell cat windows.env) go build $(GOVSNFLAG) -x -v $(APPS)
	mv mcdex.exe /builds

linux:
	$(shell cat linux.x64.env) go build $(GOVSNFLAG) -x -v $(APPS)
	mv mcdex /builds/mcdex.linux.x64

darwin.x64:
	$(shell cat darwin.x64.env) go build $(GOVSNFLAG) -x -v $(APPS)
	mv mcdex builds/mcdex.darwin.x64

shell: docker.init
	docker run -ti $(DOCKER_ARGS) /bin/sh

docker.init:
	docker build -t mcdex -f Dockerfile .

docker.windows: docker.init
	docker run $(DOCKER_ARGS) make windows

docker.linux: docker.init
	docker run $(DOCKER_ARGS) make linux

docker.shell: docker.init
	docker run -ti $(DOCKER_ARGS) /bin/bash
