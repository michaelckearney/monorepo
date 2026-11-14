//go:build darwin

package securitykey

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// IOKit and CoreFoundation, which every Mac has, are called through purego
// rather than cgo, so an app that reaches security keys builds with
// CGO_ENABLED=0, on any machine, like the rest of the app.

const (
	kCFNumberIntType          = 9
	kCFStringEncodingUTF8     = 0x08000100
	kIOHIDOptionsTypeNone     = 0
	kIOHIDReportTypeOutput    = 1
	kIOReturnSuccess          = 0
	fidoUsagePage, fidoUsage  = 0xF1D0, 1
	kIOHIDPrimaryUsagePageKey = "PrimaryUsagePage"
	kIOHIDPrimaryUsageKey     = "PrimaryUsage"
)

var (
	loadOnce sync.Once
	loadErr  error

	// The type callbacks CFDictionaryCreate takes, and the run loop mode,
	// are data CoreFoundation exports rather than functions.
	keyCallbacks, valueCallbacks uintptr
	defaultMode                  uintptr

	// One callback of each kind serves every device: purego makes a limited
	// number in a process, and never frees them. Each call says which
	// device it is for with the context the device was registered with.
	reportCallback, removalCallback uintptr

	malloc func(size uintptr) uintptr
	free   func(ptr uintptr)

	cfRetain               func(cf uintptr) uintptr
	cfRelease              func(cf uintptr)
	cfNumberCreate         func(alloc uintptr, kind int, value unsafe.Pointer) uintptr
	cfStringCreateWithCStr func(alloc uintptr, s string, encoding uint32) uintptr
	cfDictionaryCreate     func(alloc uintptr, keys, values unsafe.Pointer, count int, keyCB, valueCB uintptr) uintptr
	cfSetGetCount          func(set uintptr) int
	cfSetGetValues         func(set uintptr, values unsafe.Pointer)
	cfRunLoopGetCurrent    func() uintptr
	cfRunLoopRun           func()
	cfRunLoopStop          func(loop uintptr)

	hidManagerCreate               func(alloc uintptr, options uint32) uintptr
	hidManagerSetDeviceMatching    func(manager, match uintptr)
	hidManagerCopyDevices          func(manager uintptr) uintptr
	hidDeviceOpen                  func(device uintptr, options uint32) int32
	hidDeviceClose                 func(device uintptr, options uint32) int32
	hidDeviceRegisterInputReport   func(device, report uintptr, length int, callback, context uintptr)
	hidDeviceRegisterRemoval       func(device, callback, context uintptr)
	hidDeviceScheduleWithRunLoop   func(device, loop, mode uintptr)
	hidDeviceUnscheduleFromRunLoop func(device, loop, mode uintptr)
	hidDeviceSetReport             func(device uintptr, kind uint32, id int, report unsafe.Pointer, length int) int32
)

// devices are the open devices, by the context their callbacks carry.
var (
	devices    sync.Map // uintptr -> *iokit
	nextDevice atomic.Uintptr
)

func load() error {
	loadOnce.Do(func() {
		libs := map[string]uintptr{}
		for name, path := range map[string]string{
			"system": "/usr/lib/libSystem.B.dylib",
			"cf":     "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			"iokit":  "/System/Library/Frameworks/IOKit.framework/IOKit",
		} {
			h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				loadErr = fmt.Errorf("could not load %s: %w", path, err)
				return
			}
			libs[name] = h
		}
		for _, f := range []struct {
			lib, name string
			fn        any
		}{
			{"system", "malloc", &malloc},
			{"system", "free", &free},
			{"cf", "CFRetain", &cfRetain},
			{"cf", "CFRelease", &cfRelease},
			{"cf", "CFNumberCreate", &cfNumberCreate},
			{"cf", "CFStringCreateWithCString", &cfStringCreateWithCStr},
			{"cf", "CFDictionaryCreate", &cfDictionaryCreate},
			{"cf", "CFSetGetCount", &cfSetGetCount},
			{"cf", "CFSetGetValues", &cfSetGetValues},
			{"cf", "CFRunLoopGetCurrent", &cfRunLoopGetCurrent},
			{"cf", "CFRunLoopRun", &cfRunLoopRun},
			{"cf", "CFRunLoopStop", &cfRunLoopStop},
			{"iokit", "IOHIDManagerCreate", &hidManagerCreate},
			{"iokit", "IOHIDManagerSetDeviceMatching", &hidManagerSetDeviceMatching},
			{"iokit", "IOHIDManagerCopyDevices", &hidManagerCopyDevices},
			{"iokit", "IOHIDDeviceOpen", &hidDeviceOpen},
			{"iokit", "IOHIDDeviceClose", &hidDeviceClose},
			{"iokit", "IOHIDDeviceRegisterInputReportCallback", &hidDeviceRegisterInputReport},
			{"iokit", "IOHIDDeviceRegisterRemovalCallback", &hidDeviceRegisterRemoval},
			{"iokit", "IOHIDDeviceScheduleWithRunLoop", &hidDeviceScheduleWithRunLoop},
			{"iokit", "IOHIDDeviceUnscheduleFromRunLoop", &hidDeviceUnscheduleFromRunLoop},
			{"iokit", "IOHIDDeviceSetReport", &hidDeviceSetReport},
		} {
			purego.RegisterLibFunc(f.fn, libs[f.lib], f.name)
		}
		var err error
		if keyCallbacks, err = purego.Dlsym(libs["cf"], "kCFTypeDictionaryKeyCallBacks"); err != nil {
			loadErr = err
			return
		}
		if valueCallbacks, err = purego.Dlsym(libs["cf"], "kCFTypeDictionaryValueCallBacks"); err != nil {
			loadErr = err
			return
		}
		mode, err := purego.Dlsym(libs["cf"], "kCFRunLoopDefaultMode")
		if err != nil {
			loadErr = err
			return
		}
		// mode is the address of the variable, which holds the string.
		defaultMode = **(**uintptr)(unsafe.Pointer(&mode))

		// IOHIDReportCallback: context, result, sender, type, report ID,
		// report, length.
		reportCallback = purego.NewCallback(func(context uintptr, result int32, sender uintptr, kind uint32, id uint32, report uintptr, length int) {
			if d, ok := devices.Load(context); ok {
				// report is IOKit's buffer, not Go memory.
				data := unsafe.Slice(*(**byte)(unsafe.Pointer(&report)), length)
				d.(*iokit).put(append([]byte(nil), data...))
			}
		})
		// IOHIDCallback: context, result, sender.
		removalCallback = purego.NewCallback(func(context uintptr, result int32, sender uintptr) {
			if d, ok := devices.Load(context); ok {
				d.(*iokit).stop(errors.New("the security key was unplugged"))
			}
		})
	})
	return loadErr
}

// Devices are the security keys plugged in, through IOKit.
func Devices() ([]Device, error) {
	if err := load(); err != nil {
		return nil, err
	}
	refs := fidoDevices()
	var devs []Device
	for _, ref := range refs {
		if res := hidDeviceOpen(ref, kIOHIDOptionsTypeNone); res != kIOReturnSuccess {
			cfRelease(ref)
			continue
		}
		d := &iokit{ref: ref, reports: newReports(), id: nextDevice.Add(1), loop: make(chan uintptr, 1)}
		devices.Store(d.id, d)
		go d.listen()
		d.runLoop = <-d.loop
		devs = append(devs, d)
	}
	return devs, nil
}

// fidoDevices returns the FIDO devices plugged in, each retained.
func fidoDevices() []uintptr {
	manager := hidManagerCreate(0, kIOHIDOptionsTypeNone)
	if manager == 0 {
		return nil
	}
	defer cfRelease(manager)
	page, usage := int32(fidoUsagePage), int32(fidoUsage)
	pageNum := cfNumberCreate(0, kCFNumberIntType, unsafe.Pointer(&page))
	defer cfRelease(pageNum)
	usageNum := cfNumberCreate(0, kCFNumberIntType, unsafe.Pointer(&usage))
	defer cfRelease(usageNum)
	pageKey := cfStringCreateWithCStr(0, kIOHIDPrimaryUsagePageKey, kCFStringEncodingUTF8)
	defer cfRelease(pageKey)
	usageKey := cfStringCreateWithCStr(0, kIOHIDPrimaryUsageKey, kCFStringEncodingUTF8)
	defer cfRelease(usageKey)
	keys := [2]uintptr{pageKey, usageKey}
	values := [2]uintptr{pageNum, usageNum}
	match := cfDictionaryCreate(0, unsafe.Pointer(&keys[0]), unsafe.Pointer(&values[0]), 2, keyCallbacks, valueCallbacks)
	hidManagerSetDeviceMatching(manager, match)
	cfRelease(match)

	set := hidManagerCopyDevices(manager)
	if set == 0 {
		return nil
	}
	defer cfRelease(set)
	n := cfSetGetCount(set)
	if n == 0 {
		return nil
	}
	refs := make([]uintptr, n)
	cfSetGetValues(set, unsafe.Pointer(&refs[0]))
	for _, ref := range refs {
		cfRetain(ref)
	}
	return refs
}

type iokit struct {
	ref     uintptr
	id      uintptr
	loop    chan uintptr
	runLoop uintptr
	*reports
}

// listen runs the device's run loop on a thread of its own, until Close
// stops it; then, with no callback left to come, it lets the device go.
func (d *iokit) listen() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// IOKit writes each report into the buffer after this function has
	// handed it over, so it is not Go memory, which the collector may move
	// or free.
	buffer := malloc(reportSize)
	loop := cfRunLoopGetCurrent()
	hidDeviceRegisterInputReport(d.ref, buffer, reportSize, reportCallback, d.id)
	hidDeviceRegisterRemoval(d.ref, removalCallback, d.id)
	hidDeviceScheduleWithRunLoop(d.ref, loop, defaultMode)
	d.loop <- loop
	cfRunLoopRun()
	hidDeviceUnscheduleFromRunLoop(d.ref, loop, defaultMode)
	hidDeviceClose(d.ref, kIOHIDOptionsTypeNone)
	cfRelease(d.ref)
	free(buffer)
	devices.Delete(d.id)
}

func (d *iokit) Write(report []byte) error {
	res := hidDeviceSetReport(d.ref, kIOHIDReportTypeOutput, 0, unsafe.Pointer(&report[0]), len(report))
	runtime.KeepAlive(report)
	if res != kIOReturnSuccess {
		return fmt.Errorf("could not write to the security key (IOKit error %#x)", uint32(res))
	}
	return nil
}

func (d *iokit) Close() error {
	d.stop(nil)
	cfRunLoopStop(d.runLoop)
	return nil
}
