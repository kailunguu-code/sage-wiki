package skill

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed packs/*.md.tmpl
var packFS embed.FS

var packFiles = map[PackName]string{
	PackCodebaseMemory:       "packs/codebase-memory.md.tmpl",
	PackResearchLibrary:      "packs/research-library.md.tmpl",
	PackMeetingNotes:         "packs/meeting-notes.md.tmpl",
	PackDocumentationCurator: "packs/documentation-curator.md.tmpl",
}

func RenderPack(pack PackName, data TemplateData) (string, error) {
	filename, ok := packFiles[pack]
	if !ok {
		return "", fmt.Errorf("unknown pack %q", pack)
	}

	content, err := packFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("read pack template: %w", err)
	}

	tmpl, err := template.New(filename).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parse pack template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute pack template: %w", err)
	}

	return buf.String(), nil
}
