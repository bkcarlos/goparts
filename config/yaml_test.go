package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func yamlFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

type yamlConfig struct {
	Name    string        `json:"name" yaml:"app_name" default:"default-name" env:"NAME" required:"true"`
	Port    int           `yaml:"port" default:"8080" env:"PORT"`
	Enabled bool          `yaml:"enabled" default:"true"`
	Timeout time.Duration `yaml:"timeout" default:"5s" env:"TIMEOUT"`
	Hosts   []string      `yaml:"hosts" default:"a,b"`
	Nested  struct {
		Token   string `yaml:"token" default:"default-token" env:"TOKEN" required:"true"`
		Retries int    `yaml:"retries" default:"3"`
	} `yaml:"nested"`
}

func TestYAMLPrecedenceAndTypes(t *testing.T) {
	for _, extension := range []string{".yaml", ".yml", ".YAML"} {
		t.Run(extension, func(t *testing.T) {
			path := yamlFile(t, "config"+extension, `# defaults are applied before this file
app_name: "yaml-service"
port: 9000
enabled: false
timeout: 250ms
hosts:
  - first
  - second
nested:
  token: file-token
`)
			cfg, err := Load[yamlConfig](Options{File: path, EnvPrefix: "APP_", LookupEnv: lookup(map[string]string{"APP_PORT": "9090", "APP_TOKEN": "env-token"})})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Name != "yaml-service" || cfg.Port != 9090 || cfg.Enabled || cfg.Timeout != 250*time.Millisecond || strings.Join(cfg.Hosts, ",") != "first,second" || cfg.Nested.Token != "env-token" || cfg.Nested.Retries != 3 {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
	cfg, err := Load[yamlConfig](Options{File: yamlFile(t, "empty.yaml", "{}\n"), LookupEnv: lookup(nil)})
	if err != nil || cfg.Name != "default-name" || cfg.Port != 8080 || cfg.Timeout != 5*time.Second || cfg.Nested.Retries != 3 {
		t.Fatalf("defaults lost: %+v %v", cfg, err)
	}
}

func TestFormatSelection(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		format     Format
		want       string
	}{
		{"config.json", `{"name":"json"}`, FormatAuto, "json"},
		{"config", `{"name":"legacy"}`, FormatAuto, "legacy"},
		{"config.conf", `{"name":"legacy-ext"}`, FormatAuto, "legacy-ext"},
		{"config.conf", "name: explicit-yaml\n", FormatYAML, "explicit-yaml"},
		{"config.json", "name: override-yaml\n", FormatYAML, "override-yaml"},
		{"config.yaml", `{"name":"override-json"}`, FormatJSON, "override-json"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			cfg, err := Load[struct {
				Name string `json:"name" yaml:"name"`
			}](Options{File: yamlFile(t, tc.name, tc.body), Format: tc.format, LookupEnv: lookup(nil)})
			if err != nil || cfg.Name != tc.want {
				t.Fatalf("cfg=%+v err=%v", cfg, err)
			}
		})
	}
	if _, err := Load[yamlConfig](Options{Format: "toml"}); err == nil {
		t.Fatal("unsupported format accepted")
	}
}

func TestYAMLRejectsInvalidAndHidesValues(t *testing.T) {
	for name, body := range map[string]string{
		"unknown":                 "unknown: super-secret\n",
		"nested unknown":          "nested:\n  unknown: super-secret\n",
		"malformed":               "port: [super-secret\n",
		"wrong type":              "port: super-secret\n",
		"duplicate":               "port: 80\nport: 81\n",
		"nested duplicate":        "nested:\n  retries: 1\n  retries: 2\n",
		"multi document":          "app_name: first\n---\napp_name: second\n",
		"trailing empty document": "{}\n---\n",
		"trailing garbage":        "{}\nsuper-secret",
		"empty":                   "",
		"comments only":           "# super-secret\n",
		"null":                    "null\n",
		"sequence":                "- super-secret\n",
		"scalar":                  "super-secret\n",
		"bad duration":            "timeout: super-secret\n",
		"integer duration":        "timeout: 5000000000\n",
		"recursive alias":         "hosts: &loop [*loop]\n",
		"oversized":               strings.Repeat("#", maxFileBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load[yamlConfig](Options{File: yamlFile(t, "config.yaml", body), LookupEnv: lookup(nil)})
			if err == nil {
				t.Fatal("invalid YAML accepted")
			}
			if !reflect.DeepEqual(cfg, yamlConfig{}) {
				t.Fatalf("partial result returned: %+v", cfg)
			}
			if strings.Contains(err.Error(), "super-secret") {
				t.Fatalf("value leaked: %v", err)
			}
		})
	}
}

func TestYAMLRequiredValidationAndEnvironment(t *testing.T) {
	path := yamlFile(t, "config.yaml", "app_name: valid\n")
	for _, value := range []string{"", "env-name"} {
		cfg, err := Load[yamlConfig](Options{File: path, LookupEnv: lookup(map[string]string{"NAME": value, "TIMEOUT": "1s"})})
		if value == "" {
			if err == nil || !reflect.DeepEqual(cfg, yamlConfig{}) {
				t.Fatal("empty env bypassed required")
			}
		} else if err != nil || cfg.Name != value || cfg.Timeout != time.Second {
			t.Fatalf("env: %+v %v", cfg, err)
		}
	}
	_, err := Load[validated](Options{File: yamlFile(t, "config.yaml", "min: 20\nmax: 10\n"), LookupEnv: lookup(nil)})
	if !errors.Is(err, errValidation) {
		t.Fatalf("validation chain lost: %v", err)
	}
}

func TestYAMLAnchorsAndTextUnmarshaler(t *testing.T) {
	type config struct {
		Name string    `yaml:"name"`
		Copy string    `yaml:"copy"`
		When time.Time `yaml:"when"`
	}
	cfg, err := Load[config](Options{File: yamlFile(t, "config.yaml", "name: &label hello\ncopy: *label\nwhen: 2026-10-04T00:00:00Z\n"), LookupEnv: lookup(nil)})
	if err != nil || cfg.Name != "hello" || cfg.Copy != "hello" || cfg.When.Year() != 2026 {
		t.Fatalf("YAML types: %+v %v", cfg, err)
	}
}
