package plc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/danomagnum/gologix"
)

// translateError maps raw gologix / network errors to project-level PLC
// sentinel errors (design §8, PLC-ERR-1.3, PLC-ERR-1.5).
//
// op is one of "read" or "write" and is used to choose between ErrPLCRead and
// ErrPLCWrite when a *gologix.CIPError is encountered.
// tag is the PLC tag name, included in the error message for diagnostics.
//
// Rules (evaluated in order):
//  1. nil → nil
//  2. net.Error with Timeout() == true → ErrPLCTimeout
//  3. *gologix.CIPError + op=="write" → ErrPLCWrite
//  4. *gologix.CIPError (any other op) → ErrPLCRead
//  5. *net.OpError → ErrPLCConnect
//  6. io.EOF → ErrPLCConnect
//  7. message contains "not connected" → ErrPLCConnect
//  8. any other error → ErrPLCRead (no panic, per PLC-ERR-1.5)
func translateError(err error, op string, tag string) error {
	if err == nil {
		return nil
	}

	// Check for timeout first — many net.Error subtypes also match *net.OpError,
	// so we must check the Timeout interface before unwrapping further.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("plc: %s %q: %w: %w", op, tag, ErrPLCTimeout, err)
	}

	// CIP-layer errors from gologix.
	var cipErr *gologix.CIPError
	if errors.As(err, &cipErr) {
		if op == "write" {
			return fmt.Errorf("plc: write %q: %w: %w", tag, ErrPLCWrite, err)
		}
		return fmt.Errorf("plc: read %q: %w: %w", tag, ErrPLCRead, err)
	}

	// Network-layer connection errors.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return fmt.Errorf("plc: %s %q: %w: %w", op, tag, ErrPLCConnect, err)
	}

	// Unexpected EOF — indicates the remote side closed the connection.
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("plc: %s %q: %w: %w", op, tag, ErrPLCConnect, err)
	}

	// "not connected and AutoConnect not enabled" from gologix.checkConnection.
	if strings.Contains(err.Error(), "not connected") {
		return fmt.Errorf("plc: %s %q: %w: %w", op, tag, ErrPLCConnect, err)
	}

	// Fallback: unknown error — wrap as ErrPLCRead to avoid panics (PLC-ERR-1.5).
	return fmt.Errorf("plc: %s %q: %w: %w", op, tag, ErrPLCRead, err)
}
