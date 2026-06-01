//go:build !windows

package singleinstance

type Lock struct{}

func Acquire(string) (*Lock, bool, error) {
	return &Lock{}, false, nil
}

func (l *Lock) Release() {}

func ActivateExistingWindow(string, string) bool {
	return false
}
