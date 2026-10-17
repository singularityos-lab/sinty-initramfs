# sinty-initramfs

The initramfs init of SintyOS. It runs before the real init and decides
which root filesystem the machine boots: the one whose dm-verity tree matches
the root hash in the signed kernel command line.

## Why it exists

The boot itself is the [Atom Loops](https://github.com/mirkobrombin/atomloops)
`boot` package: root selection by the signed hash, loop devices and dm-verity
through the kernel interfaces, the persistent `/var`, the verified firmware
bundles and the switch to the real init. Atom Loops defines that contract, so
SintyOS builds on the package instead of keeping its own copy of the logic.

This program is the part that is particular to SintyOS. The initramfs holds
no shell interpreter, no BusyBox and no device-mapper tools: one Go binary
does the boot.

## What it adds

- **The SintyOS command line:** `sing.roothash=`, `sing.live`,
  `sing.portable` (portable roots on the partitions named `sing-portable-root`
  and `sing-portable-hash`), `init=` and `sinit.debug`, which is passed on to
  sinit.
- **The first-boot login setup.** On an installed system whose first-boot
  setup has not completed, the login configuration of the setup is put back,
  so a device wiped by a bootloader unlock still starts the setup and gets an
  account.
- **Boot diagnostics.** A boot that cannot continue saves the reason, the
  command line, the block devices and the tail of the kernel log to
  `SINTYRESCUE.txt` on the `SINTYLOGS` partition and says so on the screen.
- **Unlock.** `sintykey` tells whether the owner unlocked the bootloader. A
  locked device that cannot boot powers off; an unlocked one offers a root
  shell after the phrase `OPEN DEBUG SHELL` is typed.

## Build

    make build            # ./sinty-initramfs
    make static           # stripped static binary for the initramfs
    make test
    go build ./...        # plain build

Requires Go >= 1.26 and Linux. The initramfs carries the static binary as
`/init`.
