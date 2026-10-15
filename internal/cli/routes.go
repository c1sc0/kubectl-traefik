package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/c1sc0/kubectl-traefik/internal/routes"
)

func newRoutesCommand(o *Options) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:     "routes",
		Aliases: []string{"r", "route"},
		Short:   "List IngressRoutes, Ingresses and HTTPRoutes handled by Traefik",
		Example: `  kubectl traefik routes -A
  kubectl traefik routes -n web -o wide
  kubectl traefik routes -A -o json | jq '.[] | select(.tls == null)'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snap, err := o.load(cmd.Context(), false)
			if err != nil {
				return err
			}
			for _, w := range snap.Warnings {
				fmt.Fprintln(o.IO.ErrOut, "warning:", w)
			}
			list := routes.FromSnapshot(snap)
			switch output {
			case "json":
				enc := json.NewEncoder(o.IO.Out)
				enc.SetIndent("", "  ")
				if list == nil {
					list = []routes.Route{}
				}
				return enc.Encode(list)
			case "", "wide":
				return printRoutes(o.IO.Out, list, o.AllNamespaces, output == "wide")
			default:
				return errorf("unsupported output format %q, use wide or json", output)
			}
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: wide or json")
	return cmd
}

func printRoutes(w io.Writer, list []routes.Route, showNamespace, wide bool) error {
	if len(list) == 0 {
		_, err := fmt.Fprintln(w, "No Traefik routes found.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	header := []string{"KIND", "NAME", "RULE", "SERVICES", "MIDDLEWARES", "TLS"}
	if showNamespace {
		header = append([]string{"NAMESPACE"}, header...)
	}
	if wide {
		header = append(header, "ENTRYPOINTS", "PRIORITY", "PARENTS")
	}
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	for _, r := range list {
		row := []string{r.Kind, r.Name, r.Rule, refs(r.Services), refs(r.Middlewares), dash(r.TLS.String())}
		if showNamespace {
			row = append([]string{r.Namespace}, row...)
		}
		if wide {
			priority := "-"
			if r.Priority != 0 {
				priority = fmt.Sprint(r.Priority)
			}
			row = append(row, dash(strings.Join(r.EntryPoints, ",")), priority, refs(r.Parents))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func refs(list []routes.Ref) string {
	if len(list) == 0 {
		return "-"
	}
	parts := make([]string, len(list))
	for i, r := range list {
		r.Namespace = ""
		parts[i] = r.String()
	}
	return strings.Join(parts, ",")
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
