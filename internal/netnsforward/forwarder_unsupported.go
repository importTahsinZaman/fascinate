//go:build !linux

package netnsforward

import (
	"context"
	"fmt"
	"os"
)

type Config struct {
	Namespace string
	Listen    string
	Target    string
	PortFile  string
}

func Run(_ context.Context, _ Config) error {
	return fmt.Errorf("network namespace forwarding is only supported on linux hosts")
}

func Signals() []os.Signal {
	return nil
}
