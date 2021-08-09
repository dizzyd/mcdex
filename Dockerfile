FROM golang:1.16

RUN apt-get -y update && apt-get install -y build-essential mingw-w64 openjdk-11-jre-headless git

RUN mkdir /usr/local/go/src/mcdex
ADD . /usr/local/go/src/mcdex
