// status.go — lgb status subcommand.
//
// Prints a JSON health snapshot stub.
// Requirements: MVP-FND-1.5. Design: §6.1.
package cmd

import (
	"encoding/json"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// statusOutput is the Phase 0 stub response.
type statusOutput struct {
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	UptimeSeconds int    `json:"uptime_seconds"`
}

// NewStatusCmd returns the status subcommand.
func NewStatusCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print gateway status snapshot as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusToWriter(d, cmd.OutOrStdout())
		},
	}
}

// runStatusToWriter writes the status JSON to w. Called by RunE and tests.
func runStatusToWriter(_ *Deps, w io.Writer) error {
	if w == nil {
		w = os.Stdout
	}
	out := statusOutput{
		Status:        "ok",
		Phase:         "0",
		UptimeSeconds: 0,
	}
	return json.NewEncoder(w).Encode(out)
}
