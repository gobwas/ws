FROM alpine:3.7

RUN apk add --no-cache python3 python3-dev gcc build-base musl-dev libffi-dev openssl-dev && \
	python3 -m ensurepip && \
	pip3 install --upgrade pip && \
	pip3 install autobahntestsuite

VOLUME /config
VOLUME /report

CMD ["/usr/bin/wstest", "--mode", "fuzzingclient", "--spec", "/config/fuzzingclient.json"]
