# Based on https://github.com/crossbario/autobahn-testsuite/blob/09cfbf74b0c8e335c6fc7df88e5c88349ca66879/docker/Dockerfile

FROM pypy:2-slim

# make "pypy" available as "python"
RUN ln -s /usr/local/bin/pypy /usr/local/bin/python

# We need this to fix pip & cryptography
RUN apt-get update && apt-get install -y build-essential libssl-dev

# install Autobahn|Testsuite
RUN pip install -U pip typing && \
	pip install autobahntestsuite=='0.8.2'

VOLUME /config
VOLUME /report

WORKDIR /
EXPOSE 9001 9001

CMD ["wstest", "--mode", "fuzzingclient", "--spec", "/config/fuzzingclient.json"]
