package main

import (
	_ "embed"
	"html/template"
	"strings"
)

//go:embed templates/preview.html
var previewHTML string

//go:embed templates/style.css
var styleCSS string

var previewTmpl *template.Template

func init() {
	html := strings.Replace(previewHTML, "/* INJECT_CSS */", styleCSS, 1)
	previewTmpl = template.Must(template.New("preview").Parse(html))
}
