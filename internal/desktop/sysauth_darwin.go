//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework Foundation -framework LocalAuthentication
#include <stdlib.h>
int peltonCanAuthenticate(void);
int peltonAuthenticate(const char *reason, long *code);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// laUserCancel, laSystemCancel and laAppCancel are the LocalAuthentication
// errors for a prompt the user or the system dismissed, which are an answer
// rather than a failure worth logging.
const (
	laUserCancel   = -2
	laSystemCancel = -4
	laAppCancel    = -9
)

// localAuth is the macOS device owner prompt: Touch ID where the Mac has it,
// the account password otherwise or as the fallback.
type localAuth struct{}

func newSystemAuth() systemAuth { return localAuth{} }

func (localAuth) available() bool {
	return C.peltonCanAuthenticate() == 1
}

func (localAuth) authenticate(reason string) (bool, error) {
	cReason := C.CString(reason)
	defer C.free(unsafe.Pointer(cReason))
	var code C.long
	if C.peltonAuthenticate(cReason, &code) == 1 {
		return true, nil
	}
	switch code {
	case 0, laUserCancel, laSystemCancel, laAppCancel:
		return false, nil
	}
	return false, fmt.Errorf("local authentication failed with code %d", int(code))
}
