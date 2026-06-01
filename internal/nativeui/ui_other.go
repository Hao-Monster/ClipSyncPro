//go:build !windows

package nativeui

import (
	"context"
	"errors"

	"clipsyncpro/internal/app"
)

func Run(context.Context, *app.Service, <-chan struct{}) error {
	return errors.New("native Windows UI is only available on Windows")
}
