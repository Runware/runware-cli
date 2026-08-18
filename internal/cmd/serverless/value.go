package serverless

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// readValueFlag returns a string from --value, or from --value-file (a path,
// or "-" for stdin). A trailing newline is stripped from file/stdin so typical
// text files and printf pipelines do not keep a spurious \n.
func readValueFlag(value, valueFile string, stdin io.Reader) (string, error) {
	if valueFile == "" {
		return value, nil
	}

	var (
		raw []byte
		err error
	)
	if valueFile == "-" {
		raw, err = io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read value from stdin: %w", err)
		}
	} else {
		raw, err = os.ReadFile(valueFile)
		if err != nil {
			return "", fmt.Errorf("read value from %s: %w", valueFile, err)
		}
	}

	s := strings.TrimSuffix(string(raw), "\n")
	return strings.TrimSuffix(s, "\r"), nil
}
