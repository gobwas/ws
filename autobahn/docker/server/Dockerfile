FROM golang:1.15.6-alpine3.12

RUN	apk add --no-cache git && \
	go get github.com/gobwas/httphead/... && \
	go get github.com/gobwas/pool/... 

COPY . /go/src/github.com/gobwas/ws
RUN go test -c -tags autobahn -coverpkg "github.com/gobwas/ws/..." github.com/gobwas/ws/example/autobahn

CMD ["./autobahn.test", "-test.coverprofile", "/report/server.coverage"]
