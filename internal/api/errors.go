package api

import "errors"

// ErrModelRequired is returned by Run when the model argument is empty.
var ErrModelRequired = errors.New("model is required")
