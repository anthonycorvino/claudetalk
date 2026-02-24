package protocol

import "path/filepath"

// Payload carries the content of a message.
type Payload struct {
	Text     string `json:"text,omitempty"`
	Code     string `json:"code,omitempty"`
	Diff     string `json:"diff,omitempty"`
	FilePath string `json:"file_path,omitempty"`
	Language string `json:"language,omitempty"`
}

// Message types.
const (
	TypeText   = "text"
	TypeCode   = "code"
	TypeDiff   = "diff"
	TypeSystem = "system"
	TypeFile   = "file"
	TypeSpawn  = "spawn"
)

// NewTextPayload creates a payload for a plain text message.
func NewTextPayload(text string) Payload {
	return Payload{Text: text}
}

// NewCodePayload creates a payload for a code snippet.
func NewCodePayload(code, filePath, language string) Payload {
	if language == "" && filePath != "" {
		language = DetectLanguage(filePath)
	}
	return Payload{Code: code, FilePath: filePath, Language: language}
}

// NewDiffPayload creates a payload for a diff.
func NewDiffPayload(diff, filePath string) Payload {
	return Payload{Diff: diff, FilePath: filePath}
}

// DetectLanguage guesses a language from a file extension.
func DetectLanguage(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".h", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".sh", ".bash":
		return "bash"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".dockerfile":
		return "dockerfile"
	default:
		return ""
	}
}
