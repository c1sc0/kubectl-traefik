# kubectl-traefik

A kubectl plugin that shows what Traefik will route, straight from the Kubernetes API.

It reads `IngressRoute`, `Ingress` and `HTTPRoute` objects that belong to Traefik and turns them into one routing table, with the Traefik rule for each route. No port-forward to the Traefik dashboard or API is needed.

## Commands

| Command | What it does |
|---|---|
| `kubectl traefik routes` | List every Traefik route: rule, services, middlewares, TLS |
| `kubectl traefik doctor` | Find broken references and failing routes, exit 1 on errors |
| `kubectl traefik match URL` | Show which route serves a URL (planned) |

### routes

```console
$ kubectl traefik routes -A
NAMESPACE  KIND          NAME       RULE                                              SERVICES       MIDDLEWARES          TLS
shop       HTTPRoute     shop       Host(`shop.example.com`) && PathPrefix(`/cart`)   cart:8080      -                    -
web        Ingress       site       Host(`site.example.com`) && PathPrefix(`/`)       site:80        web-auth@kubernetescrd  site-tls
web        IngressRoute  api        Host(`api.example.com`) && PathPrefix(`/v1`)      api:8080       auth,ratelimit       resolver:le
```

- `-o wide` adds entry points, priority and Gateway parents.
- `-o json` prints the full route list for scripting.

Ingress and HTTPRoute rules are shown the way Traefik builds them, for example a `Prefix` path becomes `PathPrefix(...)` and a wildcard host becomes `HostRegexp(...)`.

Only routes Traefik serves are listed:

- **IngressRoute**: all, from `traefik.io` and the legacy `traefik.containo.us` group.
- **Ingress**: when its class points at an IngressClass with controller `traefik.io/ingress-controller`, or it has no class and a Traefik IngressClass is the default.
- **HTTPRoute**: when it is attached to a Gateway whose GatewayClass uses `traefik.io/gateway-controller`.

### doctor

```console
$ kubectl traefik doctor -A
SEVERITY  KIND          OBJECT                 MESSAGE
error     Ingress       shop/storefront        Service shop/storefront-v2 not found
error     IngressRoute  web/api                Middleware web/ratelimit not found
error     Ingress       ops/longhorn           TLS secret ops/longhorn-tls not found
warning   IngressRoute  web/api                Middleware shared/auth is in another namespace, Traefik ignores it unless providers.kubernetesCRD.allowCrossNamespace=true
error     HTTPRoute     shop/shop              parent public: ResolvedRefs=False (RefNotPermitted) ...
```

Checks:

- Services exist and expose the referenced port (name or number)
- TraefikServices, Middlewares and TLSOptions exist
- `router.middlewares` annotation entries resolve to a real `<namespace>-<name>@kubernetescrd` Middleware
- TLS secrets exist (read as metadata only, secret data is never fetched)
- Cross-namespace IngressRoute references, which Traefik drops unless `allowCrossNamespace` is enabled (pass `--allow-cross-namespace` if it is)
- ExternalName services, which need `allowExternalNameServices`
- HTTPRoute status conditions reported by Traefik that are `False`

References into namespaces outside the `-n` scope are reported as `info`, not errors. Use `-A` for a full check.

`doctor` exits with status 1 when it finds an error, so it can gate a CI pipeline or a Flux/Argo post-sync check.

## Install

From krew (after the first release is accepted into the index):

```sh
kubectl krew install traefik
```

From source:

```sh
make install
```

## Permissions

`list` on ingressroutes, middlewares, traefikservices, tlsoptions, ingresses, httproutes, gateways and services in the target namespaces, plus `list` on ingressclasses and gatewayclasses. `doctor` also lists secrets, through the metadata API only. Missing permissions are reported as warnings and the related checks are skipped.

## Roadmap

- `match`: evaluate Traefik rules (`Host`, `HostRegexp`, `Path`, `PathPrefix`, `PathRegexp`, `Header`, `Method`, `Query`, `ClientIP`) against a URL, apply priority and rule-length ordering, and explain why the winner won
- IngressRouteTCP and IngressRouteUDP
- Weighted and mirrored TraefikService trees
- Checks for certificates about to expire

## Development

```sh
make test
make build
./bin/kubectl-traefik routes -A
```

## License

Apache 2.0
