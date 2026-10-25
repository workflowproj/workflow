# Deployment

`wfm` ships as one container image that runs in three modes, and one Helm chart that deploys
them the same way on k3s and on EKS. Only values change between clusters. Argo CD is the
expected way to apply it, but plain `helm` works too.

```
                 internet                              corporate network / VPN
                    │                                            │
        public LB / Traefik / ALB                   internal LB / Traefik / ALB
          (only /hooks, /scim)                       (UI, /api, /auth, SSE)
                    │                                            │
            ┌───────▼───────┐                            ┌───────▼───────┐
            │  wfm-hooks    │                            │   wfm-web     │──► IdPs (OIDC), SCM APIs, S3
            │ HPA, PDB      │                            │ HPA, PDB      │
            └───────┬───────┘                            └───────┬───────┘
                    │        ┌────────────────────┐              │
                    └───────►│ PostgreSQL 17      │◄─────────────┘
                             │ CloudNativePG      │◄──── KEDA (queue depth)
                             │ WAL → S3           │
                             └─────────▲──────────┘
                                       │
                               ┌───────┴───────┐
                               │  wfm-worker   │──► SCM APIs, outbound webhooks, Google directory, S3
                               │ KEDA, PDB     │
                               └───────────────┘
```

| Component | Command | Serves | Exposure |
|---|---|---|---|
| web | `wfm serve --mode=web` | UI, `/api/v1`, `/auth/*`, SSE at `/api/v1/events` on :8080 | internal ingress |
| hooks | `wfm serve --mode=hooks` | `POST /hooks/{provider}/{connectionId}`, `/scim/v2/*` on :8080 | **the only public ingress** |
| worker | `wfm serve --mode=worker` | River job workers | none |
| migrate | `wfm migrate` | schema migrations (Job) | none |

Every mode serves `/metrics`, `/healthz` and `/readyz` on :9090; web and hooks also serve
`/healthz` (liveness) and `/readyz` (pings the database) on :8080. Configuration is environment
variables only ([configuration.md](configuration.md)); logs are JSON on stdout.

## Artifacts

Each `vX.Y.Z` tag runs `.github/workflows/release.yml`:

| Artifact | Where |
|---|---|
| Image (linux/amd64 + linux/arm64, distroless `static-debian12:nonroot`, UID 65532) | `ghcr.io/geoffo-dev/workflow-management:X.Y.Z` |
| Keyless Cosign signature, SPDX SBOM attestation (Syft), SLSA provenance (BuildKit) | same repository |
| Helm chart (OCI, Cosign-signed) | `oci://ghcr.io/geoffo-dev/charts/wfm` version `X.Y.Z` |
| Offline bundles (amd64, arm64), SBOM, checksums | GitHub release assets |

Verify before deploying (or enforce it with Kyverno / the Sigstore policy-controller):

```sh
IMG=ghcr.io/geoffo-dev/workflow-management@sha256:...
cosign verify "$IMG" \
  --certificate-identity-regexp '^https://github.com/geoffo-dev/workflow-management/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
cosign verify-attestation "$IMG" --type spdxjson \
  --certificate-identity-regexp '^https://github.com/geoffo-dev/workflow-management/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Pin `image.digest` in production; the tag is then ignored.

Build locally with `make image` (or `docker build --build-arg VERSION=dev .`). The Dockerfile
builds the UI in a Node 22 stage, embeds it in a static Go binary (`CGO_ENABLED=0`, `-trimpath`,
version stamped through `-ldflags`), and copies only the binary into distroless.
`--build-arg UI_STAGE=ui-none` skips the UI (the binary then serves a "UI not built" page).

## Prerequisites

| | k3s | EKS |
|---|---|---|
| Kubernetes | 1.30+ (the chart uses `preStop.sleep`) | 1.30+ |
| CloudNativePG operator | 1.30.x (`kubectl apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.30/releases/cnpg-1.30.1.yaml`, or its Helm chart) | same |
| Ingress | bundled Traefik | AWS Load Balancer Controller (or Envoy Gateway via `gatewayAPI.*`) |
| TLS | cert-manager (DNS-01 for internal names) | ACM certificates on the ALBs |
| Secrets | SOPS (helm-secrets / KSOPS) or a pre-created Secret | External Secrets Operator ≥ 0.17 (`external-secrets.io/v1`) |
| Worker autoscaling | optional (fixed replicas, or a CPU HPA) | KEDA 2.x |
| NetworkPolicy enforcement | built in | VPC CNI with `enableNetworkPolicy: "true"` (or Cilium/Calico) |
| Storage | `local-path` | EBS CSI driver with a `gp3` StorageClass |
| Metrics | optional | Prometheus Operator (`serviceMonitor.enabled`), with a Grafana dashboard (`grafanaDashboard.enabled`) and alerts (`prometheusRule.enabled`) |

Every CRD-backed template (CNPG `Cluster`, KEDA, `ExternalSecret`, `ServiceMonitor`, `PrometheusRule`, `HTTPRoute`)
is off unless enabled by values, except the CNPG cluster, which is on by default. Disable it with
`database.cnpg.enabled=false` and point at another PostgreSQL (below).

## The chart

`deploy/helm/wfm` — the main values (see `values.yaml` for every option; `values.schema.json`
rejects typos):

| Values | Purpose |
|---|---|
| `image.*` | repository, tag (defaults to the chart's appVersion), digest |
| `config` | non-secret env → ConfigMap loaded by every pod (`WFM_*`, `OTEL_*`) |
| `secrets.*` | secret env: `existingSecret`, a chart-created Secret, or an `ExternalSecret` |
| `database.cnpg.*` / `database.external.*` | CloudNativePG cluster, or any PostgreSQL 17 |
| `web`, `hooks`, `worker` | replicas, resources, `autoscaling` (HPA), `worker.keda`, `pdb`, `topologySpread`, scheduling, probes, extra env/volumes |
| `migrations.*` | the `wfm migrate` Job and its hook wiring |
| `ingress.web`, `ingress.hooks` | two Ingresses: internal UI/API and public `/hooks` + `/scim` |
| `gatewayAPI.web`, `gatewayAPI.hooks` | the same as Gateway API `HTTPRoute`s |
| `networkPolicy.*` | default-deny ingress, per-component ingress/egress, database ingress |
| `serviceMonitor.*` | Prometheus Operator scraping |
| `grafanaDashboard.*` | the "Workflow" Grafana dashboard as a ConfigMap for the kube-prometheus-stack sidecar (label `grafana_dashboard: "1"`): traffic, 5xx ratio, latency and slowest routes, request limits, background jobs, Git webhooks, live streams, CPU/memory/goroutines. The JSON is also in `deploy/helm/wfm/dashboards/wfm.json` to import by hand |
| `prometheusRule.*` | alerts: high server-error ratio, slow requests (p95), failing jobs, failing Git webhooks, a component with no healthy target; thresholds in values |
| `extraObjects` | extra manifests (Traefik middlewares, SecretStores, ...) |

Example overlays: `values-k3s.yaml` (one node, one CNPG instance, Traefik, SOPS) and
`values-eks.yaml` (3 CNPG instances across zones with WAL archiving to S3, ALBs, External Secrets,
KEDA, ServiceMonitor). `deploy/k3d/values-dev.yaml` is the development/CI profile.

Pods run as UID 65532 with a read-only root filesystem (an `emptyDir` on `/tmp`), no capabilities,
`RuntimeDefault` seccomp, no service account token, and `GOMEMLIMIT` at 90% of the memory limit.

## Database

### CloudNativePG (default)

The chart creates a `postgresql.cnpg.io/v1` `Cluster` named `<release>-db` (`database.cnpg.name`)
with database and owner `wfm`. The operator generates the Secret `<cluster>-app`; the app, the
migration job and KEDA read `WFM_DATABASE_URL` from its `uri` key. The Cluster carries
`helm.sh/resource-policy: keep` and `argocd.argoproj.io/sync-options: Prune=false,Delete=false`,
so deleting the release or the Argo CD Application never deletes the database.

- k3s: `instances: 1` on `local-path`. Single-node means no failover; enable backups.
- EKS: `instances: 3` with required zone anti-affinity, separate WAL volumes, synchronous
  failover handled by CNPG; PodDisruptionBudgets come from the operator.
- Connections: each app pod opens up to 10 connections (more on large nodes). Size
  `max_connections` for `(web + hooks + worker max replicas) × 10 + 20`. The web pods hold a
  `LISTEN` connection for live events, so do not put a transaction-mode PgBouncer (CNPG `Pooler`)
  in front of them.

### Backups and restore

`database.cnpg.backup` turns on continuous WAL archiving plus a daily base backup
(`ScheduledBackup`, six-field cron) to S3 or any S3-compatible store:

- `method: barmanObjectStore` uses CNPG's in-tree Barman Cloud support (deprecated upstream since
  1.26 but still served); it needs a `*-system-*` PostgreSQL image (the default).
- `method: plugin` uses the Barman Cloud Plugin: install the plugin, then the chart creates a
  `barmancloud.cnpg.io/v1` `ObjectStore` and wires it as the WAL archiver.
- Credentials: on EKS use IRSA (`s3Credentials.inheritFromIAMRole: true` and
  `serviceAccountAnnotations: {eks.amazonaws.com/role-arn: ...}`); elsewhere a Secret with access
  keys (`s3Credentials.secretName`), plus `endpointURL` for MinIO/Garage/SeaweedFS.

Check with `kubectl cnpg status <cluster>` or `kubectl get backups`. To restore (whole cluster or
point in time), bootstrap a **new** cluster from the object store, then switch the app to it:

```yaml
database:
  cnpg:
    name: wfm-db-restored
    bootstrap:
      recovery:
        source: origin
        recoveryTarget: {targetTime: "2026-10-20T09:00:00Z"}   # omit for the latest WAL
    externalClusters:
      - name: origin
        barmanObjectStore:       # same destinationPath/s3Credentials as the original cluster
          destinationPath: s3://example-wfm-backups/prod
          serverName: wfm-db
          s3Credentials: {inheritFromIAMRole: true}
```

Give the restored cluster a different backup `destinationPath` (or `serverName`) so the two
clusters never archive into the same place.

### External PostgreSQL

Set `database.cnpg.enabled=false` and one of:

- `database.external.existingSecret` / `existingSecretKey`: a Secret holding the URL (RDS via
  External Secrets, for example);
- `database.external.url`: rendered into a chart Secret (development);
- nothing: the app Secret (`secrets.*`) must contain `WFM_DATABASE_URL`.

With NetworkPolicies on, list the database's addresses in `networkPolicy.egress.database.to`.

## Secrets

All keys of the app Secret become environment variables (`envFrom`). It must contain
`WFM_SECRET_KEY` (32+ characters; keep it stable, it encrypts stored SCM keys and webhook secrets)
and whatever else is secret: `WFM_GOOGLE_CLIENT_SECRET`, `WFM_OIDC_<ID>_CLIENT_SECRET`,
`WFM_GOOGLE_SERVICE_ACCOUNT_KEY`, `WFM_S3_ACCESS_KEY`/`WFM_S3_SECRET_KEY` (omit with IRSA).

- **Production (EKS): External Secrets.** `secrets.externalSecret.enabled=true` with a
  `ClusterSecretStore` (AWS Secrets Manager, Vault, ...). `dataFrom.extract` maps a JSON document
  to env vars; `data` maps individual keys.
- **k3s: SOPS.** Keep `secrets.values` in a SOPS-encrypted file next to your values and let Argo CD
  decrypt it (the helm-secrets plugin with a `secrets://` value file, or KSOPS), or
  `helm secrets upgrade ...` from a workstation. Alternatively create the Secret yourself
  (Sealed Secrets, `kubectl create secret`) and set `secrets.existingSecret`.
- `secrets.create` with plain values is for development only. With `WFM_ENV=production` the chart
  refuses to render without `WFM_SECRET_KEY`.

Pods roll automatically when chart-rendered config or the chart-created Secret changes. For
externally managed Secrets, add Stakater Reloader (`podAnnotations`) or restart the Deployments.

## Migrations and upgrades

`wfm migrate` applies the goose migrations, then River's queue schema. It retries the connection
for about a minute, and the Job retries after that (`migrations.backoffLimit`).

Migrations are **expand, then contract**: a release only adds (tables, nullable columns, indexes
built concurrently, new enum values) and backfills in jobs; it removes or tightens things in a
later release, once no running version uses them. So the new schema is applied while the old
pods are still serving, then the Deployments roll with `maxUnavailable: 0`, and a rollback of the
Deployments is always safe (the schema is never rolled back).

How the Job is ordered:

| Tool | Wiring | Order |
|---|---|---|
| Argo CD | `argocd.argoproj.io/hook: Sync`, sync wave `-1`, `BeforeHookCreation` | wave −3: ServiceAccount, ConfigMap, Secrets/ExternalSecret, NetworkPolicies → wave −2: CNPG `Cluster` (Argo CD waits until Healthy) → wave −1: migration Job (must succeed) → wave 0: Deployments, Services, Ingresses, autoscalers |
| Helm | `helm.sh/hook: post-install,pre-upgrade` | first install: after all objects exist; upgrades: before anything changes |

Why not a `PreSync` hook: PreSync runs before *any* object of the release exists, so on the first
install the job would wait forever for a database and Secret that this same release creates. A
Sync-phase hook in an early wave gives the same guarantee (no pod rolls before the schema is
migrated, and a failed migration stops the sync) and also works on day one. When the database and
the Secret are managed outside the release (another Application, RDS), `migrations.argocd.hook:
PreSync` is fine. Argo CD ignores the `helm.sh/hook` annotations because the Argo CD ones are set.

Argo CD must know when the CNPG `Cluster` is healthy. Recent Argo CD versions include a health
check for `postgresql.cnpg.io/Cluster`; if yours reports it Healthy straight away, add:

```yaml
# argocd-cm
data:
  resource.customizations.health.postgresql.cnpg.io_Cluster: |
    hs = {status = "Progressing", message = "Waiting for the cluster"}
    if obj.status ~= nil and obj.status.conditions ~= nil then
      for _, c in ipairs(obj.status.conditions) do
        if c.type == "Ready" then
          if c.status == "True" then hs.status = "Healthy" end
          hs.message = c.message or obj.status.phase
        end
      end
    end
    return hs
```

With plain Helm, `--wait` works on the first install too: pods retry the database connection
while it comes up, and the worker waits (Ready, but idle) until the post-install migration has
created its tables, then starts processing jobs.

## Traffic

Only `hooks` faces the internet, and it serves nothing but webhook deliveries, SCIM and the
signed one-click unsubscribe links of notification emails (`/hooks/email/unsubscribe`, which
mail providers call from the internet). The UI, the API and OIDC sign-in are on `web` behind an
internal entry point. Set `WFM_PUBLIC_URL` to the internal URL and `WFM_HOOKS_PUBLIC_URL` to the
public one: webhook URLs shown to admins, the SCIM base URL and unsubscribe links use the latter.

Server-sent events (`/api/v1/events`) need a proxy that streams: no response buffering and an
idle timeout above the 25-second keep-alive the server sends. The server also sets
`X-Accel-Buffering: no`.

**k3s (Traefik).** `values-k3s.yaml` routes both hosts through the bundled Traefik. Only the hooks
host is in public DNS / port-forwarded; the web router gets an `ipAllowList` middleware limiting
it to private ranges (declared via `extraObjects`). For stronger separation run a second Traefik
on another address and give it its own IngressClass. Traefik streams SSE without extra settings.
TLS comes from cert-manager (`ingress.*.certManager.clusterIssuer`; internal names need DNS-01).

**EKS (AWS Load Balancer Controller).** Two ALBs: `scheme: internal` for web, `internet-facing`
(with WAF) for hooks, IP targets, ACM certificates, `healthcheck-path: /readyz`. The web ALB gets a
3600 s idle timeout for SSE. Label the namespace `elbv2.k8s.aws/pod-readiness-gate-inject=enabled`
so rollouts wait for target registration; `preStopSleepSeconds: 15` keeps terminating pods serving
while the ALB deregisters them.

**Gateway API.** `gatewayAPI.web` / `gatewayAPI.hooks` render `HTTPRoute`s attached to Gateways
you own (`parentRefs`; listeners and certificates live on the Gateway). The web route gives
`/api/v1/events` `timeouts.request: 0s`, because Envoy-based implementations otherwise cut
streams at their default route timeout.

ingress-nginx users: add `nginx.ingress.kubernetes.io/proxy-buffering: "off"` and
`proxy-read-timeout: "3600"` to `ingress.web.annotations`.

## NetworkPolicies

With `networkPolicy.enabled` (default) the chart renders:

| Policy | Allows |
|---|---|
| `<release>-default-deny-ingress` | nothing: every pod in the namespace denies ingress unless allowed below |
| `<release>-web` | :8080 from `networkPolicy.web.ingressFrom` (internal ingress controller); :9090 from `metrics.from`; egress DNS, database, HTTPS (OIDC token exchange, SCM APIs, S3) |
| `<release>-hooks` | :8080 from `networkPolicy.hooks.ingressFrom` (**public** controller only); :9090 from `metrics.from`; egress DNS and database only |
| `<release>-worker` | :9090 from `metrics.from`; egress DNS, database, HTTPS |
| `<release>-migrate` | no ingress; egress DNS and database |
| `<release>-database` (CNPG) | :5432 from the app pods and the KEDA namespace, :8000/:5432 from the CNPG operator namespace, everything between the cluster's own pods, :9187 from `metrics.from` |

HTTPS egress is `0.0.0.0/0` on 443 except the instance metadata endpoint; add rules with
`networkPolicy.egress.extraRules` (OTLP collector, a self-hosted GitLab on another port, EKS Pod
Identity's agent at `169.254.170.23:80`). Ingress peers per cluster:

- k3s: Traefik pods in `kube-system` (`values-k3s.yaml`).
- EKS with ALB IP targets: traffic comes from the load balancers' ENIs, so allow the subnets each
  ALB lives in: private subnets for the internal ALB → web, public subnets for the internet-facing
  ALB → hooks (`values-eks.yaml`).

## Scaling and availability

- **web, hooks:** `autoscaling/v2` HPAs on CPU (memory optional). When an autoscaler is enabled
  the chart omits `replicas`, so Argo CD does not fight the HPA.
- **worker:** with `worker.keda.enabled`, a KEDA `ScaledObject` runs
  `SELECT count(*) FROM river_job WHERE state = 'available' AND scheduled_at <= now()` through
  KEDA's PostgreSQL scaler (connection string from the same Secret as the app, via a
  `TriggerAuthentication`) and targets `targetQueryValue` runnable jobs per replica. It never
  scales below 1: the River leader that enqueues periodic jobs (directory sync, SCM reconcile)
  runs in a worker. `fallback` holds a fixed replica count if the query fails; without KEDA,
  `worker.autoscaling` is a plain CPU HPA.
- **Disruption and placement:** PDBs (`maxUnavailable: 1`), topology spread across zones and
  nodes (`ScheduleAnyway`; switch to `DoNotSchedule` for hard spreading), rolling updates with
  `maxUnavailable: 0`.
- **Shutdown:** web/hooks sleep `preStopSleepSeconds`, then the binary drains for up to 25 s;
  the worker stops fetching and waits for running jobs. Grace periods are set accordingly.

## Deploying with Argo CD

k3s, chart from this repository, secrets decrypted by the helm-secrets plugin:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: wfm
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/geoffo-dev/workflow-management
    targetRevision: v1.2.3
    path: deploy/helm/wfm
    helm:
      valueFiles:
        - values-k3s.yaml
        - secrets://../../../clusters/home/wfm-secrets.enc.yaml   # SOPS
  destination:
    server: https://kubernetes.default.svc
    namespace: wfm
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions: [CreateNamespace=true]
```

EKS, signed OCI chart plus values from a GitOps repository (register `ghcr.io/geoffo-dev/charts`
as a Helm repository with `enableOCI: "true"`):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: wfm
  namespace: argocd
spec:
  project: platform
  sources:
    - repoURL: ghcr.io/geoffo-dev/charts
      chart: wfm
      targetRevision: 1.2.3
      helm:
        valueFiles:
          - $gitops/clusters/prod-eks/wfm/values.yaml   # based on values-eks.yaml
    - repoURL: https://github.com/example/gitops
      targetRevision: main
      ref: gitops
  destination:
    server: https://kubernetes.default.svc
    namespace: wfm
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions: [CreateNamespace=true]
```

Install the operators (CNPG, KEDA, External Secrets, AWS Load Balancer Controller,
cert-manager) as their own Applications in earlier waves of an app-of-apps.

## k3s step by step

```sh
# 1. operator
kubectl apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.30/releases/cnpg-1.30.1.yaml
# 2. values: copy values-k3s.yaml, set hosts, URLs and sign-in; encrypt the secrets with SOPS
# 3. install (or create the Argo CD Application above)
helm upgrade --install wfm deploy/helm/wfm -n wfm --create-namespace \
  -f my-values.yaml -f secrets://my-secrets.enc.yaml
kubectl -n wfm logs job/wfm-migrate
kubectl -n wfm get cluster.postgresql.cnpg.io,pods
```

## EKS step by step

1. Cluster add-ons: AWS Load Balancer Controller, EBS CSI driver + `gp3` StorageClass, VPC CNI
   with network policy enabled, CloudNativePG, KEDA, External Secrets with a
   `ClusterSecretStore`, Prometheus Operator.
2. IAM: an IRSA role for the CNPG instances (S3 backup bucket) and one for the app (attachments
   bucket), referenced in `database.cnpg.serviceAccountAnnotations` and
   `serviceAccount.annotations`.
3. Secrets Manager: a JSON secret with `WFM_SECRET_KEY` and the IdP client secret(s).
4. ACM certificates for both hosts; DNS for the internal host in a private zone.
5. Adapt `values-eks.yaml` (ARNs, subnets CIDRs, hosts, buckets) and create the Application.

## Air-gapped installs

Each release attaches `wfm-offline-X.Y.Z-linux-{amd64,arm64}.tar.gz`, built by
`scripts/bundle.sh`: every image the install needs (`docker save`, zstd/gzip, ready for the k3s
airgap images directory or `k3s ctr images import`), the packaged chart, the CNPG operator
manifest with `imagePullPolicy: IfNotPresent`, a k3s `HelmChart` manifest with the chart embedded
(installs through k3s' helm-controller without the helm CLI), the example values, the `wfm` CLI,
`SHA256SUMS` and an `INSTALL.md` with the exact steps.

Build one yourself (from a machine with registry access):

```sh
scripts/bundle.sh --version 1.2.3 --arch arm64 --out dist
scripts/bundle.sh --version dev --no-pull --image ghcr.io/geoffo-dev/workflow-management:dev   # a local build
EXTRA_IMAGES="ghcr.io/kedacore/keda:2.18.0" scripts/bundle.sh --version 1.2.3                    # extra images
```

Inside the air gap set `WFM_WEBHOOK_ALLOW_PRIVATE=true` if outbound webhooks target internal
services, and use an internal OIDC provider and S3-compatible store.

## Observability

- **Metrics:** `/metrics` on :9090 in every mode: `wfm_http_requests_total` and
  `wfm_http_request_duration_seconds` by route, `wfm_jobs_total` and `wfm_job_duration_seconds` by
  job kind and outcome, `wfm_scm_webhooks_received_total`, plus Go runtime metrics.
  `serviceMonitor.enabled` scrapes all three components (with a `component` label);
  `database.cnpg.enablePodMonitor` scrapes PostgreSQL. Useful alerts: runnable `river_job` backlog
  growing (the KEDA-managed HPA shows it as an external metric), `wfm_jobs_total` failures, 5xx
  rate on hooks (webhook senders retry, but not forever), readiness flapping (database
  reachability), CNPG replication lag and the age of the last archived WAL.
- **Probes:** liveness `/healthz` never touches the database; readiness `/readyz` pings it, so a
  database outage drains traffic instead of restarting pods.
- **Logs:** JSON on stdout (`WFM_LOG_LEVEL`); ship them with the cluster's agent.
- **Traces:** set `OTEL_EXPORTER_OTLP_ENDPOINT` in `config` (OTLP/HTTP). The chart adds
  `k8s.namespace.name`, `k8s.pod.name`, `k8s.node.name` and `k8s.deployment.name` to
  `OTEL_RESOURCE_ATTRIBUTES`; allow egress to the collector in `networkPolicy.egress.extraRules`.

## Local cluster, inner loop and CI

```sh
k3d cluster create --config deploy/k3d/cluster.yaml   # k3s + local registry, Traefik on :8081
tilt up                                               # builds the image, deploys the chart, port-forwards
open http://localhost:8080                            # or http://wfm.localhost:8081 through Traefik
tilt up -- --cnpg                                     # CloudNativePG instead of the throwaway PostgreSQL
tilt down && k3d cluster delete wfm
```

Tilt deploys the same chart with `deploy/k3d/values-dev.yaml` (development sign-in, one replica
each, NetworkPolicies on). For UI work trigger the `vite` resource: Vite on :5173 proxies `/api` to
the port-forwarded web pod.

CI (`.github/workflows/ci.yml`) runs on every pull request: Go vet, generated-code drift and tests
against a `postgres:17` service; web typecheck, tests and build; `helm lint` + `kubeconform` of
every overlay (CRDs validated against the datree catalog); the image build; and an end-to-end job
that creates an ephemeral k3d cluster, installs CloudNativePG, installs and upgrades the chart
with development sign-in, checks the ingress split through Traefik, and runs the Playwright suite
against a port-forward of the web Service.
