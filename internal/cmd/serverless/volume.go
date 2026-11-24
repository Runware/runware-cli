package serverless

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// Volume limits, mirrored from the server. A local copy is worth its upkeep: a
// code deployment can build for up to ninety minutes, and learning only then
// that two mounts overlap is the whole of that wait wasted. The server validates
// every request independently regardless -- this is a courtesy, not a boundary.
const (
	// maxVolumes counts --volume and --mount entries together: the server
	// holds both lists to one limit.
	maxVolumes             = 30
	maxVolumeMountPathLen  = 2048
	maxVolumePathComponent = 255
	// The characters the server's mountPath pattern allows besides letters and
	// digits: ^/[A-Za-z0-9._/+@-]+$
	volumeMountPathExtra = "._-/+@"
)

const (
	volumeFlag   = "volume"
	mountFlag    = "mount"
	noMountsFlag = "no-mounts"
)

// volumeNamePattern is the server's VolumeName rule.
var volumeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// sandboxPath is one requested path inside the sandbox, with the flag that asked
// for it, so an error can name the flag the person has to change.
type sandboxPath struct {
	flag  string
	raw   string
	clean string
}

// buildSandboxPaths turns --volume and --mount flags into the wire types,
// rejecting anything the server would reject. Both lists share one limit and
// one overlap check, as on the server: a Shared Volume mounted inside a
// node-local volume, or over it, would leave no answer to which one owns the
// shared subtree.
//
// On an update the request carries only the mounts, and the app read does not
// return the node-local volumes it keeps, so an overlap with those is left to
// the server.
//
// No entries send nil rather than an empty array: the fields are optional and
// an explicit [] is a different statement from saying nothing.
func buildSandboxPaths(volumePaths, mountSpecs []string) (*[]serverlessapi.AppVolume, *[]serverlessapi.VolumeMount, error) {
	if total := len(volumePaths) + len(mountSpecs); total > maxVolumes {
		return nil, nil, fmt.Errorf("at most %d --volume and --mount entries together (got %d)", maxVolumes, total)
	}

	seen := make([]sandboxPath, 0, len(volumePaths)+len(mountSpecs))
	accept := func(flag, raw, mountPath string) (string, error) {
		clean, err := validateMountPath(flag, raw, mountPath, seen)
		if err != nil {
			return "", err
		}
		seen = append(seen, sandboxPath{
			flag:  flag,
			raw:   raw,
			clean: clean,
		})
		return clean, nil
	}

	var volumes *[]serverlessapi.AppVolume
	if len(volumePaths) > 0 {
		list := make([]serverlessapi.AppVolume, 0, len(volumePaths))
		for _, raw := range volumePaths {
			clean, err := accept(volumeFlag, raw, raw)
			if err != nil {
				return nil, nil, err
			}
			list = append(list, serverlessapi.AppVolume{MountPath: clean})
		}
		volumes = &list
	}

	var mounts *[]serverlessapi.VolumeMount
	if len(mountSpecs) > 0 {
		list := make([]serverlessapi.VolumeMount, 0, len(mountSpecs))
		for _, raw := range mountSpecs {
			name, mountPath, err := parseMountSpec(raw)
			if err != nil {
				return nil, nil, err
			}
			clean, err := accept(mountFlag, raw, mountPath)
			if err != nil {
				return nil, nil, err
			}
			list = append(list, serverlessapi.VolumeMount{
				Kind:      serverlessapi.VolumeMountKindVolume,
				Name:      name,
				MountPath: clean,
			})
		}
		mounts = &list
	}
	return volumes, mounts, nil
}

// parseMountSpec splits <volume>:<path>. A Volume name has no colon, so the
// first one separates the two.
func parseMountSpec(raw string) (name, mountPath string, err error) {
	name, mountPath, ok := strings.Cut(raw, ":")
	if !ok || name == "" || mountPath == "" {
		return "", "", fmt.Errorf("--%s %q: want <volume>:<path>, such as model-weights:/models", mountFlag, raw)
	}
	if !volumeNamePattern.MatchString(name) {
		return "", "", fmt.Errorf(
			"--%s %q: Volume name %q must be lowercase letters, digits, '.', '_' and '-', starting with a letter or digit, at most 63 characters",
			mountFlag, raw, name)
	}
	return name, mountPath, nil
}

// validateMountPath returns the cleaned mountPath, or an error naming the flag
// value and what is wrong with it. `seen` holds the already-accepted paths.
func validateMountPath(flag, raw, mountPath string, seen []sandboxPath) (string, error) {
	// path.Clean, not filepath.Clean: this is a path inside the Linux sandbox
	// the app runs in, whatever the machine the CLI runs on.
	clean := path.Clean(mountPath)

	switch {
	case !path.IsAbs(clean):
		return "", fmt.Errorf("--%s %q: must be an absolute path", flag, raw)
	case clean == "/":
		return "", fmt.Errorf("--%s %q: must not be the root directory", flag, raw)
	case len(clean) > maxVolumeMountPathLen:
		return "", fmt.Errorf("--%s %q: exceeds the %d character limit", flag, raw, maxVolumeMountPathLen)
	}

	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || strings.ContainsRune(volumeMountPathExtra, r) {
			continue
		}
		return "", fmt.Errorf("--%s %q: contains unsupported character %q", flag, raw, r)
	}

	for component := range strings.SplitSeq(strings.TrimPrefix(clean, "/"), "/") {
		if len(component) > maxVolumePathComponent {
			return "", fmt.Errorf("--%s %q: path component exceeds %d bytes", flag, raw, maxVolumePathComponent)
		}
	}

	// Overlap, not just duplication: two entries where one contains the other
	// would bind the same subtree into the sandbox twice.
	for _, previous := range seen {
		same := previous.clean == clean
		if !same && !strings.HasPrefix(clean, previous.clean+"/") && !strings.HasPrefix(previous.clean, clean+"/") {
			continue
		}
		return "", pathConflictError(sandboxPath{
			flag:  flag,
			raw:   raw,
			clean: clean,
		}, previous, same)
	}
	return clean, nil
}

// pathConflictError names both entries and the fix. Two node-local volumes
// that nest are one volume declared twice; anything involving a mount needs
// its own path.
func pathConflictError(entry, previous sandboxPath, same bool) error {
	if entry.flag == volumeFlag && previous.flag == volumeFlag {
		if same {
			return fmt.Errorf("--%s %q is listed twice", volumeFlag, entry.clean)
		}
		return fmt.Errorf("--%s %q overlaps %q; declare only the outer path", volumeFlag, entry.clean, previous.clean)
	}
	relation := "overlaps"
	if same {
		relation = "uses the same path as"
	}
	return fmt.Errorf("--%s %q %s --%s %q; give the mount a path that neither contains nor is inside another --volume or --mount",
		entry.flag, entry.raw, relation, previous.flag, previous.raw)
}
