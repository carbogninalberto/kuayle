#include <tunables/global>

profile kuayle-dev-machine flags=(attach_disconnected,mediate_deleted) {
  #include <abstractions/base>
  #include <abstractions/nameservice>

  network inet stream,
  network inet6 stream,
  capability setgid,
  capability setuid,

  /usr/** rix,
  /bin/** rix,
  /lib/** mr,
  /lib64/** mr,
  /etc/** r,
  /workspace/** rwk,
  /tmp/** rwk,
  /run/** rwk,
  /home/kuayle/** rwk,

  deny /var/run/docker.sock rwklx,
  deny /proc/sys/** w,
  deny /sys/** w,
  deny mount,
  deny ptrace,
}
