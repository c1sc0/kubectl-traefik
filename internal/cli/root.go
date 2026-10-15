package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"

	"github.com/c1sc0/kubectl-traefik/internal/cluster"
)

type Options struct {
	ConfigFlags   *genericclioptions.ConfigFlags
	AllNamespaces bool
	IO            genericiooptions.IOStreams
}

func (o *Options) namespace() (string, error) {
	if o.AllNamespaces {
		return "", nil
	}
	ns, _, err := o.ConfigFlags.ToRawKubeConfigLoader().Namespace()
	return ns, err
}

func (o *Options) load(ctx context.Context, withSecrets bool) (*cluster.Snapshot, error) {
	cfg, err := o.ConfigFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	meta, err := metadata.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	ns, err := o.namespace()
	if err != nil {
		return nil, err
	}
	return (&cluster.Loader{Dynamic: dyn, Metadata: meta}).Load(ctx, ns, withSecrets)
}

func NewRootCommand(version string, streams genericiooptions.IOStreams) *cobra.Command {
	o := &Options{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
		IO:          streams,
	}

	cmd := &cobra.Command{
		Use:   "kubectl-traefik",
		Short: "Inspect and debug Traefik routing from Kubernetes objects",
		Long: `kubectl traefik reads IngressRoute, Ingress and HTTPRoute objects served by Traefik
and shows what Traefik will route, which URL matches which route, and which references are broken.`,
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "kubectl traefik",
		},
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)

	o.ConfigFlags.AddFlags(cmd.PersistentFlags())
	cmd.PersistentFlags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", false, "Query all namespaces")

	cmd.AddCommand(newRoutesCommand(o), newDoctorCommand(o), newMatchCommand(o))
	return cmd
}

func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
