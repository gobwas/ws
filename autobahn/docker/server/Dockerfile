# GO_VERSION is passed by autobahn/script/test.sh from the host's
# `go env GOVERSION`, so the server under test is built with the same Go as the
# CI matrix entry. The default is only a fallback for manual builds.
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-alpine

WORKDIR /go/src/github.com/gobwas/ws

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go test -c -tags autobahn -coverpkg "github.com/gobwas/ws/..." github.com/gobwas/ws/example/autobahn

CMD ["./autobahn.test", "-test.coverprofile", "/report/server.coverage"]
