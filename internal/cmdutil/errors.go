package cmdutil

import (
	"errors"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
)

// PrintError logs err to logger with appropriate formatting.
//
// Special cases:
//   - transport.ErrNoAPIKey → friendly "no key" message with auth hint
//   - *transport.RunwareError CodeAuth → "Authentication failed" with auth hint
//   - *transport.RunwareError other → re.Message, with docs URL as a "docs" field
//
// For all other errors, err.Error() is used as the message.
func PrintError(logger *log.Logger, err error) {
	if errors.Is(err, transport.ErrNoAPIKey) {
		logger.Error("No API key configured. Run 'runware auth login' to set your API key.")
		return
	}

	var re *transport.RunwareError
	if errors.As(err, &re) {
		switch re.Code {
		case transport.CodeAuth:
			if re.Documentation != "" {
				logger.Error("Authentication failed. Run 'runware auth status' to verify your credentials.", "docs", re.Documentation)
			} else {
				logger.Error("Authentication failed. Run 'runware auth status' to verify your credentials.")
			}
		default:
			if re.Documentation != "" {
				logger.Error(re.Message, "docs", re.Documentation)
			} else {
				logger.Error(re.Message)
			}
		}
		return
	}

	logger.Error(err.Error())
}

// PrintErrorMsg logs a custom message to logger, appending the documentation
// URL from err as a "docs" field when err is a *transport.RunwareError with one.
// Use this when a command needs to surface a context-specific message for an
// error but continue executing (warning-style) rather than exit.
func PrintErrorMsg(logger *log.Logger, message string, err error) {
	var re *transport.RunwareError
	if errors.As(err, &re) && re.Documentation != "" {
		logger.Error(message, "docs", re.Documentation)
		return
	}
	logger.Error(message)
}
