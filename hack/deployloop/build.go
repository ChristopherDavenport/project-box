package main

import (
	"context"

	"github.com/ChristopherDavenport/project-box/internal/deploy"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/render"
)

func deployBuild(ctx context.Context, ws forge.Workspace, task, name, image, tag string) (forge.Commit, error) {
	b := deploy.Builder{
		Service: render.Service{Name: name, Box: "shop", Image: image, Tag: tag, Port: 80, Replicas: 2},
		Target:  render.Target{Env: "prod", Namespace: "shop"},
	}
	return b.Build(ctx, ws, task)
}
