BENCH     ?=.
BENCH_BASE?=master

bin/reporter:
	go build -o bin/reporter ./autobahn

autobahn: bin/reporter
	./autobahn/script/test.sh --build ws --build autobahn --network ts0
	bin/reporter $(PWD)/autobahn/report/index.json


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

