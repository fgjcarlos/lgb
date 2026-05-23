// config_validate.go — lgb config validate subcommand.
//
// Loads and validates the YAML config. Exits 0 on success, 1 on failure.
// Does NOT start the server or any goroutines.
// Requirements: MVP-FND-1.6. Design: §6.1, §6.5.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/config"
)

// configValidateOutput is the JSON shape for --json output.
type configValidateOutput struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// NewConfigValidateCmd returns the config validate subcommand.
func NewConfigValidateCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigValidateTo(d, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

// runConfigValidateTo performs config validation and writes results to stdout/stderr.
// Called by RunE and tests.
func runConfigValidateTo(d *Deps, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	cfg, err := config.Load(d.ConfigPath)
	if err != nil {
		if d.JSON {
			out := configValidateOutput{Valid: false, Errors: []string{err.Error()}}
			_ = json.NewEncoder(stdout).Encode(out)
		} else {
			fmt.Fprintf(stderr, "config error: %v\n", err)
		}
		return err
	}

	if err := cfg.Validate(); err != nil {
		violations := extractViolations(err)
		if d.JSON {
			out := configValidateOutput{Valid: false, Errors: violations}
			_ = json.NewEncoder(stdout).Encode(out)
		} else {
			for _, v := range violations {
				fmt.Fprintf(stderr, "%s\n", v)
			}
		}
		return err
	}

	if d.JSON {
		out := configValidateOutput{Valid: true}
		return json.NewEncoder(stdout).Encode(out)
	}
	fmt.Fprintln(stdout, "config OK")
	return nil
}

// extractViolations unwraps a joined error into individual violation strings.
func extractViolations(err error) []string {
	if err == nil {
		return nil
	}
	// errors.Join (Go 1.20+) implements an Unwrap() []error interface.
	type unwrapMultiple interface {
		Unwrap() []error
	}
	if mErr, ok := err.(unwrapMultiple); ok {
		var msgs []string
		for _, e := range mErr.Unwrap() {
			msgs = append(msgs, e.Error())
		}
		return msgs
	}
	return []string{err.Error()}
}
