FROM ubuntu:latest

RUN apt-get -y update && apt-get install -y build-essential golang mingw-w64 openjdk-8-jre-headless git
RUN go get golang.org/dl/go1.11
RUN ~/go/bin/go1.11 download

RUN mkdir /mcdex
ADD . /mcdex
