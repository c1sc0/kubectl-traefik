package cluster

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"
)

var (
	IngressRoutes = []schema.GroupVersionResource{
		{Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"},
		{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "ingressroutes"},
	}
	Middlewares = []schema.GroupVersionResource{
		{Group: "traefik.io", Version: "v1alpha1", Resource: "middlewares"},
		{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "middlewares"},
	}
	TraefikServices = []schema.GroupVersionResource{
		{Group: "traefik.io", Version: "v1alpha1", Resource: "traefikservices"},
		{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "traefikservices"},
	}
	TLSOptions = []schema.GroupVersionResource{
		{Group: "traefik.io", Version: "v1alpha1", Resource: "tlsoptions"},
		{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "tlsoptions"},
	}
	Ingresses      = []schema.GroupVersionResource{{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}}
	IngressClasses = []schema.GroupVersionResource{{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}}
	HTTPRoutes     = []schema.GroupVersionResource{{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}}
	Gateways       = []schema.GroupVersionResource{{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}}
	GatewayClasses = []schema.GroupVersionResource{{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}}
	Services       = []schema.GroupVersionResource{{Version: "v1", Resource: "services"}}
	Secrets        = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
)

type Snapshot struct {
	Namespace string

	IngressRoutes   []unstructured.Unstructured
	Middlewares     []unstructured.Unstructured
	TraefikServices []unstructured.Unstructured
	TLSOptions      []unstructured.Unstructured
	Ingresses       []unstructured.Unstructured
	IngressClasses  []unstructured.Unstructured
	HTTPRoutes      []unstructured.Unstructured
	Gateways        []unstructured.Unstructured
	GatewayClasses  []unstructured.Unstructured
	Services        []unstructured.Unstructured
	SecretNames     []metav1.PartialObjectMetadata

	SecretsLoaded bool
	Warnings      []string
}

func (s *Snapshot) InScope(namespace string) bool {
	return s.Namespace == "" || s.Namespace == namespace
}

type Loader struct {
	Dynamic  dynamic.Interface
	Metadata metadata.Interface
}

func (l *Loader) Load(ctx context.Context, namespace string, withSecrets bool) (*Snapshot, error) {
	s := &Snapshot{Namespace: namespace}

	namespaced := []struct {
		gvrs []schema.GroupVersionResource
		dst  *[]unstructured.Unstructured
	}{
		{IngressRoutes, &s.IngressRoutes},
		{Middlewares, &s.Middlewares},
		{TraefikServices, &s.TraefikServices},
		{TLSOptions, &s.TLSOptions},
		{Ingresses, &s.Ingresses},
		{HTTPRoutes, &s.HTTPRoutes},
		{Gateways, &s.Gateways},
		{Services, &s.Services},
	}
	for _, r := range namespaced {
		items, err := l.list(ctx, r.gvrs, namespace, s)
		if err != nil {
			return nil, err
		}
		*r.dst = items
	}

	clusterScoped := []struct {
		gvrs []schema.GroupVersionResource
		dst  *[]unstructured.Unstructured
	}{
		{IngressClasses, &s.IngressClasses},
		{GatewayClasses, &s.GatewayClasses},
	}
	for _, r := range clusterScoped {
		items, err := l.list(ctx, r.gvrs, "", s)
		if err != nil {
			return nil, err
		}
		*r.dst = items
	}

	if withSecrets && l.Metadata != nil {
		list, err := l.Metadata.Resource(Secrets).Namespace(namespace).List(ctx, metav1.ListOptions{})
		switch {
		case err == nil:
			s.SecretNames = list.Items
			s.SecretsLoaded = true
		case apierrors.IsForbidden(err):
			s.Warnings = append(s.Warnings, "cannot list secrets, TLS secret checks skipped")
		default:
			return nil, fmt.Errorf("list secrets: %w", err)
		}
	}

	return s, nil
}

func (l *Loader) list(ctx context.Context, gvrs []schema.GroupVersionResource, namespace string, s *Snapshot) ([]unstructured.Unstructured, error) {
	var out []unstructured.Unstructured
	for _, gvr := range gvrs {
		list, err := l.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		switch {
		case err == nil:
			out = append(out, list.Items...)
		case apierrors.IsNotFound(err):
		case apierrors.IsForbidden(err):
			s.Warnings = append(s.Warnings, fmt.Sprintf("cannot list %s, skipped", gvr.GroupResource()))
		default:
			return nil, fmt.Errorf("list %s: %w", gvr.GroupResource(), err)
		}
	}
	return out, nil
}
