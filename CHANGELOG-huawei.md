# Changelog

## v3.3 (2016-03-31)

+ Add support for SELinux on overlay
- Dont relay on network in .ensure-syscall-test
- Add image tag to Dockerfile.huawei

## v3.3-rc1 (2016-03-17)

- Update release version to 1.10.3
- Fix TestRmContainerWithRemovedVolume
- Fix bug for tls cert verification with push
- Fix race condition when exec with tty
- Enable selinux for overlay fs
- Fix problems when update swap memory

## v3.2 (2016-02-25)

- Update release version to 1.10.2
- Fix problem that daemon not working on host without ipv6 kernel config enabled
- Fix update docker problem when container is restarting
- Fix docker version show git commit number and build time

## v3.2-rc1 (2016-02-18)

- Update release version to 1.10.1
- Fix the actions when pull from notary contains `push`
- Add /etc/sysconfig/docker[-storage/-network] configuration file support
- Fix the boot error on 1.10.1 with ipv6 config disabled

## v3.1 (2016-01-28)

- Fix flaky test StatsNoStreamGetCpu
- Fix auto-creating non-existant volume host path

## v3.1-rc1 (2016-01-14)

+ Add support to build Docker on ARM64 with Docker container
+ Add support to bind mount some proc entries (part of fuse feature)

## v3.0 (2015-12-31)

- Fix seccomp issue on ubuntu trusty
- Fix docker version show wrong commit on EulerOS
- Fix failed test case 15+

## v3.0-rc2 (2015-12-24)

- Fix test case for seccomp
- Fix seccomp issue with rpm/deb installation
+ Add engine support for mirror private registry

## v3.0-rc1 (2015-12-17)

+ Add support for seccomp on Docker side
+ Add spec file for EulerOS

## v1.2-rc2 (2015-11-24)

- Update release version to 1.9.1

## v1.1 (2015-10-29)

+ Add CHANGELOG-huawei.md

## v1.1-rc2 (2015-10-23)

- Change the directory of aeskey to ~/.docker/
- Fix the problem that can't use systemd to manage Docker service

## v1.1-rc1 (2015-10-15)

- Update release version to 1.8.3
- Update plaintext encryption to use aes from golang instead of aes from PAAS

## v1.0 (2015-09-30)

- Fix: Docker Daemon doesn't send appropriate scope actions to auth server
+ Add support file system quota on devicemapper with xfs
+ Add support for encrypting the plaintext user credential

Feature details:
- disk quota
  http://3ms.huawei.com/hi/blog/511507_1918509.html
- plaintext encryption
  http://3ms.huawei.com/hi/blog/500999_1918429.html
