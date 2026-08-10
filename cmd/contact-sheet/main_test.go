package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miyamo2/contact-sheet/internal/render"
	"github.com/miyamo2/contact-sheet/internal/sheet"
)

func TestLoadTemplateFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.tmpl")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadTemplate(context.Background(), http.DefaultClient, path)
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestLoadTemplateOverHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// nothing of the caller's should reach a third-party host
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("the request carried an Authorization header")
		}
		_, _ = w.Write([]byte("### {{ .Title }}"))
	}))
	defer server.Close()

	got, err := loadTemplate(context.Background(), server.Client(), server.URL+"/templates/gallery.tmpl")
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if got != "### {{ .Title }}" {
		t.Errorf("got %q", got)
	}
}

// A 404 that becomes a comment body would post GitHub's error page to the pull
// request, so anything but 200 has to stop the run.
func TestLoadTemplateRejectsNotOK(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such file", http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := loadTemplate(context.Background(), server.Client(), server.URL+"/gone.tmpl"); err == nil {
		t.Fatal("want an error for a 404")
	}
}

// The body ends up in a comment on the caller's pull request; over plain HTTP
// anyone on the path could choose what it says.
func TestLoadTemplateRejectsPlainHTTP(t *testing.T) {
	_, err := loadTemplate(context.Background(), http.DefaultClient, "http://example.com/x.tmpl")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("want an https-only error, got %v", err)
	}
}

func TestLoadTemplateRejectsHugeBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, remoteTemplateLimit+1))
	}))
	defer server.Close()

	if _, err := loadTemplate(context.Background(), server.Client(), server.URL+"/big.tmpl"); err == nil {
		t.Fatal("want an error for a body over the limit")
	}
}

// The key names the comment, so a remote template and a local copy of it have
// to land on the same one.
func TestTemplateKey(t *testing.T) {
	for ref, want := range map[string]string{
		"templates/gallery.tmpl":                                    "gallery",
		"./.github/contact-sheet.tmpl":                              "contact-sheet",
		"https://raw.githubusercontent.com/o/r/v1/tpl/gallery.tmpl": "gallery",
		"https://example.com/a/summary.tmpl?ref=v2":                 "summary",
	} {
		got, err := templateKey(ref)
		if err != nil {
			t.Errorf("templateKey(%q): %v", ref, err)
			continue
		}
		if got != want {
			t.Errorf("templateKey(%q) = %q, want %q", ref, got, want)
		}
	}
}

// Two entries landing on the same key would have one comment overwrite the
// other, which is worse than refusing to start.
func TestTemplatesRejectDuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, sub := range []string{a, b} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "sheet.tmpl"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config{templateFiles: filepath.Join(a, "sheet.tmpl") + "," + filepath.Join(b, "sheet.tmpl")}
	if _, err := templatesOf(context.Background(), cfg); err == nil {
		t.Fatal("want an error when two entries share a key")
	}
}

// The templates in this repository are advertised in the README and fetched by
// URL, so a broken one is broken for everyone who pointed at it.
func TestShippedTemplatesRender(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "templates", "*.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no templates found")
	}

	images := []sheet.Image{
		withURL(sheet.NewImage("desktop/about-light.png", map[string]string{"screen": "about", "theme": "light"})),
		withURL(sheet.NewImage("desktop/about-dark.png", map[string]string{"screen": "about", "theme": "dark"})),
		withURL(sheet.NewImage("mobile/menu.png", map[string]string{"screen": "menu"})),
	}
	states := []render.Context{
		{State: render.StatePublished, Status: "success", Images: images, Total: len(images), Ref: "refs/contact-sheet/pr-1/1.1"},
		{State: render.StatePublishFailed, Status: "success", Total: 3, Failure: "remote hung up"},
		{State: render.StateEmpty, Status: "failure"},
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, ctx := range states {
			renderer, err := render.New(file, string(raw), render.Options{ImageWidth: 360, Limit: 65536})
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			body, err := renderer.Render(ctx)
			if err != nil {
				t.Fatalf("%s (%s): %v", file, ctx.State, err)
			}
			if strings.TrimSpace(body) == "" {
				t.Errorf("%s (%s) rendered nothing", file, ctx.State)
			}
			if strings.Contains(body, "<no value>") {
				t.Errorf("%s (%s) has an unresolved field:\n%s", file, ctx.State, body)
			}
		}
	}
}

func withURL(i sheet.Image) sheet.Image {
	i.URL = "https://raw.example/" + i.Path
	return i
}
