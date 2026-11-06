package main

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/mirkobrombin/atomloops/boot"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	screenFont    = "/usr/share/fonts/Inter-Regular.ttf"
	fbGetVar      = 0x4600
	fbPutVar      = 0x4601
	fbGetFix      = 0x4602
	fbPan         = 0x4606
	fbActivateNow = 0
	fbActivateFor = 128
	kdSetMode     = 0x4B3A
	kdText        = 0
	kdGraphics    = 1
	logoSize      = 256.0
	lineGray      = 158
	blackLevelMax = 10
	lineHold      = 700 * time.Millisecond
)

// screen repaints the frame the loader showed and writes one status line where
// its progress bar was. It is opened only when there is something to say, so
// a healthy boot never touches the display.
type screen struct {
	file    *os.File
	fb      []byte
	vs      [160]byte
	w, h    int
	stride  int
	r, g, b uint
	face    font.Face
	top     int
	height  int
	base    int
	tty     *os.File
	shown   time.Time
}

var (
	display     *screen
	displayOpen bool
)

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

func openScreen() *screen {
	if displayOpen {
		return display
	}
	displayOpen = true
	s, err := newScreen()
	if err != nil {
		boot.Logf("status screen unavailable: %v", err)
		return nil
	}
	display = s
	return s
}

func newScreen() (*screen, error) {
	ttf, err := os.ReadFile(screenFont)
	if err != nil {
		return nil, err
	}
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile("/dev/fb0", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	s := &screen{file: f}
	var fs [80]byte
	if err := ioctl(f.Fd(), fbGetVar, uintptr(unsafe.Pointer(&s.vs[0]))); err != nil {
		f.Close()
		return nil, err
	}
	le := binary.LittleEndian
	le.PutUint32(s.vs[84:], fbActivateNow|fbActivateFor)
	ioctl(f.Fd(), fbPutVar, uintptr(unsafe.Pointer(&s.vs[0])))
	if err := ioctl(f.Fd(), fbGetFix, uintptr(unsafe.Pointer(&fs[0]))); err != nil {
		f.Close()
		return nil, err
	}
	s.w, s.h = int(le.Uint32(s.vs[0:])), int(le.Uint32(s.vs[4:]))
	s.stride = int(le.Uint32(fs[48:]))
	s.r, s.g, s.b = uint(le.Uint32(s.vs[32:])), uint(le.Uint32(s.vs[44:])), uint(le.Uint32(s.vs[56:]))
	if le.Uint32(s.vs[24:]) != 32 || s.w <= 0 || s.h <= 0 || s.stride < s.w*4 {
		f.Close()
		return nil, errors.New("framebuffer is not 32 bits per pixel")
	}
	size := int(le.Uint32(fs[24:]))
	if size < s.stride*s.h {
		size = s.stride * s.h
	}
	if s.fb, err = syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED); err != nil {
		f.Close()
		return nil, err
	}
	if tty, err := os.OpenFile("/dev/tty0", os.O_RDWR, 0); err == nil {
		ioctl(tty.Fd(), kdSetMode, kdGraphics)
		s.tty = tty
	}
	s.paintSurface(readFromESP("surface.bin"))
	k := 1.0
	switch {
	case s.h >= 2000:
		k = 2
	case s.h >= 1400:
		k = 1.5
	}
	if s.face, err = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 14 * k, DPI: 72, Hinting: font.HintingFull}); err != nil {
		return nil, err
	}
	m := s.face.Metrics()
	glyphs := (m.Ascent + m.Descent).Ceil()
	s.height = glyphs + int(8*k)
	s.top = s.barCenter(k) - s.height/2
	s.base = s.top + (s.height-glyphs)/2 + m.Ascent.Ceil()
	return s, nil
}

func readFromESP(name string) []byte {
	const dir = "/run/esp-surface"
	for _, p := range boot.Partitions() {
		if boot.Mount(p.Dev, dir, "vfat", syscall.MS_RDONLY|syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "") != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "EFI", "atom", name))
		syscall.Unmount(dir, 0)
		if err == nil {
			return b
		}
	}
	return nil
}

func (s *screen) set(x, y int, v uint32) {
	binary.LittleEndian.PutUint32(s.fb[y*s.stride+x*4:], v)
}

// paintSurface draws what the loader draws: black, then the boot surface
// ([u32 w][u32 h][w*h BGRX]) centred.
func (s *screen) paintSurface(asset []byte) {
	for y := 0; y < s.h; y++ {
		clear(s.fb[y*s.stride : y*s.stride+s.w*4])
	}
	defer s.flush()
	if len(asset) < 8 {
		return
	}
	le := binary.LittleEndian
	lw, lh := int(le.Uint32(asset[0:])), int(le.Uint32(asset[4:]))
	if lw <= 0 || lh <= 0 || lw > s.w || lh > s.h || len(asset) < 8+lw*lh*4 {
		return
	}
	ox, oy := (s.w-lw)/2, (s.h-lh)/2
	for y := 0; y < lh; y++ {
		for x := 0; x < lw; x++ {
			v := le.Uint32(asset[8+(y*lw+x)*4:])
			r, g, b := v>>16&0xff, v>>8&0xff, v&0xff
			s.set(ox+x, oy+y, r<<s.r|g<<s.g|b<<s.b)
		}
	}
}

func (s *screen) flush() {
	ioctl(s.file.Fd(), fbPan, uintptr(unsafe.Pointer(&s.vs[0])))
}

func (s *screen) lit(x, y int) bool {
	v := binary.LittleEndian.Uint32(s.fb[y*s.stride+x*4:])
	return uint8(v>>s.r) > blackLevelMax || uint8(v>>s.g) > blackLevelMax || uint8(v>>s.b) > blackLevelMax
}

// barCenter finds the progress bar in the frame: the lowest lit row in the
// middle of the screen, centred and wider than a twentieth of it. Without a
// frame it falls back to where the boot splash places the bar.
func (s *screen) barCenter(k float64) int {
	for y := s.h - 1; y > s.h/2; y-- {
		minX, maxX := -1, -1
		for x := s.w / 4; x < s.w*3/4; x++ {
			if s.lit(x, y) {
				if minX < 0 {
					minX = x
				}
				maxX = x
			}
		}
		if minX < 0 {
			continue
		}
		mid := (minX + maxX) / 2
		if maxX-minX < s.w/20 || mid < s.w/2-4 || mid > s.w/2+4 {
			break
		}
		top := y
		for top > s.h/2 && s.lit(s.w/2, top-1) {
			top--
		}
		if y-top > 16 {
			break
		}
		return (top + y) / 2
	}
	size := logoSize * k
	return int((float64(s.h)-size)/2 + size + size*48/256)
}

func (s *screen) line(text string) {
	if wait := lineHold - time.Since(s.shown); !s.shown.IsZero() && wait > 0 {
		time.Sleep(wait)
	}
	defer func() { s.shown = time.Now() }()
	img := image.NewGray(image.Rect(0, 0, s.w, s.height))
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{Y: lineGray}), Face: s.face}
	d.Dot = fixed.P((s.w-d.MeasureString(text).Ceil())/2, s.base-s.top)
	d.DrawString(text)
	for y := 0; y < s.height; y++ {
		fy := s.top + y
		if fy < 0 || fy >= s.h {
			continue
		}
		for x := 0; x < s.w; x++ {
			v := uint32(img.Pix[y*img.Stride+x])
			s.set(x, fy, v<<s.r|v<<s.g|v<<s.b)
		}
	}
	s.flush()
}

// textMode hands the display back to the console, for the rescue shell.
func textMode() {
	if display == nil {
		return
	}
	if display.tty != nil {
		ioctl(display.tty.Fd(), kdSetMode, kdText)
		display.tty.Close()
	}
	display.file.Close()
	display = nil
}
