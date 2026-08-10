package main

import (
	"flag"
	"os"
	"strconv"
)

// Each flag's default comes from a CONTACT_SHEET_* variable. The composite
// action sets those rather than building a command line, so a template or a
// title containing quotes or newlines never has to survive a shell.
func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.path, "path", envString("PATH", ""),
		"directory of images to attach (required)")
	flag.StringVar(&cfg.layout, "layout", envString("LAYOUT", ""),
		"expression filtering the files and naming their captures; empty collects every image file")
	flag.StringVar(&cfg.templateFiles, "template-files", envString("TEMPLATE_FILES", ""),
		"comma-separated text/template files, one comment each (default: the built-in one)")
	flag.StringVar(&cfg.commentID, "comment-id", envString("COMMENT_ID", "contact-sheet"),
		"identifies the comment to rewrite; two workflows in one repository need two ids")
	flag.StringVar(&cfg.title, "title", envString("TITLE", "Contact Sheet"),
		"heading passed to the template")
	flag.StringVar(&cfg.status, "status", envString("STATUS", "success"),
		"outcome of the job that produced the images: success or failure")
	flag.StringVar(&cfg.refNamespace, "ref-namespace", envString("REF_NAMESPACE", "refs/contact-sheet"),
		"ref prefix the images are pushed under; must be outside refs/heads/*")
	flag.StringVar(&cfg.rowLabel, "row-label", envString("ROW_LABEL", "file name"),
		"header of the first column of a table built by the Table helper")
	flag.IntVar(&cfg.imageWidth, "image-width", envInt("IMAGE_WIDTH", 360),
		"width attribute on each <img>; 0 omits it")
	flag.IntVar(&cfg.pullNumber, "pull-number", envInt("PULL_NUMBER", 0),
		"pull request to comment on; 0 resolves it from GITHUB_SHA")
	flag.BoolVar(&cfg.dryRun, "dry-run", envBool("DRY_RUN", false),
		"push nothing, comment nothing, print the body to stdout")
	flag.Parse()
	return cfg
}

func envString(key, fallback string) string {
	if value, ok := os.LookupEnv("CONTACT_SHEET_" + key); ok && value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value, err := strconv.Atoi(envString(key, "")); err == nil {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if value, err := strconv.ParseBool(envString(key, "")); err == nil {
		return value
	}
	return fallback
}
