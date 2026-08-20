package serverless

import (
	"fmt"
	"path"
	"strings"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// Volume limits, mirrored from the server. A local copy is worth its upkeep: a
// code deployment can build for up to ninety minutes, and learning only then
// that two mounts overlap is the whole of that wait wasted. The server validates
// every request independently regardless -- this is a courtesy, not a boundary.
const (
	maxVolumes             = 30
	maxVolumeMountPathLen  = 2048
	maxVolumePathComponent = 255
	// The characters the server's mountPath pattern allows besides letters and
	// digits: ^/[A-Za-z0-9._/+@-]+$
	volumeMountPathExtra = "._-/+@"
)

// buildVolumes turns --volume flags into the wire type, rejecting anything the
// server would reject.
//
// The mount path is the volume's whole identity: there is no name, because the
// path is what the app opens and what the node-local directory is keyed by. So
// nothing here needs a second field, and two entries that resolve to the same
// place are a mistake rather than a merge.
func buildVolumes(mountPaths []string) (*[]serverlessapi.AppVolume, error) {
	if len(mountPaths) == 0 {
		return nil, nil
	}
	if len(mountPaths) > maxVolumes {
		return nil, fmt.Errorf("at most %d volumes (got %d)", maxVolumes, len(mountPaths))
	}

	volumes := make([]serverlessapi.AppVolume, 0, len(mountPaths))
	seen := make([]string, 0, len(mountPaths))
	for _, raw := range mountPaths {
		mount, err := validateMountPath(raw, seen)
		if err != nil {
			return nil, err
		}
		seen = append(seen, mount)
		volumes = append(volumes, serverlessapi.AppVolume{MountPath: mount})
	}
	return &volumes, nil
}

// validateMountPath returns the cleaned path, or an error naming what is wrong
// with it. `seen` holds the already-accepted paths, cleaned.
func validateMountPath(raw string, seen []string) (string, error) {
	// path.Clean, not filepath.Clean: this is a path inside the Linux sandbox
	// the app runs in, whatever the machine the CLI runs on.
	mount := path.Clean(raw)

	switch {
	case !path.IsAbs(mount):
		return "", fmt.Errorf("volume %q: must be an absolute path", raw)
	case mount == "/":
		return "", fmt.Errorf("volume %q: must not be the root directory", raw)
	case len(mount) > maxVolumeMountPathLen:
		return "", fmt.Errorf("volume %q: exceeds the %d character limit", raw, maxVolumeMountPathLen)
	}

	for _, r := range mount {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || strings.ContainsRune(volumeMountPathExtra, r) {
			continue
		}
		return "", fmt.Errorf("volume %q: contains unsupported character %q", raw, r)
	}

	for _, component := range strings.Split(strings.TrimPrefix(mount, "/"), "/") {
		if len(component) > maxVolumePathComponent {
			return "", fmt.Errorf("volume %q: path component exceeds %d bytes", raw, maxVolumePathComponent)
		}
	}

	// Overlap, not just duplication: two volumes where one contains the other
	// would bind-mount the same node directory into the sandbox twice, and there
	// is no answer to which one owns the shared subtree.
	for _, previous := range seen {
		if previous == mount {
			return "", fmt.Errorf("volume %q is listed twice", mount)
		}
		if strings.HasPrefix(mount, previous+"/") || strings.HasPrefix(previous, mount+"/") {
			return "", fmt.Errorf("volume %q overlaps %q; declare only the outer path", mount, previous)
		}
	}
	return mount, nil
}
