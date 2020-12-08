FROM alpine:3.7

RUN apk add --no-cache python python-dev gcc build-base musl-dev libffi-dev openssl openssl-dev && \
	python -m ensurepip && \
	pip install autobahntestsuite

VOLUME /config
VOLUME /report

CMD ["/usr/bin/wstest", "--mode", "fuzzingclient", "--spec", "/config/fuzzingclient.json"]
