package tools

import "context"

var OrchardRepos = Define(Tool{
	Name:   "orchard_repos",
	Traits: Private | Estate,
	Description: "Read Isaac's own git remote at repos.bythewood.me: every repository, its " +
		"description, size, branch and tag counts, when it was last pushed, and how close it is to " +
		"the push size limit. Use it for what he is working on or what a repository holds. Read only.",
}, func(ctx context.Context, d *Turn, _ noArgs) (any, error) {
	var out any
	err := estateGet(ctx, d, reposBase+"/api/repos", &out)
	return out, err
})
