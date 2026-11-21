package skills

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

// builtinFS holds the skills that ship with ASG (terminal, dashboard and
// desktop app). Each is builtin/<name>/SKILL.md.
//
//go:embed builtin/*/SKILL.md
var builtinFS embed.FS

// Builtins returns the bundled skills, parsed fresh each call.
func Builtins() []*Skill {
	var out []*Skill
	dirs, _ := fs.ReadDir(builtinFS, "builtin")
	for _, d := range dirs {
		data, err := builtinFS.ReadFile(path.Join("builtin", d.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		meta, body := parseFrontMatter(string(data))
		s := &Skill{Name: meta["name"], Description: meta["description"], Body: strings.TrimSpace(body), Source: "built-in", Builtin: true}
		if s.Name == "" {
			s.Name = d.Name()
		}
		out = append(out, s)
	}
	return out
}
