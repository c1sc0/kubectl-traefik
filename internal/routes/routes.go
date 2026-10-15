package routes

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/c1sc0/kubectl-traefik/internal/cluster"
)

const (
	KindIngressRoute = "IngressRoute"
	KindIngress      = "Ingress"
	KindHTTPRoute    = "HTTPRoute"

	IngressController = "traefik.io/ingress-controller"
	GatewayController = "traefik.io/gateway-controller"

	annotationMiddlewares = "traefik.ingress.kubernetes.io/router.middlewares"
	annotationEntryPoints = "traefik.ingress.kubernetes.io/router.entrypoints"
	annotationPriority    = "traefik.ingress.kubernetes.io/router.priority"
	annotationLegacyClass = "kubernetes.io/ingress.class"
	annotationDefaultIC   = "ingressclass.kubernetes.io/is-default-class"
)

type Ref struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Port      string `json:"port,omitempty"`
}

func (r Ref) String() string {
	s := r.Name
	if r.Namespace != "" {
		s = r.Namespace + "/" + s
	}
	if r.Port != "" {
		s += ":" + r.Port
	}
	if r.Kind == "TraefikService" {
		s += "(ts)"
	}
	return s
}

type TLS struct {
	SecretName   string `json:"secretName,omitempty"`
	CertResolver string `json:"certResolver,omitempty"`
	Options      *Ref   `json:"options,omitempty"`
}

func (t *TLS) String() string {
	switch {
	case t == nil:
		return ""
	case t.SecretName != "":
		return t.SecretName
	case t.CertResolver != "":
		return "resolver:" + t.CertResolver
	default:
		return "default"
	}
}

type Route struct {
	Kind        string   `json:"kind"`
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	EntryPoints []string `json:"entryPoints,omitempty"`
	Rule        string   `json:"rule"`
	Priority    int64    `json:"priority,omitempty"`
	Services    []Ref    `json:"services,omitempty"`
	Middlewares []Ref    `json:"middlewares,omitempty"`
	TLS         *TLS     `json:"tls,omitempty"`
	Parents     []Ref    `json:"parents,omitempty"`
}

func FromSnapshot(s *cluster.Snapshot) []Route {
	var out []Route
	for i := range s.IngressRoutes {
		out = append(out, FromIngressRoute(&s.IngressRoutes[i])...)
	}
	classes := TraefikIngressClasses(s.IngressClasses)
	for i := range s.Ingresses {
		if IsTraefikIngress(&s.Ingresses[i], classes) {
			out = append(out, FromIngress(&s.Ingresses[i])...)
		}
	}
	gateways := TraefikGateways(s.Gateways, s.GatewayClasses)
	for i := range s.HTTPRoutes {
		if AttachedToTraefik(&s.HTTPRoutes[i], gateways) {
			out = append(out, FromHTTPRoute(&s.HTTPRoutes[i])...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func FromIngressRoute(u *unstructured.Unstructured) []Route {
	ns := u.GetNamespace()
	entryPoints, _, _ := unstructured.NestedStringSlice(u.Object, "spec", "entryPoints")
	tls := ingressRouteTLS(u, ns)

	var out []Route
	for _, r := range slice(u.Object, "spec", "routes") {
		route := Route{
			Kind:        KindIngressRoute,
			Namespace:   ns,
			Name:        u.GetName(),
			EntryPoints: entryPoints,
			Rule:        str(r, "match"),
			TLS:         tls,
		}
		if p, ok, _ := unstructured.NestedFieldNoCopy(r, "priority"); ok {
			route.Priority = toInt64(p)
		}
		for _, m := range slice(r, "middlewares") {
			route.Middlewares = append(route.Middlewares, Ref{
				Kind:      "Middleware",
				Namespace: or(str(m, "namespace"), ns),
				Name:      str(m, "name"),
			})
		}
		for _, svc := range slice(r, "services") {
			route.Services = append(route.Services, Ref{
				Kind:      or(str(svc, "kind"), "Service"),
				Namespace: or(str(svc, "namespace"), ns),
				Name:      str(svc, "name"),
				Port:      scalar(svc, "port"),
			})
		}
		out = append(out, route)
	}
	return out
}

func ingressRouteTLS(u *unstructured.Unstructured, ns string) *TLS {
	raw, ok, _ := unstructured.NestedMap(u.Object, "spec", "tls")
	if !ok {
		return nil
	}
	tls := &TLS{
		SecretName:   str(raw, "secretName"),
		CertResolver: str(raw, "certResolver"),
	}
	if opts, ok, _ := unstructured.NestedMap(raw, "options"); ok && str(opts, "name") != "" {
		tls.Options = &Ref{Kind: "TLSOption", Namespace: or(str(opts, "namespace"), ns), Name: str(opts, "name")}
	}
	return tls
}

func FromIngress(u *unstructured.Unstructured) []Route {
	ns := u.GetNamespace()
	ann := u.GetAnnotations()

	var entryPoints []string
	if v := ann[annotationEntryPoints]; v != "" {
		entryPoints = splitTrim(v)
	}
	var middlewares []Ref
	if v := ann[annotationMiddlewares]; v != "" {
		for _, m := range splitTrim(v) {
			middlewares = append(middlewares, Ref{Kind: "Middleware", Name: m})
		}
	}
	var priority int64
	if v := ann[annotationPriority]; v != "" {
		fmt.Sscan(v, &priority)
	}

	tlsByHost := map[string]string{}
	for _, t := range slice(u.Object, "spec", "tls") {
		hosts, _, _ := unstructured.NestedStringSlice(t, "hosts")
		for _, h := range hosts {
			tlsByHost[h] = str(t, "secretName")
		}
	}

	base := Route{
		Kind:        KindIngress,
		Namespace:   ns,
		Name:        u.GetName(),
		EntryPoints: entryPoints,
		Priority:    priority,
		Middlewares: middlewares,
	}

	var out []Route
	if def, ok, _ := unstructured.NestedMap(u.Object, "spec", "defaultBackend"); ok {
		r := base
		r.Rule = "PathPrefix(`/`)"
		r.Services = []Ref{ingressBackend(def, ns)}
		out = append(out, r)
	}
	for _, rule := range slice(u.Object, "spec", "rules") {
		host := str(rule, "host")
		for _, p := range slice(rule, "http", "paths") {
			r := base
			r.Rule = ingressRule(host, str(p, "path"), str(p, "pathType"))
			if backend, ok, _ := unstructured.NestedMap(p, "backend"); ok {
				r.Services = []Ref{ingressBackend(backend, ns)}
			}
			if secret, ok := tlsByHost[host]; ok {
				r.TLS = &TLS{SecretName: secret}
			}
			out = append(out, r)
		}
	}
	return out
}

func ingressRule(host, path, pathType string) string {
	var parts []string
	if host != "" {
		if strings.HasPrefix(host, "*.") {
			parts = append(parts, fmt.Sprintf("HostRegexp(`^[a-zA-Z0-9-]+\\.%s$`)", strings.ReplaceAll(host[2:], ".", `\.`)))
		} else {
			parts = append(parts, fmt.Sprintf("Host(`%s`)", host))
		}
	}
	if path != "" {
		if pathType == "Exact" {
			parts = append(parts, fmt.Sprintf("Path(`%s`)", path))
		} else {
			parts = append(parts, fmt.Sprintf("PathPrefix(`%s`)", path))
		}
	}
	if len(parts) == 0 {
		return "PathPrefix(`/`)"
	}
	return strings.Join(parts, " && ")
}

func ingressBackend(b map[string]any, ns string) Ref {
	if res, ok, _ := unstructured.NestedMap(b, "resource"); ok {
		return Ref{Kind: str(res, "kind"), Namespace: ns, Name: str(res, "name")}
	}
	port := scalar(b, "service", "port", "number")
	if port == "" {
		port = str(b, "service", "port", "name")
	}
	return Ref{Kind: "Service", Namespace: ns, Name: str(b, "service", "name"), Port: port}
}

func FromHTTPRoute(u *unstructured.Unstructured) []Route {
	ns := u.GetNamespace()
	hostnames, _, _ := unstructured.NestedStringSlice(u.Object, "spec", "hostnames")

	var parents []Ref
	for _, p := range slice(u.Object, "spec", "parentRefs") {
		parents = append(parents, Ref{
			Kind:      or(str(p, "kind"), "Gateway"),
			Namespace: or(str(p, "namespace"), ns),
			Name:      str(p, "name"),
			Port:      or(str(p, "sectionName"), scalar(p, "port")),
		})
	}

	var out []Route
	for _, rule := range slice(u.Object, "spec", "rules") {
		r := Route{
			Kind:      KindHTTPRoute,
			Namespace: ns,
			Name:      u.GetName(),
			Rule:      httpRouteRule(hostnames, slice(rule, "matches")),
			Parents:   parents,
		}
		for _, b := range slice(rule, "backendRefs") {
			r.Services = append(r.Services, Ref{
				Kind:      or(str(b, "kind"), "Service"),
				Namespace: or(str(b, "namespace"), ns),
				Name:      str(b, "name"),
				Port:      scalar(b, "port"),
			})
		}
		for _, f := range slice(rule, "filters") {
			if str(f, "type") == "ExtensionRef" {
				r.Middlewares = append(r.Middlewares, Ref{
					Kind:      str(f, "extensionRef", "kind"),
					Namespace: ns,
					Name:      str(f, "extensionRef", "name"),
				})
			}
		}
		out = append(out, r)
	}
	return out
}

func httpRouteRule(hostnames []string, matches []map[string]any) string {
	var hostExpr string
	if len(hostnames) > 0 {
		hosts := make([]string, len(hostnames))
		for i, h := range hostnames {
			hosts[i] = fmt.Sprintf("Host(`%s`)", h)
		}
		hostExpr = strings.Join(hosts, " || ")
		if len(hosts) > 1 {
			hostExpr = "(" + hostExpr + ")"
		}
	}

	var matchExprs []string
	for _, m := range matches {
		var parts []string
		if p, ok, _ := unstructured.NestedMap(m, "path"); ok {
			value := or(str(p, "value"), "/")
			switch str(p, "type") {
			case "Exact":
				parts = append(parts, fmt.Sprintf("Path(`%s`)", value))
			case "RegularExpression":
				parts = append(parts, fmt.Sprintf("PathRegexp(`%s`)", value))
			default:
				parts = append(parts, fmt.Sprintf("PathPrefix(`%s`)", value))
			}
		}
		if method := str(m, "method"); method != "" {
			parts = append(parts, fmt.Sprintf("Method(`%s`)", method))
		}
		for _, h := range slice(m, "headers") {
			parts = append(parts, fmt.Sprintf("Header(`%s`, `%s`)", str(h, "name"), str(h, "value")))
		}
		if len(parts) > 0 {
			matchExprs = append(matchExprs, strings.Join(parts, " && "))
		}
	}

	var matchExpr string
	switch len(matchExprs) {
	case 0:
	case 1:
		matchExpr = matchExprs[0]
	default:
		matchExpr = "((" + strings.Join(matchExprs, ") || (") + "))"
	}

	switch {
	case hostExpr != "" && matchExpr != "":
		return hostExpr + " && " + matchExpr
	case hostExpr != "":
		return hostExpr
	case matchExpr != "":
		return matchExpr
	default:
		return "PathPrefix(`/`)"
	}
}

type IngressClassSet struct {
	Names     map[string]bool
	IsDefault bool
}

func TraefikIngressClasses(classes []unstructured.Unstructured) IngressClassSet {
	set := IngressClassSet{Names: map[string]bool{}}
	for i := range classes {
		c := &classes[i]
		if str(c.Object, "spec", "controller") != IngressController {
			continue
		}
		set.Names[c.GetName()] = true
		if c.GetAnnotations()[annotationDefaultIC] == "true" {
			set.IsDefault = true
		}
	}
	return set
}

func IsTraefikIngress(u *unstructured.Unstructured, classes IngressClassSet) bool {
	class := str(u.Object, "spec", "ingressClassName")
	if class == "" {
		class = u.GetAnnotations()[annotationLegacyClass]
	}
	if class == "" {
		return classes.IsDefault
	}
	if len(classes.Names) == 0 {
		return strings.Contains(class, "traefik")
	}
	return classes.Names[class]
}

func TraefikGateways(gateways, classes []unstructured.Unstructured) map[string]bool {
	traefikClasses := map[string]bool{}
	for i := range classes {
		if str(classes[i].Object, "spec", "controllerName") == GatewayController {
			traefikClasses[classes[i].GetName()] = true
		}
	}
	out := map[string]bool{}
	for i := range gateways {
		g := &gateways[i]
		if traefikClasses[str(g.Object, "spec", "gatewayClassName")] {
			out[g.GetNamespace()+"/"+g.GetName()] = true
		}
	}
	return out
}

func AttachedToTraefik(u *unstructured.Unstructured, gateways map[string]bool) bool {
	for _, p := range slice(u.Object, "spec", "parentRefs") {
		if kind := str(p, "kind"); kind != "" && kind != "Gateway" {
			continue
		}
		if gateways[or(str(p, "namespace"), u.GetNamespace())+"/"+str(p, "name")] {
			return true
		}
	}
	for _, p := range slice(u.Object, "status", "parents") {
		if str(p, "controllerName") == GatewayController {
			return true
		}
	}
	return false
}

func slice(obj map[string]any, fields ...string) []map[string]any {
	raw, ok, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(obj map[string]any, fields ...string) string {
	v, _, _ := unstructured.NestedString(obj, fields...)
	return v
}

func scalar(obj map[string]any, fields ...string) string {
	raw, ok, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	if !ok || raw == nil {
		return ""
	}
	return fmt.Sprint(raw)
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func or(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func splitTrim(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
