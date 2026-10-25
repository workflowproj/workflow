#!/usr/bin/env bash
# Build an air-gapped install bundle for one CPU architecture:
#
#   images/      every container image the install needs (docker save; k3s airgap format)
#   charts/      the packaged Helm chart
#   manifests/   the CloudNativePG operator manifest (pull policy rewritten to IfNotPresent)
#                and a k3s HelmChart manifest that installs wfm without the helm CLI
#   values/      the example overlays (values-k3s.yaml, values-eks.yaml)
#   bin/         the wfm CLI (workflow apply/validate/export) extracted from the image
#   INSTALL.md   offline installation steps
#   SHA256SUMS   checksums of everything above
#
# Usage:
#   scripts/bundle.sh [--version 1.2.3] [--arch amd64|arm64] [--image REF] [--chart FILE.tgz]
#                     [--out DIR] [--no-pull] [--extra-image REF]...
#
#   --image      image to bundle (default ghcr.io/workflowproj/workflow:VERSION); a digest
#                reference is fine, it is re-tagged to what the chart expects
#   --chart      packaged chart (default: package deploy/helm/wfm with VERSION)
#   --no-pull    use images already in the local Docker daemon (e.g. a fresh `make image`)
#   --extra-image  add another image, e.g. KEDA or the Barman Cloud plugin (repeatable)
#
# Environment: CNPG_VERSION (default 1.30.1), CNPG_MANIFEST_URL, EXTRA_IMAGES (space separated).
# Needs: docker, helm, curl, sha256sum, tar (zstd optional: smaller image archives).
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
REPO=ghcr.io/workflowproj/workflow
VERSION=""
ARCH=amd64
IMAGE=""
CHART=""
OUT="$ROOT/dist"
PULL=true
EXTRA=(${EXTRA_IMAGES:-})
CNPG_VERSION=${CNPG_VERSION:-1.30.1}
CNPG_MANIFEST_URL=${CNPG_MANIFEST_URL:-https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-${CNPG_VERSION%.*}/releases/cnpg-${CNPG_VERSION}.yaml}

die() { echo "bundle: $*" >&2; exit 1; }
log() { echo "==> $*" >&2; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION=${2#v}; shift 2 ;;
    --arch) ARCH=$2; shift 2 ;;
    --image) IMAGE=$2; shift 2 ;;
    --chart) CHART=$2; shift 2 ;;
    --out) OUT=$2; shift 2 ;;
    --no-pull) PULL=false; shift ;;
    --extra-image) EXTRA+=("$2"); shift 2 ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; /^set -euo/d'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

for tool in docker helm curl sha256sum tar; do command -v "$tool" >/dev/null || die "$tool is required"; done
[[ -n "$VERSION" ]] || VERSION=$(git -C "$ROOT" describe --tags --always 2>/dev/null | sed 's/^v//') || true
[[ -n "$VERSION" ]] || die "--version is required outside a git checkout"
case "$ARCH" in amd64|arm64) ;; *) die "--arch must be amd64 or arm64" ;; esac
PLATFORM=linux/$ARCH
APP_REF="$REPO:$VERSION"
IMAGE=${IMAGE:-$APP_REF}

NAME="wfm-offline-${VERSION}-linux-${ARCH}"
WORK="$OUT/$NAME"
[[ ! -e "$WORK" ]] || die "$WORK already exists"
mkdir -p "$WORK"/{images,charts,manifests,values,bin}

# -- chart ---------------------------------------------------------------------------------------
if [[ -n "$CHART" ]]; then
  cp "$CHART" "$WORK/charts/"
else
  log "packaging the chart ($VERSION)"
  helm package "$ROOT/deploy/helm/wfm" --version "$VERSION" --app-version "$VERSION" --destination "$WORK/charts" >/dev/null
fi
CHART_TGZ=$(ls "$WORK"/charts/wfm-*.tgz)
cp "$ROOT"/deploy/helm/wfm/values-k3s.yaml "$ROOT"/deploy/helm/wfm/values-eks.yaml "$WORK/values/"

# -- CloudNativePG operator ----------------------------------------------------------------------
log "fetching CloudNativePG $CNPG_VERSION"
CNPG_FILE="$WORK/manifests/cnpg-${CNPG_VERSION}.yaml"
curl -sSfL "$CNPG_MANIFEST_URL" -o "$CNPG_FILE"
# The operator ships with imagePullPolicy: Always, which fails without a registry.
sed -i.bak 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/' "$CNPG_FILE" && rm -f "$CNPG_FILE.bak"

# -- image list ----------------------------------------------------------------------------------
# Everything the chart renders with the k3s overlay (app + PostgreSQL), plus the operator.
rendered=$(helm template wfm "$CHART_TGZ" -f "$WORK/values/values-k3s.yaml" \
  --set secrets.values.WFM_SECRET_KEY=bundle-render-only-0123456789abcdef \
  --set image.tag="$VERSION")
mapfile -t IMAGES < <(
  {
    printf '%s\n' "$rendered" "$(cat "$CNPG_FILE")" \
      | sed -nE 's/^[[:space:]]*(-[[:space:]]*)?(image|imageName):[[:space:]]*"?([^"[:space:]]+)"?.*$/\3/p'
    printf '%s\n' "${EXTRA[@]}"
  } | grep -v '^$' | sort -u
)
printf '%s\n' "${IMAGES[@]}" > "$WORK/images/images.txt"
log "images: ${IMAGES[*]}"

# -- pull, tag, save -----------------------------------------------------------------------------
if $PULL; then
  log "pulling $IMAGE for $PLATFORM"
  docker pull --quiet --platform "$PLATFORM" "$IMAGE" >/dev/null
fi
if [[ "$IMAGE" != "$APP_REF" ]]; then docker tag "$IMAGE" "$APP_REF"; fi
for img in "${IMAGES[@]}"; do
  [[ "$img" == "$APP_REF" ]] && continue
  if $PULL; then
    log "pulling $img for $PLATFORM"
    docker pull --quiet --platform "$PLATFORM" "$img" >/dev/null
  else
    docker image inspect "$img" >/dev/null 2>&1 || die "$img is not in the local Docker daemon (drop --no-pull)"
  fi
done

save_args=()
if docker image save --help 2>/dev/null | grep -q -- '--platform'; then save_args+=(--platform "$PLATFORM"); fi
TAR="$WORK/images/wfm-images-${ARCH}.tar"
log "saving ${#IMAGES[@]} images"
docker image save "${save_args[@]}" -o "$TAR" "${IMAGES[@]}"
chmod 0644 "$TAR"
if command -v zstd >/dev/null; then
  zstd -q -T0 -19 --rm "$TAR"
else
  gzip -9 "$TAR"
fi

# -- wfm CLI ---------------------------------------------------------------------------------------
cid=$(docker create --platform "$PLATFORM" "$APP_REF" version)
docker cp "$cid:/wfm" "$WORK/bin/wfm-linux-${ARCH}" >/dev/null
docker rm "$cid" >/dev/null

# -- k3s HelmChart (installs through k3s' built-in helm-controller; no helm CLI needed) -------------
{
  cat <<EOF
# Copy to /var/lib/rancher/k3s/server/manifests/ on a k3s server node (after editing the values).
# k3s' helm-controller installs and upgrades the release; the chart is embedded below.
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: wfm
  namespace: kube-system
spec:
  targetNamespace: wfm
  createNamespace: true
  chartContent: $(base64 -w0 < "$CHART_TGZ" 2>/dev/null || base64 < "$CHART_TGZ" | tr -d '\n')
  valuesContent: |-
EOF
  sed 's/^/    /' "$WORK/values/values-k3s.yaml"
} > "$WORK/manifests/helmchart-k3s.yaml"

# -- INSTALL.md ----------------------------------------------------------------------------------
IMAGES_ARCHIVE=$(basename "$(ls "$WORK"/images/wfm-images-*)")
cat > "$WORK/INSTALL.md" <<EOF
# wfm ${VERSION} offline install (linux/${ARCH})

Contents: \`images/${IMAGES_ARCHIVE}\` ($(wc -l < "$WORK/images/images.txt") images, listed in
\`images/images.txt\`), the Helm chart \`charts/$(basename "$CHART_TGZ")\`, the CloudNativePG
${CNPG_VERSION} operator manifest, example values, and the \`wfm\` CLI.

Verify the copy first:

\`\`\`sh
sha256sum -c SHA256SUMS
\`\`\`

## 1. Load the images into k3s

Either let k3s import them at start-up (every node that may run the pods):

\`\`\`sh
sudo mkdir -p /var/lib/rancher/k3s/agent/images
sudo cp images/${IMAGES_ARCHIVE} /var/lib/rancher/k3s/agent/images/
sudo systemctl restart k3s        # k3s-agent on agent nodes
\`\`\`

or import them into the running containerd:

\`\`\`sh
$( [[ "$IMAGES_ARCHIVE" == *.zst ]] && echo "zstd -d images/${IMAGES_ARCHIVE} -o /tmp/wfm-images.tar" || echo "gunzip -c images/${IMAGES_ARCHIVE} > /tmp/wfm-images.tar" )
sudo k3s ctr images import /tmp/wfm-images.tar
sudo k3s crictl images | grep -E 'workflowproj/workflow|cloudnative-pg'
\`\`\`

k3s itself needs its own air-gap images (k3s-airgap-images-${ARCH}.tar.zst from the k3s release)
in the same directory; see https://docs.k3s.io/installation/airgap.

## 2. Install the CloudNativePG operator

\`\`\`sh
kubectl apply --server-side -f manifests/cnpg-${CNPG_VERSION}.yaml
kubectl -n cnpg-system rollout status deployment/cnpg-controller-manager
\`\`\`

## 3. Install wfm

Edit \`values/values-k3s.yaml\` (hosts, \`WFM_PUBLIC_URL\`, \`WFM_HOOKS_PUBLIC_URL\`, sign-in) and
provide the secrets. With no internet egress also set \`WFM_WEBHOOK_ALLOW_PRIVATE: "true"\` if
outbound webhooks target internal services.

With the helm CLI:

\`\`\`sh
helm upgrade --install wfm charts/$(basename "$CHART_TGZ") -n wfm --create-namespace \\
  -f values/values-k3s.yaml --set-string secrets.values.WFM_SECRET_KEY="\$(openssl rand -hex 32)"
kubectl -n wfm logs job/wfm-migrate
kubectl -n wfm rollout status deployment/wfm-web
\`\`\`

(The schema migration runs as a post-install hook; the worker waits for it before processing jobs.)

Without the helm CLI, k3s' helm-controller can install the embedded chart: edit
\`valuesContent\` in \`manifests/helmchart-k3s.yaml\` (add \`secrets.values.WFM_SECRET_KEY\`, or point
\`secrets.existingSecret\` at a Secret you create), then

\`\`\`sh
sudo cp manifests/helmchart-k3s.yaml /var/lib/rancher/k3s/server/manifests/wfm.yaml
kubectl -n kube-system logs -f job/helm-install-wfm
\`\`\`

The images are referenced by tag with \`imagePullPolicy: IfNotPresent\`, so nothing is pulled.

## 4. CLI

\`\`\`sh
install -m 0755 bin/wfm-linux-${ARCH} /usr/local/bin/wfm
WFM_SERVER=https://wfm.example.internal WFM_TOKEN=... wfm workflow validate -f workflow.yaml
\`\`\`

## Upgrades

Build or download the next bundle, load its images (step 1), then \`helm upgrade\` with the new
chart (or replace the HelmChart manifest). Migrations run before the new pods roll out and are
backwards compatible with the previous version (expand, then contract in a later release).
EOF

# -- checksums + archive ---------------------------------------------------------------------------
( cd "$WORK" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )
tar -C "$OUT" -czf "$OUT/$NAME.tar.gz" "$NAME"
( cd "$OUT" && sha256sum "$NAME.tar.gz" > "$NAME.tar.gz.sha256" )
log "wrote $OUT/$NAME.tar.gz ($(du -h "$OUT/$NAME.tar.gz" | cut -f1))"
