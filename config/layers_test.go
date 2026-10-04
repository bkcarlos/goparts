package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

type nestedSettings struct {
	Port int    `json:"port" yaml:"port" default:"42"`
	URL  string `json:"url" yaml:"url" url:"true"`
}

func (n *nestedSettings) Validate() error {
	if n.Port < 1 {
		return errors.New("bad port")
	}
	return nil
}
func TestLayersPointersAndRules(t *testing.T) {
	type C struct {
		Server   *nestedSettings     `json:"server" yaml:"server"`
		Optional *struct{ X string } `json:"optional"`
		Mode     string              `json:"mode" yaml:"mode" default:"dev" oneof:"dev,prod"`
		N        *int                `default:"5"`
		Tags     map[string]string   `json:"tags" yaml:"tags"`
	}
	fs := fstest.MapFS{"defaults.yaml": {Data: []byte("server:\n  url: https://example.com\ntags:\n  a: a\n")}}
	p := filepath.Join(t.TempDir(), "overlay.yaml")
	os.WriteFile(p, []byte("server:\n  port: 80\ntags:\n  b: b\n"), 0600)
	c, err := Load[C](Options{Defaults: fs, DefaultsFile: "defaults.yaml", Files: []string{p}})
	if err != nil || c.Server.Port != 80 || c.Optional != nil || *c.N != 5 || len(c.Tags) != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	os.WriteFile(p, []byte("server:\n  port: 0\n"), 0600)
	if _, err = Load[C](Options{Defaults: fs, DefaultsFile: "defaults.yaml", File: p}); err == nil {
		t.Fatal("nested validation skipped")
	}
}
func TestOptionalRecursiveAndRegistry(t *testing.T) {
	type Node struct{ Next *Node }
	v, err := Load[Node](Options{})
	if err != nil || v.Next != nil {
		t.Fatal(err)
	}
	r, err := LoadRegistry[struct {
		X string `yaml:"x"`
	}](fstest.MapFS{"a.yml": {Data: []byte("x: a")}, "b.yml": {Data: []byte("x: b")}}, "*.yml", Options{})
	if err != nil || len(r) != 2 || r["b.yml"].X != "b" {
		t.Fatal(r, err)
	}
}
