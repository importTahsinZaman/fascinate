//go:build linux

package netnsforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type Config struct {
	Namespace string
	Listen    string
	Target    string
	PortFile  string
}

func Run(ctx context.Context, cfg Config) error {
	namespace := strings.TrimSpace(cfg.Namespace)
	listenAddr := strings.TrimSpace(cfg.Listen)
	targetAddr := strings.TrimSpace(cfg.Target)
	portFile := strings.TrimSpace(cfg.PortFile)
	if namespace == "" || listenAddr == "" || targetAddr == "" || portFile == "" {
		return fmt.Errorf("namespace, listen, target, and port file are required")
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("unexpected listener address %T", listener.Addr())
	}
	if err := os.MkdirAll(filepath.Dir(portFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(tcpAddr.Port)+"\n"), 0o600); err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Temporary() {
				continue
			}
			return err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()

			targetConn, err := dialInNamespace(namespace, targetAddr)
			if err != nil {
				return
			}
			defer targetConn.Close()

			var copyWG sync.WaitGroup
			copyWG.Add(2)
			go proxyCopy(&copyWG, targetConn, conn)
			go proxyCopy(&copyWG, conn, targetConn)
			copyWG.Wait()
		}()
	}
}

func dialInNamespace(namespace, targetAddr string) (net.Conn, error) {
	namespace = strings.TrimSpace(namespace)
	targetAddr = strings.TrimSpace(targetAddr)
	if namespace == "" || targetAddr == "" {
		return nil, fmt.Errorf("namespace and target are required")
	}

	var (
		conn net.Conn
		err  error
	)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNS, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, err
	}
	defer origNS.Close()

	targetNS, err := os.Open(filepath.Join("/var/run/netns", namespace))
	if err != nil {
		return nil, err
	}
	defer targetNS.Close()

	if err := unix.Setns(int(targetNS.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns %s: %w", namespace, err)
	}
	defer func() {
		restoreErr := unix.Setns(int(origNS.Fd()), unix.CLONE_NEWNET)
		if err == nil && restoreErr != nil {
			err = restoreErr
		}
		if err != nil && conn != nil {
			_ = conn.Close()
			conn = nil
		}
	}()

	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err = dialer.Dial("tcp", targetAddr)
	return conn, err
}

func proxyCopy(wg *sync.WaitGroup, dst io.Writer, src io.Reader) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
	if closer, ok := dst.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}
}

func Signals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}
