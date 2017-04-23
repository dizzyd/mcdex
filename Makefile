
APPS := mcdex

DOCKER_ARGS := -v $(shell pwd)/bin/docker:/mcdex/bin -w /mcdex mcdex

all:
	go install $(APPS)

clean:
	rm -rf pkg
	rm -rf bin

windows:
	$(shell cat windows.env) go install -x -v $(APPS)

linux:
	$(shell cat linux.env) go install -x -v $(APPS)

docker: docker.windows docker.linux

docker.init:
	docker build -t mcdex -f Dockerfile .

docker.windows: docker.init
	docker run $(DOCKER_ARGS) make windows

docker.linux: docker.init
	docker run $(DOCKER_ARGS) make linux

docker.shell: docker.init
	docker run -ti $(DOCKER_ARGS) /bin/bash
