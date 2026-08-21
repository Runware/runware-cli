package serverless

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Environment variable limits, mirrored from the server's EnvironmentVariableName
// and its deployment_configs column CHECK.
const (
	maxEnvVars      = 100
	maxEnvNameLen   = 128
	maxEnvValueLen  = 4096
	envAssignSuffix = "=VALUE"
)

// envNamePattern is the server's EnvironmentVariableName rule: POSIX-style, so a
// name this accepts is a name the API can store rather than one it rejects after
// the archive has already been uploaded.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// buildEnvironmentVariables turns --env KEY=VALUE pairs and --env-file paths into
// the create request's map.
//
// These belong on the CREATE request and nowhere else: an app's environment is
// frozen into its version snapshot, which is what the deployer renders from, and
// no endpoint creates a further version -- `deploy` re-applies an existing one by
// number and says so. So a variable set through the /environment-variables
// endpoints after the app exists is stored, listed back, and never reaches a
// worker. Passing it here is the only route that ends up in a pod.
//
// Files are read before the inline pairs are applied, so an explicit --env wins
// over a file entry with the same name.
func buildEnvironmentVariables(files, pairs []string) (*map[string]string, error) {
	if len(files) == 0 && len(pairs) == 0 {
		return nil, nil
	}

	env := make(map[string]string)
	for _, path := range files {
		if err := readEnvFile(path, env); err != nil {
			return nil, err
		}
	}
	for _, pair := range pairs {
		name, value, err := splitEnvAssignment(pair)
		if err != nil {
			return nil, err
		}
		env[name] = value
	}

	if len(env) > maxEnvVars {
		return nil, fmt.Errorf("at most %d environment variables (got %d)", maxEnvVars, len(env))
	}
	return &env, nil
}

// readEnvFile reads KEY=VALUE lines into env, skipping blanks and # comments.
//
// The point of a file is that a secret never reaches an argv: a value passed as
// --env is visible in the process list to every other user on the machine for as
// long as the command runs, and the shell records it in history.
func readEnvFile(path string, env map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// `export FOO=bar` is what a shell-sourced file looks like, and pasting one
		// in is the obvious mistake to absorb rather than reject.
		trimmed = strings.TrimPrefix(trimmed, "export ")
		name, value, err := splitEnvAssignment(trimmed)
		if err != nil {
			return fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		env[name] = value
	}
	return nil
}

// splitEnvAssignment parses one KEY=VALUE, validating the name and value against
// the limits the server enforces.
func splitEnvAssignment(assignment string) (name, value string, err error) {
	name, value, found := strings.Cut(assignment, "=")
	if !found {
		return "", "", fmt.Errorf("%q is not KEY%s", assignment, envAssignSuffix)
	}
	// The name is trimmed but the value is not: trailing whitespace in a value can
	// be deliberate, and a token with a stray newline is a 401 the app cannot
	// explain -- so callers pass values through a file rather than have this guess.
	name = strings.TrimSpace(name)

	switch {
	case name == "":
		return "", "", fmt.Errorf("%q has an empty name", assignment)
	case len(name) > maxEnvNameLen:
		return "", "", fmt.Errorf("environment variable name %q exceeds %d characters", name, maxEnvNameLen)
	case !envNamePattern.MatchString(name):
		return "", "", fmt.Errorf(
			"environment variable name %q must be POSIX-style: letters, digits and underscore, not starting with a digit",
			name,
		)
	case len(value) > maxEnvValueLen:
		return "", "", fmt.Errorf("value for %q exceeds %d characters", name, maxEnvValueLen)
	}
	return name, value, nil
}
