package appdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

var ErrAlreadyRunning = errors.New("CV++ is already running")

type Instance struct {
	paths Paths
	lock  *os.File
}

func AcquireInstance(paths Paths, state RuntimeState) (*Instance, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	lockPath := paths.RuntimeState + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			if existing, readErr := ReadRuntimeState(paths.RuntimeState); readErr == nil && instanceReachable(existing) {
				return nil, fmt.Errorf("%w: %s", ErrAlreadyRunning, existing.URL)
			}
			// A lock without a reachable process is stale (for example after a
			// power loss). It is safe to remove this narrow, app-owned file.
			_ = os.Remove(lockPath)
			lock, err = os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		}
		if err != nil {
			return nil, err
		}
	}
	instance := &Instance{paths: paths, lock: lock}
	if err := instance.WriteState(state); err != nil {
		_ = instance.Close()
		return nil, err
	}
	return instance, nil
}

func instanceReachable(state RuntimeState) bool {
	if state.Port <= 0 {
		return false
	}
	address := fmt.Sprintf("127.0.0.1:%d", state.Port)
	for attempt := 0; attempt < 5; attempt++ {
		conn, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func (i *Instance) WriteState(state RuntimeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return AtomicWrite(i.paths.RuntimeState, append(data, '\n'), 0o600)
}

func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	if i.lock != nil {
		_ = i.lock.Close()
	}
	_ = os.Remove(i.paths.RuntimeState + ".lock")
	_ = os.Remove(i.paths.RuntimeState)
	return nil
}
