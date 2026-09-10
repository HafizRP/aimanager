// Package web embeds the static assets and HTML templates of the dashboard.
package web

import "embed"

//go:embed templates/* static/*
var FS embed.FS
