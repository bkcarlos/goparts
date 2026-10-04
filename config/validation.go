package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

func validateRules(f reflect.StructField, v reflect.Value, path string) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	fail := func(rule string) error { return fmt.Errorf("config: %s failed %s validation", path, rule) }
	for _, rule := range []string{"url", "host", "oneof", "regex"} {
		spec, ok := f.Tag.Lookup(rule)
		if !ok || spec == "false" {
			continue
		}
		if v.Kind() != reflect.String {
			return fail(rule)
		}
		value := v.String()
		switch rule {
		case "url":
			u, err := url.Parse(value)
			if spec != "true" || err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
				return fail(rule)
			}
		case "host":
			if spec != "true" || !validHost(value) {
				return fail(rule)
			}
		case "oneof":
			found := false
			for _, allowed := range strings.Split(spec, ",") {
				if strings.TrimSpace(allowed) == value {
					found = true
				}
			}
			if !found {
				return fail(rule)
			}
		case "regex":
			r, err := regexp.Compile(spec)
			if err != nil || !r.MatchString(value) {
				return fail(rule)
			}
		}
	}
	return nil
}
func validHost(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func validateNested(v reflect.Value, depth int) error {
	if depth > 64 {
		return errors.New("config: nesting exceeds limit")
	}
	for i := 0; i < v.NumField(); i++ {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		value := v.Field(i)
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				continue
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			continue
		}
		if err := validateNested(value, depth+1); err != nil {
			return err
		}
		if validator, ok := value.Addr().Interface().(Validator); ok {
			if err := validator.Validate(); err != nil {
				return fmt.Errorf("config: %s validation failed: %w", v.Type().Field(i).Name, err)
			}
		}
	}
	return nil
}

// LoadRegistry loads matching FS files in lexical order. Keys are complete FS
// paths, preventing basename collisions. Each entry gets defaults/env/validation.
func LoadRegistry[T any](filesystem fs.FS, pattern string, opts Options) (map[string]T, error) {
	if filesystem == nil {
		return nil, errors.New("config: FS required")
	}
	paths, err := fs.Glob(filesystem, pattern)
	if err != nil {
		return nil, err
	}
	out := map[string]T{}
	for _, p := range paths {
		entryOpts := opts
		entryOpts.Defaults = filesystem
		entryOpts.DefaultsFile = p
		entry, err := Load[T](entryOpts)
		if err != nil {
			return nil, fmt.Errorf("config: registry %s: %w", p, err)
		}
		out[p] = entry
	}
	return out, nil
}
