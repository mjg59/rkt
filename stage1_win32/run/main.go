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

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/appc/spec/schema"
	"github.com/appc/spec/schema/types"
	"github.com/hashicorp/errwrap"
	"github.com/Microsoft/hcsshim"

	"github.com/coreos/rkt/common"
	rktlog "github.com/coreos/rkt/pkg/log"
	"github.com/coreos/rkt/pkg/sys"
	stage1common "github.com/coreos/rkt/stage1/common"
	stage1commontypes "github.com/coreos/rkt/stage1/common/types"
)

const (
	flavor = "fly"
)

type flyMount struct {
	HostPath         string
	TargetPrefixPath string
	RelTargetPath    string
	Fs               string
	Flags            uintptr
}

type volumeMountTuple struct {
	V types.Volume
	M schema.Mount
}

var (
	debug bool

	discardNetlist common.NetList
	discardBool    bool
	discardString  string

	log  *rktlog.Logger
	diag *rktlog.Logger
)

func init() {
	flag.BoolVar(&debug, "debug", false, "Run in debug mode")

	// The following flags need to be supported by stage1 according to
	// https://github.com/coreos/rkt/blob/master/Documentation/devel/stage1-implementors-guide.md
	// TODO: either implement functionality or give not implemented warnings
	flag.Var(&discardNetlist, "net", "Setup networking")
	flag.BoolVar(&discardBool, "interactive", true, "The pod is interactive")
	flag.StringVar(&discardString, "mds-token", "", "MDS auth token")
	flag.StringVar(&discardString, "local-config", common.DefaultLocalConfigDir, "Local config path")
}

func stage1() int {
	uuid, err := types.NewUUID(flag.Arg(0))
	if err != nil {
		log.Print("UUID is missing or malformed\n")
		return 1
	}

	root := "."
	p, err := stage1commontypes.LoadPod(root, uuid)
	if err != nil {
		log.PrintE("can't load pod", err)
		return 1
	}

	// Sanity checks
	if len(p.Manifest.Apps) != 1 {
		log.Printf("flavor %q only supports 1 application per Pod for now", flavor)
		return 1
	}

	ra := p.Manifest.Apps[0]

	imgName := p.AppNameToImageName(ra.Name)
	args := ra.App.Exec
	if len(args) == 0 {
		log.Printf(`image %q has an empty "exec" (try --exec=BINARY)`, imgName)
		return 1
	}

	workDir := "/"
	if ra.App.WorkingDirectory != "" {
		workDir = ra.App.WorkingDirectory
	}

	env := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	for _, e := range ra.App.Environment {
		env = append(env, e.Name+"="+e.Value)
	}

	rfs := filepath.Join(common.AppPath(p.Root, ra.Name), "rootfs")

	if err = stage1common.WritePpid(os.Getpid()); err != nil {
		log.Error(err)
		return 4
	}

	if err = hcsshim.StartComputeSytem(uuid); err != nil {
		log.PrintE(fmt.Sprintf("Failed to start compute system: %v", err))
		return 1
	}

	createProcessParams := hcsshim.CreateProcessParams{
		EmulateConsole: true,
		WorkingDirectory: rfs,
		ConsoleSize: "80x24",
		CommandLine: args,
	}

	pid, iopipe.Stdin, stdout, stderr, err := hcsshim.CreateProcessInComputeSystem(uuid, true, true, true, createProcessParams)

	if err != nil {
		log.PrintE(fmt.Sprintf("can't execute %q", args[0]), err)
		hcsshim.TerminateComputeSystem(uuid, hcsshim.TimeoutInfinite, "CreateProcessInComputeSystem failed");		
		return 7
	}

	exitcode, err := hcsshim.WaitForProcessInComputeSystem(uuid, pid, hcsshim.TimeoutInfinite)
	
	return 0
}

func main() {
	flag.Parse()

	log, diag, _ = rktlog.NewLogSet("run", debug)
	if !debug {
		diag.SetOutput(ioutil.Discard)
	}

	// move code into stage1() helper so defered fns get run
	os.Exit(stage1())
}
