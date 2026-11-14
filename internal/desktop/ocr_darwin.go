//go:build darwin

package desktop

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Vision
#include <stdlib.h>
char *peltonRecognizeText(const void *bytes, int length, char **errOut);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

func nativeOCRAvailable() bool {
	return true
}

func nativeOCR(png []byte) ([]OCRWordDTO, error) {
	var cerr *C.char
	out := C.peltonRecognizeText(unsafe.Pointer(&png[0]), C.int(len(png)), &cerr)
	if out == nil {
		msg := "text recognition failed"
		if cerr != nil {
			msg = C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return nil, errors.New(msg)
	}
	defer C.free(unsafe.Pointer(out))
	words := []OCRWordDTO{}
	if err := json.Unmarshal([]byte(C.GoString(out)), &words); err != nil {
		return nil, err
	}
	return words, nil
}
