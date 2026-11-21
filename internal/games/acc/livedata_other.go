//go:build !windows

package acc

import "errors"

func readShared(name string, size int) ([]byte, error) {
	return nil, errors.New("shared memory is only available on Windows")
}
