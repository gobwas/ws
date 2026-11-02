# Use the upstream Autobahn|Testsuite image, pinned by version so that CI runs
# are reproducible; bump it deliberately.
FROM crossbario/autobahn-testsuite:25.10.1

VOLUME /config
VOLUME /report

WORKDIR /
EXPOSE 9001 9001

CMD ["wstest", "--mode", "fuzzingclient", "--spec", "/config/fuzzingclient.json"]
