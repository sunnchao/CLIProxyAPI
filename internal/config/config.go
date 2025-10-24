// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// Config represents the application's configuration, loaded from a YAML file.
type Config struct {
	config.SDKConfig `yaml:",inline"`
	// Port is the network port on which the API server will listen.
	Port int `yaml:"port" json:"-"`

	// AuthDir is the directory where authentication token files are stored.
	AuthDir string `yaml:"auth-dir" json:"-"`

	// Debug enables or disables debug-level logging and other debug features.
	Debug bool `yaml:"debug" json:"debug"`

	// LoggingToFile controls whether application logs are written to rotating files or stdout.
	LoggingToFile bool `yaml:"logging-to-file" json:"logging-to-file"`

	// UsageStatisticsEnabled toggles in-memory usage aggregation; when false, usage data is discarded.
	UsageStatisticsEnabled bool `yaml:"usage-statistics-enabled" json:"usage-statistics-enabled"`

	// DisableCooling disables quota cooldown scheduling when true.
	DisableCooling bool `yaml:"disable-cooling" json:"disable-cooling"`

	// QuotaExceeded defines the behavior when a quota is exceeded.
	QuotaExceeded QuotaExceeded `yaml:"quota-exceeded" json:"quota-exceeded"`

	// GlAPIKey is the API key for the generative language API.
	GlAPIKey []string `yaml:"generative-language-api-key" json:"generative-language-api-key"`

	// RequestRetry defines the retry times when the request failed.
	RequestRetry int `yaml:"request-retry" json:"request-retry"`

	// ClaudeKey defines a list of Claude API key configurations as specified in the YAML configuration file.
	ClaudeKey []ClaudeKey `yaml:"claude-api-key" json:"claude-api-key"`

	// Codex defines a list of Codex API key configurations as specified in the YAML configuration file.
	CodexKey []CodexKey `yaml:"codex-api-key" json:"codex-api-key"`

	// OpenAICompatibility defines OpenAI API compatibility configurations for external providers.
	OpenAICompatibility []OpenAICompatibility `yaml:"openai-compatibility" json:"openai-compatibility"`

	// RemoteManagement nests management-related options under 'remote-management'.
	RemoteManagement RemoteManagement `yaml:"remote-management" json:"-"`
}

// RemoteManagement holds management API configuration under 'remote-management'.
type RemoteManagement struct {
	// AllowRemote toggles remote (non-localhost) access to management API.
	AllowRemote bool `yaml:"allow-remote"`
	// SecretKey is the management key (plaintext or bcrypt hashed). YAML key intentionally 'secret-key'.
	SecretKey string `yaml:"secret-key"`
	// DisableControlPanel skips serving and syncing the bundled management UI when true.
	DisableControlPanel bool `yaml:"disable-control-panel"`
}

// QuotaExceeded defines the behavior when API quota limits are exceeded.
// It provides configuration options for automatic failover mechanisms.
type QuotaExceeded struct {
	// SwitchProject indicates whether to automatically switch to another project when a quota is exceeded.
	SwitchProject bool `yaml:"switch-project" json:"switch-project"`

	// SwitchPreviewModel indicates whether to automatically switch to a preview model when a quota is exceeded.
	SwitchPreviewModel bool `yaml:"switch-preview-model" json:"switch-preview-model"`
}

// ClaudeKey represents the configuration for a Claude API key,
// including the API key itself and an optional base URL for the API endpoint.
type ClaudeKey struct {
	// APIKey is the authentication key for accessing Claude API services.
	APIKey string `yaml:"api-key" json:"api-key"`

	// BaseURL is the base URL for the Claude API endpoint.
	// If empty, the default Claude API URL will be used.
	BaseURL string `yaml:"base-url" json:"base-url"`

	// ProxyURL overrides the global proxy setting for this API key if provided.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`
}

// CodexKey represents the configuration for a Codex API key,
// including the API key itself and an optional base URL for the API endpoint.
type CodexKey struct {
	// APIKey is the authentication key for accessing Codex API services.
	APIKey string `yaml:"api-key" json:"api-key"`

	// BaseURL is the base URL for the Codex API endpoint.
	// If empty, the default Codex API URL will be used.
	BaseURL string `yaml:"base-url" json:"base-url"`

	// ProxyURL overrides the global proxy setting for this API key if provided.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`
}

// OpenAICompatibility represents the configuration for OpenAI API compatibility
// with external providers, allowing model aliases to be routed through OpenAI API format.
type OpenAICompatibility struct {
	// Name is the identifier for this OpenAI compatibility configuration.
	Name string `yaml:"name" json:"name"`

	// BaseURL is the base URL for the external OpenAI-compatible API endpoint.
	BaseURL string `yaml:"base-url" json:"base-url"`

	// APIKeys are the authentication keys for accessing the external API services.
	// Deprecated: Use APIKeyEntries instead to support per-key proxy configuration.
	APIKeys []string `yaml:"api-keys,omitempty" json:"api-keys,omitempty"`

	// APIKeyEntries defines API keys with optional per-key proxy configuration.
	APIKeyEntries []OpenAICompatibilityAPIKey `yaml:"api-key-entries,omitempty" json:"api-key-entries,omitempty"`

	// Models defines the model configurations including aliases for routing.
	Models []OpenAICompatibilityModel `yaml:"models" json:"models"`
}

// OpenAICompatibilityAPIKey represents an API key configuration with optional proxy setting.
type OpenAICompatibilityAPIKey struct {
	// APIKey is the authentication key for accessing the external API services.
	APIKey string `yaml:"api-key" json:"api-key"`

	// ProxyURL overrides the global proxy setting for this API key if provided.
	ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`
}

// OpenAICompatibilityModel represents a model configuration for OpenAI compatibility,
// including the actual model name and its alias for API routing.
type OpenAICompatibilityModel struct {
	// Name is the actual model name used by the external provider.
	Name string `yaml:"name" json:"name"`

	// Alias is the model name alias that clients will use to reference this model.
	Alias string `yaml:"alias" json:"alias"`
}

// LoadConfig reads a YAML configuration file from the given path,
// unmarshals it into a Config struct, applies environment variable overrides,
// and returns it.
//
// Parameters:
//   - configFile: The path to the YAML configuration file
//
// Returns:
//   - *Config: The loaded configuration
//   - error: An error if the configuration could not be loaded
func LoadConfig(configFile string) (*Config, error) {
	return LoadConfigOptional(configFile, false)
}

// LoadConfigOptional reads YAML from configFile.
// If optional is true and the file is missing, it returns an empty Config.
// If optional is true and the file is empty or invalid, it returns an empty Config.
func LoadConfigOptional(configFile string, optional bool) (*Config, error) {
	// Read the entire configuration file into memory.
	data, err := os.ReadFile(configFile)
	if err != nil {
		if optional {
			if os.IsNotExist(err) || errors.Is(err, syscall.EISDIR) {
				// Missing and optional: return empty config (cloud deploy standby).
				return &Config{}, nil
			}
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// In cloud deploy mode (optional=true), if file is empty or contains only whitespace, return empty config.
	if optional && len(data) == 0 {
		return &Config{}, nil
	}

	// Unmarshal the YAML data into the Config struct.
	var cfg Config
	// Set defaults before unmarshal so that absent keys keep defaults.
	cfg.LoggingToFile = false
	cfg.UsageStatisticsEnabled = false
	cfg.DisableCooling = false
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		if optional {
			// In cloud deploy mode, if YAML parsing fails, return empty config instead of error.
			return &Config{}, nil
		}
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Hash remote management key if plaintext is detected (nested)
	// We consider a value to be already hashed if it looks like a bcrypt hash ($2a$, $2b$, or $2y$ prefix).
	if cfg.RemoteManagement.SecretKey != "" && !looksLikeBcrypt(cfg.RemoteManagement.SecretKey) {
		hashed, errHash := hashSecret(cfg.RemoteManagement.SecretKey)
		if errHash != nil {
			return nil, fmt.Errorf("failed to hash remote management key: %w", errHash)
		}
		cfg.RemoteManagement.SecretKey = hashed

		// Persist the hashed value back to the config file to avoid re-hashing on next startup.
		// Preserve YAML comments and ordering; update only the nested key.
		_ = SaveConfigPreserveCommentsUpdateNestedScalar(configFile, []string{"remote-management", "secret-key"}, hashed)
	}

	// Sync request authentication providers with inline API keys for backwards compatibility.
	syncInlineAccessProvider(&cfg)

	// Sanitize OpenAI compatibility providers: drop entries without base-url
	sanitizeOpenAICompatibility(&cfg)

	// Sanitize Codex keys: drop entries without base-url
	sanitizeCodexKeys(&cfg)

	// Return the populated configuration struct.
	return &cfg, nil
}

// sanitizeOpenAICompatibility removes OpenAI-compatibility provider entries that are
// not actionable, specifically those missing a BaseURL. It trims whitespace before
// evaluation and preserves the relative order of remaining entries.
func sanitizeOpenAICompatibility(cfg *Config) {
	if cfg == nil || len(cfg.OpenAICompatibility) == 0 {
		return
	}
	out := make([]OpenAICompatibility, 0, len(cfg.OpenAICompatibility))
	for i := range cfg.OpenAICompatibility {
		e := cfg.OpenAICompatibility[i]
		e.Name = strings.TrimSpace(e.Name)
		e.BaseURL = strings.TrimSpace(e.BaseURL)
		if e.BaseURL == "" {
			// Skip providers with no base-url; treated as removed
			continue
		}
		out = append(out, e)
	}
	cfg.OpenAICompatibility = out
}

// sanitizeCodexKeys removes Codex API key entries missing a BaseURL.
// It trims whitespace and preserves order for remaining entries.
func sanitizeCodexKeys(cfg *Config) {
	if cfg == nil || len(cfg.CodexKey) == 0 {
		return
	}
	out := make([]CodexKey, 0, len(cfg.CodexKey))
	for i := range cfg.CodexKey {
		e := cfg.CodexKey[i]
		e.BaseURL = strings.TrimSpace(e.BaseURL)
		if e.BaseURL == "" {
			continue
		}
		out = append(out, e)
	}
	cfg.CodexKey = out
}

func syncInlineAccessProvider(cfg *Config) {
	if cfg == nil {
		return
	}
	if len(cfg.APIKeys) == 0 {
		if provider := cfg.ConfigAPIKeyProvider(); provider != nil && len(provider.APIKeys) > 0 {
			cfg.APIKeys = append([]string(nil), provider.APIKeys...)
		}
	}
	cfg.Access.Providers = nil
}

// looksLikeBcrypt returns true if the provided string appears to be a bcrypt hash.
func looksLikeBcrypt(s string) bool {
	return len(s) > 4 && (s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$")
}

// hashSecret hashes the given secret using bcrypt.
func hashSecret(secret string) (string, error) {
	// Use default cost for simplicity.
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

// SaveConfigPreserveComments writes the config back to YAML while preserving existing comments
// and key ordering by loading the original file into a yaml.Node tree and updating values in-place.
func SaveConfigPreserveComments(configFile string, cfg *Config) error {
	persistCfg := sanitizeConfigForPersist(cfg)
	// Load original YAML as a node tree to preserve comments and ordering.
	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	var original yaml.Node
	if err = yaml.Unmarshal(data, &original); err != nil {
		return err
	}
	if original.Kind != yaml.DocumentNode || len(original.Content) == 0 {
		return fmt.Errorf("invalid yaml document structure")
	}
	if original.Content[0] == nil || original.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("expected root mapping node")
	}

	// Marshal the current cfg to YAML, then unmarshal to a yaml.Node we can merge from.
	rendered, err := yaml.Marshal(persistCfg)
	if err != nil {
		return err
	}
	var generated yaml.Node
	if err = yaml.Unmarshal(rendered, &generated); err != nil {
		return err
	}
	if generated.Kind != yaml.DocumentNode || len(generated.Content) == 0 || generated.Content[0] == nil {
		return fmt.Errorf("invalid generated yaml structure")
	}
	if generated.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("expected generated root mapping node")
	}

	// Remove deprecated auth block before merging to avoid persisting it again.
	removeMapKey(original.Content[0], "auth")

	// Merge generated into original in-place, preserving comments/order of existing nodes.
	mergeMappingPreserve(original.Content[0], generated.Content[0])
	normalizeCollectionNodeStyles(original.Content[0])

	// Write back.
	f, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err = enc.Encode(&original); err != nil {
		_ = enc.Close()
		return err
	}
	return enc.Close()
}

func sanitizeConfigForPersist(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.SDKConfig = cfg.SDKConfig
	clone.SDKConfig.Access = config.AccessConfig{}
	return &clone
}

// SaveConfigPreserveCommentsUpdateNestedScalar updates a nested scalar key path like ["a","b"]
// while preserving comments and positions.
func SaveConfigPreserveCommentsUpdateNestedScalar(configFile string, path []string, value string) error {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err = yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("invalid yaml document structure")
	}
	node := root.Content[0]
	// descend mapping nodes following path
	for i, key := range path {
		if i == len(path)-1 {
			// set final scalar
			v := getOrCreateMapValue(node, key)
			v.Kind = yaml.ScalarNode
			v.Tag = "!!str"
			v.Value = value
		} else {
			next := getOrCreateMapValue(node, key)
			if next.Kind != yaml.MappingNode {
				next.Kind = yaml.MappingNode
				next.Tag = "!!map"
			}
			node = next
		}
	}
	f, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err = enc.Encode(&root); err != nil {
		_ = enc.Close()
		return err
	}
	return enc.Close()
}

// getOrCreateMapValue finds the value node for a given key in a mapping node.
// If not found, it appends a new key/value pair and returns the new value node.
func getOrCreateMapValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode.Kind != yaml.MappingNode {
		mapNode.Kind = yaml.MappingNode
		mapNode.Tag = "!!map"
		mapNode.Content = nil
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		k := mapNode.Content[i]
		if k.Value == key {
			return mapNode.Content[i+1]
		}
	}
	// append new key/value
	mapNode.Content = append(mapNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key})
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
	mapNode.Content = append(mapNode.Content, val)
	return val
}

// mergeMappingPreserve merges keys from src into dst mapping node while preserving
// key order and comments of existing keys in dst. Unknown keys from src are appended
// to dst at the end, copying their node structure from src.
func mergeMappingPreserve(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		// If kinds do not match, prefer replacing dst with src semantics in-place
		// but keep dst node object to preserve any attached comments at the parent level.
		copyNodeShallow(dst, src)
		return
	}
	// Build a lookup of existing keys in dst
	for i := 0; i+1 < len(src.Content); i += 2 {
		sk := src.Content[i]
		sv := src.Content[i+1]
		idx := findMapKeyIndex(dst, sk.Value)
		if idx >= 0 {
			// Merge into existing value node
			dv := dst.Content[idx+1]
			mergeNodePreserve(dv, sv)
		} else {
			// Append new key/value pair by deep-copying from src
			dst.Content = append(dst.Content, deepCopyNode(sk), deepCopyNode(sv))
		}
	}
}

// mergeNodePreserve merges src into dst for scalars, mappings and sequences while
// reusing destination nodes to keep comments and anchors. For sequences, it updates
// in-place by index.
func mergeNodePreserve(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}
	switch src.Kind {
	case yaml.MappingNode:
		if dst.Kind != yaml.MappingNode {
			copyNodeShallow(dst, src)
		}
		mergeMappingPreserve(dst, src)
	case yaml.SequenceNode:
		// Preserve explicit null style if dst was null and src is empty sequence
		if dst.Kind == yaml.ScalarNode && dst.Tag == "!!null" && len(src.Content) == 0 {
			// Keep as null to preserve original style
			return
		}
		if dst.Kind != yaml.SequenceNode {
			dst.Kind = yaml.SequenceNode
			dst.Tag = "!!seq"
			dst.Content = nil
		}
		// Update elements in place
		minContent := len(dst.Content)
		if len(src.Content) < minContent {
			minContent = len(src.Content)
		}
		for i := 0; i < minContent; i++ {
			if dst.Content[i] == nil {
				dst.Content[i] = deepCopyNode(src.Content[i])
				continue
			}
			mergeNodePreserve(dst.Content[i], src.Content[i])
		}
		// Append any extra items from src
		for i := len(dst.Content); i < len(src.Content); i++ {
			dst.Content = append(dst.Content, deepCopyNode(src.Content[i]))
		}
		// Truncate if dst has extra items not in src
		if len(src.Content) < len(dst.Content) {
			dst.Content = dst.Content[:len(src.Content)]
		}
	case yaml.ScalarNode, yaml.AliasNode:
		// For scalars, update Tag and Value but keep Style from dst to preserve quoting
		dst.Kind = src.Kind
		dst.Tag = src.Tag
		dst.Value = src.Value
		// Keep dst.Style as-is intentionally
	case 0:
		// Unknown/empty kind; do nothing
	default:
		// Fallback: replace shallowly
		copyNodeShallow(dst, src)
	}
}

// findMapKeyIndex returns the index of key node in dst mapping (index of key, not value).
// Returns -1 when not found.
func findMapKeyIndex(mapNode *yaml.Node, key string) int {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i] != nil && mapNode.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// deepCopyNode creates a deep copy of a yaml.Node graph.
func deepCopyNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cp := *n
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i := range n.Content {
			cp.Content[i] = deepCopyNode(n.Content[i])
		}
	}
	return &cp
}

// copyNodeShallow copies type/tag/value and resets content to match src, but
// keeps the same destination node pointer to preserve parent relations/comments.
func copyNodeShallow(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}
	dst.Kind = src.Kind
	dst.Tag = src.Tag
	dst.Value = src.Value
	// Replace content with deep copy from src
	if len(src.Content) > 0 {
		dst.Content = make([]*yaml.Node, len(src.Content))
		for i := range src.Content {
			dst.Content[i] = deepCopyNode(src.Content[i])
		}
	} else {
		dst.Content = nil
	}
}

func removeMapKey(mapNode *yaml.Node, key string) {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode || key == "" {
		return
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i] != nil && mapNode.Content[i].Value == key {
			mapNode.Content = append(mapNode.Content[:i], mapNode.Content[i+2:]...)
			return
		}
	}
}

// normalizeCollectionNodeStyles forces YAML collections to use block notation, keeping
// lists and maps readable. Empty sequences retain flow style ([]) so empty list markers
// remain compact.
func normalizeCollectionNodeStyles(node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.MappingNode:
		node.Style = 0
		for i := range node.Content {
			normalizeCollectionNodeStyles(node.Content[i])
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			node.Style = yaml.FlowStyle
		} else {
			node.Style = 0
		}
		for i := range node.Content {
			normalizeCollectionNodeStyles(node.Content[i])
		}
	default:
		// Scalars keep their existing style to preserve quoting
	}
}
