package manage

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// GenerateKey generates a 32-byte random key and returns it as a hex string.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// AppendEnvVar appends a KEY=VALUE line to the env file.
// Returns an error if the variable already exists.
func AppendEnvVar(path, name, value string) error {
	// Check for existing var
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, name+"=") {
				return fmt.Errorf("env var %q already exists in %s", name, path)
			}
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s=%s\n", name, value); err != nil {
		return fmt.Errorf("write env var: %w", err)
	}
	return nil
}

// RemoveEnvVar removes a KEY=... line from the env file.
func RemoveEnvVar(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), name+"=") {
			lines = append(lines, line)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// configFile is a minimal representation for YAML round-trip.
type configFile struct {
	root yaml.Node
}

func loadConfigFile(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &configFile{root: root}, nil
}

func (c *configFile) save(path string) error {
	data, err := yaml.Marshal(&c.root)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// mappingNode returns the top-level mapping node.
func (c *configFile) mapping() *yaml.Node {
	if c.root.Kind == yaml.DocumentNode && len(c.root.Content) > 0 {
		return c.root.Content[0]
	}
	return &c.root
}

// findKey finds a key in a mapping node and returns the value node.
func findKey(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// AddAgent adds a new agent to the config file.
func AddAgent(configPath, id, keyEnv string) error {
	cf, err := loadConfigFile(configPath)
	if err != nil {
		return err
	}

	m := cf.mapping()
	agentsNode := findKey(m, "agents")

	newAgent := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "id"},
			{Kind: yaml.ScalarNode, Value: id},
			{Kind: yaml.ScalarNode, Value: "key_env"},
			{Kind: yaml.ScalarNode, Value: keyEnv},
		},
	}

	if agentsNode == nil {
		// Add agents key to mapping
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "agents"},
			&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{newAgent}},
		)
	} else {
		agentsNode.Content = append(agentsNode.Content, newAgent)
	}

	return cf.save(configPath)
}

// RemoveAgent removes an agent by ID from the config file.
func RemoveAgent(configPath, id string) error {
	cf, err := loadConfigFile(configPath)
	if err != nil {
		return err
	}

	m := cf.mapping()
	agentsNode := findKey(m, "agents")
	if agentsNode == nil {
		return fmt.Errorf("no agents section in config")
	}

	var kept []*yaml.Node
	for _, item := range agentsNode.Content {
		if item.Kind == yaml.MappingNode {
			idNode := findKey(item, "id")
			if idNode != nil && idNode.Value == id {
				continue // skip this agent
			}
		}
		kept = append(kept, item)
	}
	agentsNode.Content = kept
	return cf.save(configPath)
}

// AddConclave adds a new conclave to the config file.
func AddConclave(configPath, name, keyEnv, description string) error {
	cf, err := loadConfigFile(configPath)
	if err != nil {
		return err
	}

	m := cf.mapping()
	conclavesNode := findKey(m, "conclaves")

	conclaveValue := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "description"},
			{Kind: yaml.ScalarNode, Value: description},
			{Kind: yaml.ScalarNode, Value: "key_env"},
			{Kind: yaml.ScalarNode, Value: keyEnv},
			{Kind: yaml.ScalarNode, Value: "rules"},
			{Kind: yaml.SequenceNode, Content: []*yaml.Node{
				{Kind: yaml.MappingNode, Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "match"},
					{Kind: yaml.MappingNode, Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Value: "command"},
						{Kind: yaml.ScalarNode, Value: "*"},
					}},
					{Kind: yaml.ScalarNode, Value: "action"},
					{Kind: yaml.ScalarNode, Value: "deny"},
				}},
			}},
		},
	}

	if conclavesNode == nil {
		conclavesNode = &yaml.Node{Kind: yaml.MappingNode}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "conclaves"},
			conclavesNode,
		)
	}

	conclavesNode.Content = append(conclavesNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: name},
		conclaveValue,
	)

	return cf.save(configPath)
}

// RemoveConclave removes a conclave by name from the config file.
func RemoveConclave(configPath, name string) error {
	cf, err := loadConfigFile(configPath)
	if err != nil {
		return err
	}

	m := cf.mapping()
	conclavesNode := findKey(m, "conclaves")
	if conclavesNode == nil {
		return fmt.Errorf("no conclaves section in config")
	}

	var kept []*yaml.Node
	for i := 0; i < len(conclavesNode.Content)-1; i += 2 {
		if conclavesNode.Content[i].Value == name {
			continue // skip key and value
		}
		kept = append(kept, conclavesNode.Content[i], conclavesNode.Content[i+1])
	}
	conclavesNode.Content = kept
	return cf.save(configPath)
}

// AgentInfo holds basic agent info for listing.
type AgentInfo struct {
	ID     string
	KeyEnv string
}

// ListAgents returns all agents from the config file.
func ListAgents(configPath string) ([]AgentInfo, error) {
	cf, err := loadConfigFile(configPath)
	if err != nil {
		return nil, err
	}
	m := cf.mapping()
	agentsNode := findKey(m, "agents")
	if agentsNode == nil {
		return nil, nil
	}

	var agents []AgentInfo
	for _, item := range agentsNode.Content {
		if item.Kind == yaml.MappingNode {
			a := AgentInfo{}
			if n := findKey(item, "id"); n != nil {
				a.ID = n.Value
			}
			if n := findKey(item, "key_env"); n != nil {
				a.KeyEnv = n.Value
			}
			agents = append(agents, a)
		}
	}
	return agents, nil
}

// ConclaveInfo holds basic conclave info for listing.
type ConclaveInfo struct {
	Name        string
	KeyEnv      string
	Description string
}

// ListConclaves returns all conclaves from the config file.
func ListConclaves(configPath string) ([]ConclaveInfo, error) {
	cf, err := loadConfigFile(configPath)
	if err != nil {
		return nil, err
	}
	m := cf.mapping()
	conclavesNode := findKey(m, "conclaves")
	if conclavesNode == nil {
		return nil, nil
	}

	var conclaves []ConclaveInfo
	for i := 0; i < len(conclavesNode.Content)-1; i += 2 {
		name := conclavesNode.Content[i].Value
		valueNode := conclavesNode.Content[i+1]
		c := ConclaveInfo{Name: name}
		if n := findKey(valueNode, "key_env"); n != nil {
			c.KeyEnv = n.Value
		}
		if n := findKey(valueNode, "description"); n != nil {
			c.Description = n.Value
		}
		conclaves = append(conclaves, c)
	}
	return conclaves, nil
}
