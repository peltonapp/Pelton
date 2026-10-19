//go:build windows

package desktop

import (
	"syscall"
	"unsafe"
)

// micaMinBuild is the first Windows 11 build with DWM system backdrops. Below
// it there is no Mica to ask for.
const micaMinBuild = 22621

// osVersionInfo mirrors RTL_OSVERSIONINFOW. GetVersionEx lies about the build
// on anything without a matching app manifest, so RtlGetVersion is the only
// reading worth trusting here.
type osVersionInfo struct {
	osVersionInfoSize uint32
	majorVersion      uint32
	minorVersion      uint32
	buildNumber       uint32
	platformID        uint32
	csdVersion        [128]uint16
}

// supportsMica reports whether this Windows can draw the Mica material.
func supportsMica() bool {
	proc := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if err := proc.Find(); err != nil {
		return false
	}
	var info osVersionInfo
	info.osVersionInfoSize = uint32(unsafe.Sizeof(info))
	if ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&info))); ret != 0 {
		return false
	}
	return info.majorVersion > 10 ||
		(info.majorVersion == 10 && info.buildNumber >= micaMinBuild)
}
