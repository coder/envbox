package cli

import (
	"github.com/spf13/cobra"
)

func Root(ch chan func() error) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "envbox",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(dockerCmd(ch))
	return cmd
}
