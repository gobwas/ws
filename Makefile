TEST ?=.

BENCH     ?=.
BENCH_BASE?=master

autobahn:
	go build -o ./bin/autobahn ./example/autobahn

test:
	go test -run=$(TEST) -cover ./...

testrfc: PID:=$(shell mktemp -t autobahn.XXXX)
testrfc: autobahn
	./bin/autobahn & echo $$! > $(PID)
	if [ -z "$$(ps | grep $$(cat $(PID)) | grep autobahn)" ]; then\
		echo "could not start autobahn";\
		exit 1;\
	fi;\
	wstest -m fuzzingclient -s ./example/autobahn/fuzzingclient.json
	pkill -9 -F $(PID)

rfc: testrfc
	open ./example/autobahn/reports/servers/index.html

bench:
	go test -run=none -bench=$(BENCH) -benchmem

benchcmp: BENCH_BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
benchcmp: BENCH_OLD:=$(shell mktemp -t old.XXXX)
benchcmp: BENCH_NEW:=$(shell mktemp -t new.XXXX)
benchcmp:
	if [ ! -z "$(shell git status -s)" ]; then\
		echo "could not compare with $(BENCH_BASE) – found unstaged changes";\
		exit 1;\
	fi;\
	if [ "$(BENCH_BRANCH)" == "$(BENCH_BASE)" ]; then\
		echo "comparing the same branches";\
		exit 1;\
	fi;\
	echo "benchmarking $(BENCH_BRANCH)...";\
	go test -run=none -bench=$(BENCH) -benchmem > $(BENCH_NEW);\
	echo "benchmarking $(BENCH_BASE)...";\
	git checkout -q $(BENCH_BASE);\
	go test -run=none -bench=$(BENCH) -benchmem > $(BENCH_OLD);\
	git checkout -q $(BENCH_BRANCH);\
	echo "\nresults:";\
	echo "========\n";\
	benchcmp $(BENCH_OLD) $(BENCH_NEW);\

