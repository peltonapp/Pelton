//go:build !darwin

package desktop

import "errors"

func nativeOCRAvailable() bool {
	return false
}

func nativeOCR([]byte) ([]OCRWordDTO, error) {
	return nil, errors.New("text recognition is not available on this system")
}
