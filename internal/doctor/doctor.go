package doctor

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/c1sc0/kubectl-traefik/internal/cluster"
	"github.com/c1sc0/kubectl-traefik/internal/routes"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Info    Severity = "info"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Kind     string   `json:"kind"`
	Object   string   `json:"object"`
	Message  string   `json:"message"`
}

type Options struct {
	AllowCrossNamespace bool
}

type index struct {
	snap            *cluster.Snapshot
	middlewares     map[string]bool
	traefikServices map[string]bool
	tlsOptions      map[string]bool
	secrets         map[string]bool
	services        map[string]*unstructured.Unstructured
}

func newIndex(s *cluster.Snapshot) *index {
	idx := &index{
		snap:            s,
		middlewares:     keys(s.Middlewares),
		traefikServices: keys(s.TraefikServices),
		tlsOptions:      keys(s.TLSOptions),
		secrets:         map[string]bool{},
		services:        map[string]*unstructured.Unstructured{},
	}
	for _, m := range s.SecretNames {
		idx.secrets[m.Namespace+"/"+m.Name] = true
	}
	for i := range s.Services {
		idx.services[s.Services[i].GetNamespace()+"/"+s.Services[i].GetName()] = &s.Services[i]
	}
	return idx
}

func Run(s *cluster.Snapshot, opts Options) []Finding {
	idx := newIndex(s)
	var out []Finding

	for _, w := range s.Warnings {
		out = append(out, Finding{Severity: Info, Kind: "-", Object: "-", Message: w})
	}

	seen := map[string]bool{}
	for _, r := range routes.FromSnapshot(s) {
		obj := r.Namespace + "/" + r.Name
		add := func(sev Severity, format string, args ...any) {
			f := Finding{Severity: sev, Kind: r.Kind, Object: obj, Message: fmt.Sprintf(format, args...)}
			key := string(f.Severity) + f.Kind + f.Object + f.Message
			if !seen[key] {
				seen[key] = true
				out = append(out, f)
			}
		}

		if r.Kind == routes.KindIngressRoute && r.Rule == "" {
			add(Error, "route has an empty match rule")
		}
		if len(r.Services) == 0 && r.Kind != routes.KindHTTPRoute {
			add(Error, "route has no services")
		}

		for _, svc := range r.Services {
			idx.checkService(r, svc, opts, add)
		}
		for _, mw := range r.Middlewares {
			idx.checkMiddleware(r, mw, opts, add)
		}
		if r.TLS != nil {
			idx.checkTLS(r, opts, add)
		}
	}

	for i := range s.HTTPRoutes {
		out = append(out, httpRouteConditions(&s.HTTPRoutes[i])...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return rank(out[i].Severity) < rank(out[j].Severity)
	})
	return out
}

type addFunc func(Severity, string, ...any)

func (idx *index) crossNamespace(r routes.Route, ref routes.Ref, opts Options, add addFunc) bool {
	if ref.Namespace == "" || ref.Namespace == r.Namespace {
		return false
	}
	if r.Kind == routes.KindIngressRoute && !opts.AllowCrossNamespace {
		add(Warning, "%s %s is in another namespace, Traefik ignores it unless providers.kubernetesCRD.allowCrossNamespace=true", ref.Kind, ref)
	}
	return !idx.snap.InScope(ref.Namespace)
}

func (idx *index) checkService(r routes.Route, svc routes.Ref, opts Options, add addFunc) {
	if svc.Name == "" {
		add(Error, "service reference without a name")
		return
	}
	if strings.Contains(svc.Name, "@") {
		return
	}
	if idx.crossNamespace(r, svc, opts, add) {
		add(Info, "%s %s not verified, outside namespace scope (use -A)", svc.Kind, svc)
		return
	}
	key := svc.Namespace + "/" + svc.Name
	switch svc.Kind {
	case "TraefikService":
		if !idx.traefikServices[key] {
			add(Error, "TraefikService %s not found", key)
		}
	case "Service", "":
		obj, ok := idx.services[key]
		if !ok {
			add(Error, "Service %s not found", key)
			return
		}
		if svc.Port != "" && !hasPort(obj, svc.Port) {
			add(Error, "Service %s has no port %s", key, svc.Port)
		}
		if str(obj.Object, "spec", "type") == "ExternalName" && r.Kind == routes.KindIngressRoute {
			add(Info, "Service %s is ExternalName, requires providers.kubernetesCRD.allowExternalNameServices=true", key)
		}
	}
}

func (idx *index) checkMiddleware(r routes.Route, mw routes.Ref, opts Options, add addFunc) {
	if r.Kind == routes.KindIngress {
		idx.checkAnnotationMiddleware(mw.Name, add)
		return
	}
	if (mw.Kind != "" && mw.Kind != "Middleware") || strings.Contains(mw.Name, "@") {
		return
	}
	if idx.crossNamespace(r, mw, opts, add) {
		add(Info, "Middleware %s not verified, outside namespace scope (use -A)", mw)
		return
	}
	if !idx.middlewares[mw.Namespace+"/"+mw.Name] {
		add(Error, "Middleware %s/%s not found", mw.Namespace, mw.Name)
	}
}

func (idx *index) checkAnnotationMiddleware(raw string, add addFunc) {
	name, provider, hasProvider := strings.Cut(raw, "@")
	if !hasProvider {
		add(Error, "middleware %q in router.middlewares has no provider suffix, expected <namespace>-<name>@kubernetescrd", raw)
		return
	}
	if provider != "kubernetescrd" {
		return
	}
	for key := range idx.middlewares {
		if strings.Replace(key, "/", "-", 1) == name {
			return
		}
	}
	if idx.snap.Namespace != "" && !strings.HasPrefix(name, idx.snap.Namespace+"-") {
		add(Info, "middleware %s not verified, outside namespace scope (use -A)", raw)
		return
	}
	add(Error, "middleware %s not found, expected a Middleware named <namespace>-<name>", raw)
}

func (idx *index) checkTLS(r routes.Route, opts Options, add addFunc) {
	if s := r.TLS.SecretName; s != "" && idx.snap.SecretsLoaded {
		if !idx.secrets[r.Namespace+"/"+s] {
			add(Error, "TLS secret %s/%s not found", r.Namespace, s)
		}
	}
	if o := r.TLS.Options; o != nil {
		if idx.crossNamespace(r, *o, opts, add) {
			return
		}
		if !idx.tlsOptions[o.Namespace+"/"+o.Name] {
			add(Error, "TLSOption %s/%s not found", o.Namespace, o.Name)
		}
	}
}

func httpRouteConditions(u *unstructured.Unstructured) []Finding {
	var out []Finding
	parents, _, _ := unstructured.NestedSlice(u.Object, "status", "parents")
	for _, p := range parents {
		pm, ok := p.(map[string]any)
		if !ok || str(pm, "controllerName") != routes.GatewayController {
			continue
		}
		conds, _, _ := unstructured.NestedSlice(pm, "conditions")
		for _, c := range conds {
			cm, ok := c.(map[string]any)
			if !ok || str(cm, "status") != "False" {
				continue
			}
			out = append(out, Finding{
				Severity: Error,
				Kind:     routes.KindHTTPRoute,
				Object:   u.GetNamespace() + "/" + u.GetName(),
				Message:  fmt.Sprintf("parent %s: %s=False (%s) %s", str(pm, "parentRef", "name"), str(cm, "type"), str(cm, "reason"), str(cm, "message")),
			})
		}
	}
	return out
}

func hasPort(svc *unstructured.Unstructured, port string) bool {
	ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
	for _, p := range ports {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if str(pm, "name") == port || fmt.Sprint(pm["port"]) == port {
			return true
		}
	}
	return false
}

func keys(items []unstructured.Unstructured) map[string]bool {
	out := make(map[string]bool, len(items))
	for i := range items {
		out[items[i].GetNamespace()+"/"+items[i].GetName()] = true
	}
	return out
}

func str(obj map[string]any, fields ...string) string {
	v, _, _ := unstructured.NestedString(obj, fields...)
	return v
}

func rank(s Severity) int {
	switch s {
	case Error:
		return 0
	case Warning:
		return 1
	}
	return 2
}

func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == Error {
			return true
		}
	}
	return false
}
