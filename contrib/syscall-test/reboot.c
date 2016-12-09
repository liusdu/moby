#define _GNU_SOURCE
#include <unistd.h>
#include <sys/reboot.h>
#include <errno.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>

int main(int argc, char **argv) {
	int	ret;
	ret = reboot(RB_AUTOBOOT);
	if (ret != 0) {
		fprintf(stderr, "reboot failed: %s\n", strerror(errno));
		exit(EXIT_FAILURE);
	}
	exit(EXIT_SUCCESS);
}
