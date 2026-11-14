// Package ui is the frontend every app shares: the design system, its
// components, and its themes. desktop copies it into each project's
// .desktop/ui, where the frontend imports it as "@ui". See README.md.
package ui

import "embed"

// Files are the shared frontend's files, by their path in this folder.
//
//go:embed *.css *.ts components lib
var Files embed.FS
