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

//+build linux

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coreos/rkt/Godeps/_workspace/src/github.com/appc/spec/schema"
	"github.com/coreos/rkt/Godeps/_workspace/src/github.com/appc/spec/schema/types"
	"github.com/coreos/rkt/Godeps/_workspace/src/github.com/coreos/go-systemd/unit"
	"github.com/coreos/rkt/common"
	"github.com/coreos/rkt/common/cgroup"
	"github.com/coreos/rkt/pkg/label"
	"github.com/coreos/rkt/pkg/selinux"
	"github.com/coreos/rkt/pkg/uid"
	"github.com/coreos/rkt/stage1/init/kvm"
)

// Pod encapsulates a PodManifest and ImageManifests
type Pod struct {
	Root               string // root directory where the pod will be located
	UUID               types.UUID
	Manifest           *schema.PodManifest
	Images             map[string]*schema.ImageManifest
	MetadataServiceURL string
	Networks           []string
}

const (
	// Name of the file storing the pod's flavor
	flavorFile = "flavor"
)

var (
	defaultEnv = map[string]string{
		"PATH":    "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"SHELL":   "/bin/sh",
		"USER":    "root",
		"LOGNAME": "root",
		"HOME":    "/root",
	}
)

// LoadPod loads a Pod Manifest (as prepared by stage0) and
// its associated Application Manifests, under $root/stage1/opt/stage1/$apphash
func LoadPod(root string, uuid *types.UUID) (*Pod, error) {
	p := &Pod{
		Root:   root,
		UUID:   *uuid,
		Images: make(map[string]*schema.ImageManifest),
	}

	buf, err := ioutil.ReadFile(common.PodManifestPath(p.Root))
	if err != nil {
		return nil, fmt.Errorf("failed reading pod manifest: %v", err)
	}

	pm := &schema.PodManifest{}
	if err := json.Unmarshal(buf, pm); err != nil {
		return nil, fmt.Errorf("failed unmarshalling pod manifest: %v", err)
	}
	p.Manifest = pm

	for i, app := range p.Manifest.Apps {
		ampath := common.ImageManifestPath(p.Root, app.Name)
		buf, err := ioutil.ReadFile(ampath)
		if err != nil {
			return nil, fmt.Errorf("failed reading app manifest %q: %v", ampath, err)
		}

		am := &schema.ImageManifest{}
		if err = json.Unmarshal(buf, am); err != nil {
			return nil, fmt.Errorf("failed unmarshalling app manifest %q: %v", ampath, err)
		}

		if _, ok := p.Images[app.Name.String()]; ok {
			return nil, fmt.Errorf("got multiple definitions for app: %v", app.Name)
		}
		if app.App == nil {
			p.Manifest.Apps[i].App = am.App
		}
		p.Images[app.Name.String()] = am
	}

	return p, nil
}

// quoteExec returns an array of quoted strings appropriate for systemd execStart usage
func quoteExec(exec []string) string {
	if len(exec) == 0 {
		// existing callers prefix {"/diagexec", "/app/root", "/work/dir", "/env/file"} so this shouldn't occur.
		panic("empty exec")
	}

	var qexec []string
	qexec = append(qexec, exec[0])
	// FIXME(vc): systemd gets angry if qexec[0] is quoted
	// https://bugs.freedesktop.org/show_bug.cgi?id=86171

	if len(exec) > 1 {
		for _, arg := range exec[1:] {
			escArg := strings.Replace(arg, `\`, `\\`, -1)
			escArg = strings.Replace(escArg, `"`, `\"`, -1)
			escArg = strings.Replace(escArg, `'`, `\'`, -1)
			escArg = strings.Replace(escArg, `$`, `$$`, -1)
			qexec = append(qexec, `"`+escArg+`"`)
		}
	}

	return strings.Join(qexec, " ")
}

func (p *Pod) WritePrepareAppTemplate() error {
	opts := []*unit.UnitOption{
		unit.NewUnitOption("Unit", "Description", "Prepare minimum environment for chrooted applications"),
		unit.NewUnitOption("Unit", "DefaultDependencies", "false"),
		unit.NewUnitOption("Unit", "OnFailureJobMode", "fail"),
		unit.NewUnitOption("Unit", "Requires", "systemd-journald.service"),
		unit.NewUnitOption("Unit", "After", "systemd-journald.service"),
		unit.NewUnitOption("Service", "Type", "oneshot"),
		unit.NewUnitOption("Service", "Restart", "no"),
		unit.NewUnitOption("Service", "ExecStart", "/prepare-app %I"),
		unit.NewUnitOption("Service", "User", "0"),
		unit.NewUnitOption("Service", "Group", "0"),
		unit.NewUnitOption("Service", "CapabilityBoundingSet", "CAP_SYS_ADMIN CAP_DAC_OVERRIDE"),
	}

	unitsPath := filepath.Join(common.Stage1RootfsPath(p.Root), unitsDir)
	file, err := os.OpenFile(filepath.Join(unitsPath, "prepare-app@.service"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to create service unit file: %v", err)
	}
	defer file.Close()

	if _, err = io.Copy(file, unit.Serialize(opts)); err != nil {
		return fmt.Errorf("failed to write service unit file: %v", err)
	}

	return nil
}

// appToSystemd transforms the provided RuntimeApp+ImageManifest into systemd units
func (p *Pod) appToSystemd(ra *schema.RuntimeApp, interactive bool, flavor string, privateUsers string) error {
	app := ra.App
	appName := ra.Name
	image, ok := p.Images[appName.String()]
	if !ok {
		// This is impossible as we have updated the map in LoadPod().
		panic(fmt.Sprintf("No images for app %q", ra.Name.String()))
	}
	imgName := image.Name

	workDir := "/"
	if app.WorkingDirectory != "" {
		workDir = app.WorkingDirectory
	}

	env := app.Environment

	env.Set("AC_APP_NAME", appName.String())
	env.Set("AC_METADATA_URL", p.MetadataServiceURL)

	if err := p.writeEnvFile(env, appName, privateUsers); err != nil {
		return fmt.Errorf("unable to write environment file: %v", err)
	}

	// This is a partial implementation for app.User and app.Group:
	// For now, only numeric ids (and the string "root") are supported.
	var uid, gid int
	var err error
	if app.User == "root" {
		uid = 0
	} else {
		uid, err = strconv.Atoi(app.User)
		if err != nil {
			return fmt.Errorf("non-numerical user id not supported yet")
		}
	}
	if app.Group == "root" {
		gid = 0
	} else {
		gid, err = strconv.Atoi(app.Group)
		if err != nil {
			return fmt.Errorf("non-numerical group id not supported yet")
		}
	}

	execWrap := []string{"/diagexec", common.RelAppRootfsPath(appName), workDir, RelEnvFilePath(appName), strconv.Itoa(uid), strconv.Itoa(gid)}
	execStart := quoteExec(append(execWrap, app.Exec...))
	opts := []*unit.UnitOption{
		unit.NewUnitOption("Unit", "Description", fmt.Sprintf("Application=%v Image=%v", appName, imgName)),
		unit.NewUnitOption("Unit", "DefaultDependencies", "false"),
		unit.NewUnitOption("Unit", "OnFailure", "reaper.service"),
		unit.NewUnitOption("Unit", "Wants", "exit-watcher.service"),
		unit.NewUnitOption("Service", "Restart", "no"),
		unit.NewUnitOption("Service", "ExecStart", execStart),
		unit.NewUnitOption("Service", "User", "0"),
		unit.NewUnitOption("Service", "Group", "0"),
	}

	if interactive {
		opts = append(opts, unit.NewUnitOption("Service", "StandardInput", "tty"))
		opts = append(opts, unit.NewUnitOption("Service", "StandardOutput", "tty"))
		opts = append(opts, unit.NewUnitOption("Service", "StandardError", "tty"))
	} else {
		opts = append(opts, unit.NewUnitOption("Service", "StandardOutput", "journal+console"))
		opts = append(opts, unit.NewUnitOption("Service", "StandardError", "journal+console"))
		opts = append(opts, unit.NewUnitOption("Service", "SyslogIdentifier", filepath.Base(app.Exec[0])))
	}

	for _, eh := range app.EventHandlers {
		var typ string
		switch eh.Name {
		case "pre-start":
			typ = "ExecStartPre"
		case "post-stop":
			typ = "ExecStopPost"
		default:
			return fmt.Errorf("unrecognized eventHandler: %v", eh.Name)
		}
		exec := quoteExec(append(execWrap, eh.Exec...))
		opts = append(opts, unit.NewUnitOption("Service", typ, exec))
	}

	saPorts := []types.Port{}
	for _, p := range app.Ports {
		if p.SocketActivated {
			saPorts = append(saPorts, p)
		}
	}

	for _, i := range app.Isolators {
		switch v := i.Value().(type) {
		case *types.ResourceMemory:
			opts, err = cgroup.MaybeAddIsolator(opts, "memory", v.Limit())
			if err != nil {
				return err
			}
		case *types.ResourceCPU:
			opts, err = cgroup.MaybeAddIsolator(opts, "cpu", v.Limit())
			if err != nil {
				return err
			}
		}
	}

	if len(saPorts) > 0 {
		sockopts := []*unit.UnitOption{
			unit.NewUnitOption("Unit", "Description", fmt.Sprintf("Application=%v Image=%v %s", appName, imgName, "socket-activated ports")),
			unit.NewUnitOption("Unit", "DefaultDependencies", "false"),
			unit.NewUnitOption("Socket", "BindIPv6Only", "both"),
			unit.NewUnitOption("Socket", "Service", ServiceUnitName(appName)),
		}

		for _, sap := range saPorts {
			var proto string
			switch sap.Protocol {
			case "tcp":
				proto = "ListenStream"
			case "udp":
				proto = "ListenDatagram"
			default:
				return fmt.Errorf("unrecognized protocol: %v", sap.Protocol)
			}
			sockopts = append(sockopts, unit.NewUnitOption("Socket", proto, fmt.Sprintf("%v", sap.Port)))
		}

		file, err := os.OpenFile(SocketUnitPath(p.Root, appName), os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("failed to create socket file: %v", err)
		}
		defer file.Close()

		if _, err = io.Copy(file, unit.Serialize(sockopts)); err != nil {
			return fmt.Errorf("failed to write socket unit file: %v", err)
		}

		if err = os.Symlink(path.Join("..", SocketUnitName(appName)), SocketWantPath(p.Root, appName)); err != nil {
			return fmt.Errorf("failed to link socket want: %v", err)
		}

		opts = append(opts, unit.NewUnitOption("Unit", "Requires", SocketUnitName(appName)))
	}

	opts = append(opts, unit.NewUnitOption("Unit", "Requires", InstantiatedPrepareAppUnitName(appName)))
	opts = append(opts, unit.NewUnitOption("Unit", "After", InstantiatedPrepareAppUnitName(appName)))

	file, err := os.OpenFile(ServiceUnitPath(p.Root, appName), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to create service unit file: %v", err)
	}
	defer file.Close()

	if _, err = io.Copy(file, unit.Serialize(opts)); err != nil {
		return fmt.Errorf("failed to write service unit file: %v", err)
	}

	if err = os.Symlink(path.Join("..", ServiceUnitName(appName)), ServiceWantPath(p.Root, appName)); err != nil {
		return fmt.Errorf("failed to link service want: %v", err)
	}

	if flavor == "kvm" {
		// bind mount all shared volumes from /mnt/volumeName (we don't use mechanism for bind-mounting given by nspawn)
		err := kvm.AppToSystemdMountUnits(common.Stage1RootfsPath(p.Root), appName, app.MountPoints, unitsDir)
		if err != nil {
			return fmt.Errorf("failed to prepare mount units: %v", err)
		}

	}

	return nil
}

// writeEnvFile creates an environment file for given app name, the minimum
// required environment variables by the appc spec will be set to sensible
// defaults here if they're not provided by env.
func (p *Pod) writeEnvFile(env types.Environment, appName types.ACName, privateUsers string) error {
	ef := bytes.Buffer{}

	for dk, dv := range defaultEnv {
		if _, exists := env.Get(dk); !exists {
			fmt.Fprintf(&ef, "%s=%s\000", dk, dv)
		}
	}

	for _, e := range env {
		fmt.Fprintf(&ef, "%s=%s\000", e.Name, e.Value)
	}

	uidRange := uid.NewBlankUidRange()
	if err := uidRange.Deserialize([]byte(privateUsers)); err != nil {
		return err
	}

	envFilePath := EnvFilePath(p.Root, appName)
	if err := ioutil.WriteFile(envFilePath, ef.Bytes(), 0644); err != nil {
		return err
	}

	if uidRange.Shift != 0 && uidRange.Count != 0 {
		if err := os.Chown(envFilePath, int(uidRange.Shift), int(uidRange.Shift)); err != nil {
			return err
		}
	}

	return nil
}

// PodToSystemd creates the appropriate systemd service unit files for
// all the constituent apps of the Pod
func (p *Pod) PodToSystemd(interactive bool, flavor string, privateUsers string) error {

	if flavor == "kvm" {
		// prepare all applications names to become dependency for mount units
		// all host-shared folder has to become available before applications starts
		appNames := []types.ACName{}
		for _, runtimeApp := range p.Manifest.Apps {
			appNames = append(appNames, runtimeApp.Name)
		}

		// mount host volumes through some remote file system e.g. 9p to /mnt/volumeName location
		// order is important here: podToSystemHostMountUnits prepares folders that are checked by each appToSystemdMountUnits later
		err := kvm.PodToSystemdHostMountUnits(common.Stage1RootfsPath(p.Root), p.Manifest.Volumes, appNames, unitsDir)
		if err != nil {
			return fmt.Errorf("failed to transform pod volumes into mount units: %v", err)
		}
	}

	for i := range p.Manifest.Apps {
		ra := &p.Manifest.Apps[i]
		if err := p.appToSystemd(ra, interactive, flavor, privateUsers); err != nil {
			return fmt.Errorf("failed to transform app %q into systemd service: %v", ra.Name, err)
		}
	}
	return nil
}

// appToNspawnArgs transforms the given app manifest, with the given associated
// app name, into a subset of applicable systemd-nspawn argument
func (p *Pod) appToNspawnArgs(ra *schema.RuntimeApp) ([]string, error) {
	args := []string{}
	appName := ra.Name
	id := ra.Image.ID
	app := ra.App

	vols := make(map[types.ACName]types.Volume)

	// Bind mounts should be mounted as a generic svirt target without any unique
	// per-container context in order to allow their contents to be shared - use
	// the specific context but strip the level down to s0
	mountlabel := os.Getenv(common.SELinuxLabel)
	if mountlabel != "" {
		c := selinux.NewContext(mountlabel)
		c["level"] = "s0"
		mountlabel = c.Get()
	}

	// TODO(philips): this is implicitly creating a mapping from MountPoint
	// to volumes. This is a nice convenience for users but we will need to
	// introduce a --mount flag so they can control which mountPoint maps to
	// which volume.

	for _, v := range p.Manifest.Volumes {
		vols[v.Name] = v
	}

	for _, mp := range app.MountPoints {
		key := mp.Name
		vol, ok := vols[key]
		if !ok {
			catCmd := fmt.Sprintf("sudo rkt image cat-manifest --pretty-print %v", id)
			volumeCmd := ""
			for _, mp := range app.MountPoints {
				volumeCmd += fmt.Sprintf("--volume %s,kind=host,source=/some/path ", mp.Name)
			}

			return nil, fmt.Errorf("no volume for mountpoint %q in app %q.\n"+
				"You can inspect the volumes with:\n\t%v\n"+
				"App %q requires the following volumes:\n\t%v",
				key, appName, catCmd, appName, volumeCmd)
		}
		opt := make([]string, 4)

		// If the readonly flag in the pod manifest is not nil,
		// then use it to override the readonly flag in the image manifest.
		readOnly := mp.ReadOnly
		if vol.ReadOnly != nil {
			readOnly = *vol.ReadOnly
		}

		// Verify whether the mount already has the correct label, and if not
		// relabel it
		if mountlabel != "" {
			currentlabel, err := selinux.Getfilecon(vol.Source)
			if err != nil {
				return nil, fmt.Errorf("Unable to validate SELinux label for %q: %s", vol.Source, err.Error)
			}
			if currentlabel != mountlabel {
				err = label.Relabel(vol.Source, mountlabel, "z")
				if err != nil {
					return nil, fmt.Errorf("Unable to relabel %q: %s", vol.Source, err.Error)
				}
			}
		}

		if readOnly {
			opt[0] = "--bind-ro="
		} else {
			opt[0] = "--bind="
		}

		opt[1] = vol.Source
		opt[2] = ":"
		opt[3] = filepath.Join(common.RelAppRootfsPath(appName), mp.Path)

		args = append(args, strings.Join(opt, ""))
	}

	for _, i := range app.Isolators {
		switch v := i.Value().(type) {
		case types.LinuxCapabilitiesSet:
			var caps []string
			// TODO: cleanup the API on LinuxCapabilitiesSet to give strings easily.
			for _, c := range v.Set() {
				caps = append(caps, string(c))
			}
			if i.Name == types.LinuxCapabilitiesRetainSetName {
				capList := strings.Join(caps, ",")
				args = append(args, "--capability="+capList)
			}
		}
	}

	return args, nil
}

// PodToNspawnArgs renders a prepared Pod as a systemd-nspawn
// argument list ready to be executed
func (p *Pod) PodToNspawnArgs() ([]string, error) {
	args := []string{
		"--uuid=" + p.UUID.String(),
		"--machine=" + p.GetMachineID(),
		"--directory=" + common.Stage1RootfsPath(p.Root),
	}

	for i := range p.Manifest.Apps {
		aa, err := p.appToNspawnArgs(&p.Manifest.Apps[i])
		if err != nil {
			return nil, err
		}
		args = append(args, aa...)
	}

	return args, nil
}

func (p *Pod) getFlavor() (flavor string, systemdVersion string, err error) {
	flavor, err = os.Readlink(filepath.Join(common.Stage1RootfsPath(p.Root), "flavor"))
	if err != nil {
		return "", "", fmt.Errorf("unable to determine stage1 flavor: %v", err)
	}

	if flavor == "host" {
		// This flavor does not contain systemd, so don't return systemdVersion
		return flavor, "", nil
	}

	systemdVersionBytes, err := ioutil.ReadFile(filepath.Join(common.Stage1RootfsPath(p.Root), "systemd-version"))
	if err != nil {
		return "", "", fmt.Errorf("unable to determine stage1's systemd version: %v", err)
	}
	systemdVersion = strings.Trim(string(systemdVersionBytes), " \n")
	return flavor, systemdVersion, nil
}

// GetAppHashes returns a list of hashes of the apps in this pod
func (p *Pod) GetAppHashes() []types.Hash {
	var names []types.Hash
	for _, a := range p.Manifest.Apps {
		names = append(names, a.Image.ID)
	}

	return names
}

// GetMachineID returns the machine id string of the pod to be passed to
// systemd-nspawn
func (p *Pod) GetMachineID() string {
	return "rkt-" + p.UUID.String()
}
