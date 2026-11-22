package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// NewServiceTokenCmd returns the command that prints a short-lived token for one service.
func NewServiceTokenCmd() *cobra.Command {
	var service string

	cmd := &cobra.Command{
		Use:   "service-token",
		Short: "Print a short-lived access token for a service API",
		Long: `Exchange the stored catalog session (see 'catalog login') for a short-lived token
that a service API, such as Digitize, accepts.

The token is printed to stdout only, so it can be used as $(...). It is never written to disk.
Tokens expire after at most an hour (the catalog's --service-token-ttl, 1 hour by default);
request a new one when needed.`,
		Example: `  curl -H "Authorization: Bearer $(ai-services catalog service-token --service digitize)" $DIGITIZE_URL/v1/documents`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			c, err := client.New(cmd.Context())
			if err != nil {
				return fmt.Errorf("not logged in (run 'ai-services catalog login'): %w", err)
			}

			resp, err := c.ServiceToken(cmd.Context(), service)
			if err != nil {
				return err
			}

			// Token on stdout; nothing else, so command substitution stays clean.
			_, err = fmt.Fprintln(cmd.OutOrStdout(), resp.Token)

			return err
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "Service to request a token for, for example digitize (required)")
	_ = cmd.MarkFlagRequired("service")

	return cmd
}
