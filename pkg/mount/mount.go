package mount

import (
	"sort"
	"strings"
)

// GetMounts retrieves a list of mounts for the current running process.
func GetMounts() ([]*Info, error) {
	return parseMountTable()
}

// Mounted looks at /proc/self/mountinfo to determine of the specified
// mountpoint has been mounted
func Mounted(mountpoint string) (bool, error) {
	entries, err := parseMountTable()
	if err != nil {
		return false, err
	}

	// Search the table for the mountpoint
	for _, e := range entries {
		if e.Mountpoint == mountpoint {
			return true, nil
		}
	}
	return false, nil
}

// Mount will mount filesystem according to the specified configuration, on the
// condition that the target path is *not* already mounted. Options must be
// specified like the mount or fstab unix commands: "opt1=val1,opt2=val2". See
// flags.go for supported option flags.
func Mount(device, target, mType, options string) error {
	flag, _ := parseOptions(options)
	if flag&REMOUNT != REMOUNT {
		if mounted, err := Mounted(target); err != nil || mounted {
			return err
		}
	}
	return ForceMount(device, target, mType, options)
}

// ForceMount will mount a filesystem according to the specified configuration,
// *regardless* if the target path is not already mounted. Options must be
// specified like the mount or fstab unix commands: "opt1=val1,opt2=val2". See
// flags.go for supported option flags.
func ForceMount(device, target, mType, options string) error {
	flag, data := parseOptions(options)
	if err := mount(device, target, mType, uintptr(flag), data); err != nil {
		return err
	}
	return nil
}

// Unmount lazily unmounts a filesystem on supported platforms, otherwise
// does a normal unmount.
func Unmount(target string) error {
	if mounted, err := Mounted(target); err != nil || !mounted {
		return err
	}
	return unmount(target, mntDetach)
}

// RecursiveUnmount unmounts the target and all mounts underneath, starting with
// the deepsest mount first.
func RecursiveUnmount(target string) error {
	mounts, err := GetMounts()
	if err != nil {
		return err
	}

	// Make the deepest mount be first
	sort.Sort(sort.Reverse(byMountpoint(mounts)))

	for i, m := range mounts {
		if !strings.HasPrefix(m.Mountpoint, target) {
			continue
		}
		if err := Unmount(m.Mountpoint); err != nil && i == len(mounts)-1 {
			if mounted, err := Mounted(m.Mountpoint); err != nil || mounted {
				return err
			}
			// Ignore errors for submounts and continue trying to unmount others
			// The final unmount should fail if there ane any submounts remaining
		}
	}
	return nil
}
