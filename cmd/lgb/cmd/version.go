// version.go — lgb version subcommand.
//
// Prints version, commit, and date from internal/version.
// With --json: {"version":"…","commit":"…","date":"…"}
// Requirements: MVP-FND-1.2, MVP-FND-1.7. Design: §6.1, §6.5.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/version"
)

// versionOutput is the JSON shape for --json output.
type versionOutput struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// NewVersionCmd returns the version subcommand.
func NewVersionCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersionToWriter(d, cmd.OutOrStdout())
		},
	}
}

// runVersionToWriter writes version output to w. Called by RunE and tests.
func runVersionToWriter(d *Deps, w io.Writer) error {
	if w == nil {
		w = os.Stdout
	}
	info := version.Info()
	if d.JSON {
		out := versionOutput{
			Version: info.Version,
			Commit:  info.Commit,
			Date:    info.Date,
		}
		return json.NewEncoder(w).Encode(out)
	}
	fmt.Fprintf(w, "lgb %s (commit: %s, built: %s)\n", info.Version, info.Commit, info.Date)
	return nil
}
