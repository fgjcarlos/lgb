// config.go — lgb config command group.
//
// This file registers the "config" parent command group and its children.
// Requirements: MVP-FND-1.6. Design: §6.1.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewConfigCmd returns the config command group.
func NewConfigCmd(d *Deps) *cobra.Command {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Configuration management commands",
		// Group command: running bare `lgb config` shows help.
	}

	cfg.AddCommand(NewConfigValidateCmd(d))
	return cfg
}
