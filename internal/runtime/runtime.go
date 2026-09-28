package runtime

import (
	"fmt"
	"sync"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
)

// Definition configures execution for a programming language.
type Definition struct {
	Language       domain.Language `json:"language"`
	DisplayName    string          `json:"display_name"`
	SourceFileName string          `json:"source_file_name"`
	Extension      string          `json:"extension"`
	Image          string          `json:"image"`
	CompileCmd     []string        `json:"compile_cmd,omitempty"`
	RunCmd         []string        `json:"run_cmd"`
	DefaultTimeout time.Duration   `json:"default_timeout"`
	MonacoLanguage string          `json:"monaco_language"`
	StarterCode    string          `json:"starter_code"`
}

// Registry stores language definitions.
type Registry struct {
	mu       sync.RWMutex
	runtimes map[domain.Language]Definition
}

var defaultRegistry *Registry

func init() {
	defaultRegistry = NewRegistry()
	registerDefaults(defaultRegistry)
}

// NewRegistry initializes an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		runtimes: make(map[domain.Language]Definition),
	}
}

// Default returns the default global registry.
func Default() *Registry {
	return defaultRegistry
}

// Register adds or updates a language runtime definition.
func (r *Registry) Register(def Definition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[def.Language] = def
}

// Get retrieves a language definition.
func (r *Registry) Get(lang domain.Language) (Definition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.runtimes[lang]
	if !ok {
		return Definition{}, fmt.Errorf("%w: %s", domain.ErrInvalidLanguage, lang)
	}
	return def, nil
}

// List returns all registered language definitions.
func (r *Registry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Definition, 0, len(r.runtimes))
	for _, def := range r.runtimes {
		list = append(list, def)
	}
	return list
}

func registerDefaults(r *Registry) {
	r.Register(Definition{
		Language:       domain.LanguagePython,
		DisplayName:    "Python 3.12",
		SourceFileName: "main.py",
		Extension:      ".py",
		Image:          "python:3.12-alpine",
		RunCmd:         []string{"python3", "-u", "main.py"},
		DefaultTimeout: 10 * time.Second,
		MonacoLanguage: "python",
		StarterCode:    "print(\"Hello ForgeQueue\")\n",
	})

	r.Register(Definition{
		Language:       domain.LanguageJavascript,
		DisplayName:    "JavaScript (Node.js 20)",
		SourceFileName: "index.js",
		Extension:      ".js",
		Image:          "node:20-alpine",
		RunCmd:         []string{"node", "index.js"},
		DefaultTimeout: 10 * time.Second,
		MonacoLanguage: "javascript",
		StarterCode:    "console.log(\"Hello ForgeQueue\");\n",
	})

	r.Register(Definition{
		Language:       domain.LanguageGo,
		DisplayName:    "Go 1.22",
		SourceFileName: "main.go",
		Extension:      ".go",
		Image:          "golang:1.22-alpine",
		CompileCmd:     []string{"go", "build", "-o", "solution", "main.go"},
		RunCmd:         []string{"./solution"},
		DefaultTimeout: 15 * time.Second,
		MonacoLanguage: "go",
		StarterCode:    "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello ForgeQueue\")\n}\n",
	})

	r.Register(Definition{
		Language:       domain.LanguageCpp,
		DisplayName:    "C++ 17 (GCC 13)",
		SourceFileName: "solution.cpp",
		Extension:      ".cpp",
		Image:          "gcc:13-alpine",
		CompileCmd:     []string{"g++", "-O2", "-std=c++17", "-o", "solution", "solution.cpp"},
		RunCmd:         []string{"./solution"},
		DefaultTimeout: 15 * time.Second,
		MonacoLanguage: "cpp",
		StarterCode:    "#include <iostream>\n\nint main() {\n    std::cout << \"Hello ForgeQueue\" << std::endl;\n    return 0;\n}\n",
	})

	r.Register(Definition{
		Language:       domain.LanguageJava,
		DisplayName:    "Java 21 (Temurin)",
		SourceFileName: "Main.java",
		Extension:      ".java",
		Image:          "eclipse-temurin:21-alpine",
		CompileCmd:     []string{"javac", "Main.java"},
		RunCmd:         []string{"java", "Main"},
		DefaultTimeout: 15 * time.Second,
		MonacoLanguage: "java",
		StarterCode:    "public class Main {\n    public static void main(String[] args) {\n        System.out.println(\"Hello ForgeQueue\");\n    }\n}\n",
	})
}
