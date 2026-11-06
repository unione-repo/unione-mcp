// Package brand holds the installation this server fronts: the product name, the
// tool names, the docs and the API it calls. The brand is the file next to this
// one that sets current; a binary carries one brand only.
package brand

import (
	"fmt"
	"regexp"
	"strings"
)

type Brand struct {
	// Key is the server name (<key>-mcp).
	Key string
	// Name is how instructions, descriptions and errors call the product.
	Name    string
	DocsURL string
	// APIRefURL is the docs page of the Web API reference, whose sections tool
	// descriptions link to; empty when the brand has no such page.
	APIRefURL string
	// EnvPrefix names the brand's variables: <EnvPrefix>_API_KEY, <EnvPrefix>_API_URL.
	EnvPrefix string
	// Defaults for <EnvPrefix>_API_URL, AUTHORIZATION_SERVER_URL and OAUTH_TOKEN_PREFIX;
	// empty means the environment must supply it.
	APIURL      string
	AuthServer  string
	TokenPrefix string
	// ToolNames gives every tool the brand's name: the code's id (account.get) → the
	// name the model sees. The code's ids are nobody's names, so every tool is here.
	ToolNames map[string]string
}

// Current is the brand this binary is built for.
func Current() Brand { return current }

// ServerName is serverInfo.name and the User-Agent product.
func (b Brand) ServerName() string { return b.Key + "-mcp" }

// APIKeyEnv and APIURLEnv are the variables of the brand's API key and API URL.
func (b Brand) APIKeyEnv() string { return b.EnvPrefix + "_API_KEY" }
func (b Brand) APIURLEnv() string { return b.EnvPrefix + "_API_URL" }

// Tool is the brand's name for the tool the code calls id. A tool without a name
// is a programming error: the id would reach the model.
func (b Brand) Tool(id string) string {
	name, ok := b.ToolNames[id]
	if !ok {
		panic(fmt.Sprintf("brand %s: no name for tool %s", b.Key, id))
	}
	return name
}

// Render fills a text written for every brand: [[name]] is the product name,
// [[tool:email.send]] a tool's name, [[api_key_env]] and [[api_url_env]] the
// variables of the API key and URL, [[docs_ref]] " (<docs URL>)" or nothing
// when the brand has no docs, and [[api_ref:webhook]] ", <API reference URL>#webhook"
// or nothing when the brand has no API reference page. The texts sit in Go strings and struct tags, so the
// markers need no quotes, and they are not {{ }} because the texts quote the API's
// own {{placeholder}} syntax. An unknown marker is a programming error.
func (b Brand) Render(text string) string {
	return marker.ReplaceAllStringFunc(text, func(m string) string {
		name, arg, _ := strings.Cut(m[2:len(m)-2], ":")
		switch {
		case name == "name" && arg == "":
			return b.Name
		case name == "tool" && arg != "":
			return b.Tool(arg)
		case name == "api_key_env" && arg == "":
			return b.APIKeyEnv()
		case name == "api_url_env" && arg == "":
			return b.APIURLEnv()
		case name == "docs_ref" && arg == "":
			if b.DocsURL == "" {
				return ""
			}
			return " (" + b.DocsURL + ")"
		case name == "api_ref" && arg != "":
			if b.APIRefURL == "" {
				return ""
			}
			return ", " + b.APIRefURL + "#" + arg
		}
		panic(fmt.Sprintf("brand: unknown marker %s", m))
	})
}

var marker = regexp.MustCompile(`\[\[[a-z_]+(?::[a-z_.]+)?\]\]`)
