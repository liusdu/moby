# Changelog

 ## v1.11.2.5-IT (2016-12-03)

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
