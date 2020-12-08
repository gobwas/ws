#!/bin/bash

FOLLOW_LOGS=0

while [[ $# -gt 0 ]]; do
	key="$1"
	case $key in
		--network)
		NETWORK="$2"
		shift
		;;

		--build)
		case "$2" in
			autobahn)
				docker build . --file autobahn/docker/autobahn/Dockerfile --tag ws-autobahn
				shift
			;;
			server)
				docker build . --file autobahn/docker/server/Dockerfile --tag ws-server
				shift
			;;
			*)
				docker build . --file autobahn/docker/autobahn/Dockerfile --tag ws-autobahn
				docker build . --file autobahn/docker/server/Dockerfile --tag ws-server
			;;
		esac
		;;

		--run)
		docker run \
			--interactive \
			--tty \
			${@:2}
		exit $?
		;;

		--follow-logs)
		FOLLOW_LOGS=1
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

random=$(xxd -l 4 -p /dev/random)
server="${random}_ws-server"
autobahn="${random}_ws-autobahn"

network="ws-network-$random"
docker network create --driver bridge "$network"
if [ $? -ne 0 ]; then
	exit 1
fi

docker run \
	--interactive \
	--tty \
	--detach \
	--network="$network" \
	--network-alias="ws-server" \
	-v $(pwd)/autobahn/report:/report \
	--name="$server" \
	"ws-server"

docker run \
	--interactive \
	--tty \
	--detach \
	--network="$network" \
	-v $(pwd)/autobahn/config:/config \
	-v $(pwd)/autobahn/report:/report \
   	--name="$autobahn" \
	"ws-autobahn"


if [[ $FOLLOW_LOGS -eq 1 ]]; then
	(with_prefix "$(tput setaf 3)[ws-autobahn]: $(tput sgr0)" docker logs --follow "$autobahn")&
	(with_prefix "$(tput setaf 5)[ws-server]:   $(tput sgr0)" docker logs --follow "$server")&
fi

trap ctrl_c INT
ctrl_c () {
	echo "SIGINT received; cleaning up"
	docker kill --signal INT "$autobahn" >/dev/null
	docker kill --signal INT "$server" >/dev/null
	cleanup
	exit 130
} 

cleanup() {
	docker rm "$server" >/dev/null
	docker rm "$autobahn" >/dev/null
	docker network rm "$network"
}

docker wait "$autobahn" >/dev/null
docker stop "$server" >/dev/null

cleanup
