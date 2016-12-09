#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>
#include <sys/mount.h>

int main(int argc, char *argv[])
{
    char *pathname;

    if (argc != 2) {
        fprintf(stderr, "Usage: %s target\n", argv[0]);
        exit(EXIT_FAILURE);
    }

    pathname = argv[1];

    if (umount2(pathname, 0) != 0) {
        fprintf(stderr, "umount failed: %s\n", strerror(errno));
        exit(EXIT_FAILURE);
    }

    exit(EXIT_SUCCESS);
}

