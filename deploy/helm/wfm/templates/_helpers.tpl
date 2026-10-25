{{/* vim: set filetype=mustache: */}}

{{- define "wfm.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "wfm.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "wfm.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "wfm.selectorLabels" -}}
app.kubernetes.io/name: {{ include "wfm.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "wfm.labels" -}}
helm.sh/chart: {{ include "wfm.chart" . }}
{{ include "wfm.selectorLabels" . }}
app.kubernetes.io/version: {{ include "wfm.imageTag" . | trunc 63 | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: workflow-management
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* Labels for one component. Call with (dict "root" $ "component" "web"). */}}
{{- define "wfm.componentLabels" -}}
{{ include "wfm.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "wfm.componentSelectorLabels" -}}
{{ include "wfm.selectorLabels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "wfm.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "wfm.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "wfm.imageTag" -}}
{{- default .Chart.AppVersion .Values.image.tag }}
{{- end }}

{{- define "wfm.image" -}}
{{- if .Values.image.digest }}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else }}
{{- printf "%s:%s" .Values.image.repository (include "wfm.imageTag" .) }}
{{- end }}
{{- end }}

{{/* ---------------------------------------------------------------- secrets */}}

{{/* Name of the Secret whose keys become env vars, or "" when there is none. */}}
{{- define "wfm.secretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else if and .Values.secrets.externalSecret.enabled .Values.secrets.externalSecret.targetName }}
{{- .Values.secrets.externalSecret.targetName }}
{{- else if or .Values.secrets.create .Values.secrets.externalSecret.enabled }}
{{- include "wfm.fullname" . }}
{{- end }}
{{- end }}

{{/* ---------------------------------------------------------------- database */}}

{{- define "wfm.cnpgName" -}}
{{- default (printf "%s-db" (include "wfm.fullname" .)) .Values.database.cnpg.name | trunc 50 | trimSuffix "-" }}
{{- end }}

{{- define "wfm.cnpgPoolerName" -}}
{{- printf "%s-pooler-rw" (include "wfm.cnpgName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "wfm.poolerEnabled" -}}
{{- if and .Values.database.cnpg.enabled .Values.database.cnpg.pooler.enabled }}true{{ end }}
{{- end }}

{{/* Secret name holding the database URL. */}}
{{- define "wfm.databaseSecretName" -}}
{{- if .Values.database.cnpg.enabled }}
{{- printf "%s-app" (include "wfm.cnpgName" .) }}
{{- else if .Values.database.external.existingSecret }}
{{- .Values.database.external.existingSecret }}
{{- else if .Values.database.external.url }}
{{- printf "%s-database" (include "wfm.fullname" .) }}
{{- else }}
{{- required "database: enable database.cnpg, or set database.external.existingSecret / database.external.url, or provide WFM_DATABASE_URL through secrets.*" (include "wfm.secretName" .) }}
{{- end }}
{{- end }}

{{/* Key in that Secret. */}}
{{- define "wfm.databaseSecretKey" -}}
{{- if .Values.database.cnpg.enabled }}uri
{{- else if .Values.database.external.existingSecret }}{{ .Values.database.external.existingSecretKey }}
{{- else if .Values.database.external.url }}uri
{{- else }}WFM_DATABASE_URL
{{- end }}
{{- end }}

{{/* ---------------------------------------------------------------- argo cd */}}

{{- define "wfm.syncWave" -}}
argocd.argoproj.io/sync-wave: {{ . | quote }}
{{- end }}

{{/* ---------------------------------------------------------------- pods */}}

{{/* Environment shared by every app container and the migration job. */}}
{{- define "wfm.commonEnv" -}}
- name: WFM_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "wfm.databaseSecretName" . }}
      key: {{ include "wfm.databaseSecretKey" . }}
- name: WFM_HTTP_ADDR
  value: {{ printf ":%v" .Values.ports.http | quote }}
- name: WFM_METRICS_ADDR
  value: {{ printf ":%v" .Values.ports.metrics | quote }}
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: NODE_NAME
  valueFrom:
    fieldRef:
      fieldPath: spec.nodeName
{{- end }}

{{/* envFrom shared by every app container. */}}
{{- define "wfm.commonEnvFrom" -}}
- configMapRef:
    name: {{ include "wfm.fullname" . }}-config
{{- with include "wfm.secretName" . }}
- secretRef:
    name: {{ . }}
{{- end }}
{{- with .Values.extraEnvFrom }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* Annotations that roll pods when chart-rendered config changes. */}}
{{- define "wfm.checksums" -}}
checksum/config: {{ include (print .Template.BasePath "/configmap.yaml") . | sha256sum }}
{{- if .Values.secrets.create }}
checksum/secret: {{ include (print .Template.BasePath "/secret.yaml") . | sha256sum }}
{{- end }}
{{- end }}

{{/* Default topology spread across zones and nodes. Call with (dict "root" $ "component" "web" "c" $c). */}}
{{- define "wfm.topologySpread" -}}
{{- $sel := include "wfm.componentSelectorLabels" (dict "root" .root "component" .component) | fromYaml }}
{{- if .c.topologySpread.constraints }}
{{- range .c.topologySpread.constraints }}
{{- $tsc := . }}
{{- if not (hasKey $tsc "labelSelector") }}
{{- $tsc = merge (dict "labelSelector" (dict "matchLabels" $sel)) $tsc }}
{{- end }}
- {{ toYaml $tsc | nindent 2 | trim }}
{{- end }}
{{- else }}
{{- range list "topology.kubernetes.io/zone" "kubernetes.io/hostname" }}
- maxSkew: 1
  topologyKey: {{ . }}
  whenUnsatisfiable: {{ $.c.topologySpread.whenUnsatisfiable }}
  labelSelector:
    matchLabels:
      {{- toYaml $sel | nindent 6 }}
  matchLabelKeys:
    - pod-template-hash
{{- end }}
{{- end }}
{{- end }}

{{/* cert-manager annotations for an ingress config block. */}}
{{- define "wfm.certManagerAnnotations" -}}
{{- if .certManager.clusterIssuer }}
cert-manager.io/cluster-issuer: {{ .certManager.clusterIssuer | quote }}
{{- else if .certManager.issuer }}
cert-manager.io/issuer: {{ .certManager.issuer | quote }}
{{- end }}
{{- end }}

{{/* Barman Cloud object store configuration (in-tree barmanObjectStore or plugin ObjectStore). */}}
{{- define "wfm.cnpgBarmanConfig" -}}
destinationPath: {{ .destinationPath | quote }}
{{- with .endpointURL }}
endpointURL: {{ . | quote }}
{{- end }}
s3Credentials:
  {{- if .s3Credentials.inheritFromIAMRole }}
  inheritFromIAMRole: true
  {{- else }}
  {{- if not .s3Credentials.secretName }}
  {{- fail "database.cnpg.backup.s3Credentials: set inheritFromIAMRole or secretName" }}
  {{- end }}
  accessKeyId:
    name: {{ .s3Credentials.secretName }}
    key: {{ .s3Credentials.accessKeyIdKey }}
  secretAccessKey:
    name: {{ .s3Credentials.secretName }}
    key: {{ .s3Credentials.secretAccessKeyKey }}
  {{- with .s3Credentials.regionKey }}
  region:
    name: {{ $.s3Credentials.secretName }}
    key: {{ . }}
  {{- end }}
  {{- end }}
wal:
  {{- toYaml .wal | nindent 2 }}
data:
  {{- toYaml .data | nindent 2 }}
{{- end }}

{{/* Egress rules for app pods. Call with (dict "root" $ "internet" bool). */}}
{{- define "wfm.np.egress" -}}
{{- $np := .root.Values.networkPolicy }}
- to:
    {{- toYaml $np.egress.dns.to | nindent 4 }}
  ports:
    {{- toYaml $np.egress.dns.ports | nindent 4 }}
{{- if .root.Values.database.cnpg.enabled }}
- to:
    - podSelector:
        matchLabels:
          cnpg.io/cluster: {{ include "wfm.cnpgName" .root }}
    {{- if include "wfm.poolerEnabled" .root }}
    - podSelector:
        matchLabels:
          cnpg.io/poolerName: {{ include "wfm.cnpgPoolerName" .root }}
    {{- end }}
  ports:
    - port: 5432
      protocol: TCP
{{- else }}
- {{- with $np.egress.database.to }}
  to:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  ports:
    - port: {{ $np.egress.database.port }}
      protocol: TCP
{{- end }}
{{- if .internet }}
- to:
    {{- range $np.egress.internet.cidrs }}
    - ipBlock:
        cidr: {{ . }}
        {{- with $np.egress.internet.except }}
        except:
          {{- toYaml . | nindent 10 }}
        {{- end }}
    {{- end }}
  ports:
    {{- range $np.egress.internet.ports }}
    - port: {{ . }}
      protocol: TCP
    {{- end }}
{{- end }}
{{- with $np.egress.extraRules }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* 90% of a Kubernetes memory quantity, in bytes (for GOMEMLIMIT). */}}
{{- define "wfm.goMemLimit" -}}
{{- $q := toString . -}}
{{- $units := dict "Ki" 1024 "Mi" 1048576 "Gi" 1073741824 "Ti" 1099511627776 "k" 1000 "K" 1000 "M" 1000000 "G" 1000000000 "T" 1000000000000 -}}
{{- $num := regexFind "^[0-9.]+" $q -}}
{{- $unit := trimPrefix $num $q -}}
{{- $mult := 1 -}}
{{- if $unit }}{{ $mult = required (printf "unsupported memory quantity %q" $q) (get $units $unit) }}{{ end -}}
{{- mulf (float64 $num) (float64 $mult) 0.9 | int64 -}}
{{- end }}
