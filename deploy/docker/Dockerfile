FROM golang:1.14.1 as builder

WORKDIR $GOPATH/github.com/mKaloer/TFServingCache

# Fetch dependencies
COPY go.mod go.sum ./
COPY proto/tensorflow/core/go.mod ./proto/tensorflow/core/
RUN GO111MODULE=on go mod download

# Now pull in our code
COPY . .

# Build
RUN CGO_ENABLED=0 go build -a -o ./bin/taskhandler ./cmd/taskhandler/

FROM alpine:3.12
RUN apk --no-cache add ca-certificates

WORKDIR /tfservingcache/

COPY --from=builder /go/github.com/mKaloer/TFServingCache/bin/* .
ADD deploy/docker/config.yaml .

ENTRYPOINT ["./taskhandler"]
