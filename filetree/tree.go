// Package filetree walks local trees without following symlinks.
package filetree

import (
	"io/fs"
	"path/filepath"
	"strings"
)

type Filter func(relative string, entry fs.DirEntry) bool
type Node struct {
	Name      string
	Path      string
	Directory bool
	Size      int64
	Children  []*Node
}

func GetFileTree(root string, filters ...Filter) (*Node, error) {
	var result *Node
	nodes := map[string]*Node{}
	filter := Combine(filters...)
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel != "." && !filter(filepath.ToSlash(rel), e) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		node := &Node{Name: e.Name(), Path: path, Directory: e.IsDir(), Size: info.Size()}
		if rel == "." {
			result = node
		} else {
			parent := nodes[filepath.Dir(path)]
			parent.Children = append(parent.Children, node)
		}
		if e.IsDir() {
			nodes[path] = node
		}
		return nil
	})
	return result, err
}
func GetFileTreeFilesOnly(root string, filters ...Filter) ([]string, error) {
	tree, err := GetFileTree(root, filters...)
	if err != nil {
		return nil, err
	}
	var paths []string
	var walk func(*Node)
	walk = func(n *Node) {
		if !n.Directory {
			paths = append(paths, n.Path)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	if tree != nil {
		walk(tree)
	}
	return paths, nil
}
func Combine(filters ...Filter) Filter {
	return func(p string, e fs.DirEntry) bool {
		for _, f := range filters {
			if f != nil && !f(p, e) {
				return false
			}
		}
		return true
	}
}
func FilterByExtension(extensions ...string) Filter {
	return func(p string, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		ext := filepath.Ext(p)
		for _, wanted := range extensions {
			if !strings.HasPrefix(wanted, ".") {
				wanted = "." + wanted
			}
			if strings.EqualFold(ext, wanted) {
				return true
			}
		}
		return false
	}
}

// Pattern is filepath.Match on the base name; invalid patterns return an error.
func FilterByPattern(pattern string) (Filter, error) {
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}
	return func(p string, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		ok, _ := filepath.Match(pattern, filepath.Base(p))
		return ok
	}, nil
}
func FilterByPrefix(prefix string) Filter {
	return func(p string, e fs.DirEntry) bool { return e.IsDir() || strings.HasPrefix(e.Name(), prefix) }
}
func FilterBySuffix(suffix string) Filter {
	return func(p string, e fs.DirEntry) bool { return e.IsDir() || strings.HasSuffix(e.Name(), suffix) }
}
func ExcludeHidden(p string, e fs.DirEntry) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
