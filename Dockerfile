FROM golang:1.12

RUN apt-get -y update && apt-get install -y build-essential mingw-w64 openjdk-11-jre-headless git

RUN mkdir /mcdex
ADD . /mcdex
