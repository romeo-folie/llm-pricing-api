package handlers

// ModelAliases maps common lowercase model name aliases to their canonical
// provider/model-slug identifiers. Used by the /v1/ask NL parser to normalise
// free-form model references in user queries.
//
// Keys must be lowercased and may contain spaces (the parser lowercases the
// query before lookup). Values are canonical slugs matching the slug column
// in the models table.
var ModelAliases = map[string]string{
	// OpenAI
	"gpt4":            "openai/gpt-4",
	"gpt-4":           "openai/gpt-4",
	"gpt4o":           "openai/gpt-4o",
	"gpt-4o":          "openai/gpt-4o",
	"gpt4o mini":      "openai/gpt-4o-mini",
	"gpt-4o mini":     "openai/gpt-4o-mini",
	"gpt4o-mini":      "openai/gpt-4o-mini",
	"gpt-4o-mini":     "openai/gpt-4o-mini",
	"gpt4 turbo":      "openai/gpt-4-turbo",
	"gpt-4 turbo":     "openai/gpt-4-turbo",
	"o1":              "openai/o1",
	"o1 mini":         "openai/o1-mini",
	"o3 mini":         "openai/o3-mini",
	"openai":          "openai/gpt-4o",

	// Anthropic
	"claude":              "anthropic/claude-3-5-sonnet-20241022",
	"claude sonnet":       "anthropic/claude-3-5-sonnet-20241022",
	"claude 3.5 sonnet":   "anthropic/claude-3-5-sonnet-20241022",
	"claude-3.5-sonnet":   "anthropic/claude-3-5-sonnet-20241022",
	"claude opus":         "anthropic/claude-3-opus-20240229",
	"claude 3 opus":       "anthropic/claude-3-opus-20240229",
	"claude-3-opus":       "anthropic/claude-3-opus-20240229",
	"claude haiku":        "anthropic/claude-3-haiku-20240307",
	"claude 3 haiku":      "anthropic/claude-3-haiku-20240307",
	"claude-3-haiku":      "anthropic/claude-3-haiku-20240307",
	"claude 3.5 haiku":    "anthropic/claude-3-5-haiku-20241022",
	"claude-3.5-haiku":    "anthropic/claude-3-5-haiku-20241022",
	"anthropic":           "anthropic/claude-3-5-sonnet-20241022",

	// Google
	"gemini":              "google/gemini-pro-1.5",
	"gemini pro":          "google/gemini-pro-1.5",
	"gemini 1.5 pro":      "google/gemini-pro-1.5",
	"gemini flash":        "google/gemini-flash-1.5",
	"gemini 1.5 flash":    "google/gemini-flash-1.5",
	"gemini 2.0 flash":    "google/gemini-2.0-flash-001",
	"gemini ultra":        "google/gemini-ultra",
	"google":              "google/gemini-pro-1.5",

	// Meta
	"llama":            "meta-llama/llama-3.1-70b-instruct",
	"llama 3":          "meta-llama/llama-3.1-70b-instruct",
	"llama 3.1":        "meta-llama/llama-3.1-70b-instruct",
	"llama 3.1 70b":    "meta-llama/llama-3.1-70b-instruct",
	"llama 3.1 8b":     "meta-llama/llama-3.1-8b-instruct",

	// Mistral
	"mistral":         "mistralai/mistral-7b-instruct",
	"mistral 7b":      "mistralai/mistral-7b-instruct",
	"mixtral":         "mistralai/mixtral-8x7b-instruct",
	"mistral large":   "mistralai/mistral-large",
	"mistral small":   "mistralai/mistral-small",

	// DeepSeek
	"deepseek":        "deepseek/deepseek-chat",
	"deepseek chat":   "deepseek/deepseek-chat",
	"deepseek coder":  "deepseek/deepseek-coder",
	"deepseek r1":     "deepseek/deepseek-r1",
}
