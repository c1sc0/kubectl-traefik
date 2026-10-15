package doctor

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func messages(findings []Finding, sev Severity) []string {
	var out []string
	for _, f := range findings {
		if f.Severity == sev {
			out = append(out, f.Message)
		}
	}
	return out
}

func contains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestIngressRouteReferences(t *testing.T) {
	s := &cluster.Snapshot{
		IngressRoutes: []unstructured.Unstructured{obj(t, `
metadata: {name: app, namespace: web}
spec:
  routes:
  - match: Host(`+"`a.test`"+`)
    middlewares: [{name: auth}, {name: missing}, {name: shared-mw, namespace: infra}, {name: compress@file}]
    services: [{name: api, port: 8080}, {name: api, port: 9090}, {name: gone, port: 80}, {name: api@internal, kind: TraefikService}]
  tls: {secretName: nope, options: {name: strict}}
`)},
		Middlewares: []unstructured.Unstructured{
			obj(t, `{metadata: {name: auth, namespace: web}}`),
			obj(t, `{metadata: {name: shared-mw, namespace: infra}}`),
		},
		Services:      []unstructured.Unstructured{obj(t, `{metadata: {name: api, namespace: web}, spec: {ports: [{name: http, port: 8080}]}}`)},
		SecretNames:   []metav1.PartialObjectMetadata{{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "web"}}},
		SecretsLoaded: true,
	}

	f := Run(s, Options{})
	errs := messages(f, Error)
	for _, want := range []string{
		"Middleware web/missing not found",
		"Service web/api has no port 9090",
		"Service web/gone not found",
		"TLS secret web/nope not found",
		"TLSOption web/strict not found",
	} {
		if !contains(errs, want) {
			t.Errorf("missing error %q in %v", want, errs)
		}
	}
	if contains(errs, "web/auth") || contains(errs, "port 8080") || contains(errs, "@") {
		t.Errorf("false positive in %v", errs)
	}
	if !contains(messages(f, Warning), "allowCrossNamespace") {
		t.Errorf("expected cross-namespace warning, got %v", f)
	}
	if contains(messages(Run(s, Options{AllowCrossNamespace: true}), Warning), "allowCrossNamespace") {
		t.Errorf("cross-namespace warning should be suppressed")
	}
}

func TestIngressAnnotationMiddlewares(t *testing.T) {
	s := &cluster.Snapshot{
		IngressClasses: []unstructured.Unstructured{obj(t, `{metadata: {name: traefik}, spec: {controller: traefik.io/ingress-controller}}`)},
		Ingresses: []unstructured.Unstructured{obj(t, `
metadata:
  name: site
  namespace: my-app
  annotations:
    traefik.ingress.kubernetes.io/router.middlewares: my-app-auth@kubernetescrd,my-app-typo@kubernetescrd,noprovider
spec:
  ingressClassName: traefik
  rules: [{host: s.test, http: {paths: [{path: /, backend: {service: {name: site, port: {number: 80}}}}]}}]
`)},
		Middlewares: []unstructured.Unstructured{obj(t, `{metadata: {name: auth, namespace: my-app}}`)},
		Services:    []unstructured.Unstructured{obj(t, `{metadata: {name: site, namespace: my-app}, spec: {ports: [{port: 80}]}}`)},
	}
	errs := messages(Run(s, Options{}), Error)
	if contains(errs, "my-app-auth") {
		t.Errorf("my-app-auth should resolve: %v", errs)
	}
	if !contains(errs, "my-app-typo@kubernetescrd not found") || !contains(errs, `"noprovider"`) {
		t.Errorf("expected typo and provider errors: %v", errs)
	}
}

func TestOutOfScopeReferencesAreNotErrors(t *testing.T) {
	s := &cluster.Snapshot{
		Namespace: "web",
		IngressRoutes: []unstructured.Unstructured{obj(t, `
metadata: {name: app, namespace: web}
spec:
  routes:
  - match: Host(`+"`a.test`"+`)
    middlewares: [{name: mw, namespace: infra}]
    services: [{name: svc, namespace: infra, port: 80}]
`)},
	}
	f := Run(s, Options{AllowCrossNamespace: true})
	if HasErrors(f) {
		t.Errorf("out-of-scope refs must not be errors: %v", f)
	}
	if len(messages(f, Info)) != 2 {
		t.Errorf("expected 2 info findings, got %v", f)
	}
}

func TestHTTPRouteConditions(t *testing.T) {
	s := &cluster.Snapshot{
		HTTPRoutes: []unstructured.Unstructured{obj(t, `
metadata: {name: shop, namespace: web}
spec: {parentRefs: [{name: gw}], rules: [{backendRefs: [{name: shop, port: 80}]}]}
status:
  parents:
  - parentRef: {name: gw}
    controllerName: traefik.io/gateway-controller
    conditions:
    - {type: Accepted, status: "True"}
    - {type: ResolvedRefs, status: "False", reason: BackendNotFound, message: service web/shop not found}
`)},
	}
	errs := messages(Run(s, Options{}), Error)
	if !contains(errs, "ResolvedRefs=False (BackendNotFound)") {
		t.Errorf("expected ResolvedRefs error: %v", errs)
	}
}
