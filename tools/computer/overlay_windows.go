//go:build windows

package computer

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The overlay thread owns a frame of thin topmost windows drawn around
// the monitor ASG is controlling (amber while acting, grey while paused),
// and the low-level mouse / keyboard hooks that tell the user's own input
// from input ASG injects: physical Esc is the emergency stop, and other
// physical input mid-action is a takeover.

var (
	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostThreadMessageW       = user32.NewProc("PostThreadMessageW")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pSetLayeredWindowAttrs    = user32.NewProc("SetLayeredWindowAttributes")
	pSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")
	pSetWindowsHookExW        = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx      = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx           = user32.NewProc("CallNextHookEx")
	pPeekMessageW             = user32.NewProc("PeekMessageW")
	pCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	pGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

const (
	wmApp        = 0x8000
	wmUpdate     = wmApp + 1
	frameThick   = 4
	hwndTopmost  = ^uintptr(0) // -1
	swpNoActive  = 0x0010
	swpShow      = 0x0040
	swpHide      = 0x0080
	whMouseLL    = 14
	whKeyboardLL = 13
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point32
	_       uint32
}

type msLLHook struct {
	Pt        point32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type kbLLHook struct {
	Vk, Scan, Flags, Time uint32
	Extra                 uintptr
}

var ui struct {
	once   sync.Once
	thread atomic.Uint32
	mu     sync.Mutex
	want   State // what to show
	frame  Rect
	hooks  bool
	// set on the UI thread only
	active, paused [4]uintptr
	mouseHook      uintptr
	keyHook        uintptr
	start          point32
	moved          bool
}

var wndProc = syscall.NewCallback(func(h, m, w, l uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(h, m, w, l)
	return r
})

// mouseProc and keyProc are made on the UI thread (a package-level
// initializer would form an init cycle through physical → Stop → indicate).
var mouseProc, keyProc uintptr

func mouseHook(code, w, l uintptr) uintptr {
	if int32(code) >= 0 {
		in := *(**msLLHook)(unsafe.Pointer(&l)) // lParam points at the hook struct
		const injected = 0x1 | 0x2              // LLMHF_INJECTED | LLMHF_LOWER_IL_INJECTED
		if in.Flags&injected == 0 {
			switch w {
			case 0x0200: // WM_MOUSEMOVE: ignore a few pixels of jitter
				if !ui.moved {
					ui.start, ui.moved = in.Pt, true
				} else if math.Hypot(float64(in.Pt.X-ui.start.X), float64(in.Pt.Y-ui.start.Y)) > 10 {
					ui.start = in.Pt
					physical("mouse")
				}
			default: // buttons and wheel
				physical("mouse")
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, code, w, l)
	return r
}

func keyHook(code, w, l uintptr) uintptr {
	if int32(code) >= 0 {
		in := *(**kbLLHook)(unsafe.Pointer(&l))
		const injected = 0x10 | 0x02                                // LLKHF_INJECTED | LLKHF_LOWER_IL_INJECTED
		if in.Flags&injected == 0 && (w == 0x0100 || w == 0x0104) { // WM_KEYDOWN, WM_SYSKEYDOWN
			if in.Vk == 0x1B {
				physical("stop")
			} else {
				physical("keyboard")
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, code, w, l)
	return r
}

func stopKey() string { return "Esc" }

// startUI starts the overlay thread once.
func startUI() {
	ui.once.Do(func() {
		ready := make(chan struct{})
		go uiLoop(ready)
		<-ready
	})
}

func uiLoop(ready chan struct{}) {
	runtime.LockOSThread()
	dpiAware()
	mouseProc, keyProc = syscall.NewCallback(mouseHook), syscall.NewCallback(keyHook)
	tid, _, _ := pGetCurrentThreadId.Call()
	ui.thread.Store(uint32(tid))
	inst, _, _ := pGetModuleHandleW.Call(0)
	mk := func(name string, rgb uint32) [4]uintptr {
		cls, _ := syscall.UTF16PtrFromString(name)
		brush, _, _ := pCreateSolidBrush.Call(uintptr(rgb))
		wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: wndProc, Instance: inst, Background: brush, ClassName: cls}
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		var out [4]uintptr
		const (
			exTopmost, exTool, exNoActivate, exTransparent, exLayered = 0x8, 0x80, 0x08000000, 0x20, 0x80000
			wsPopup                                                   = 0x80000000
		)
		for i := range out {
			h, _, _ := pCreateWindowExW.Call(exTopmost|exTool|exNoActivate|exTransparent|exLayered, uintptr(unsafe.Pointer(cls)), 0, wsPopup, 0, 0, 1, 1, 0, 0, inst, 0)
			if h == 0 {
				continue
			}
			const lwaAlpha = 0x2
			pSetLayeredWindowAttrs.Call(h, 0, 230, lwaAlpha)
			if pSetWindowDisplayAffinity.Find() == nil {
				const wdaExcludeFromCapture = 0x11 // keeps the frame out of screenshots
				pSetWindowDisplayAffinity.Call(h, wdaExcludeFromCapture)
			}
			out[i] = h
		}
		return out
	}
	// COLORREF is 0x00BBGGRR.
	ui.active = mk("ASGComputerActive", 0x0024A8F5) // amber #F5A824
	ui.paused = mk("ASGComputerPaused", 0x00A0A0A0)
	// Make sure the thread has a message queue before anyone posts to it.
	var m msg
	pPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 0)
	close(ready)
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if m.Hwnd == 0 && m.Message == wmUpdate {
			uiApply()
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// uiApply shows the wanted frame and hooks; runs on the UI thread.
func uiApply() {
	ui.mu.Lock()
	st, fr, hooks := ui.want, ui.frame, ui.hooks
	ui.mu.Unlock()
	show := func(ws [4]uintptr, on bool) {
		rs := [4]Rect{
			{fr.X, fr.Y, fr.W, frameThick},
			{fr.X, fr.Y + fr.H - frameThick, fr.W, frameThick},
			{fr.X, fr.Y, frameThick, fr.H},
			{fr.X + fr.W - frameThick, fr.Y, frameThick, fr.H},
		}
		for i, h := range ws {
			if h == 0 {
				continue
			}
			if on && !fr.empty() {
				r := rs[i]
				pSetWindowPos.Call(h, hwndTopmost, uintptr(int32(r.X)), uintptr(int32(r.Y)), uintptr(r.W), uintptr(r.H), swpNoActive|swpShow)
			} else {
				pSetWindowPos.Call(h, 0, 0, 0, 0, 0, swpNoActive|swpHide|0x1|0x2|0x4)
			}
		}
	}
	active := st.Active && !st.Stopped
	show(ui.active, active && !st.Paused)
	show(ui.paused, active && st.Paused)

	inst, _, _ := pGetModuleHandleW.Call(0)
	if hooks && ui.mouseHook == 0 {
		ui.moved = false
		ui.mouseHook, _, _ = pSetWindowsHookExW.Call(whMouseLL, mouseProc, inst, 0)
		ui.keyHook, _, _ = pSetWindowsHookExW.Call(whKeyboardLL, keyProc, inst, 0)
	}
	if !hooks && ui.mouseHook != 0 {
		pUnhookWindowsHookEx.Call(ui.mouseHook)
		pUnhookWindowsHookEx.Call(ui.keyHook)
		ui.mouseHook, ui.keyHook = 0, 0
	}
}

func post() {
	if t := ui.thread.Load(); t != 0 {
		pPostThreadMessageW.Call(uintptr(t), wmUpdate, 0, 0)
	}
}

// indicate shows or hides the frame for a state.
func indicate(st State) {
	if !st.Active && ui.thread.Load() == 0 {
		return // never shown: don't start a thread just to hide nothing
	}
	startUI()
	fr := Rect{}
	ctl.mu.Lock()
	if ctl.run != nil && ctl.run.view != nil {
		fr = ctl.run.view.Rect
	}
	ctl.mu.Unlock()
	if fr.empty() {
		if ms, err := monitors(); err == nil {
			fr = ms[0].Rect
		}
	}
	ui.mu.Lock()
	ui.want, ui.frame = st, fr
	ui.mu.Unlock()
	post()
}

// watchInput installs (or removes) the low-level input hooks.
func watchInput(on bool) {
	if !on && ui.thread.Load() == 0 {
		return
	}
	startUI()
	ui.mu.Lock()
	ui.hooks = on
	ui.mu.Unlock()
	post()
}
