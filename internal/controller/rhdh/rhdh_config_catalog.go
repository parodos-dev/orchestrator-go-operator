package rhdh

const RHDHCatalogTempl = `
catalog:
  rules:
    - allow:
        [
          Component,
          System,
          Group,
          Resource,
          Location,
          Template,
          API,
          User,
          Domain,
        ]
  locations:
    {{- if .DevMode }}
    - type: url
      target: https://github.com/parodos-dev/orchestrator-helm-chart/blob/main/resources/users.yaml
    {{- end }}
    - type: url
      target: https://github.com/parodos-dev/workflow-software-templates/blob/{{ .CatalogBranch }}/entities/workflow-resources.yaml
    - type: url
      target: https://github.com/parodos-dev/workflow-software-templates/blob/{{ .CatalogBranch }}/scaffolder-templates/basic-workflow/template.yaml
    - type: url
      target: https://github.com/parodos-dev/workflow-software-templates/blob/{{ .CatalogBranch }}/scaffolder-templates/complex-assessment-workflow/template.yaml

`

type RHDHConfigCatalog struct {
	EnableGuestProvider bool
	CatalogBranch       string
}
