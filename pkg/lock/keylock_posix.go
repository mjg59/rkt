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

// +build linux freebsd netbsd openbsd darwin

package lock

import (
	"syscall"
)

func compareFiles(lfd int, fd int) (bool, error) {
	var lockStat, curStat syscall.Stat_t

	err := syscall.Fstat(lfd, &lockStat)
	if err != nil {
		return false, err
	}
	err = syscall.Fstat(fd, &curStat)
	if err != nil {
		return false, err
	}
	if lockStat.Ino == curStat.Ino && lockStat.Dev == curStat.Dev {
		return true, nil
	}
	return false, nil
}
	
