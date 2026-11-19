package main

import (
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/mirkobrombin/atomloops/boot"
)

type mmioWrite struct {
	addr  int64
	value uint32
}

var displayQuirks = map[string][]mmioWrite{
	// DEVICE-FIX(gs201): DECON0 keeps the panel on the bootloader image until its trigger is switched to software, so nothing the kernel draws reaches the Pixel 7 Pro screen until gs-decon loads
	"google,gs201-cheetah": {{0x1c240030, 0x3061}},
}

func firstCompatible() string {
	data, err := os.ReadFile("/proc/device-tree/compatible")
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(data), "\x00")
	return first
}

func applyDisplayQuirks() {
	writes := displayQuirks[firstCompatible()]
	if len(writes) == 0 {
		return
	}
	f, err := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		boot.Logf("display quirk: %v", err)
		return
	}
	defer f.Close()
	page := int64(os.Getpagesize())
	for _, w := range writes {
		base := w.addr &^ (page - 1)
		mem, err := syscall.Mmap(int(f.Fd()), base, int(page), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			boot.Logf("display quirk %#x: %v", w.addr, err)
			continue
		}
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&mem[w.addr-base])), w.value)
		syscall.Munmap(mem)
	}
}
