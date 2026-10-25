# -*- mode: Python -*-
# Inner loop on a local k3d cluster: build the image, deploy the Helm chart exactly as production
# does (web, hooks and worker Deployments + the migration Job), port-forward everything.
#
#   k3d cluster create --config deploy/k3d/cluster.yaml
#   tilt up                    # throwaway PostgreSQL 17 (deploy/k3d/postgres.yaml)
#   tilt up -- --cnpg          # CloudNativePG instead (install the operator first, see docs/deployment.md)
#   tilt up -- --no-ui         # skip the Node build; pair with the "vite" resource for UI work
#   tilt down && k3d cluster delete wfm
#
# UI work is fastest with Vite's dev server against the port-forwarded API: trigger the "vite"
# resource (http://localhost:5173, proxies /api and /auth to localhost:8080).

config.define_bool("cnpg", usage="use a CloudNativePG cluster instead of the throwaway PostgreSQL")
config.define_bool("no-ui", usage="build the image without the React UI (faster backend iterations)")
cfg = config.parse()
use_cnpg = cfg.get("cnpg", False)
no_ui = cfg.get("no-ui", False)

allow_k8s_contexts(["k3d-wfm"])
update_settings(k8s_upsert_timeout_secs=120)

NAMESPACE = "wfm"
IMAGE = "ghcr.io/geoffo-dev/workflow-management"

k8s_yaml(blob("""
apiVersion: v1
kind: Namespace
metadata:
  name: %s
""" % NAMESPACE))

# -- image ---------------------------------------------------------------------------------------
# The k3d registry (wfm-registry.localhost:5005) is discovered automatically by Tilt.
docker_build(
    IMAGE,
    ".",
    build_args={"VERSION": "dev", "UI_STAGE": "ui-none" if no_ui else "web"},
    only=["go.mod", "go.sum", "api", "cmd", "internal"] + ([] if no_ui else ["web"]),
    ignore=[
        "**/*_test.go",
        "internal/integration",
        "web/node_modules",
        "web/dist",
        "web/test-results",
        "web/playwright-report",
    ],
)

# -- database ------------------------------------------------------------------------------------
db_deps = []
if not use_cnpg:
    objs = decode_yaml_stream(read_file("deploy/k3d/postgres.yaml"))
    for o in objs:
        o["metadata"]["namespace"] = NAMESPACE
    k8s_yaml(encode_yaml_stream(objs))
    k8s_resource("postgres", port_forwards=["5433:5432"], labels=["infra"],
                 objects=["wfm-dev-database:secret", "postgres:networkpolicy"])
    db_deps = ["postgres"]

# -- keycloak ------------------------------------------------------------------------------------
# Sign-in, directory sync and API tokens (values-dev.yaml points the app at it); the realm and its
# test users are described in docs/identity.md.
k8s_yaml(kustomize("deploy/k3d/keycloak"))
k8s_resource("keycloak", labels=["infra"],
             links=[link("http://keycloak.localhost:8081/admin", "Keycloak admin (admin / admin)")])

# -- chart ---------------------------------------------------------------------------------------
values = ["deploy/k3d/values-dev.yaml"] + (["deploy/k3d/values-cnpg.yaml"] if use_cnpg else [])
k8s_yaml(helm(
    "deploy/helm/wfm",
    name="wfm",
    namespace=NAMESPACE,
    values=values,
    set=["image.repository=" + IMAGE, "image.tag=dev"],
    kube_version="1.33.0",
))

# The migration Job is a Helm hook in real installs; here it is a plain Job the Deployments wait for.
k8s_resource("wfm-migrate", labels=["app"], resource_deps=db_deps)
k8s_resource("wfm-web", labels=["app"], resource_deps=["wfm-migrate"],
             port_forwards=["8080:8080", "9090:9090"],
             links=[link("http://localhost:8080", "UI / API"), link("http://wfm.localhost:8081", "UI via Traefik")])
k8s_resource("wfm-hooks", labels=["app"], resource_deps=["wfm-migrate"],
             port_forwards=["8082:8080", "9091:9090"],
             links=[link("http://hooks.localhost:8081/scim/v2/ServiceProviderConfig", "SCIM via Traefik")])
k8s_resource("wfm-worker", labels=["app"], resource_deps=["wfm-migrate"], port_forwards=["9092:9090"])

# -- optional: Vite dev server for UI work -------------------------------------------------------
local_resource(
    "vite",
    serve_cmd="npm run dev",
    serve_dir="web",
    serve_env={"WFM_API": "http://localhost:8080"},
    deps=[],
    labels=["ui"],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    links=[link("http://localhost:5173", "Vite")],
    resource_deps=["wfm-web"],
)
