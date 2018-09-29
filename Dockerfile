FROM golang:1.11

RUN apt-get -y update && apt-get install -y build-essential mingw-w64 openjdk-8-jre-headless git

RUN mkdir /mcdex
ADD . /mcdex
