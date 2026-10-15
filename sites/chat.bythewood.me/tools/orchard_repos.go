package tools

import "context"

var OrchardRepos = Tool{
	Name: "orchard_repos",
	Description: "Read Isaac's own git remote at repos.bythewood.me: every repository, its " +
		"description, size, branch and tag counts, when it was last pushed, and how close it is to " +
		"the push size limit. Use it for what he is working on or what a repository holds. Read only.",
	Schema: obj(map[string]any{}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		var out any
		err := estateGet(ctx, d, reposBase+"/api/repos", &out)
		return out, err
	},
}
