# Changelog


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
