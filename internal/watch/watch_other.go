//go:build !windows

package watch

// Watch is not implemented outside Windows; folders are then only scanned on start
// and on manual rescan.
func Watch(dir string, onChange func()) (stop func(), err error) {
	return func() {}, nil
}
