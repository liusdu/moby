#!/bin/bash

IMG=empty-image:latest

fail(){
	rm -rf $WORK_DIR
	exit 1
}

usage(){
	echo "Usage: empty-image.sh [options]"
	echo " Options:"
	echo "  -h     Show help messages"
	echo "  -t     Specify the empty image name: '<name[:tag]>'"
	echo "         String format is the same with docker"
	echo " "
	echo " This tool is used to build empty images for docker"
}
while getopts "ht:" arg
do
	case $arg in
		h)
			usage
			shift
			exit 0
			;;
		t)
			IMG=$OPTARG
			shift 2
			;;
		?)
			echo "unkonw argument"
			usage
			shift
			exit 1
			;;
	esac
done

# unhandled parameter
if [ $# -gt 0 ]; then
	usage
	exit 1
fi

WORK_DIR=`mktemp -d`
Dockerfile="$WORK_DIR/dockerfile"

cat > $Dockerfile << EOF
FROM scratch
ENV container docker
STOPSIGNAL SIGRTMIN+3
CMD [ "/sbin/init" ]
EOF

docker build --force-rm -t $IMG -f $Dockerfile $WORK_DIR || fail
rm -rf $WORK_DIR

echo "Empty Image [$IMG] is generated successfully"
