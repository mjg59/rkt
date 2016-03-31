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
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/coreos/rkt/pkg/uid"
)

func os_CopyTree(src, dest string, uidRange *uid.UidRange) error {
	cleanSrc := filepath.Clean(src)
	dirs := make(map[string][]syscall.Timespec)
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
		if mode.IsDir() {
			dirs[target] = ts
		}

		return nil
	}

        if err := filepath.Walk(cleanSrc, copyWalker); err != nil {
		return err
	}

	return nil
}
