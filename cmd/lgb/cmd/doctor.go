// doctor.go — lgb doctor subcommand.
//
// Runs all registered doctor checks concurrently and reports results.
// Exit code is determined by the worst check status per MVP-FND-8.3.
// Requirements: MVP-FND-1.4, MVP-FND-8.3–8.5. Design: §6.1, §6.3, §20.4.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/doctor"
)

// doctorCheckJSON is one check entry in the JSON output.
type doctorCheckJSON struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// doctorOutput is the JSON output structure for --json.
type doctorOutput struct {
	Checks  []doctorCheckJSON `json:"checks"`
	Overall string            `json:"overall"`
}

// NewDoctorCmd returns the doctor subcommand.
func NewDoctorCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runDoctorTo(d, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if code != 0 {
				// Return a sentinel that maps to exit code 1.
				return fmt.Errorf("doctor: one or more checks failed")
			}
			return nil
		},
	}
}

// runDoctorTo runs all checks and writes output to stdout/stderr.
// Returns (exitCode, error). Called by RunE and tests.
func runDoctorTo(d *Deps, stdout, stderr io.Writer) (int, error) {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	// Use injected registry (from tests) or build the default one.
	reg := d.DoctorRegistry
	if reg == nil {
		if d.Config == nil {
			fmt.Fprintln(stderr, "error: config not loaded — cannot run doctor checks")
			return 2, fmt.Errorf("doctor: config not loaded")
		}
		reg = doctor.Default(d.Config)
	}

	results := doctor.Run(context.Background(), reg)
	code := doctor.ExitCodeFromResults(results)
	overall := worstStatus(results)

	if d.JSON {
		out := doctorOutput{
			Checks:  make([]doctorCheckJSON, len(results)),
			Overall: overall,
		}
		for i, r := range results {
			out.Checks[i] = doctorCheckJSON{
				Name:    r.Name,
				Status:  r.Status.String(),
				Message: r.Message,
			}
		}
		if err := json.NewEncoder(stdout).Encode(out); err != nil {
			return 2, err
		}
		return code, nil
	}

	// Plain output.
	for _, r := range results {
		prefix := statusPrefix(r.Status)
		fmt.Fprintf(stdout, "%s %s: %s\n", prefix, r.Name, r.Message)
	}
	return code, nil
}

// statusPrefix returns the human-readable prefix for a check status.
func statusPrefix(s doctor.CheckStatus) string {
	switch s {
	case doctor.StatusInfo:
		return "[INFO]"
	case doctor.StatusPass:
		return "[PASS]"
	case doctor.StatusWarn:
		return "[WARN]"
	case doctor.StatusFail:
		return "[FAIL]"
	default:
		return "[????]"
	}
}

// worstStatus returns the string representation of the worst status in results.
func worstStatus(results []doctor.Result) string {
	worst := doctor.StatusPass
	for _, r := range results {
		if r.Status > worst {
			worst = r.Status
		}
	}
	if worst == doctor.StatusFail {
		return "fail"
	}
	if worst == doctor.StatusWarn {
		return "warn"
	}
	return "pass"
}
