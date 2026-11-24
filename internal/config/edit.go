package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// File is mira.yaml as Mira's page edits it (docs/spec/editing.md): the
// only truth. An edit reads the file afresh, changes the entry in its YAML
// tree, so comments and order stay, checks the whole result as Mira would
// load it, and writes it through a temporary file.
type File struct {
	Path    string
	Secrets Secrets
}

// ConfigPath is the file Mira reads and the page writes: the given path;
// when there is no file there and its folder cannot be written (a container's
// /etc/mira), mira.yaml in the data folder.
func ConfigPath(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if dirWritable(filepath.Dir(path)) {
		return path
	}
	if alt := dataPath("mira.yaml"); alt != path {
		return alt
	}
	return path
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".mira-write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// Writable says why the file cannot be written, or nil.
func (f File) Writable() error {
	st, err := os.Stat(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		if dirWritable(filepath.Dir(f.Path)) {
			return nil
		}
		return fmt.Errorf("%s cannot be created: its folder is read-only", f.Path)
	}
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a folder", f.Path)
	}
	fh, err := os.OpenFile(f.Path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("%s is read-only: change it by hand", f.Path)
	}
	fh.Close()
	return nil
}

// Read is the file's top mapping; a missing or empty file is an empty one.
func (f File) Read() (*yaml.Node, error) {
	raw, err := os.ReadFile(f.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("parse config: the file is no mapping")
	}
	return &doc, nil
}

// fileLocks hold one lock per file, so the page, the schedules and the
// setup never write mira.yaml at the same time (docs/spec/main-in-file.md).
var fileLocks sync.Map

// LockWaits, when set, is told that a write waits for another one on the
// same file; only tests set it, to force an order by a signal.
var LockWaits func(path string)

func (f File) lock() func() {
	path := filepath.Clean(f.Path)
	m, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	if !mu.TryLock() {
		if LockWaits != nil {
			LockWaits(path)
		}
		mu.Lock()
	}
	return mu.Unlock
}

// Edit applies fn to the file's top mapping, checks the result as Mira
// would load it and writes it. Nothing is written when fn or the check
// fails; the error says why.
func (f File) Edit(fn func(top *yaml.Node) error) error {
	defer f.lock()()
	return f.edit(fn, nil, true)
}

// Change writes secrets (when given) and then edits the file; when the edit
// fails, the secrets go back as they were (docs/spec/editing.md, decision 2).
// An empty value removes a secret.
func (f File) Change(secrets map[string]string, fn func(top *yaml.Node) error) error {
	defer f.lock()()
	if len(secrets) == 0 {
		return f.edit(fn, nil, true)
	}
	all, err := f.Secrets.Load()
	if err != nil {
		return fmt.Errorf("%s: %w", f.Secrets.Path, err)
	}
	before := map[string]string{}
	for k := range secrets {
		before[k] = all[k]
	}
	if err := f.Secrets.Set(secrets); err != nil {
		return fmt.Errorf("%s: %w", f.Secrets.Path, err)
	}
	if err := f.edit(fn, nil, true); err != nil {
		if err := f.Secrets.Set(before); err != nil {
			// docs/spec/fallbacks.md, row 31.
			slog.Error("the change failed and its secrets could not be put back; the secrets file holds the new ones",
				"file", f.Secrets.Path, "error", err)
		}
		return err
	}
	return nil
}

// Check is Change without writing anything: whether both files can be
// written and the result loads as Mira would read it.
func (f File) Check(secrets map[string]string, fn func(top *yaml.Node) error) error {
	defer f.lock()()
	if len(secrets) > 0 {
		if _, err := f.Secrets.Load(); err != nil {
			return fmt.Errorf("%s: %w", f.Secrets.Path, err)
		}
		if dir := filepath.Dir(f.Secrets.Path); !dirWritable(dir) {
			return fmt.Errorf("%s cannot be written: its folder is read-only", f.Secrets.Path)
		}
	}
	return f.edit(fn, secrets, false)
}

// edit is Edit under the lock; extra are secrets not written yet, for the
// check of the result; write false only checks.
func (f File) edit(fn func(top *yaml.Node) error, extra map[string]string, write bool) error {
	if f.Path == "" {
		return errors.New("there is no mira.yaml to write")
	}
	if err := f.Writable(); err != nil {
		return err
	}
	doc, err := f.Read()
	if err != nil {
		return err
	}
	if err := fn(doc.Content[0]); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	enc.Close()
	base := f.Secrets.Lookup(OSLookup)
	lookup := func(name string) (string, bool) {
		if v, ok := extra[name]; ok {
			return v, v != ""
		}
		return base(name)
	}
	if _, err := Parse(buf.Bytes(), lookup); err != nil {
		return err
	}
	if !write {
		return nil
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(f.Path); err == nil {
		mode = st.Mode().Perm()
	}
	return WriteAtomic(f.Path, buf.Bytes(), mode)
}

// MapGet is the value under key in a mapping, or nil.
func MapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// MapSet puts val under key, in place of what was there (keeping the key's
// comments), or at the end.
func MapSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
}

// MapDelete removes key; false if it was not there.
func MapDelete(m *yaml.Node, key string) bool {
	if m == nil {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// MapRename gives an entry another key, keeping its place and value.
func MapRename(m *yaml.Node, from, to string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == from {
			m.Content[i].Value = to
			return true
		}
	}
	return false
}

// MapChild is the mapping under key, made when it is missing or null.
func MapChild(m *yaml.Node, key string) *yaml.Node {
	if c := MapGet(m, key); c != nil && c.Kind == yaml.MappingNode {
		return c
	}
	c := &yaml.Node{Kind: yaml.MappingNode}
	MapSet(m, key, c)
	return c
}

// Keys are a mapping's keys in order.
func Keys(m *yaml.Node) []string {
	var out []string
	if m == nil || m.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		out = append(out, m.Content[i].Value)
	}
	return out
}

// Scalar is a plain value node.
func Scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }

// NodeOf turns a Go value into a node.
func NodeOf(v any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	return &n, nil
}
