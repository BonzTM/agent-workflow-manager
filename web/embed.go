// Package web embeds the awm-web UI assets so the binary serves them without
// any on-disk installation.
package web

import "embed"

// Static holds the embedded UI assets (index.html, status.html, static/)
// served by awm-web.
//
//go:embed index.html status.html static
var Static embed.FS
