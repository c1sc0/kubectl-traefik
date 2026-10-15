package routes

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/c1sc0/kubectl-traefik/internal/cluster"
)

func obj(t *testing.T, doc string) unstructured.Unstructured {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return unstructured.Unstructured{Object: m}
}

func TestFromIngressRoute(t *testing.T) {
	u := obj(t, `
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata: {name: app, namespace: web}
spec:
  entryPoints: [websecure]
  routes:
  - match: Host(`+"`app.example.com`"+`) && PathPrefix(`+"`/api`"+`)
    priority: 20
    middlewares:
    - name: auth
    - name: strip
      namespace: shared
    services:
    - name: api
      port: 8080
    - name: canary
      kind: TraefikService
  tls:
    secretName: app-tls
    options: {name: modern}
`)
	got := FromIngressRoute(&u)
	if len(got) != 1 {
		t.Fatalf("want 1 route, got %d", len(got))
	}
	r := got[0]
	if r.Rule != "Host(`app.example.com`) && PathPrefix(`/api`)" || r.Priority != 20 {
		t.Errorf("rule/priority: %q %d", r.Rule, r.Priority)
	}
	if len(r.Middlewares) != 2 || r.Middlewares[0].Namespace != "web" || r.Middlewares[1].Namespace != "shared" {
		t.Errorf("middlewares: %+v", r.Middlewares)
	}
	if r.Services[0].Port != "8080" || r.Services[1].Kind != "TraefikService" {
		t.Errorf("services: %+v", r.Services)
	}
	if r.TLS.SecretName != "app-tls" || r.TLS.Options.Namespace != "web" {
		t.Errorf("tls: %+v", r.TLS)
	}
}

func TestFromIngress(t *testing.T) {
	u := obj(t, `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: site
  namespace: web
  annotations:
    traefik.ingress.kubernetes.io/router.middlewares: web-auth@kubernetescrd, web-gzip@kubernetescrd
spec:
  ingressClassName: traefik
  tls:
  - hosts: [site.example.com]
    secretName: site-tls
  rules:
  - host: site.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend: {service: {name: site, port: {number: 80}}}
      - path: /health
        pathType: Exact
        backend: {service: {name: site, port: {name: http}}}
  - host: "*.example.com"
    http:
      paths:
      - path: /
        pathType: Prefix
        backend: {service: {name: wildcard, port: {number: 80}}}
`)
	got := FromIngress(&u)
	if len(got) != 3 {
		t.Fatalf("want 3 routes, got %d", len(got))
	}
	want := []string{
		"Host(`site.example.com`) && PathPrefix(`/`)",
		"Host(`site.example.com`) && Path(`/health`)",
		"HostRegexp(`^[a-zA-Z0-9-]+\\.example\\.com$`) && PathPrefix(`/`)",
	}
	for i, w := range want {
		if got[i].Rule != w {
			t.Errorf("route %d rule = %q, want %q", i, got[i].Rule, w)
		}
	}
	if got[0].TLS == nil || got[0].TLS.SecretName != "site-tls" || got[2].TLS != nil {
		t.Errorf("tls mapping wrong: %+v %+v", got[0].TLS, got[2].TLS)
	}
	if got[1].Services[0].Port != "http" || len(got[0].Middlewares) != 2 {
		t.Errorf("services/middlewares: %+v %+v", got[1].Services, got[0].Middlewares)
	}
}

func TestFromHTTPRoute(t *testing.T) {
	u := obj(t, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: {name: shop, namespace: web}
spec:
  parentRefs: [{name: public, namespace: gw}]
  hostnames: [shop.example.com, www.shop.example.com]
  rules:
  - matches:
    - path: {type: PathPrefix, value: /cart}
      method: POST
    - path: {type: Exact, value: /}
    backendRefs: [{name: shop, port: 8080}]
`)
	got := FromHTTPRoute(&u)
	if len(got) != 1 {
		t.Fatalf("want 1 route, got %d", len(got))
	}
	want := "(Host(`shop.example.com`) || Host(`www.shop.example.com`)) && ((PathPrefix(`/cart`) && Method(`POST`)) || (Path(`/`)))"
	if got[0].Rule != want {
		t.Errorf("rule = %q\nwant  %q", got[0].Rule, want)
	}
	if got[0].Parents[0].Namespace != "gw" || got[0].Services[0].Port != "8080" {
		t.Errorf("parents/services: %+v %+v", got[0].Parents, got[0].Services)
	}
}

func TestFromSnapshotFiltersByController(t *testing.T) {
	s := &cluster.Snapshot{
		IngressClasses: []unstructured.Unstructured{
			obj(t, `{metadata: {name: traefik}, spec: {controller: traefik.io/ingress-controller}}`),
			obj(t, `{metadata: {name: nginx}, spec: {controller: k8s.io/ingress-nginx}}`),
		},
		Ingresses: []unstructured.Unstructured{
			obj(t, `{metadata: {name: a, namespace: x}, spec: {ingressClassName: traefik, rules: [{host: a.test, http: {paths: [{path: /, backend: {service: {name: a, port: {number: 80}}}}]}}]}}`),
			obj(t, `{metadata: {name: b, namespace: x}, spec: {ingressClassName: nginx, rules: [{host: b.test, http: {paths: [{path: /, backend: {service: {name: b, port: {number: 80}}}}]}}]}}`),
		},
		GatewayClasses: []unstructured.Unstructured{
			obj(t, `{metadata: {name: traefik}, spec: {controllerName: traefik.io/gateway-controller}}`),
		},
		Gateways: []unstructured.Unstructured{
			obj(t, `{metadata: {name: gw, namespace: x}, spec: {gatewayClassName: traefik}}`),
			obj(t, `{metadata: {name: other, namespace: x}, spec: {gatewayClassName: envoy}}`),
		},
		HTTPRoutes: []unstructured.Unstructured{
			obj(t, `{metadata: {name: c, namespace: x}, spec: {parentRefs: [{name: gw}], rules: [{backendRefs: [{name: c, port: 80}]}]}}`),
			obj(t, `{metadata: {name: d, namespace: x}, spec: {parentRefs: [{name: other}], rules: [{backendRefs: [{name: d, port: 80}]}]}}`),
		},
	}
	got := FromSnapshot(s)
	names := map[string]bool{}
	for _, r := range got {
		names[r.Name] = true
	}
	if !names["a"] || !names["c"] || names["b"] || names["d"] {
		t.Errorf("unexpected routes: %v", names)
	}
}
