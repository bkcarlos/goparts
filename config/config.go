// Package config loads typed configuration from defaults, JSON and environment variables.
package config

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const maxFileBytes = 1024 * 1024

type Options struct {
	File      string // optional JSON file; an explicitly supplied missing file is an error
	EnvPrefix string
	LookupEnv func(string) (string, bool) // nil uses os.LookupEnv
}

// Validator optionally checks cross-field constraints after loading.
type Validator interface{ Validate() error }

// Load loads a struct T in order: default tags, JSON file, then env tags.
// Nested value structs are supported. required:"true" rejects zero values.
// A load failure returns the zero T; no partially loaded configuration is exposed.
func Load[T any](opts Options) (T, error) {
	var result, zero T
	v := reflect.ValueOf(&result).Elem()
	if v.Kind() != reflect.Struct {
		return zero, errors.New("config: T must be a struct")
	}
	if err := walk(v, "", func(f reflect.StructField, value reflect.Value, path string) error {
		if s, ok := f.Tag.Lookup("default"); ok {
			if err := set(value, s); err != nil {
				return fmt.Errorf("config: invalid default for %s (%s)", path, value.Type())
			}
		}
		return nil
	}); err != nil {
		return zero, err
	}
	if opts.File != "" {
		if err := loadFile(opts.File, &result); err != nil {
			return zero, err
		}
	}
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if err := walk(v, "", func(f reflect.StructField, value reflect.Value, path string) error {
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
		return nil
	}); err != nil {
		return zero, err
	}
	if validator, ok := any(&result).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return zero, fmt.Errorf("config: validation failed: %w", err)
		}
	}
	return result, nil
}

func loadFile(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("config: open file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return fmt.Errorf("config: read file: %w", err)
	}
	if len(data) > maxFileBytes {
		return errors.New("config: file exceeds 1 MiB")
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
	for i := 0; i < v.NumField(); i++ {
		f, value := v.Type().Field(i), v.Field(i)
		if !f.IsExported() {
			continue
		}
		path := prefix + f.Name
		if err := fn(f, value, path); err != nil {
			return err
		}
		if value.Kind() == reflect.Struct && !value.Addr().Type().Implements(textType) {
			if err := walk(value, path+".", fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func set(v reflect.Value, s string) error {
	if v.Kind() == reflect.Pointer {
		return errors.New("pointer tags are unsupported")
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
