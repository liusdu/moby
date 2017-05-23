# Changelog

## v1.11.2.29 (2017-05-23)

- bump containerd
  - Fix exit transaction handler type convert issue (fix DTS2017052304178)
- Bugfix: Fix can't run image while the image is not in `docker images` (mr 489 fix DTS2017052208437)

## v1.11.2.28 (2017-05-19)

- bump containerd:
   - Fix close a nil chan err (containerd mr 49 fix DTS2017051803220)
   - Add transaction to containerd (containerd mr 48 fix DTS2017051004714)
- Bugfix: Fix save & rmi image competition (mr 472 fix DTS2017051508777)
- Bugfix: Add a error check in postHijacked to avoid docker exec command blocking (mr 457 fix DTS2017050904057)
- Bugfix: Update docs for `--cgroup-parent` flag (mr 474 fix DTS2017051801560)
- Bugfix: remove redundant containers/xxx/shm mountpoint (mr 463 fix DTS2017051117457)
- Bugfix: Fix bad slot store reference during daemon stop (mr 469 fix DTS2017051505870)
- Bugfix: Fix bug of un-allocated persistent accelerator (mr 462 fix DTS2017051202238)
- Bugfix: Fix can't pull image while the image is not in `docker images` (mr 482 fix DTS2017051903916)

## v1.11.2.27 (2017-05-11)

- bump containerd: 
   - Use temp console path for consoleSocket (containerd mr 46 fix DTS2017050909332)
   - Delay io closure until process exit (containerd mr 45)
   - Close stdin after copy returns (containerd mr 47)
- Backport: Revert "Revert "Remove timeout on fifos opening""  (mr 454)
- Bugfix: accel,bugfix: Move empty accelerator name check to daemon (mr 456 fix DTS2017051007724)
- Bugfix: fix inconsistent state string with containerd (mr fix 452 fix DTS2017042511512)


## v1.11.2.26 (2017-5-05)

- bump containerd:
   - fix event lost after continuous containerd restart (mr 449 fix DTS2017042509842)
- Bugfix: Fix remove none image when HoldOff the image (mr 430 fix DTS2017042800691)
- Bugfix: Recheck the container's state to avoid attach block (mr 419 fix DTS2017041703963)
- Bugfix: if failed to stop container with rpc error, just retry it (mr 437 fix DTS2017042807580)
- Bugfix: Enhance accelerator driver plugin error processing and fakefpga buildin driver (mr 433)
- Bugfix: docker rename fix to address the issue of renaming with the same name issue #23319 (mr 446 fix DTS2017050408229)
- Bugfix: Limit max backoff delay to 2 seconds for GRPC connection (mr 445 )
- Bugfix: Stop holding container lock while waiting on streams to fix deadlock (mr 432 fix DTS2017042502681)
- Bugfix: fix when rpc reports "xx is closing" error, health check go routine will exit (mr 441 fix DTS2017050304742)
- Bugfix: plugin,bugfix: Fix plugin security bug caused by unchecked plugin name (mr 442 fix DTS2017050300240)
- Feature: support overlay2 graphdriver

## v1.11.2.25 (2017-4-26)

- bump containerd: 
   - Kill the runtime process if the bundle is not exist (mr 42 fix DTS2017041109100)
   - Add timeout check to avoid block when shim open pipe (mr 42 fix DTS2017041109100)
   - Fix the busy loop for system-container (mr 41)
- Bugfix: fix failed to build a image while deleting it at the same time (mr 405 fix DTS2017041804117 DTS2017041804117)
- Bugfix: Revert "fix 'unrecognize image ID' error" (mr 425 fix DTS2017041803349)
- Feature: Add accelerator support for CloudRAN (mr 296)

## v1.11.2.24 (2017-4-21)

- Bugfix: deny attach-output to a paused container(mr 412 fix DTS2017041807694)
- Bugfix: Revert "Remove timeout on fifos opening"(mr 409 fix DTS2017041410483)
- Bugfix: builder: ignore errors when removing intermediate container failed(mr 414)
- Bugfix: devicemapper: remove redundant mountpoint when docker restart(mr 410 DTS2017041711043)
- bump containerd:
   - Fix broken /dev/console when running system-container with systemd >=231(mr 40)

## v1.11.2.23 (2017-4-20)

- Bugfix: change from to From when saving image(mr 413)

## v1.11.2.22 (2017-4-18)

- Bugfix: Fix 'RemoveDeviceDeferred dm_task_run failed' error(mr 403 DTS2017032104618)
- Bugfix: devicemapper: remove redundant mountpoint when docker restart(mr 404 fix DTS2017041711043)

## v1.11.2.21 (2017-4-12)

- Bugfix: Fix when containerd restarted, event handler may exit (mr 247 fix DTS2017041111320)
- Backport: Fix docker build error in daemon (mr 393)
- Backport: Fix update memory without memoryswap (mr 389)
- Feature: Add support for setting sysctls (mr 385)

## v1.11.2.20 (2017-04-07)

- Bugfix: devicemapper: remove thin pool if 'initDevmapper' failed (mr 374 fix DTS2017040505495)
- Bugfix: Fix delete a image while saving it, delete successfully but failed to save it (mr 370 fix DTS2017033003238)
- Bugfix: Update go-patricia to 2.2.6 to fix memory leak (mr 379 fix DTS2017040103654 )
- Bugfix: Fix RefCounter count memory leak (mr 380 fix DTS2017040103654 )
- Bugfix: Lock the RWLayer while committing/exporting (mr 375 fix DTS2017033104732 DTS2017040106675)
- Bugfix: fix 'unrecognize image ID' error (mr 382 fix DTS2017040607485 )
- Bugfix: Add check when execute docker {cp, export, diff} (mr 383 fix DTS2017040704111)

## v1.11.2.19 (2017-03-30)

- Bugfix: Increase udev wait timeout to 185s (mr 365)
- Bugfix: Add restarting check before attach a container (mr 362)
- Bubfix: daemon: reorder mounts before setting them (mr 350)
- Feature: modify centos rpm spec according to that of EulerOS (mr 361)
- Backport: fix TestDaemonRestartWithInvalidBasesize (mr 358)
- Backport: Ignore "failed to close stdin" if container or process not found (mr 364)
- Backport: Reset RemovalInProgress flag on daemon restart (mr 360)
- Backport: backport some prs after bumping containerd to v0.2.x branch (mr 342) 
  - Fix restoring behavior when live-restore is not set (https://github.com/docker/docker/pull/24984)
  - Fix a race in libcontainerd/pausemonitor_linux.go (https://github.com/docker/docker/pull/26695)
  - Fix libcontainerd: attach streams before create (https://github.com/docker/docker/pull/26744)
  - add lock in libcontainerd client AddProcess (https://github.com/docker/docker/pull/27094)
  - Fix issues with fifos blocking on open (https://github.com/docker/docker/pull/27405)
  - inherit the daemon log options when creating containers (https://github.com/docker/docker/pull/21153)
  - Fix panic while merging log configs to nil map (https://github.com/docker/docker/pull/24548)
  - Move stdio attach from libcontainerd backend to callback (https://github.com/docker/docker/pull/27467)
  - Fix race on sending stdin close event (https://github.com/docker/docker/pull/28682)
  - Handle paused container when restoring without live-restore set (https://github.com/docker/docker/pull/31704)
  - Fix daemon panic on restoring containers (https://github.com/docker/docker/pull/25111)

## v1.11.2.18 (2017-03-25)

- Bugfix: disable systemd create /var/run/docker.sock for docker starting (mr 351 fix DTS2017031709612)

## v1.11.2.17 (2017-03-24)

- Bugfix: Fix devicemapper issue: power off the VM while loading a image, couldn't load it after VM bootup (mr 339)
- Bugfix: Remove redundant Mounts when start docker daemon (mr 315)
- Bugfix: Fix docker logs a dead container (mr 353)
- Bugfix: fix TestCreateShrinkRootfs and TestCreateShrinkRootfs (mr 349)
- Bugfix: Close context when error out in attach-output (mr 335)
- Backport: Update layer store to sync transaction files before committing (mr 347)
- Backport: Set permission on atomic file write (mr347)
- Backport: Safer file io for configuration files (mr347)
- Backport: fix relative symlinks don't work with --device argument (mr 348)
- Backport: Fix inspect Dead container (352)

## v1.11.2.16 (2017-03-16)

- Bugfix: Use default tag to fill 'From' field in config if no tag provided (mr 332)
- Bugfix: Change tag to lowercase when used as part of name(mr 333)
- Bugfix: Ignore `layer does not exist` error from `docker images`(mr 331)
- Bugfix: Don't set 'Parent' field when building a partial image(mr 327)
- Bugfix: Delete intermediate images after build Dockerfile with parameter --no-parent (mr 325)
- Bugfix: Don't add device to list if it doesn't exist anymore(mr 322)
- Bugfix: Refactor specify external rootfs to enable specify user(mr 323)
- Improvement: Modify some hint information(mr 329)
- Improvement: Allow granular vendoring( mr 324)
- bump containerd
	- Allow granular vendoring(mr 36)
- bump runc
	- bugfix: Don't add device to list if it doesn't exist anymore(mr 39)

## v1.11.2.15 (2017-03-10)

- Backport: cherry pick devmapper relative commits from upstream (mr 281)
- Bugfix: fix a race in daemon/logger.TestCopier (mr 317)
- Bugfix: clean up tmp files for interrupted `docker build` (mr 310)
- Bugfix: fix flaky TestSaveLoadParents (mr 313)

## v1.11.2.14.ict (2017-03-07)

- Add feature "Build and run partial image" (mr 302)
- Bugfix: wait for containerd to die before restarting it (mr 292 )
- Bugfix: fix flaky test TestBuildApiDockerFileRemote (mr 294)
- Bugfix: fix suspend removed device for devicemapper graphdriver(mr 298, refactor 282)
- Bugfix: Add more locking to storage drivers (mr 312)

## v1.11.2.13.ict (2017-02-23)

- fix race condition between device deferred removal and resume device (mr 282)
- start GC on daemon restart (mr 279)
- Fix docker leaks ExecIds on failed exec -i (mr 275)

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
