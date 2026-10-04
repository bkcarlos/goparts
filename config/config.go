// Package config loads typed configuration from defaults, JSON/YAML and environment variables.
package config

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const maxFileBytes = 1024 * 1024

type Format string

const (
	FormatAuto Format = "" // .yaml/.yml select YAML; all other extensions retain JSON behavior
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

type Options struct {
	Defaults     fs.FS // embedded defaults; DefaultsFile selects a file in it
	DefaultsFile string
	Files        []string // overlay files in order, before File
	File         string   // optional JSON/YAML file; an explicitly supplied missing file is an error
	Format       Format   // default auto-detects from extension; explicit format overrides extension
	EnvPrefix    string
	LookupEnv    func(string) (string, bool) // nil uses os.LookupEnv
}

// Validator optionally checks cross-field constraints after loading.
type Validator interface{ Validate() error }

// Load loads a struct T in order: default tags, JSON/YAML file, then env tags.
// Nested value structs are supported. required:"true" rejects zero values.
// A load failure returns the zero T; no partially loaded configuration is exposed.
func Load[T any](opts Options) (T, error) {
	var result, zero T
	switch opts.Format {
	case FormatAuto, FormatJSON, FormatYAML:
	default:
		return zero, errors.New("config: unsupported file format")
	}
	v := reflect.ValueOf(&result).Elem()
	if v.Kind() != reflect.Struct {
		return zero, errors.New("config: T must be a struct")
	}
	if err := walkInitializing(v, "", func(f reflect.StructField, value reflect.Value, path string) error {
		if s, ok := f.Tag.Lookup("default"); ok {
			if err := set(value, s); err != nil {
				return fmt.Errorf("config: invalid default for %s (%s)", path, value.Type())
			}
		}
		return nil
	}); err != nil {
		return zero, err
	}
	if opts.DefaultsFile != "" {
		if opts.Defaults == nil {
			return zero, errors.New("config: Defaults FS required")
		}
		f, err := opts.Defaults.Open(opts.DefaultsFile)
		if err != nil {
			return zero, err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
		f.Close()
		if err != nil {
			return zero, err
		}
		if err = decodeFile(data, opts.DefaultsFile, opts.Format, &result); err != nil {
			return zero, err
		}
	}
	for _, path := range opts.Files {
		if err := loadFile(path, opts.Format, &result); err != nil {
			return zero, err
		}
	}
	if opts.File != "" {
		if err := loadFile(opts.File, opts.Format, &result); err != nil {
			return zero, err
		}
	}
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if err := walkInitializing(v, "", func(f reflect.StructField, value reflect.Value, path string) error {
		if name := f.Tag.Get("env"); name != "" && name != "-" {
			key := opts.EnvPrefix + name
			if s, ok := lookup(key); ok {
				if err := set(value, s); err != nil {
					return fmt.Errorf("config: invalid environment variable %s for %s (%s)", key, path, value.Type())
				}
			}
		}
		return nil
	}); err != nil {
		return zero, err
	}
	if err := walk(v, "", func(f reflect.StructField, value reflect.Value, path string) error {
		required := f.Tag.Get("required")
		if required != "" && required != "true" && required != "false" {
			return fmt.Errorf("config: invalid required tag on %s", path)
		}
		if required == "true" && value.IsZero() {
			return fmt.Errorf("config: %s is required", path)
		}
		return validateRules(f, value, path)
	}); err != nil {
		return zero, err
	}
	if err := validateNested(v, 0); err != nil {
		return zero, err
	}
	if validator, ok := any(&result).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return zero, fmt.Errorf("config: validation failed: %w", err)
		}
	}
	return result, nil
}

func loadFile(path string, format Format, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("config: open file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return fmt.Errorf("config: read file: %w", err)
	}
	return decodeFile(data, path, format, target)
}

func decodeFile(data []byte, path string, format Format, target any) error {
	if len(data) > maxFileBytes {
		return errors.New("config: file exceeds 1 MiB")
	}
	if format == FormatAuto {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml":
			format = FormatYAML
		default:
			format = FormatJSON
		}
	}
	if format == FormatYAML {
		return decodeYAML(data, target)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return errors.New("config: file must contain a JSON object")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return errors.New("config: invalid JSON configuration or unknown field")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("config: file must contain exactly one JSON object")
	}
	return nil
}

var textType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
var durationType = reflect.TypeOf(time.Duration(0))

func walk(v reflect.Value, prefix string, fn func(reflect.StructField, reflect.Value, string) error) error {
	return walkFields(v, prefix, fn, false, map[reflect.Type]bool{}, 0)
}
func walkInitializing(v reflect.Value, prefix string, fn func(reflect.StructField, reflect.Value, string) error) error {
	return walkFields(v, prefix, fn, true, map[reflect.Type]bool{}, 0)
}
func walkFields(v reflect.Value, prefix string, fn func(reflect.StructField, reflect.Value, string) error, initialize bool, ancestors map[reflect.Type]bool, depth int) error {
	if depth > 64 {
		return errors.New("config: nesting exceeds limit")
	}
	ancestors[v.Type()] = true
	defer delete(ancestors, v.Type())
	for i := 0; i < v.NumField(); i++ {
		f, value := v.Type().Field(i), v.Field(i)
		if !f.IsExported() {
			continue
		}
		path := prefix + f.Name
		if err := fn(f, value, path); err != nil {
			return err
		}
		child := value
		var pending bool
		if value.Kind() == reflect.Pointer {
			if value.Type().Elem().Kind() != reflect.Struct || value.Type().Implements(textType) {
				continue
			}
			if value.IsNil() {
				if !initialize || ancestors[value.Type().Elem()] {
					continue
				}
				child = reflect.New(value.Type().Elem()).Elem()
				pending = true
			} else {
				child = value.Elem()
			}
		}
		if child.Kind() == reflect.Struct && !child.Addr().Type().Implements(textType) {
			if err := walkFields(child, path+".", fn, initialize, ancestors, depth+1); err != nil {
				return err
			}
			if pending && !child.IsZero() {
				value.Set(child.Addr())
			}
		}
	}
	return nil
}

func set(v reflect.Value, s string) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return set(v.Elem(), s)
	}
	if v.Addr().Type().Implements(textType) {
		return v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}
	if v.Type() == durationType {
		d, err := time.ParseDuration(s)
		if err == nil {
			v.SetInt(int64(d))
		}
		return err
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		x, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		v.SetBool(x)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		x, err := strconv.ParseInt(s, 10, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetInt(x)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		x, err := strconv.ParseUint(s, 10, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetUint(x)
	case reflect.Float32, reflect.Float64:
		x, err := strconv.ParseFloat(s, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetFloat(x)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.String {
			return errors.New("only string slices are supported")
		}
		parts := []string{}
		if s != "" {
			parts = strings.Split(s, ",")
		}
		out := reflect.MakeSlice(v.Type(), len(parts), len(parts))
		for i, part := range parts {
			out.Index(i).SetString(strings.TrimSpace(part))
		}
		v.Set(out)
	default:
		return errors.New("unsupported field type")
	}
	return nil
}
