
APPS := mcdex

VSN := $(shell git describe --long)
GOVSNFLAG := -ldflags "-X main.version=$(VSN)"

DOCKER_ARGS := -v $(shell pwd)/bin/docker:/mcdex/bin -w /mcdex mcdex

all:
	go install $(GOVSNFLAG) $(APPS)

clean:
	rm -rf pkg
	rm -rf bin

publish: clean all docker
	aws --profile mcdex s3 cp bin/mcdex s3://files.mcdex.net/releases/osx/mcdex
	aws --profile mcdex s3 cp bin/docker/mcdex s3://files.mcdex.net/releases/linux/mcdex
	aws --profile mcdex s3 cp bin/docker/windows_386/mcdex.exe s3://files.mcdex.net/releases/win32/mcdex.exe

windows:
	$(shell cat windows.env) go install $(GOVSNFLAG) -x -v $(APPS)

linux:
	$(shell cat linux.env) go install $(GOVSNFLAG) -x -v $(APPS)

docker: docker.windows docker.linux

docker.init:
	docker build -t mcdex -f Dockerfile .

docker.windows: docker.init
	docker run $(DOCKER_ARGS) make windows

docker.linux: docker.init
	docker run $(DOCKER_ARGS) make linux

docker.shell: docker.init
	docker run -ti $(DOCKER_ARGS) /bin/bash
