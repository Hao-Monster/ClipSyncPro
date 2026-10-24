//go:build !windows

package autostart

func EnsureEnabled(string) error {
	return nil
}
