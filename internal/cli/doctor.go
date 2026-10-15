package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/c1sc0/kubectl-traefik/internal/doctor"
)

var ErrFindings = errors.New("doctor found errors")

func newDoctorCommand(o *Options) *cobra.Command {
	var (
		output string
		opts   doctor.Options
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Find broken references in Traefik routes",
		Long: `Checks every Traefik route for missing Services, ports, Middlewares, TraefikServices,
TLS secrets and TLSOptions, cross-namespace references, and failing HTTPRoute conditions.
Exits with status 1 when any error is found.`,
		Example: `  kubectl traefik doctor -A
  kubectl traefik doctor -n web --allow-cross-namespace`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snap, err := o.load(cmd.Context(), true)
			if err != nil {
				return err
			}
			findings := doctor.Run(snap, opts)

			switch output {
			case "json":
				if findings == nil {
					findings = []doctor.Finding{}
				}
				enc := json.NewEncoder(o.IO.Out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(findings); err != nil {
					return err
				}
			case "":
				if len(findings) == 0 {
					fmt.Fprintln(o.IO.Out, "No problems found.")
					break
				}
				tw := tabwriter.NewWriter(o.IO.Out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "SEVERITY\tKIND\tOBJECT\tMESSAGE")
				for _, f := range findings {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Severity, f.Kind, f.Object, f.Message)
				}
				if err := tw.Flush(); err != nil {
					return err
				}
			default:
				return errorf("unsupported output format %q, use json", output)
			}

			if doctor.HasErrors(findings) {
				return ErrFindings
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: json")
	cmd.Flags().BoolVar(&opts.AllowCrossNamespace, "allow-cross-namespace", false, "Treat cross-namespace IngressRoute references as allowed (matches providers.kubernetesCRD.allowCrossNamespace)")
	return cmd
}
