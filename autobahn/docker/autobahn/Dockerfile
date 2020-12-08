FROM alpine:3.12

RUN apk add --no-cache python2 python2-dev gcc build-base musl-dev libffi-dev openssl-dev && \
	python -m ensurepip && \
	pip install --upgrade pip && \
	pip install --no-python-version-warning autobahntestsuite

VOLUME /config
VOLUME /report

CMD ["/usr/bin/wstest", "--mode", "fuzzingclient", "--spec", "/config/fuzzingclient.json"]
