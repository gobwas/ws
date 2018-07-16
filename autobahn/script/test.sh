#!/bin/bash

LOG_AUTOBAHN=0
LOG_WS=0

while [[ $# -gt 0 ]]; do
	key="$1"
	case $key in
		-b|--build)
		case "$2" in
			autobahn)
			docker build . -f autobahn/docker/autobahn/Dockerfile -t autobahn
			;;
			ws)
			docker build . -f autobahn/docker/ws/Dockerfile -t ws
			;;
		esac
		shift
		;;

		--log)
		case "$2" in
			autobahn)
			LOG_AUTOBAHN=1
			;;
			ws)
			LOG_WS=1
			;;
		esac
		shift
		;;
	esac
	shift
done

with_prefix() {
	local p="$1"
	shift
	
	local out=$(mktemp -u ws.fifo.out.XXXX)
	local err=$(mktemp -u ws.fifo.err.XXXX)
	mkfifo $out $err
	if [ $? -ne 0 ]; then
		exit 1
	fi
	
	# Start two background sed processes.
	sed "s/^/$p/" <$out &
	sed "s/^/$p/" <$err >&2 &
	
	# Run the program
	"$@" >$out 2>$err
	rm $out $err
}

docker run -itd --name=ws_test --network=docker_default --network-alias=ws ws
docker run -itd --name=autobahn_test -v $(pwd)/autobahn/config:/config -v $(pwd)/autobahn/report:/report --network=docker_default autobahn

docker wait autobahn_test >/dev/null
if [[ $LOG_AUTOBAHN -eq 1 ]]; then
	with_prefix "$(tput setaf 3)[autobahn]: $(tput sgr0)" docker logs autobahn_test
fi

if [[ $LOG_WS -eq 1 ]]; then
	with_prefix "$(tput setaf 3)[ws]:       $(tput sgr0)" docker logs ws_test
fi
docker stop ws_test >/dev/null

docker rm ws_test >/dev/null
docker rm autobahn_test >/dev/null


