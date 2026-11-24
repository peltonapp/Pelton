//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CoreServices
#include <stdlib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <CoreServices/CoreServices.h>

// LSSetDefaultHandlerForURLScheme / LSCopyDefaultHandlerForURLScheme are
// deprecated since macOS 12 but remain the only supported way to read and set
// the default scheme handler without a private API; silence the warning.
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

// peltonSetMailtoDefault makes bundleID the default mailto handler. It returns
// the OSStatus (0 == noErr). macOS shows its own confirmation sheet.
static int peltonSetMailtoDefault(const char *bundleID) {
    CFStringRef bid = CFStringCreateWithCString(NULL, bundleID, kCFStringEncodingUTF8);
    OSStatus st = LSSetDefaultHandlerForURLScheme(CFSTR("mailto"), bid);
    CFRelease(bid);
    return (int)st;
}

// peltonIsMailtoDefault returns 1 when bundleID is the current default mailto
// handler, 0 when it is not, and -1 when it cannot be determined.
static int peltonIsMailtoDefault(const char *bundleID) {
    CFStringRef current = LSCopyDefaultHandlerForURLScheme(CFSTR("mailto"));
    if (current == NULL) {
        return -1;
    }
    CFStringRef bid = CFStringCreateWithCString(NULL, bundleID, kCFStringEncodingUTF8);
    Boolean eq = CFStringCompare(current, bid, kCFCompareCaseInsensitive) == kCFCompareEqualTo;
    CFRelease(current);
    CFRelease(bid);
    return eq ? 1 : 0;
}
// peltonSetICSDefault makes bundleID the default app for .ics files.
static int peltonSetICSDefault(const char *bundleID) {
    CFStringRef bid = CFStringCreateWithCString(NULL, bundleID, kCFStringEncodingUTF8);
    OSStatus st = LSSetDefaultRoleHandlerForContentType(CFSTR("com.apple.ical.ics"), kLSRolesAll, bid);
    CFRelease(bid);
    return (int)st;
}

// peltonIsICSDefault returns 1 when bundleID opens .ics files, 0 when another
// app does, and -1 when it cannot be determined.
static int peltonIsICSDefault(const char *bundleID) {
    CFStringRef current = LSCopyDefaultRoleHandlerForContentType(CFSTR("com.apple.ical.ics"), kLSRolesAll);
    if (current == NULL) {
        return -1;
    }
    CFStringRef bid = CFStringCreateWithCString(NULL, bundleID, kCFStringEncodingUTF8);
    Boolean eq = CFStringCompare(current, bid, kCFCompareCaseInsensitive) == kCFCompareEqualTo;
    CFRelease(current);
    CFRelease(bid);
    return eq ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// peltonBundleID must match CFBundleIdentifier in build/darwin/Info.plist.
const peltonBundleID = "sh.arne.pelton"

func isDefaultMailHandler() (isDefault bool, known bool) {
	cid := C.CString(peltonBundleID)
	defer C.free(unsafe.Pointer(cid))
	switch C.peltonIsMailtoDefault(cid) {
	case 1:
		return true, true
	case 0:
		return false, true
	default:
		return false, false
	}
}

func isDefaultCalendarHandler() (isDefault bool, known bool) {
	cid := C.CString(peltonBundleID)
	defer C.free(unsafe.Pointer(cid))
	switch C.peltonIsICSDefault(cid) {
	case 1:
		return true, true
	case 0:
		return false, true
	default:
		return false, false
	}
}

func setDefaultCalendarHandler() error {
	cid := C.CString(peltonBundleID)
	defer C.free(unsafe.Pointer(cid))
	if st := C.peltonSetICSDefault(cid); st != 0 {
		return fmt.Errorf("open .ics files with Pelton: LSSetDefaultRoleHandlerForContentType OSStatus %d", int(st))
	}
	return nil
}

func setDefaultMailHandler() error {
	cid := C.CString(peltonBundleID)
	defer C.free(unsafe.Pointer(cid))
	if st := C.peltonSetMailtoDefault(cid); st != 0 {
		return fmt.Errorf("set default mail handler: LSSetDefaultHandlerForURLScheme OSStatus %d", int(st))
	}
	return nil
}
