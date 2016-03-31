// Copyright 2014 The rkt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package lock implements simple locking primitives on a
// regular file or directory using flock

package lock

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	ErrLocked     = errors.New("file already locked")
	ErrNotExist   = errors.New("file does not exist")
	ErrPermission = errors.New("permission denied")
	ErrNotRegular = errors.New("not a regular file")
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	INVALID_FILE_HANDLE     = ^syscall.Handle(0)
	LOCKFILE_FAIL_IMMEDIATELY = 1
	LOCKFILE_EXCLUSIVE_LOCK = 2
)

// FileLock represents a lock on a regular file or a directory
type FileLock struct {
	path string
	fd syscall.Handle
}

type LockType int

const (
	Dir LockType = iota
	RegFile
)

func lockFileEx(h syscall.Handle, flags, reserved, locklow, lockhigh uint32, ol *syscall.Overlapped) (err error) {
	r1, _, e1 := syscall.Syscall6(procLockFileEx.Addr(), 6, uintptr(h), uintptr(flags), uintptr(reserved), uintptr(locklow), uintptr(lockhigh), uintptr(unsafe.Pointer(ol)))
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func unlockFileEx(h syscall.Handle, reserved, locklow, lockhigh uint32, ol *syscall.Overlapped) (err error) {
	r1, _, e1 := syscall.Syscall6(procUnlockFileEx.Addr(), 5, uintptr(h), uintptr(reserved), uintptr(locklow), uintptr(lockhigh), uintptr(unsafe.Pointer(ol)), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

// TryExclusiveLock takes an exclusive lock without blocking.
// This is idempotent when the Lock already represents an exclusive lock,
// and tries promote a shared lock to exclusive atomically.
// It will return ErrLocked if any lock is already held.
func (l *FileLock) TryExclusiveLock() error {
	var ol syscall.Overlapped
	if err := lockFileEx(l.fd, LOCKFILE_FAIL_IMMEDIATELY|LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol); err != nil {
		err = ErrLocked
	}
	return err
}

// TryExclusiveLock takes an exclusive lock on a file/directory without blocking.
// It will return ErrLocked if any lock is already held on the file/directory.
func TryExclusiveLock(path string, lockType LockType) (*FileLock, error) {
	l, err := NewLock(path, lockType)
	if err != nil {
		return nil, err
	}
	err = l.TryExclusiveLock()
	if err != nil {
		return nil, err
	}
	return l, err
}

// ExclusiveLock takes an exclusive lock.
// This is idempotent when the Lock already represents an exclusive lock,
// and promotes a shared lock to exclusive atomically.
// It will block if an exclusive lock is already held.
func (l *FileLock) ExclusiveLock() error {
	var ol syscall.Overlapped
	err := lockFileEx(l.fd, LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
	return err
}

// ExclusiveLock takes an exclusive lock on a file/directory.
// It will block if an exclusive lock is already held on the file/directory.
func ExclusiveLock(path string, lockType LockType) (*FileLock, error) {
	l, err := NewLock(path, lockType)
	if err == nil {
		err = l.ExclusiveLock()
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

// TrySharedLock takes a co-operative (shared) lock without blocking.
// This is idempotent when the Lock already represents a shared lock,
// and tries demote an exclusive lock to shared atomically.
// It will return ErrLocked if an exclusive lock already exists.
func (l *FileLock) TrySharedLock() error {
	var ol syscall.Overlapped
	err := lockFileEx(l.fd, LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol); if err != nil {
		err = ErrLocked
	}
	return err
}

// TrySharedLock takes a co-operative (shared) lock on a file/directory without blocking.
// It will return ErrLocked if an exclusive lock already exists on the file/directory.
func TrySharedLock(path string, lockType LockType) (*FileLock, error) {
	l, err := NewLock(path, lockType)
	if err != nil {
		return nil, err
	}
	err = l.TrySharedLock()
	if err != nil {
		return nil, err
	}
	return l, nil
}

// SharedLock takes a co-operative (shared) lock on.
// This is idempotent when the Lock already represents a shared lock,
// and demotes an exclusive lock to shared atomically.
// It will block if an exclusive lock is already held.
func (l *FileLock) SharedLock() error {
	var ol syscall.Overlapped
	err := lockFileEx(l.fd, 0, 0, 1, 0, &ol); if err != nil {
		err = ErrLocked
	}
	return err
}

// SharedLock takes a co-operative (shared) lock on a file/directory.
// It will block if an exclusive lock is already held on the file/directory.
func SharedLock(path string, lockType LockType) (*FileLock, error) {
	l, err := NewLock(path, lockType)
	if err != nil {
		return nil, err
	}
	err = l.SharedLock()
	if err != nil {
		return nil, err
	}
	return l, nil
}

// Unlock unlocks the lock
func (l *FileLock) Unlock() error {
	var ol syscall.Overlapped
	if err := unlockFileEx(l.fd, 0, 1, 0, &ol); err != nil {
		return err
	}
	return nil
}

// Fd returns the lock's file descriptor, or an error if the lock is closed
func (l *FileLock) Fd() (int, error) {
	var err error
	if l.fd == INVALID_FILE_HANDLE {
		err = errors.New("lock closed")
	}
	return l.fd, err
}

// Close closes the lock which implicitly unlocks it as well
func (l *FileLock) Close() error {
	fd := l.fd
	l.fd = -1
	return syscall.Close(fd)
}

// NewLock opens a new lock on a file without acquisition
func NewLock(path string, lockType LockType) (*FileLock, error) {
	l := &FileLock{path: path, fd: -1}

	mode := syscall.O_RDONLY
	if lockType == Dir {
		mode |= syscall.O_DIRECTORY
	}
	lfd, err := syscall.Open(l.path, mode, 0)
	if err != nil {
		if err == syscall.ENOENT {
			err = ErrNotExist
		} else if err == syscall.EACCES {
			err = ErrPermission
		}
		return nil, err
	}
	l.fd = lfd

	return l, nil
}
