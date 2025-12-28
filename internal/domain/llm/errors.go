package llm

import (
	"errors"
	"fmt"
)

var ErrInvalidArgument = errors.New("invalid argument")

func InvalidArgument(msg string) error {
	if msg == "" {
		return ErrInvalidArgument
	}
	return fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
}

