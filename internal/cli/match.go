package cli

import (
	"github.com/spf13/cobra"
)

func newMatchCommand(o *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "match URL",
		Short: "Show which Traefik route serves a URL (not implemented yet)",
		Example: `  kubectl traefik match https://app.example.com/api/v1 -A
  kubectl traefik match http://app.example.com/ --header X-Tenant=acme`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errorf("match is not implemented yet")
		},
	}
}
