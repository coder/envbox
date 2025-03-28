package xunix

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/xerrors"
	mount "k8s.io/mount-utils"

	"cdr.dev/slog"
)

var (
	gpuMountRegex     = regexp.MustCompile(`(?i)(nvidia|vulkan|cuda)`)
	gpuExtraRegex     = regexp.MustCompile(`(?i)(libgl(e|sx|\.)|nvidia|vulkan|cuda)`)
	gpuEnvRegex       = regexp.MustCompile(`(?i)nvidia`)
	sharedObjectRegex = regexp.MustCompile(`\.so(\.[0-9\.]+)?$`)
)

func GPUEnvs(ctx context.Context) []string {
	envs := Environ(ctx)

	gpus := []string{}
	for _, env := range envs {
		name := strings.Split(env, "=")[0]
		if gpuEnvRegex.MatchString(name) {
			gpus = append(gpus, env)
		}
	}

	return gpus
}

func GPUs(ctx context.Context, log slog.Logger, usrLibDir string) ([]Device, []mount.MountPoint, error) {
	var (
		afs     = GetFS(ctx)
		mounter = Mounter(ctx)
		devices = []Device{}
		binds   = []mount.MountPoint{}
	)

	mounts, err := mounter.List()
	if err != nil {
		return nil, nil, xerrors.Errorf("list mounts: %w", err)
	}

	for _, m := range mounts {
		if gpuMountRegex.MatchString(m.Path) {
			// If we find the GPU in /dev treat it as a device.
			if strings.HasPrefix(m.Path, "/dev/") {
				// TODO(JonA): We could populate the rest of the fields but it
				// doesn't seem like we need them. We'll have to expand
				// the FS interface to allow for a real unix stat.
				devices = append(devices, Device{
					Path: m.Path,
				})
				continue
			}

			// If it's not in /dev treat it as a bind mount.
			binds = append(binds, m)
			// We also want to find any symlinks that point to the target.
			// This is important for the nvidia driver as it mounts the driver
			// files with the driver version appended to the end, and creates
			// symlinks that point to the actual files.
			links, err := SameDirSymlinks(afs, m.Path)
			if err != nil {
				log.Error(ctx, "find symlinks", slog.F("path", m.Path), slog.Error(err))
			} else {
				for _, link := range links {
					log.Debug(ctx, "found symlink", slog.F("link", link), slog.F("target", m.Path))
					binds = append(binds, mount.MountPoint{
						Path: link,
						Opts: []string{"ro"},
					})
				}
			}
		}
	}

	extraGPUS, err := usrLibGPUs(ctx, log, usrLibDir)
	if err != nil {
		return nil, nil, xerrors.Errorf("find %q gpus: %w", usrLibDir, err)
	}

	for _, gpu := range extraGPUS {
		var duplicate bool
		for _, bind := range binds {
			if gpu.Path == bind.Path {
				duplicate = true
				break
			}
		}
		if !duplicate {
			binds = append(binds, gpu)
		}
	}

	return devices, binds, nil
}

func usrLibGPUs(ctx context.Context, log slog.Logger, usrLibDir string) ([]mount.MountPoint, error) {
	var (
		afs   = GetFS(ctx)
		binds = []string{}
	)

	err := afero.Walk(afs, usrLibDir,
		func(path string, _ fs.FileInfo, err error) error {
			if path == usrLibDir && err != nil {
				return xerrors.Errorf("stat /usr/lib mountpoint %q: %w", usrLibDir, err)
			}
			if err != nil {
				log.Error(ctx, "list directory", slog.F("dir", path), slog.Error(err))
				return nil
			}

			if !gpuExtraRegex.MatchString(path) {
				return nil
			}

			if !sharedObjectRegex.MatchString(path) {
				return nil
			}

			paths, err := recursiveSymlinks(afs, usrLibDir, path)
			if err != nil {
				log.Error(ctx, "find recursive symlinks", slog.F("path", path), slog.Error(err))
			}

			binds = append(binds, paths...)

			return nil
		})
	if err != nil {
		return nil, xerrors.Errorf("walk %q for GPU drivers: %w", usrLibDir, err)
	}

	mounts := make([]mount.MountPoint, 0, len(binds))
	for _, bind := range binds {
		mounts = append(mounts, mount.MountPoint{
			Path: bind,
			Opts: []string{"ro"},
		})
	}

	return mounts, nil
}

// recursiveSymlinks returns all of the paths in the chain of symlinks starting
// at `path`. If `path` isn't a symlink, only `path` is returned. If at any
// point the symlink chain goes outside of `mountpoint` then nil is returned.
// Despite its name it's interestingly enough not implemented recursively.
func recursiveSymlinks(afs FS, mountpoint string, path string) ([]string, error) {
	if !strings.HasSuffix(mountpoint, "/") {
		mountpoint += "/"
	}

	paths := []string{}
	for {
		if !strings.HasPrefix(path, mountpoint) {
			return nil, nil
		}

		stat, err := afs.LStat(path)
		if err != nil {
			return nil, xerrors.Errorf("lstat %q: %w", path, err)
		}

		paths = append(paths, path)
		if stat.Mode()&os.ModeSymlink == 0 {
			break
		}

		newPath, err := afs.Readlink(path)
		if err != nil {
			return nil, xerrors.Errorf("readlink %q: %w", path, err)
		}
		if newPath == "" {
			break
		}

		if filepath.IsAbs(newPath) {
			path = newPath
		} else {
			dir := filepath.Dir(path)
			path = filepath.Join(dir, newPath)
		}
	}

	return paths, nil
}

// SameDirSymlinks returns all links in the same directory as `target` that
// point to target, either indirectly or directly. Only symlinks in the same
// directory as `target` are considered.
func SameDirSymlinks(afs FS, target string) ([]string, error) {
	// Get the list of files in the directory of the target.
	fis, err := afero.ReadDir(afs, filepath.Dir(target))
	if err != nil {
		return nil, xerrors.Errorf("read dir %q: %w", filepath.Dir(target), err)
	}

	// Do an initial pass to map all symlinks to their destinations.
	allLinks := make(map[string]string)
	for _, fi := range fis {
		// Ignore non-symlinks.
		if fi.Mode()&os.ModeSymlink == 0 {
			continue
		}

		absPath := filepath.Join(filepath.Dir(target), fi.Name())
		link, err := afs.Readlink(filepath.Join(filepath.Dir(target), fi.Name()))
		if err != nil {
			return nil, xerrors.Errorf("readlink %q: %w", fi.Name(), err)
		}

		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(target), link)
		}
		allLinks[absPath] = link
	}

	// Now we can start checking for symlinks that point to the target.
	var (
		found = make([]string, 0)
		// Set an arbitrary upper limit to prevent infinite loops.
		maxIterations = 10
	)
	for range maxIterations {
		var foundThisTime bool
		for linkName, linkDest := range allLinks {
			// Ignore symlinks that point outside of target's directory.
			if filepath.Dir(linkName) != filepath.Dir(target) {
				continue
			}

			// If the symlink points to the target, add it to the list.
			if linkDest == target {
				if !slices.Contains(found, linkName) {
					found = append(found, linkName)
					foundThisTime = true
				}
			}

			// If the symlink points to another symlink that we already determined
			// points to the target, add it to the list.
			if slices.Contains(found, linkDest) {
				if !slices.Contains(found, linkName) {
					found = append(found, linkName)
					foundThisTime = true
				}
			}
		}
		// If we didn't find any new symlinks, we're done.
		if !foundThisTime {
			break
		}
	}
	return found, nil
}

// TryUnmountProcGPUDrivers unmounts any GPU-related mounts under /proc as it causes
// issues when creating any container in some cases. Errors encountered while
// unmounting are treated as non-fatal.
func TryUnmountProcGPUDrivers(ctx context.Context, log slog.Logger) ([]mount.MountPoint, error) {
	mounter := Mounter(ctx)

	mounts, err := mounter.List()
	if err != nil {
		return nil, xerrors.Errorf("list mounts: %w", err)
	}

	// Sort mounts list by longest paths (by segments) first.
	sort.Slice(mounts, func(i, j int) bool {
		// Sort paths with more slashes as "less".
		return strings.Count(mounts[i].Path, "/") > strings.Count(mounts[j].Path, "/")
	})

	drivers := []mount.MountPoint{}
	for _, m := range mounts {
		if strings.HasPrefix(m.Path, "/proc/") && gpuMountRegex.MatchString(m.Path) {
			err := mounter.Unmount(m.Path)
			if err != nil {
				log.Warn(ctx,
					"umount potentially problematic mount",
					slog.F("path", m.Path),
					slog.Error(err),
				)
				continue
			}
			drivers = append(drivers, m)
		}
	}

	return drivers, nil
}
