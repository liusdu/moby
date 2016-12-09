#define _GNU_SOURCE
#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>

#define errExit(msg)    do { perror(msg); exit(EXIT_FAILURE); \
                        } while (0)

int main(int argc, char *argv[])
{
    struct file_handle *fhp;
    int mount_id, fhsize, flags, dirfd;
    char *pathname;

    if (argc != 2) {
        fprintf(stderr, "Usage: %s pathname\n", argv[0]);
        exit(EXIT_FAILURE);
    }

    pathname = argv[1];

    /* Allocate file_handle structure */

    fhsize = sizeof(*fhp);
    fhp = malloc(fhsize);
    if (fhp == NULL)
        errExit("malloc");

    /* Make an initial call to name_to_handle_at() to discover
       the size required for file handle */

    dirfd = AT_FDCWD;           /* For name_to_handle_at() calls */
    flags = 0;                  /* For name_to_handle_at() calls */
    fhp->handle_bytes = 0;
    if (name_to_handle_at(dirfd, pathname, fhp,
                &mount_id, flags) != -1 || errno != EOVERFLOW) {
        fprintf(stderr, "name_to_handle_at failed: %s\n", strerror(errno));
        exit(EXIT_FAILURE);
    }

    /* Reallocate file_handle structure with correct size */
    fhsize = sizeof(struct file_handle) + fhp->handle_bytes;
    fhp = realloc(fhp, fhsize);         /* Copies fhp->handle_bytes */
    if (fhp == NULL)
	errExit("realloc");
    if (name_to_handle_at(dirfd, pathname, fhp, &mount_id, flags) == -1)
               errExit("name_to_handle_at");

    exit(EXIT_SUCCESS);
}

