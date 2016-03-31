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
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/coreos/rkt/pkg/uid"
)

func CopyRegularFile(src, dest string) (err error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		e := destFile.Close()
		if err == nil {
			err = e
		}
	}()
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}
	return nil
}

func CopySymlink(src, dest string) error {
	symTarget, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.Symlink(symTarget, dest); err != nil {
		return err
	}
	return nil
}

func CopyTree(src, dest string, uidRange *uid.UidRange) error {
	return os_CopyTree(src, dest, uidRange)
}

// TODO(sgotti) use UTIMES_OMIT on linux if Time.IsZero ?
func TimeToTimespec(time time.Time) (ts syscall.Timespec) {
	nsec := int64(0)
	if !time.IsZero() {
		nsec = time.UnixNano()
	}
	return syscall.NsecToTimespec(nsec)
}

