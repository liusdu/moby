#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>

int main(int argc, char *argv[])
{
    if (mkdir("/proc2", 0555) != 0) {
	fprintf(stderr, "mkdir failed: %s\n", strerror(errno));
        exit(EXIT_FAILURE);
    }
    if (mount("proc","/proc2", "proc", 0, NULL) !=0 && errno == EPERM ) {
        fprintf(stderr, "mount failed: %s\n", strerror(errno));
        exit(EXIT_FAILURE);
    }

    exit(EXIT_SUCCESS);
}

