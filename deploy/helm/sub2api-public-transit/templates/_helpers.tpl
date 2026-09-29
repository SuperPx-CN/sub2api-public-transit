{{- define "sub2api.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "sub2api.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "sub2api.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- define "sub2api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sub2api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "sub2api.labels" -}}
{{ include "sub2api.selectorLabels" . }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
{{- define "sub2api.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (required "image.tag or image.digest is required" .Values.image.tag) -}}
{{- end -}}
{{- end -}}
{{- define "sub2api.validate" -}}
{{- if .Values.externalRedis.auth.enabled -}}
{{- $_ := required "externalRedis.auth.existingSecret is required when Redis authentication is enabled" .Values.externalRedis.auth.existingSecret -}}
{{- end -}}
{{- if and .Values.ingress.enabled (not .Values.ingress.hosts) -}}
{{- fail "ingress.hosts is required when ingress.enabled=true" -}}
{{- end -}}
{{- $reserved := list "AUTO_SETUP" "DATA_DIR" "SERVER_HOST" "SERVER_PORT" "DATABASE_HOST" "DATABASE_PORT" "DATABASE_USER" "DATABASE_DBNAME" "DATABASE_SSLMODE" "DATABASE_PASSWORD" "REDIS_HOST" "REDIS_PORT" "REDIS_USERNAME" "REDIS_DB" "REDIS_ENABLE_TLS" "REDIS_PASSWORD" "ADMIN_EMAIL" "ADMIN_PASSWORD" "JWT_SECRET" "TOTP_ENCRYPTION_KEY" "TZ" -}}
{{- $seen := dict -}}
{{- range .Values.extraEnv -}}
{{- if or (has .name $reserved) (hasKey $seen .name) -}}
{{- fail (printf "extraEnv name %s is managed by the chart or duplicated" .name) -}}
{{- end -}}
{{- $_ := set $seen .name true -}}
{{- end -}}
{{- $volumes := dict "data" true -}}
{{- range .Values.extraVolumes -}}
{{- if hasKey $volumes .name -}}{{- fail (printf "extraVolumes name %s is reserved or duplicated" .name) -}}{{- end -}}
{{- $_ := set $volumes .name true -}}
{{- end -}}
{{- $paths := dict -}}
{{- range .Values.extraVolumeMounts -}}
{{- $path := clean .mountPath -}}
{{- if or (eq .name "data") (eq $path "/") (eq $path "/app") (eq $path "/app/data") (hasPrefix "/app/data/" $path) -}}
{{- fail "extraVolumeMounts must not replace data or shadow /app/data" -}}
{{- end -}}
{{- if not (hasKey $volumes .name) -}}{{- fail (printf "extraVolumeMounts references unknown volume %s" .name) -}}{{- end -}}
{{- if hasKey $paths $path -}}{{- fail (printf "extraVolumeMounts duplicates path %s" $path) -}}{{- end -}}
{{- $_ := set $paths $path true -}}
{{- end -}}
{{- if hasKey .Values.podAnnotations "checksum/config" -}}{{- fail "podAnnotations checksum/config is managed by the chart" -}}{{- end -}}
{{- end -}}
