# Changelog

## v1.11.2.12.ict (2017-02-13)

+ Add empty image tools(empty-image.sh) to project (mr 265)

## v1.11.2.11.ict (2017-02-07)

+ Add support for building rpm on CentOS 7.2 (mr 249)
- Fix docker save can't share layers (mr 254)
+ Add default hook support for all containers
- Bump containerd to v1.11.2.11.ict
- Bump runc to v1.11.2.11.ict
  - Fix unit test and integration test (runc mr 37)

## v1.11.2.10.it (2017-01-23)

- fixes races between list and create (mr 243)
- bugfix: report "destroy" after all volumes of container destroy (mr 243)
+ CLI flag for docker create(run) to change block device size (mr 245)
- bump go version to v1.7.4
+ Add hugetlb-limit option to docker run/create command (mr 239)
+ Make /proc/sys rw once enable --system-container (mr 247)
- bump containerd
  - bump go version to 1.7.4
  - Ignore `does not exit` error on deleting container from runtime (DTS2017010504340)
- bump runc
  - Set init processes as non-dumpable (issue 82)
  - bump go to 1.7.4
  - fix make validate failure
  - Fix cpuset issue with cpuset.cpu_exclusive (issue 80)
  - Fix leftover cgroup directory issue (issue 81)
  - Do not create cgroup dir name from combining subsystems (issue 148)

## v1.11.2.9.it (2017-01-06)

- Update go-check to print the timed out test case name (mr 209)
- Log and print original error for `start` to help debug (mr 217)
- make option `--restart=on-reboot` conflict with optioni `--rm` (issue 115 dts DTS2016122702180)
- Override or append 'container=docker' ENV when running --system-container (issue 113)
- Add bash completion for --hook-spec
- Fix failed to remove container after daemon restart (issue 117 dts DTS2016122704192)
- Add udev event time out to fix docker stuck on udev wait (issue 58)
- Remove restartmanager from libcontainerd (issue 118 119)
- update test TestUpdateNotAffectMonitorRestartPolicy (mr 231)
- Fix typo and magic number (mr 230)
- bump runc
  - Fix setup cgroup before prestart hook (runc mr 22)
  - support jenkins ci for runc (runc mr 20)
- bump containerd
  - support jenkins ci for containerd (containerd mr 28)


## v1.11.2.8.it (2016-12-24)

- Fix can't remove a restarting container (issue 106)
- Fix `docker exec -u` issue after docker daemon restart (issue 101)
- Revert "add options for 'docker update' to update config file only

## v1.11.2.7.it (2016-12-16)

- Fix docker update clear restart policy of monitor
- Fix flaky test TestEventsTimestampFormats
- Fix rpm package name
- [containerd] Remove SIGCHLD reaper from containerd process to fix SIGCHLD race issue
- [containerd] Better error msg
- [containerd] Set CONTAINER_ACTION env to support rebooting host in hook

## v1.11.2.6.it (2016-12-10)

+ add options for 'docker update' to update config file only
- Fix docker save with empty timestamp of layer created time
- Clean up the console.sock if start failed (issue 95)
- cli: move setRawTerminal and restoreTerminal to holdHijackedConnection, fix dts DTS2016120802950
- fix fail to attach a container if this container start failed and then start success next time (issue 93).
- Fix overlay test running on overlay

## v1.11.2.5.it (2016-12-03)

+ Add exec oci-systemd-hook for system container
+ Add new option '--system-container' to support for running system container
- Re-vendor syslog log driver to fix a potential dead lock problem
- Fixes the experimental tests from hanging
+ Implement on-reboot restart policy to restat container if exit code is 129
+ Add '--hook-spec' to support hooks for docker
+ Add external rootfs support
- Update runc to the latest version and rewrite console to fix tty issues (runc)

<We fork here for IT>

## v1.11.2.4 (2016-10-21)

+ Add support for --pid=container:<id>
+ Add support for --attach-output
+ Add unicorn version in docker version command
+ Add support for arm64
- Fix flaky test TestRunExitOnStdinClose
- Fix unit test

## v1.11.2.3 (2016-09-05)

- Fix original version in spec
- Fix upgrade on systemd
- Remove vendor info in euleros spec

## v1.11.2.2 (2016-08-26)

- Fix inspect network show gateway with mask
- Fix paused state error after several restarts
- Fix paused state error after several restarts

## v1.11.2.1 (2016-08-08)

+ Initial version based on v1.11.2
- Bunch of bugfixes backported from docker upstream
- Bunch of bugfixes bachported from containerd and runc upstream
- Add support for hot upgrade (enabled by --live-restore)
- Add support for login information encryption

Feature details:
- hot upgrade
  This version enable updating docker engine with running containers.
  There are some limits in using hot upgrade feature:
  a. If you using a external network plugin, the network plugin should remain running
     or the network plugin can restore the old running containers' network states after restarting.
  b. The stdio of the container passed to docker daemon through fifo. The size of the fifo buffer is 
     1M by default(it should differ from distribution to distribution). So you should make sure the log
     of the container will not exceed 1M. If the want a bigger buffer, edit /proc/sys/fs/pipe-max-size to
     change the size.
  c. The userland-proxy will die during upgrading, so if some application use loopback of host to access 
     to container, the connection will be broken. It's better to not using a userland-proxy due to some
     known issues such as memory leak, triggering kernel bug.
  d. If the container has a restart policy and it happen to die during upgrading, the restart policy wil
     not take affect. The restart policy will take affect once the daemon is up. 
- plaintext encryption
  http://3ms.huawei.com/hi/blog/500999_1918429.html
