package rhdh

const RHDHAuthTempl = `
integrations:
  github:
    - host: github.com
      token: {{ printf "${%s}" .GitHubToken }}

auth:
  environment: {{ .Environment }}
  providers:
    {{- if .GitHubClientId }}
    github:
      development:
        clientId: {{ printf "${%s}" .GitHubClientId }}
        clientSecret: {{ printf "${%s}" .GitHubClientSecret }}
    {{- end }}
    {{- if .EnableGuestProvider }}
    guest:
      dangerouslyAllowOutsideDevelopment: true
      userEntityRef: user:default/guest
    {{- end }}
`

type RHDHConfigAuth struct {
	GitHubToken         string
	Environment         string
	GitHubClientId      string
	GitHubClientSecret  string
	EnableGuestProvider bool
}
