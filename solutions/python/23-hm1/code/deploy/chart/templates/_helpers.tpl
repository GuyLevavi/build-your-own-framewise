{{- define "framewise.labels" -}}
app.kubernetes.io/name: framewise
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "framewise.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}

{{/* Environment every container of the app gets. Secrets stay out of Git and out of the chart. */}}
{{- define "framewise.env" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef: {name: {{ .Values.databaseSecret }}, key: DATABASE_URL}
- name: FRAMEWISE_API_KEY
  valueFrom:
    secretKeyRef: {name: {{ .Values.apiKeySecret }}, key: FRAMEWISE_API_KEY, optional: true}
- {name: FRAMEWISE_PROFILES, value: /etc/framewise/profiles.yaml}
{{- end -}}

{{- define "framewise.volumes" -}}
- name: profiles
  configMap: {name: {{ .Release.Name }}-profiles}
- name: data
  persistentVolumeClaim: {claimName: {{ .Release.Name }}-data}
{{- end -}}

{{- define "framewise.mounts" -}}
- {name: profiles, mountPath: /etc/framewise, readOnly: true}
- {name: data, mountPath: /data}
{{- end -}}
