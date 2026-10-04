package config

import (
	"bytes"
	"errors"
	"io"

	"go.yaml.in/yaml/v3"
)

func decodeYAML(data []byte, target any) error {
	// Inspect the document before decoding into defaults: only a single mapping
	// is configuration. Empty/null documents must not silently keep defaults.
	parser := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := parser.Decode(&document); err != nil {
		return errors.New("config: invalid YAML configuration")
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("config: YAML file must contain a mapping")
	}
	var extra yaml.Node
	if err := parser.Decode(&extra); err != io.EOF {
		return errors.New("config: YAML file must contain exactly one document")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		// YAML errors can quote scalar values. Keep config values out of errors.
		return errors.New("config: invalid YAML configuration or unknown field")
	}
	return nil
}
