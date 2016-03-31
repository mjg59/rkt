// Copyright 2015 The rkt Authors
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

package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/coreos/rkt/pkg/uid"
	"github.com/hashicorp/errwrap"
)

const (
	FILE_NAME_NORMALIZED = 0x0
	FILE_NAME_OPEND      = 0x8

	VOLUME_NAME_DOS  = 0x0
	VOLUME_NAME_GUID = 0x1
	VOLUME_NAME_NONE = 0x4
	VOLUME_NAME_NT   = 0x2
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	procGetFinalPathNameByHandleW = modkernel32.NewProc("GetFinalPathNameByHandleW")
)

var ErrNotSupportedPlatform = errors.New("function not supported on this platform")

func getFinalPathNameByHandle(handle syscall.Handle, path *uint16, pathLen uint32, flag uint32) (n uint32, err error) {
	r0, _, e1 := syscall.Syscall6(procGetFinalPathNameByHandleW.Addr(), 4,
		uintptr(handle), uintptr(unsafe.Pointer(path)), uintptr(pathLen), uintptr(flag), 0, 0)
	n = uint32(r0)
	if n == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func os_CopyTree(src, dest string, uidRange *uid.UidRange) error {
	cleanSrc := filepath.Clean(src)
	copyWalker := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rootLess := path[len(cleanSrc):]
		target := filepath.Join(dest, rootLess)
		mode := info.Mode()
		switch {
		case mode.IsDir():
			err := os.Mkdir(target, mode.Perm())
			if err != nil {
				return err
			}

			dir, err := os.Open(target)
			if err != nil {
				return err
			}
			if err := dir.Chmod(mode); err != nil {
				dir.Close()
				return err
			}
			dir.Close()
		case mode.IsRegular():
			if err := CopyRegularFile(path, target); err != nil {
				return err
			}
                default:
			return fmt.Errorf("unsupported mode: %v", mode)
		}

		return nil
	}

        if err := filepath.Walk(cleanSrc, copyWalker); err != nil {
		return err
	}

	return nil
}

// DirSize takes a path and returns its size in bytes
func DirSize(path string) (int64, error) {
	if _, err := os.Stat(path); err == nil {
		var sz int64
		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			sz += info.Size()
			return err
		})
		return sz, err
	}

	return 0, nil
}

func hasHardLinks(fi os.FileInfo) bool {
	return false
}

func getInode(fi os.FileInfo) uint64 {
	return 0
}

// These functions are from github.com/docker/docker/pkg/system

func LUtimesNano(path string, ts []syscall.Timespec) error {
	return ErrNotSupportedPlatform
}

func Lgetxattr(path string, attr string) ([]byte, error) {
	return nil, ErrNotSupportedPlatform
}

func Lsetxattr(path string, attr string, data []byte, flags int) error {
	return ErrNotSupportedPlatform
}

func Umask(umask int) int {
	return 0
}

func Mknod(path string, mode uint32, dev int) (err error) {
	return nil
}

func Mkfifo(path string, mode uint32) (err error) {
	return nil
}

func SetRoot(dir string) error {
	if err := syscall.Chdir(dir); err != nil {
		return errwrap.Wrap(errors.New("failed to chdir"), err)
	}
	return nil
}

func Openat(fd interface{}, filename string, mode int, flags uint32) (syscall.Handle, error) {
	pathLen := uint32(syscall.MAX_PATH)
	path := make([]uint16, pathLen)
	_, err := getFinalPathNameByHandle(fd.(syscall.Handle), &path[0], pathLen,
		FILE_NAME_NORMALIZED|VOLUME_NAME_DOS)
	if err != nil {
		return 0, err
	}

	dirpath := syscall.UTF16ToString(path)

	newpath := filepath.Join(dirpath, filename)
	return syscall.Open(newpath, mode, flags)
}
