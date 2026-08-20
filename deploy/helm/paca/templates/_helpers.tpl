{{/*
Standard name/label helpers — same shape `helm create`'s own scaffold
generates, kept unmodified so this chart behaves the way anyone already
familiar with Helm conventions expects.
*/}}

{{- define "paca.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "paca.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "paca.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "paca.labels" -}}
helm.sh/chart: {{ include "paca.chart" . }}
app.kubernetes.io/name: {{ include "paca.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Component-scoped labels/selector take a (root-context, component-name) pair via dict, e.g. (dict "root" . "component" "api") — every Deployment/Service/etc. below calls these the same way so a component's own Pods are never accidentally selected by a sibling's Service. */}}
{{- define "paca.selectorLabels" -}}
app.kubernetes.io/name: {{ include "paca.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "paca.componentLabels" -}}
{{ include "paca.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "paca.componentFullname" -}}
{{- printf "%s-%s" (include "paca.fullname" .root) .component | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Secret name every component's env reads from — either the chart's own
generated Secret, or Values.secrets.existingSecret when the caller manages
one themselves (see values.yaml's own doc comment on that field).
*/}}
{{- define "paca.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "paca.fullname" . -}}-secret
{{- end -}}
{{- end -}}

{{/*
Derived connection strings/URLs — computed once here so every template
that needs one (Secret, Deployments, NOTES.txt) agrees, instead of each
hand-rolling its own copy that could drift.
*/}}

{{- define "paca.databaseUrl" -}}
{{- if not .Values.postgres.enabled -}}
{{- required "externalDatabaseUrl is required when postgres.enabled is false" .Values.externalDatabaseUrl -}}
{{- else -}}
{{- printf "postgres://%s:%s@%s-postgres:5432/%s?sslmode=disable" .Values.postgres.username (required "secrets.postgresPassword is required" .Values.secrets.postgresPassword) (include "paca.fullname" .) .Values.postgres.database -}}
{{- end -}}
{{- end -}}

{{- define "paca.redisUrl" -}}
{{- if not .Values.valkey.enabled -}}
{{- required "externalRedisUrl is required when valkey.enabled is false" .Values.externalRedisUrl -}}
{{- else -}}
{{- printf "redis://%s-valkey:6379/0" (include "paca.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "paca.storageEndpoint" -}}
{{- if .Values.storage.endpoint -}}
{{- .Values.storage.endpoint -}}
{{- else if .Values.minio.enabled -}}
{{- printf "%s-minio:9000" (include "paca.fullname" .) -}}
{{- else -}}
{{- required "storage.endpoint is required when minio.enabled is false (e.g. s3.amazonaws.com, or a region-specific S3 endpoint)" .Values.storage.endpoint -}}
{{- end -}}
{{- end -}}

{{- define "paca.storagePublicUrl" -}}
{{- if .Values.storage.publicUrl -}}
{{- .Values.storage.publicUrl -}}
{{- else if .Values.publicUrl -}}
{{- printf "%s/storage" .Values.publicUrl -}}
{{- else -}}
{{- print "http://localhost/storage" -}}
{{- end -}}
{{- end -}}

{{/*
Namespace the kubernetes sandbox backend creates Jobs/Pods in — and the
same namespace templates/agent-runner/role.yaml + rolebinding.yaml grant
RBAC in, so the two never drift apart (see agentRunner.sandbox.namespace's
own doc comment in values.yaml on why that matters).
*/}}
{{- define "paca.sandboxNamespace" -}}
{{- default .Release.Namespace .Values.agentRunner.sandbox.namespace -}}
{{- end -}}

{{/*
SITE_ADDRESS for the gateway's Caddyfile (see that file's own doc comment
on what this controls). When an Ingress fronts the gateway, TLS is
terminated there, so Caddy itself should stay plain HTTP — provisioning
its own certificate for the same hostname would be redundant at best and,
since Caddy's automatic HTTPS needs ports 80+443 reachable directly from
the internet (not true once an Ingress/LoadBalancer sits in front), would
simply fail at worst. Falls back to publicUrl's own host when set and no
Ingress is used (a LoadBalancer Service exposing the gateway directly), or
plain ":80" when neither is configured.
*/}}
{{- define "paca.gatewaySiteAddress" -}}
{{- if .Values.ingress.enabled -}}
:80
{{- else if .Values.publicUrl -}}
{{- (urlParse .Values.publicUrl).host -}}
{{- else -}}
:80
{{- end -}}
{{- end -}}

{{/*
The plugins volume shared between api (writer) and gateway (reader) — see
values.yaml's api.plugins.persistence doc comment. One definition so the
api and gateway Deployments can never reference the volume differently.
*/}}
{{- define "paca.pluginsVolume" -}}
{{- if .Values.api.plugins.persistence.enabled -}}
persistentVolumeClaim:
  claimName: {{ include "paca.fullname" . }}-plugins
{{- else -}}
emptyDir: {}
{{- end -}}
{{- end -}}
