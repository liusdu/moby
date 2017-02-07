package daemon

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/container"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/volume"
	containertypes "github.com/docker/engine-api/types/container"
)

func (d *Daemon) mergeEnv(el []string, em map[string]string) []string {
	fMap := envListToMap(el)
	envMergeInMap(fMap, em)

	fEnvs := envMapToList(fMap)

	return fEnvs
}

func envMergeInMap(eMap, nEnv map[string]string) {
	for key, value := range nEnv {
		if v, ok := eMap[key]; ok {
			// env exist in map
			r := envCat(v, value)
			eMap[key] = r
		} else {
			eMap[key] = value
		}
	}
}

// cat former environment value with the new one
func envCat(former, newstr string) string {
	if former == "" {
		return newstr
	}

	if newstr == "" {
		return former
	}

	fspliter := getSpliter(former)
	nspliter := getSpliter(newstr)
	/*
		if fspliter != nspliter {
			// different spliter, not sure if this is right, just cat them
			return former + fspliter + newstr
		}
	*/

	// if newstr exist, do not suffix it to former
	fmap := make(map[string]string)
	fvs := strings.Split(former, fspliter)
	for _, value := range fvs {
		fmap[value] = value
	}

	nvs := strings.Split(newstr, nspliter)
	for _, value := range nvs {
		if _, ok := fmap[value]; !ok {
			former = former + fspliter + value
		}
	}

	return former
}

func getSpliter(s string) string {
	// first get the spliter, if not found, use ":" as default
	var spliter string
	if strings.Contains(s, ",") {
		spliter = ","
	} else if strings.Contains(s, ";") {
		spliter = ";"
	} else {
		spliter = ":"
	}
	return spliter
}

func envMapToList(envMap map[string]string) []string {
	var slist []string

	for k, v := range envMap {
		if v == "" {
			slist = append(slist, k)
		} else {
			env := fmt.Sprintf("%s=%s", k, v)
			slist = append(slist, env)
		}
	}

	return slist
}

func envListToMap(l []string) map[string]string {
	eMap := make(map[string]string)
	for _, env := range l {
		sRlt := strings.SplitN(env, "=", 2)

		key := sRlt[0]
		if len(sRlt) == 1 {
			eMap[key] = ""
		} else {
			eMap[key] = sRlt[1]
		}
	}

	return eMap
}

func mergeAccelEnv(oEnv, envs map[string]string) map[string]string {
	for k, v := range envs {
		if vbak, ok := oEnv[k]; ok {
			// just combine the older one and new one
			// not trying to clear the redundant
			newV := envCat(vbak, v)
			oEnv[k] = newV
		} else {
			oEnv[k] = v
		}
	}
	return oEnv
}

func mergeAccelDevice(dMap map[string]string, devices []string) (map[string]string, error) {
	for _, dev := range devices {
		if srcBak, ok := dMap[dev]; ok {
			if dev != srcBak {
				logrus.Errorf("Accelerator Device Merging: trying to map different source device into same target device: %s & %s", dev, srcBak)
				return nil, fmt.Errorf("device %s conflict", dev)
			}
		} else {
			dMap[dev] = dev
		}
	}

	return dMap, nil
}

func removeMergedMounts(rootPath string) error {
	mmPath := filepath.Join(rootPath, "accelerators")

	fs, _ := ioutil.ReadDir(mmPath)
	for _, f := range fs {
		if f.IsDir() {
			// try umount directory directly, ignore the error.
			if err := syscall.Unmount(filepath.Join(mmPath, f.Name()), 0); err != nil {
				logrus.Debugf("try Unmount accelerator merged mounts directory failed, try to remove directly")
			} else {
				logrus.Debugf("Unmount accelerator merged mounts directory done")
			}
			if err := os.RemoveAll(filepath.Join(mmPath, f.Name())); err != nil {
				return err
			}
		} else {
			logrus.Errorf("**Should not be here**:How come a regular file exist in containre accelerators directory")
		}
	}
	return nil
}

// CheckDeviceMapping turn AccelDevices into HostConfig.Devices
// will check devices conflict again
func getAccelDevices(devices map[string]string) []containertypes.DeviceMapping {
	var cDevs []containertypes.DeviceMapping

	for dest, hostPath := range devices {
		cDevs = append(cDevs, containertypes.DeviceMapping{
			PathOnHost:        hostPath,
			PathInContainer:   dest,
			CgroupPermissions: "rwm",
		})
	}

	return cDevs
}

func accelMounts(amm map[string]containertypes.AccelMount) []container.Mount {
	var mounts []container.Mount
	for dest, am := range amm {
		mounts = append(mounts, container.Mount{
			Source:      am.Source,
			Destination: dest,
			Writable:    am.RW,
			Propagation: am.Propagation,
		})
	}
	return mounts
}

func parseAccelMount(m driverapi.Mount) (containertypes.AccelMount, error) {
	// need a fakeVolume to avoid ":" in volume path
	var fakeVolume string
	cover := false
	cs := strings.Split(m.Mode, ",")
	for i, c := range cs {
		if c == "cv" {
			cover = true
			cs[i] = ""
		}
	}

	newMode := ""
	for _, c := range cs {
		if c != "" {
			if newMode == "" {
				newMode = c
			} else {
				newMode = newMode + "," + c
			}
		}
	}

	if newMode != "" {
		fakeVolume = "/fake_src:/fake_dest:" + newMode
	} else {
		// maybe even mode is emtpy, we need to ParseMountSpec to check our volume path
		fakeVolume = "/fake_src:/fake_dest"
	}

	mp, err := volume.ParseMountSpec(fakeVolume, "")
	if err != nil {
		return containertypes.AccelMount{}, err
	}

	am := containertypes.AccelMount{
		Cover:       cover,
		Source:      m.Source,
		Destination: m.Destination,
		RW:          mp.RW,
		Propagation: string(mp.Propagation),
		Mode:        mp.Mode,
	}

	return am, nil
}

func mergeAccelMount(mMap map[string]containertypes.AccelMount, ms []driverapi.Mount, rootPath string) (map[string]containertypes.AccelMount, error) {
	for _, m := range ms {
		am, err := parseAccelMount(m)
		if err != nil {
			return nil, err
		}
		if bakAm, ok := mMap[am.Destination]; ok { // mount info may conflict
			// TODO: check volume Mode conflict
			if am.Cover != bakAm.Cover {
				logrus.Errorf("accel mount(%s) need to be merged but have different cover mode", am.Destination)
				return nil, fmt.Errorf("failed to merge accel mount(%s) because of different cover mode", am.Destination)
			}
			if bakAm.RW != am.RW ||
				bakAm.Mode != am.Mode ||
				bakAm.Propagation != am.Propagation {
				logrus.Errorf("accel mount(%s) need to be merged but have different mount mode, please check", am.Destination)
				return nil, fmt.Errorf("failed to merge accel mount(%s) because of different mount mode", am.Destination)
			}

			if bakAm.Source != am.Source {
				// mount info conflict, dest corresponds to different src paths
				if am.Cover {
					break
				}
				logrus.Warnf("new Accelerator Mount conflict, with dest path %s, and %s, %s as source path", am.Destination, bakAm.Source, am.Source)
				newPath, err := mergeAccelPath(bakAm.Source, am.Source, am.Destination, rootPath)
				if err != nil {
					logrus.Errorf("merge Accelerator Mount %s failed", am.Destination)
					return nil, fmt.Errorf("Merge accelerator mount info %s conflict: %v", am.Destination, err)
				}
				am.Source = newPath
				mMap[am.Destination] = am
			}
		} else { // append a new mount information
			mMap[am.Destination] = am
		}
	}

	return mMap, nil
}

func getRealPath(path string) (string, error) {
	aPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	rPath, err := filepath.Abs(aPath)
	if err != nil {
		return "", err
	}

	return rPath, nil
}

func mergeAccelPath(src, p, dest, rootPath string) (string, error) {
	// incase they are links to the same directory
	rSrc, err := getRealPath(src)
	if err != nil {
		return "", fmt.Errorf("Directory path %s illegal", src)
	}
	rP, err := getRealPath(p)
	if err != nil {
		return "", fmt.Errorf("Directory path %s illegal", p)
	}
	if rSrc == rP {
		return p, nil
	}

	// dir and regular file must conflict
	if isDir(rSrc) != isDir(rP) {
		logrus.Errorf("Accelerator Mount Merging: source path seem illegal: %s or %s", src, p)
		return "", fmt.Errorf("Fail to merge directory with regular file")
	} else if !isDir(rSrc) && !isDir(rP) {
		// both are regular files, if the same just return
		// otherwise return an error
		if isSameFile(rSrc, rP) {
			return p, nil
		} else {
			logrus.Errorf("Accelerator Mount Merging: source are both regular files, but not the same one: %s and %s", src, p)
			return "", fmt.Errorf("Conflicting file")
		}

	}
	// change /usr/lib64 into usr_lib64
	dName := strings.Replace(strings.TrimPrefix(dest, "/"), "/", "_", -1)
	// newPath should be "/var/lib/docker/container/XXXX/accelerators/usr_lib64"
	// This directory will be removed in container delete progress
	newPath := filepath.Join(rootPath, "accelerators", dName)
	if err := os.MkdirAll(newPath, 0755); err != nil {
		return "", fmt.Errorf("Error Creating accelerator mount directory")
	}
	logrus.Debugf("Create a new mount source path %s for conflicting Mount", newPath)

	fMap := make(map[string]string)
	conflict := false
	// get src directory file list into a map
	if err := filepath.Walk(rSrc, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}
		if f.IsDir() {
			return nil
		}
		keyPath := strings.TrimPrefix(path, rSrc)
		fMap[keyPath] = path
		return nil
	}); err != nil {
		return "", fmt.Errorf("Failed to walk %s directory: %v", src, err)
	}

	// walk p directory to check whether the file conflict with src
	if err := filepath.Walk(rP, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}
		if f.IsDir() {
			return nil
		}
		keyPath := strings.TrimPrefix(path, rP)
		if bakPath, ok := fMap[keyPath]; ok {
			if !isSameFile(path, bakPath) {
				logrus.Errorf("Accelerator Mount Merging: files %s and %s seem conflict, merging failed", path, bakPath)
				conflict = true
			}
		}

		return nil
	}); err != nil {
		return "", fmt.Errorf("Failed to walk %s directory: %v", p, err)
	}

	if conflict {
		return "", fmt.Errorf("Accelerator Mount %s and %s conflict and failed to merge", src, p)
	}

	// if mount directory not conflict, merge them into newPath
	ufs := unionfsSupported()
	if ufs == "aufs" {
		logrus.Debugf("Detected aufs supported, try to merge directories through aufs")
		if err := aufsMerge(rSrc, rP, newPath); err == nil {
			// aufs merge succeed, just return
			return newPath, nil
		}
		logrus.Warnf("aufs merge failed, try regular way")
	} else if ufs == "overlay" {
		logrus.Debugf("Detected overlayfs supported, try to merge directories through overlayfs")
		if err := overlayMerge(rSrc, rP, newPath); err == nil {
			// overlay merge succeed, just return
			return newPath, nil
		}
		logrus.Warnf("overlayfs merge failed, try regular way")
	}

	// no unionfs supported, merge directories mannually
	if err := dirCopy(rSrc, newPath); err != nil {
		return "", err
	}
	if err := dirCopy(rP, newPath); err != nil {
		return "", err
	}

	return newPath, nil
}

// this will copy files/dirs in src into dest directory
func dirCopy(src, dest string) error {
	if src != dest {
		infos, err := ioutil.ReadDir(src)
		if err != nil {
			return err
		}

		for _, info := range infos {
			srcFile := filepath.Join(src, info.Name())
			dstFile := filepath.Join(dest)
			cmd := exec.Command("cp", "-rf", srcFile, dstFile)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("Copy directory %s to %s failed: %v", src, dest, err)
			}
		}
	}

	return nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		return os.IsExist(err)
	} else {
		return fi.IsDir()
	}

	return false
}

func isSameFile(f1, f2 string) bool {
	m1, err := md5File(f1)
	if err != nil {
		return false
	}
	m2, err := md5File(f2)
	if err != nil {
		return false
	}

	if m1 != m2 {
		return false
	}

	return true
}

func md5File(p string) (string, error) {
	file, err := os.Open(p)
	defer file.Close()
	if err != nil {
		return "", err
	}

	h := md5.New()
	_, err = io.Copy(h, file)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func aufsMerge(ldir, udir, dest string) error {
	data := "br:" + ldir + "=ro:" + udir + "=ro"
	if err := syscall.Mount("none", dest, "aufs", 0, data); err != nil {
		logrus.Errorf("aufs merge failed: %s", err)
		return err
	}
	return nil
}

func overlayMerge(ldir, udir, dest string) error {
	data := "lowerdir=" + ldir + ":" + udir
	if err := syscall.Mount("overlay", dest, "overlay", 0, data); err != nil {
		logrus.Errorf("aufs merge failed: %s", err)
		return err
	}
	return nil
}

func unionfsSupported() string {
	// try to load aufs or overlay driver
	// exec.Command("modprobe", "aufs").Run()
	// exec.Command("modprobe", "overlay").Run()

	f, err := os.Open("/proc/filesystems")
	if err != nil {
		return "none"
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.Contains(s.Text(), "aufs") {
			return "aufs"
		} else if strings.Contains(s.Text(), "overlay") {
			return "overlay"
		}
	}
	return "none"
}
