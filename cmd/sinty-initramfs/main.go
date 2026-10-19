// sinty-initramfs is the initramfs init of SintyOS. The boot itself is the
// Atom Loops boot package; this program adds what is particular to SintyOS:
// its kernel command line, the first-boot login setup, the boot diagnostics
// saved to the SINTYLOGS partition and the sinit debug switch.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	fmt.Fprintf(&b, "block: %s\n", strings.Join(devs, " "))
	s.slotReport(&b)
	fmt.Fprintf(&b, "dmesg_tail:\n%s\nSINTYRESCUE_END\n", kernelLogTail(60))
	saved := boot.SaveDiagnostics("SINTYLOGS", "SINTYRESCUE.txt", b.String())
	savedESP := saveToESP("SINTYRESCUE.txt", b.String())
	if tty, err := os.OpenFile("/dev/tty0", os.O_WRONLY, 0); err == nil {
		where := "Diagnostics could not be saved."
		switch {
		case saved:
			where = "Diagnostics saved to SINTYLOGS."
		case savedESP:
			where = "Diagnostics saved to the ESP as EFI/atom/state/SINTYRESCUE.txt."
		}
		var short strings.Builder
		s.slotReport(&short)
		report := short.String()
		if i := strings.Index(report, "mounts:"); i >= 0 {
			report = report[:i]
		}
		fmt.Fprintf(tty, "\n%s\nSinty OS could not boot: %s\n%s\n", report, reason, where)
		tty.Close()
	}
	if spendTrial() {
		if tty, err := os.OpenFile("/dev/tty0", os.O_WRONLY, 0); err == nil {
			fmt.Fprintf(tty, "The update could not start. Restarting into the previous system.\n")
			tty.Close()
		}
		time.Sleep(5 * time.Second)
		syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	}
	boot.Rescue(reason, s.unlocked, rescueSh)
}

func spendTrial() bool {
	const dir = "/run/esp-trial"
	for _, p := range boot.Partitions() {
		if boot.Mount(p.Dev, dir, "vfat", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "") != nil {
			continue
		}
		path := filepath.Join(dir, "EFI", "atom", "boot-state")
		b, err := os.ReadFile(path)
		if err != nil {
			syscall.Unmount(dir, 0)
			continue
		}
		spent := false
		if trialPending(string(b)) {
			spent = os.WriteFile(path, []byte("target=next\ntrial=1\nattempts=0\n"), 0o644) == nil
		}
		syscall.Sync()
		syscall.Unmount(dir, 0)
		return spent
	}
	return false
}

func trialPending(state string) bool {
	var next, trial bool
	for _, line := range strings.Split(state, "\n") {
		switch strings.TrimSpace(line) {
		case "target=next":
			next = true
		case "trial=1":
			trial = true
		}
	}
	return next && trial
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

func (s *state) slotReport(b *strings.Builder) {
	files, _ := filepath.Glob("/sysdata/boot/rootfs/*")
	fmt.Fprintf(b, "slot_files:\n")
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(b, "  %s size=%d mtime=%s\n", f, st.Size(), st.ModTime().UTC().Format("2006-01-02T15:04:05Z"))
		} else {
			fmt.Fprintf(b, "  %s stat=%v\n", f, err)
		}
	}
	for _, d := range s.media.SlotDatas {
		fmt.Fprintf(b, "data %s bytes=%d erofs=%v\n", d, boot.DevBytes(d), boot.HasEROFS(d))
	}
	for _, h := range s.media.SlotHashes {
		root, sb, err := boot.VerityRootHash(h)
		if err != nil {
			fmt.Fprintf(b, "hash %s bytes=%d error=%v\n", h, boot.DevBytes(h), err)
			continue
		}
		fmt.Fprintf(b, "hash %s bytes=%d root=%s blocks=%d match=%v\n", h, boot.DevBytes(h), root, sb.DataBlocks, strings.EqualFold(root, s.hash))
	}
	for _, d := range s.media.SlotDatas {
		for _, h := range s.media.SlotHashes {
			boot.CloseVerity("vdiag")
			node, err := boot.OpenVerity("vdiag", d, h, s.hash)
			if err != nil {
				fmt.Fprintf(b, "pair %s+%s open=%v\n", d, h, err)
				continue
			}
			fmt.Fprintf(b, "pair %s+%s open=ok erofs=%v\n", d, h, boot.HasEROFS(node))
			boot.CloseVerity("vdiag")
		}
	}
	if m, err := os.ReadFile("/proc/mounts"); err == nil {
		fmt.Fprintf(b, "mounts:\n%s", m)
	}
}

// saveToESP writes the report beside the boot state on the EFI system
// partition, which every installed disk has, so a failed boot leaves a trace
// that the running system or another OS can read.
func saveToESP(name, text string) bool {
	const dir = "/run/esp-diag"
	for _, p := range boot.Partitions() {
		if boot.Mount(p.Dev, dir, "vfat", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "") != nil {
			continue
		}
		state := filepath.Join(dir, "EFI", "atom", "state")
		if _, err := os.Stat(filepath.Join(dir, "EFI", "atom")); err != nil {
			syscall.Unmount(dir, 0)
			continue
		}
		os.MkdirAll(state, 0o755)
		err := os.WriteFile(filepath.Join(state, name), []byte(text), 0o644)
		syscall.Sync()
		syscall.Unmount(dir, 0)
		if err == nil {
			return true
		}
	}
	return false
}
