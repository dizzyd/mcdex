FROM ubuntu:zesty

RUN apt-get -y update && apt-get install -y build-essential golang-1.8-go mingw-w64 openjdk-8-jre-headless

RUN mkdir /mcdex
ADD . /mcdex
