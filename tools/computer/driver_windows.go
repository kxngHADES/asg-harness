//go:build windows

package computer

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pEnumDisplayMonitors           = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	pGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pSendInput                     = user32.NewProc("SendInput")
	pVkKeyScanW                    = user32.NewProc("VkKeyScanW")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pEnumWindows                   = user32.NewProc("EnumWindows")
	pIsWindowVisible               = user32.NewProc("IsWindowVisible")
	pIsIconic                      = user32.NewProc("IsIconic")
	pGetWindowTextW                = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	pGetWindowLongW                = user32.NewProc("GetWindowLongW")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pBringWindowToTop              = user32.NewProc("BringWindowToTop")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pGetAncestor                   = user32.NewProc("GetAncestor")
	pIsWindow                      = user32.NewProc("IsWindow")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")

	pOpenProcess                = kernel32.NewProc("OpenProcess")
	pCloseHandle                = kernel32.NewProc("CloseHandle")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	pGetConsoleWindow           = kernel32.NewProc("GetConsoleWindow")

	pShellExecuteW = shell32.NewProc("ShellExecuteW")

	pDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
)

var unsupported = ""

func init() { sys = &winDriver{} }

type winDriver struct{}

var dpiOnce sync.Once

// dpiAware makes coordinates physical pixels, so screenshots, monitor
// rects and mouse input all agree on HiDPI screens.
func dpiAware() {
	dpiOnce.Do(func() {
		if pSetProcessDpiAwarenessContext.Find() == nil {
			// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
			if r, _, _ := pSetProcessDpiAwarenessContext.Call(uintptr(^uintptr(3))); r != 0 {
				return
			}
		}
		if pSetProcessDPIAware.Find() == nil {
			pSetProcessDPIAware.Call()
		}
	})
}

type rect32 struct{ Left, Top, Right, Bottom int32 }

func (r rect32) rect() Rect {
	return Rect{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}
}

type point32 struct{ X, Y int32 }

type monitorInfoEx struct {
	Size    uint32
	Monitor rect32
	Work    rect32
	Flags   uint32
	Device  [32]uint16
}

var (
	enumMu   sync.Mutex
	enumMons []Monitor
	enumWins []uintptr
	monCB    = syscall.NewCallback(func(hmon, hdc, lprc, lparam uintptr) uintptr {
		mi := monitorInfoEx{Size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		if r, _, _ := pGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			enumMons = append(enumMons, Monitor{
				Name:    strings.TrimPrefix(syscall.UTF16ToString(mi.Device[:]), `\\.\`),
				Rect:    mi.Monitor.rect(),
				Primary: mi.Flags&1 != 0,
			})
		}
		return 1
	})
	winCB = syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		enumWins = append(enumWins, hwnd)
		return 1
	})
)

func (winDriver) Monitors() ([]Monitor, error) {
	dpiAware()
	enumMu.Lock()
	defer enumMu.Unlock()
	enumMons = nil
	pEnumDisplayMonitors.Call(0, 0, monCB, 0)
	out := append([]Monitor(nil), enumMons...)
	if len(out) == 0 {
		return nil, errors.New("Windows reported no monitors")
	}
	return out, nil
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func (winDriver) Capture(r Rect) (image.Image, error) {
	dpiAware()
	if r.empty() {
		return nil, errors.New("empty capture area")
	}
	screen, _, _ := pGetDC.Call(0)
	if screen == 0 {
		return nil, errors.New("GetDC failed")
	}
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, errors.New("CreateCompatibleDC failed")
	}
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBitmap.Call(screen, uintptr(r.W), uintptr(r.H))
	if bmp == 0 {
		return nil, errors.New("CreateCompatibleBitmap failed")
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	ok, _, err := pBitBlt.Call(mem, 0, 0, uintptr(r.W), uintptr(r.H), screen, uintptr(int32(r.X)), uintptr(int32(r.Y)), srcCopy|captureBlt)
	pSelectObject.Call(mem, old)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed: %v", err)
	}
	hdr := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(r.W), Height: -int32(r.H), Planes: 1, BitCount: 32}
	// BITMAPINFO is the header plus a colour table we don't use.
	bi := struct {
		H      bitmapInfoHeader
		Colors [4]uint32
	}{H: hdr}
	img := image.NewRGBA(image.Rect(0, 0, r.W, r.H))
	if n, _, _ := pGetDIBits.Call(mem, bmp, 0, uintptr(r.H), uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&bi)), 0); n == 0 {
		return nil, errors.New("GetDIBits failed")
	}
	p := img.Pix // BGRA → RGBA, opaque
	for i := 0; i+3 < len(p); i += 4 {
		p[i], p[i+2], p[i+3] = p[i+2], p[i], 255
	}
	return img, nil
}

func (winDriver) Cursor() (int, int, error) {
	dpiAware()
	var pt point32
	if r, _, err := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, fmt.Errorf("GetCursorPos: %v", err)
	}
	return int(pt.X), int(pt.Y), nil
}

// INPUT for SendInput. The union is as large as MOUSEINPUT; keyboard
// input is padded to the same size.
type mouseInput struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type keybdInput struct {
	Vk, Scan uint16
	Flags    uint32
	Time     uint32
	Extra    uintptr
	_        [8]byte
}

type inputMouse struct {
	Type uint32
	Mi   mouseInput
}

type inputKey struct {
	Type uint32
	Ki   keybdInput
}

const (
	inputMouseType, inputKeyboardType = 0, 1

	mouseMove, mouseAbsolute, mouseVirtualDesk = 0x0001, 0x8000, 0x4000
	mouseWheel, mouseHWheel                    = 0x0800, 0x1000

	keyExtended, keyUp, keyUnicode = 0x0001, 0x0002, 0x0004
)

func sendMouse(mi mouseInput) error {
	in := inputMouse{Type: inputMouseType, Mi: mi}
	if r, _, err := pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in)); r != 1 {
		return fmt.Errorf("SendInput was blocked (%v) — Windows refuses input into windows of programs running as administrator unless ASG runs as administrator too", err)
	}
	return nil
}

func sendKeys(ks []keybdInput) error {
	if len(ks) == 0 {
		return nil
	}
	in := make([]inputKey, len(ks))
	for i, k := range ks {
		in[i] = inputKey{Type: inputKeyboardType, Ki: k}
	}
	if r, _, err := pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0])); int(r) != len(in) {
		return fmt.Errorf("SendInput was blocked (%v) — Windows refuses input into windows of programs running as administrator unless ASG runs as administrator too", err)
	}
	return nil
}

func metric(i int) int {
	r, _, _ := pGetSystemMetrics.Call(uintptr(i))
	return int(int32(r))
}

func (winDriver) Move(x, y int) error {
	dpiAware()
	// Absolute moves over the virtual desktop are flagged as injected, so
	// the input watcher can tell them from the user's own hand.
	vx, vy, vw, vh := metric(76), metric(77), metric(78), metric(79)
	if vw <= 1 || vh <= 1 {
		return errors.New("could not read the screen size")
	}
	nx := int32(math.Round(float64(x-vx) * 65535 / float64(vw-1)))
	ny := int32(math.Round(float64(y-vy) * 65535 / float64(vh-1)))
	return sendMouse(mouseInput{Dx: nx, Dy: ny, Flags: mouseMove | mouseAbsolute | mouseVirtualDesk})
}

func (winDriver) Button(b Button, down bool) error {
	flags := map[Button][2]uint32{Left: {0x0002, 0x0004}, Right: {0x0008, 0x0010}, Middle: {0x0020, 0x0040}}[b]
	f := flags[1]
	if down {
		f = flags[0]
	}
	return sendMouse(mouseInput{Flags: f})
}

func (winDriver) Scroll(dx, dy int) error {
	if dy != 0 {
		if err := sendMouse(mouseInput{Flags: mouseWheel, MouseData: uint32(int32(-dy * 120))}); err != nil {
			return err
		}
	}
	if dx != 0 {
		return sendMouse(mouseInput{Flags: mouseHWheel, MouseData: uint32(int32(dx * 120))})
	}
	return nil
}

func (winDriver) Type(text string) error {
	var ks []keybdInput
	for _, r := range strings.ReplaceAll(text, "\r\n", "\n") {
		switch r {
		case '\n', '\r':
			ks = append(ks, keybdInput{Vk: 0x0D}, keybdInput{Vk: 0x0D, Flags: keyUp})
			continue
		case '\t':
			ks = append(ks, keybdInput{Vk: 0x09}, keybdInput{Vk: 0x09, Flags: keyUp})
			continue
		}
		for _, u := range utf16.Encode([]rune{r}) {
			ks = append(ks, keybdInput{Scan: u, Flags: keyUnicode}, keybdInput{Scan: u, Flags: keyUnicode | keyUp})
		}
	}
	// A few characters at a time: some apps drop keys sent in one burst.
	for i := 0; i < len(ks); i += 8 {
		if err := sendKeys(ks[i:min(i+8, len(ks))]); err != nil {
			return err
		}
		time.Sleep(6 * time.Millisecond)
	}
	return nil
}

// vkNames maps canonical key names to virtual-key codes; ext marks keys
// that need KEYEVENTF_EXTENDEDKEY.
var vkNames = map[string]struct {
	vk  uint16
	ext bool
}{
	"ctrl": {0x11, false}, "alt": {0x12, false}, "shift": {0x10, false}, "super": {0x5B, true},
	"enter": {0x0D, false}, "escape": {0x1B, false}, "tab": {0x09, false}, "space": {0x20, false},
	"backspace": {0x08, false}, "delete": {0x2E, true}, "insert": {0x2D, true},
	"home": {0x24, true}, "end": {0x23, true}, "pageup": {0x21, true}, "pagedown": {0x22, true},
	"up": {0x26, true}, "down": {0x28, true}, "left": {0x25, true}, "right": {0x27, true},
	"capslock": {0x14, false}, "printscreen": {0x2C, true}, "menu": {0x5D, true},
	"volumeup": {0xAF, true}, "volumedown": {0xAE, true}, "volumemute": {0xAD, true}, "playpause": {0xB3, true},
}

func (winDriver) Key(name string, down bool) error {
	var vk uint16
	var ext, shift bool
	if k, ok := vkNames[name]; ok {
		vk, ext = k.vk, k.ext
	} else if n, ok := fkey(name); ok {
		vk = uint16(0x70 + n - 1)
	} else if r := []rune(name); len(r) == 1 {
		switch {
		case r[0] >= 'a' && r[0] <= 'z':
			vk = uint16(r[0] - 'a' + 'A')
		case r[0] >= '0' && r[0] <= '9':
			vk = uint16(r[0])
		default:
			res, _, _ := pVkKeyScanW.Call(uintptr(r[0]))
			if int16(res) == -1 {
				// Not on this keyboard layout: type it instead.
				if down {
					return sysType(name)
				}
				return nil
			}
			vk, shift = uint16(res&0xFF), res&0x100 != 0
		}
	} else {
		return fmt.Errorf("unknown key %q", name)
	}
	var ks []keybdInput
	flags := uint32(0)
	if ext {
		flags |= keyExtended
	}
	if down {
		if shift {
			ks = append(ks, keybdInput{Vk: 0x10})
		}
		ks = append(ks, keybdInput{Vk: vk, Flags: flags})
	} else {
		ks = append(ks, keybdInput{Vk: vk, Flags: flags | keyUp})
		if shift {
			ks = append(ks, keybdInput{Vk: 0x10, Flags: keyUp})
		}
	}
	return sendKeys(ks)
}

func sysType(s string) error { return sys.Type(s) }

// ownWindows are ASG's top-level windows and the console it runs in.
func ownWindows() map[uintptr]bool {
	own := map[uintptr]bool{}
	if pGetConsoleWindow.Find() == nil {
		if c, _, _ := pGetConsoleWindow.Call(); c != 0 {
			own[c] = true
			// Windows Terminal: the pseudo console's owner is the terminal window.
			const gaRootOwner = 3
			if o, _, _ := pGetAncestor.Call(c, gaRootOwner); o != 0 {
				own[o] = true
			}
		}
	}
	return own
}

func procName(pid uint32) string {
	const queryLimited = 0x1000
	h, _, _ := pOpenProcess.Call(queryLimited, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:n]))
}

func (winDriver) Windows() ([]Window, error) {
	dpiAware()
	enumMu.Lock()
	enumWins = nil
	pEnumWindows.Call(winCB, 0)
	hwnds := append([]uintptr(nil), enumWins...)
	enumMu.Unlock()

	fg, _, _ := pGetForegroundWindow.Call()
	own := ownWindows()
	me := uint32(os.Getpid())
	names := map[uint32]string{}
	var out []Window
	for _, h := range hwnds {
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			continue
		}
		const gwlExStyle, wsExToolWindow = ^uintptr(19), 0x80 // GWL_EXSTYLE = -20
		if ex, _, _ := pGetWindowLongW.Call(h, gwlExStyle); ex&wsExToolWindow != 0 {
			continue
		}
		if pDwmGetWindowAttribute.Find() == nil {
			var cloaked uint32
			const dwmCloaked = 14
			if r, _, _ := pDwmGetWindowAttribute.Call(h, dwmCloaked, uintptr(unsafe.Pointer(&cloaked)), 4); r == 0 && cloaked != 0 {
				continue
			}
		}
		n, _, _ := pGetWindowTextLengthW.Call(h)
		if n == 0 {
			continue
		}
		buf := make([]uint16, n+1)
		pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
		var rc rect32
		pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&rc)))
		var pid uint32
		pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		name, ok := names[pid]
		if !ok {
			name = procName(pid)
			names[pid] = name
		}
		icon, _, _ := pIsIconic.Call(h)
		out = append(out, Window{
			ID: fmt.Sprintf("0x%x", h), Title: syscall.UTF16ToString(buf), App: name, Rect: rc.rect(),
			Focused: h == fg, Minimized: icon != 0, Own: pid == me || own[h],
		})
	}
	return out, nil
}

func (winDriver) Focus(id string) error {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(id), "0x"), 16, 64)
	if err != nil {
		return fmt.Errorf("bad window id %q", id)
	}
	h := uintptr(v)
	if ok, _, _ := pIsWindow.Call(h); ok == 0 {
		return fmt.Errorf("window %s no longer exists", id)
	}
	if icon, _, _ := pIsIconic.Call(h); icon != 0 {
		const swRestore = 9
		pShowWindow.Call(h, swRestore)
	}
	// Windows only lets the process that last had input change the
	// foreground window; a tap of Alt counts as input.
	_ = sendKeys([]keybdInput{{Vk: 0x12}, {Vk: 0x12, Flags: keyUp}})
	pSetForegroundWindow.Call(h)
	pBringWindowToTop.Call(h)
	time.Sleep(150 * time.Millisecond)
	if fg, _, _ := pGetForegroundWindow.Call(); fg != h {
		return errors.New("Windows did not let the window come to the front — try clicking it instead")
	}
	return nil
}

func (winDriver) Open(target string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	t, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	const swShowNormal = 1
	r, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(t)), 0, 0, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("Windows could not open %q (error %d)", target, r)
	}
	return nil
}
