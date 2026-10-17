// sinty-initramfs is the initramfs init of SintyOS. The boot itself is the
// Atom Loops boot package; this program adds what is particular to SintyOS:
// its kernel command line, the first-boot login setup, the boot diagnostics
// saved to the SINTYLOGS partition and the sinit debug switch.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/mirkobrombin/atomloops/boot"
)

const (
	sintykey   = "/usr/bin/sintykey"
	rescueSh   = "/bin/sh"
	fwRootPub  = "/etc/atom-fw-root.pub"
	oobeDone   = boot.NewRoot + "/var/lib/sinty/.oobe-done"
	oobeConfig = boot.NewRoot + "/usr/share/sinty/greetd-oobe.toml"
	greetdConf = boot.NewRoot + "/etc/greetd/config.toml"
)

type state struct {
	cmd      boot.Cmdline
	hash     string
	unlocked bool
	media    boot.Media
	root     *boot.Root
}

func main() {
	boot.MountEarly()
	boot.LoadModules("crc16", "mbcache", "jbd2", "ext4")
	s := &state{cmd: boot.ReadCmdline()}
	s.hash = s.cmd.Value("sing.roothash")
	if s.hash == "" {
		s.rescue("no sing.roothash= on the kernel command line")
	}
	s.unlocked = boot.DeviceUnlocked(sintykey)

	cfg := boot.Config{
		RootHash:     s.hash,
		Live:         s.cmd.Has("sing.live"),
		Portable:     s.cmd.Has("sing.portable"),
		PortableRoot: "sing-portable-root",
		PortableHash: "sing-portable-hash",
		Unlocked:     s.unlocked,
	}
	ops := boot.SystemOps()
	root, err := boot.SelectRoot(cfg, &s.media, ops)
	if err != nil {
		if s.unlocked {
			s.rescue("unlocked: no erofs root partition found")
		}
		s.rescue("no verified data/hash pair for sing.roothash: " + err.Error())
	}
	s.root = root
	if s.unlocked {
		boot.Logf("==================================================================")
		boot.Logf(" DEVICE UNLOCKED - verified boot is OFF, this system is not sealed")
		boot.Logf(" mounting the root image without dm-verity")
		boot.Logf("==================================================================")
	}
	held, err := boot.ReleaseUnrelatedData(cfg, &s.media, root, ops)
	if err != nil {
		s.rescue(err.Error())
	}
	installed, err := boot.MountRoot(cfg, &s.media, root, held)
	if err != nil {
		s.rescue(err.Error())
	}
	if installed {
		s.restoreFirstBoot()
	}
	boot.MountESP(&s.media, root)
	init := boot.FindInit(s.cmd.Value("init"), "/sbin/init", "/usr/lib/sinit/sinit")
	if init == "" {
		s.rescue("no init found in root")
	}
	boot.MountFirmware(fwRootPub)
	if err := boot.SwitchRoot(init, initArgs(init, s.cmd.Has("sinit.debug"))); err != nil {
		s.rescue("switch root: " + err.Error())
	}
}

// initArgs passes sinit.debug on to sinit, which takes it as a flag. Reached
// as /usr/lib/sinit/sinit rather than as init, sinit needs the init
// subcommand before the flag.
func initArgs(init string, debug bool) []string {
	if !debug {
		return nil
	}
	if strings.HasSuffix(init, "/init") {
		return []string{"--debug"}
	}
	return []string{"init", "--debug"}
}

// restoreFirstBoot puts the first-boot login setup back while the setup has
// not completed. A destructive bootloader unlock removes every account and
// the writable /etc; without this the live-media login configuration would
// start the installer and leave the wiped device without an account.
func (s *state) restoreFirstBoot() {
	if _, err := os.Stat(oobeDone); err == nil {
		return
	}
	data, err := os.ReadFile(oobeConfig)
	if err == nil {
		os.MkdirAll(boot.NewRoot+"/etc/greetd", 0755)
		err = os.WriteFile(greetdConf, data, 0644)
	}
	if err != nil {
		s.rescue("cannot restore first-boot login setup")
	}
	boot.Logf("first boot: OOBE login setup restored")
}

func (s *state) rescue(reason string) {
	var b strings.Builder
	fmt.Fprintf(&b, "SINTYRESCUE_BEGIN\nreason: %s\n", reason)
	fmt.Fprintf(&b, "cmdline: %s\n", strings.Join(s.cmd, " "))
	fmt.Fprintf(&b, "roothash=%s\n", s.hash)
	fmt.Fprintf(&b, "data=%s slots=%v hashes=%v optical=%s\n", s.media.DataDev, s.media.SlotDatas, s.media.SlotHashes, s.media.ISODev)
	if s.root != nil {
		fmt.Fprintf(&b, "root=%s data=%s hash=%s\n", s.root.Source, s.root.Data, s.root.Hash)
	}
	var devs []string
	for _, p := range boot.Partitions() {
		devs = append(devs, p.Dev)
	}
	fmt.Fprintf(&b, "block: %s\ndmesg_tail:\n%s\nSINTYRESCUE_END\n", strings.Join(devs, " "), kernelLogTail(30))
	saved := boot.SaveDiagnostics("SINTYLOGS", "SINTYRESCUE.txt", b.String())
	if tty, err := os.OpenFile("/dev/tty0", os.O_WRONLY, 0); err == nil {
		where := "Diagnostics could not be saved."
		if saved {
			where = "Diagnostics saved to SINTYLOGS."
		}
		fmt.Fprintf(tty, "\nSinty OS could not boot: %s\n%s\n", reason, where)
		tty.Close()
	}
	boot.Rescue(reason, s.unlocked, rescueSh)
}

func kernelLogTail(lines int) string {
	const readAll = 3
	size, err := syscall.Klogctl(10, nil)
	if err != nil || size <= 0 {
		size = 1 << 17
	}
	buf := make([]byte, size)
	n, err := syscall.Klogctl(readAll, buf)
	if err != nil {
		return ""
	}
	all := strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}
