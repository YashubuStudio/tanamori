package filetext

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

func ExtractTextFromJSON(path string) (string, error) {
	reader, closeFn, err := openDecodedText(path)
	if err != nil {
		return "", err
	}
	defer closeFn()
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var output strings.Builder
	flattenStructured(&output, "", value)
	return output.String(), nil
}

// ExtractTextFromJSONLines extracts one JSON value per line without loading the
// whole input into memory. Output paths start with the one-based record number.
func ExtractTextFromJSONLines(path string) (string, error) {
	reader, closeFn, err := openDecodedText(path)
	if err != nil {
		return "", err
	}
	defer closeFn()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	var output strings.Builder
	record := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record++
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return "", fmt.Errorf("filetext: JSON Lines record %d: %w", record, err)
		}
		flattenStructured(&output, fmt.Sprintf("[%d]", record), value)
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return output.String(), nil
}
func ExtractTextFromYAML(path string) (string, error) {
	content, err := ReadTextFile(path)
	if err != nil {
		return "", err
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return "", err
	}
	var output strings.Builder
	flattenYAMLNode(&output, "", &document)
	return output.String(), nil
}

func ExtractTextFromTOML(path string) (string, error) {
	content, err := ReadTextFile(path)
	if err != nil {
		return "", err
	}
	var value map[string]any
	if _, err := toml.Decode(content, &value); err != nil {
		return "", err
	}
	var output strings.Builder
	flattenStructured(&output, "", value)
	return output.String(), nil
}

func ExtractTextFromINI(path string) (string, error) {
	content, err := ReadTextFile(path)
	if err != nil {
		return "", err
	}
	config, err := ini.Load([]byte(content))
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, section := range config.Sections() {
		prefix := section.Name()
		if prefix == ini.DEFAULT_SECTION {
			prefix = ""
		}
		for _, key := range section.Keys() {
			name := key.Name()
			if prefix != "" {
				name = prefix + "." + name
			}
			writeStructuredValue(&output, name, key.Value())
		}
	}
	return output.String(), nil
}

func flattenStructured(output *strings.Builder, path string, value any) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flattenStructured(output, joinPath(path, key), value[key])
		}
	case []any:
		for index, item := range value {
			flattenStructured(output, fmt.Sprintf("%s[%d]", path, index), item)
		}
	default:
		writeStructuredValue(output, path, fmt.Sprint(value))
	}
}

func flattenYAMLNode(output *strings.Builder, path string, node *yaml.Node) {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			flattenYAMLNode(output, path, child)
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			flattenYAMLNode(output, joinPath(path, key), node.Content[index+1])
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			flattenYAMLNode(output, fmt.Sprintf("%s[%d]", path, index), child)
		}
	case yaml.AliasNode:
		flattenYAMLNode(output, path, node.Alias)
	case yaml.ScalarNode:
		writeStructuredValue(output, path, node.Value)
	}
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func writeStructuredValue(output *strings.Builder, path, value string) {
	if path != "" {
		output.WriteString(path)
		output.WriteString(": ")
	}
	output.WriteString(value)
	output.WriteByte('\n')
}
