# step1: build image
FROM golang:1.13-alpine

WORKDIR /go/src/github.com/chenjiandongx/dockerstats

ENV GO111MODULE=off
ENV TZ=Asia/Shanghai
ENV GIN_MODE=release
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
ADD . /go/src/github.com/chenjiandongx/dockerstats
RUN go build ./cmd/main.go
ENTRYPOINT ["./main"]
