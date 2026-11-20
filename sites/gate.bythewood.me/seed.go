package main

import (
	"log/slog"
	"strings"
	"time"
)

// scanners is exploit scanners probing software nothing here runs. Some
// clauses look wrong and aren't, and the README says which and why.
const scanners = `(?i)(?:^|/)(?:wp-|wordpress|wp/|xmlrpc|phpmyadmin|autodiscover|cgi-bin/|vendor/|actuator/|_profiler/|solr/|livewire/|sitecore/|_ignition/|storage/logs/|webinterface/|zabbix/|owa/|boaform/|jenkins/|hudson/|sa\.json|key\.json|\.git|\.aws|\.ssh|\.vscode|remote/fgt_lang|remote/login|global-protect/|ssl-vpn/|app/rest/|blade-auth/|blade-system/|pimcore-studio/|php-cgi/|administrator/|language/en-gb/|media/system/|api/v1/totp/|_cat/|_cluster/|_watcher/|_snapshot/|_nodes|api/datasources|api/alert-notifications|api/v1/provisioning/|api/org|api/dashboards|api/2\.0/mlflow/|containers/json|v1\.24/|images/json|v2/keys/|api/contents|auth/v1/|rest/v1/|api/v1/validate/code|api/v1/auto_login|druid/|hazelcast/|geoserver/|ws/v1/cluster|editor/filemanager/|plugins/editors/|media/jui/|@fs/|__vite_rsc_|@vite/|proc/self/|userfiles|read-document|inngest|api/fs/exec|api/exec|api/designer/|api/templates/preview|_debugbar/|_debug_toolbar/|__debug__/|debug/pprof|debug/vars|debug-kit|telescope/requests|server-status|server-info|nginx_status|graphql|actuator|swagger|openapi\.json|_payload\.json|app/etc/local\.xml|_darcs/|CVS/Root|stripe|firebase|amplifyconfiguration|amplify_outputs|awsconfiguration|aws-exports|aws_credentials|google-services|gc-service|gcp-service|client_secrets?\.|terraform\.tfstate|terraform\.tfvars|serverless\.ya?ml|sam-template|vercel\.json|netlify\.toml|docker-compose\.|compose\.ya?ml|Dockerfile$|Dockerfile\.|rclone\.conf|web\.config|gradle\.properties|application\.properties|bootstrap\.(?:yml|properties)|appsettings|settings\.py|secret_token|master\.key|schema\.prisma|composer\.(?:json|lock)|Gemfile|Rakefile|Jakefile|behat\.yml|circle\.yml|codeception\.yml|renovate\.json|platformio\.ini)|telerik|phpinfo|id_rsa|id_ed25519|credentials\.json|keyfile\.json|adminsdk|-key\.json|-sa\.json|sugar_version\.json|com_pagebuilderck|login\.do|showlogin\.cc|service-?account|\.env|joomla|keys-status|\.(?:php|asp|jsp|cgi|axd|sql|bak|esp|action)|\.(?:zip|rar|7z|tgz|tar\.gz)$|^/(?:env|_environment)$|\.astro\.mjs\.map$|\.(?:pem|key|p12|pfx|jks|keystore)$|^/(?:api/)?(?:proxy|fetch|redirect|request|url|webhook|load|out)$|^/(?:ip|null)$|^/(?:[^/]+/){0,2}_{0,2}env(?:-config)?\.(?:js|json|ts|txt|ya?ml|old)$|^/(?:[^/]+/){0,2}config(?:uration)?\.(?:json|js|ts|py|rb|ya?ml|toml|sh)$|^/(?:[^/]+/){0,2}(?:secrets?|credentials?|app-config|service_account|auth|token)\.(?:json|js|ts|py|rb|ya?ml|ini|csv|txt)$|^/config/[^/]*\.(?:ya?ml|json|exs|rb|conf|properties)$|^/(?:[^/]+/){0,2}(?:id_dsa|id_ecdsa|private-?key|privatekey)(?:$|\.)|^/api/(?:v\d+/)?(?:config|settings|keys|env|sysConfig)(?:/|$)|^/api/(?:session/properties|postman\.json)$|^/(?:s3/)?credentials$|^/s3\.(?:key|secret|ya?ml|properties)$`

// repoContent is the shape of a path repos serves out of a repository, which
// can hold any filename at all, so the scanner list can't apply under it.
const repoContent = `^/[^/]+/(?:blob|raw|tree|commit|log|archive|branches|tags)(?:/|$)`

// seedRules is what an empty database starts with, the edge's rules as they
// stood in Caddy. After that the database is the only copy.
func seedRules() []Rule {
	return []Rule{
		{Name: "claude-connector", Action: actAllow, Note: "mcp signs Claude in and answers its calls itself, wherever Anthropic calls from.",
			When: "host is mcp.bythewood.me\npath prefix /mcp /token /authorize /oauth/ /.well-known/oauth-"},
		{Name: "dotfiles", Action: actBlock, Status: 404, Quiet: true, Note: "Nothing here serves a path starting with a dot, except mcp's OAuth metadata.",
			When: "path regex ^/\\.\nnot path prefix /.well-known/oauth-"},
		{Name: "scanners", Action: actBlock, Status: 404, Quiet: true, Note: "Probes for software nothing here runs, matched anywhere in the path since most arrive under a guessed prefix.",
			When: "not host is repos.bythewood.me\npath regex " + scanners},
		{Name: "scanners-repos", Action: actBlock, Status: 404, Quiet: true, Note: "The same list on repos, except under a repository's own pages, where any filename is real content.",
			When: "host is repos.bythewood.me\nnot path regex " + repoContent + "\npath regex " + scanners},
		{Name: "sign-in-us", Action: actBlock, Status: 403, Message: "Signing in is only open from the US.", Note: "Signing in is only ever from the US. Travelling means watching this one first.",
			When: "host is auth.bythewood.me\npath is /login /code /recovery\nnot country is US"},
		{Name: "sign-in-rate", Action: actLimit, Rate: 10, Note: "Per address, so one prober can't spend a shared limit. auth's ceiling of codes an hour still holds across every address.",
			When: "host is auth.bythewood.me\nmethod is POST\npath is /login /code /recovery"},
	}
}

func seed(store *Store) {
	if store.Setting("seeded") != "" {
		return
	}
	for _, r := range seedRules() {
		r.Author, r.Created = "seed", time.Now().UTC()
		if err := store.AddRule(&r, "seed"); err != nil && !strings.Contains(err.Error(), "already exists") {
			slog.Error("seeding a rule", slog.String("rule", r.Name), slog.Any("err", err))
		}
	}
	_ = store.SetSetting("seeded", time.Now().UTC().Format(time.RFC3339))
}
