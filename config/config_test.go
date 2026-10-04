package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type appConfig struct {
	Name    string        `json:"name" default:"default-name" env:"NAME" required:"true"`
	Port    int           `json:"port" default:"8080" env:"PORT"`
	Enabled bool          `json:"enabled" default:"true" env:"ENABLED"`
	Timeout time.Duration `json:"timeout" default:"5s" env:"TIMEOUT"`
	Hosts   []string      `json:"hosts" default:"a,b" env:"HOSTS"`
	Nested  struct {
		Token string `json:"token" env:"TOKEN" required:"true"`
	} `json:"nested"`
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}

func file(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrecedenceAndTypes(t *testing.T) {
	cfg, err := Load[appConfig](Options{
		File:      file(t, `{"name":"file-name","port":9000,"enabled":true,"nested":{"token":"file-token"}}`),
		EnvPrefix: "APP_",
		LookupEnv: lookup(map[string]string{"APP_PORT": "9090", "APP_ENABLED": "false", "APP_TIMEOUT": "250ms", "APP_HOSTS": "x, y", "APP_TOKEN": "env-token"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "file-name" || cfg.Port != 9090 || cfg.Enabled || cfg.Timeout != 250*time.Millisecond || strings.Join(cfg.Hosts, ",") != "x,y" || cfg.Nested.Token != "env-token" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	defaults, err := Load[appConfig](Options{LookupEnv: lookup(map[string]string{"TOKEN": "secret"})})
	if err != nil || defaults.Port != 8080 || !defaults.Enabled || defaults.Timeout != 5*time.Second {
		t.Fatalf("defaults: %+v, %v", defaults, err)
	}
}

func TestFailuresReturnZeroAndHideValues(t *testing.T) {
	for name, options := range map[string]Options{
		"required":         {LookupEnv: lookup(nil)},
		"parse":            {LookupEnv: lookup(map[string]string{"PORT": "super-secret"})},
		"empty required":   {LookupEnv: lookup(map[string]string{"NAME": "", "TOKEN": "x"})},
		"unknown JSON":     {File: file(t, `{"unknown":1}`), LookupEnv: lookup(nil)},
		"malformed JSON":   {File: file(t, `{"port":`), LookupEnv: lookup(nil)},
		"trailing JSON":    {File: file(t, `{} {}`), LookupEnv: lookup(nil)},
		"trailing garbage": {File: file(t, `{} x`), LookupEnv: lookup(nil)},
		"null":             {File: file(t, `null`), LookupEnv: lookup(nil)},
		"missing file":     {File: filepath.Join(t.TempDir(), "missing"), LookupEnv: lookup(nil)},
		"oversized":        {File: file(t, strings.Repeat(" ", maxFileBytes+1)), LookupEnv: lookup(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load[appConfig](options)
			if err == nil {
				t.Fatal("expected failure")
			}
			if cfg.Name != "" || cfg.Port != 0 || cfg.Hosts != nil {
				t.Fatal("partial config returned")
			}
			if strings.Contains(err.Error(), "super-secret") {
				t.Fatal("value leaked")
			}
		})
	}
	if _, err := Load[int](Options{}); err == nil {
		t.Fatal("nonstruct accepted")
	}
	if _, err := Load[*appConfig](Options{}); err == nil {
		t.Fatal("pointer T accepted")
	}
}

var errValidation = errors.New("invalid range")

type validated struct {
	Min int `default:"10"`
	Max int `default:"5" env:"MAX"`
}

func (c *validated) Validate() error {
	if c.Min > c.Max {
		return errValidation
	}
	return nil
}

func TestValidationAndOSEnv(t *testing.T) {
	if _, err := Load[validated](Options{LookupEnv: lookup(nil)}); !errors.Is(err, errValidation) {
		t.Fatalf("validator error: %v", err)
	}
	t.Setenv("TEST_CONFIG_MAX", "20")
	cfg, err := Load[validated](Options{EnvPrefix: "TEST_CONFIG_"})
	if err != nil || cfg.Max != 20 {
		t.Fatalf("env config: %+v, %v", cfg, err)
	}
}

func TestSupportedScalarsAndText(t *testing.T) {
	type values struct {
		Byte    uint8     `default:"255"`
		Signed  int8      `default:"-12"`
		Ratio   float64   `default:"1.25"`
		When    time.Time `default:"2026-01-01T00:00:00Z"`
		Empty   []string  `default:""`
		Hidden  string    `env:"-" default:"keep"`
		private string
	}
	cfg, err := Load[values](Options{LookupEnv: lookup(map[string]string{"-": "replace"})})
	if err != nil || cfg.Byte != 255 || cfg.Signed != -12 || cfg.Ratio != 1.25 || cfg.When.Year() != 2026 || len(cfg.Empty) != 0 || cfg.Hidden != "keep" {
		t.Fatalf("scalar config: %+v, %v", cfg, err)
	}
}

func TestInvalidTagsAndOverflow(t *testing.T) {
	checks := []func() error{
		func() error {
			_, err := Load[struct {
				N int8 `default:"128"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N uint8 `default:"-1"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N bool `default:"yes"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N float64 `default:"bad"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N time.Duration `default:"bad"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N []int `default:"1,2"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N *int `default:"1"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N map[string]int `default:"x"`
			}](Options{})
			return err
		},
		func() error {
			_, err := Load[struct {
				N string `required:"yes"`
			}](Options{})
			return err
		},
	}
	for i, check := range checks {
		if check() == nil {
			t.Errorf("case %d accepted invalid tag", i)
		}
	}
}
