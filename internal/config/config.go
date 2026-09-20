// Package config locates the hms home directory and the user configuration
// inside it.
//
// Everything hms persists lives in one directory -- ~/hms by default -- so
// "where is my data" has a single answer, and backing the system up is copying
// one directory. The configuration file sits in that directory rather than
// somewhere else under ~, because a configuration that points at the data is
// useless separated from it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvHome overrides the hms home directory. Tests set it; so can anyone
	// keeping more than one house.
	EnvHome = "HMS_HOME"

	// EnvDBPath points at a specific database file, ignoring the configured
	// one. It stays because it is what scripts and muscle memory already use.
	EnvDBPath = "HMS_DB_PATH"

	// DirName is the hms home directory, relative to the user's home.
	DirName = "hms"

	// FileName is the configuration file inside the hms home directory.
	FileName = ".hms.json"

	// DefaultDBFile is the database hms opens when nothing says otherwise.
	DefaultDBFile = "hms.db"

	// ImportsDirName is where bulk imports are kept, inside the hms home
	// directory. An import is a folder of work in progress -- a schema handed
	// out, a photograph dropped in, a plan that came back -- so it lives beside
	// the database rather than in a scratch directory somewhere, and a backup
	// of ~/hms still contains everything.
	ImportsDirName = "imports"
)

// Config mirrors ~/hms/.hms.json.
type Config struct {
	// Home is the directory the configuration was read from. It is not
	// serialised: a file that recorded its own location would go stale the
	// first time the directory moved.
	Home string `json:"-"`

	// DBFile names the SQLite database inside Home. An absolute path is taken
	// as-is, so a database can stay where it already is.
	DBFile string `json:"db_file"`

	// ImportPlanner is the shell command `hms import` runs to turn the
	// unstructured input of an import into a plan. It is given the import's
	// directory as its working directory, the instructions on stdin, and the
	// paths in the environment (HMS_IMPORT_DIR, HMS_SCHEMA_FILE, HMS_INPUT_DIR,
	// HMS_PLAN_FILE); it is expected to write HMS_PLAN_FILE.
	//
	// Empty means hms does not run anything. That is the honest default: turning
	// a photograph of a receipt into rows is a job for an agent, and hms has no
	// business guessing which one you have. With it empty the workflow simply
	// waits for the plan file to appear, which is the same workflow with a
	// person doing the step by hand.
	ImportPlanner string `json:"import_planner"`
}

// Defaults returns the configuration hms writes on first run.
func Defaults() Config {
	return Config{DBFile: DefaultDBFile}
}

// Home returns the hms home directory: $HMS_HOME when set, else ~/hms.
func Home() (string, error) {
	if dir := os.Getenv(EnvHome); dir != "" {
		return Expand(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, DirName), nil
}

// ImportsDir returns the directory bulk imports are kept in: $HMS_HOME/imports,
// or ~/hms/imports.
//
// Here rather than in the importer for the same reason DBPath is here: where
// hms keeps things is one decision, and a second package that knew the folder
// name would be a second answer waiting to disagree with this one.
func ImportsDir() (string, error) {
	dir, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ImportsDirName), nil
}

// Path returns the configuration file location.
func Path() (string, error) {
	dir, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads the configuration, creating the home directory and the file with
// defaults when absent and filling in any missing keys. Keys hms does not
// recognise are preserved, so a newer version's settings survive an older
// binary rewriting the file.
func Load() (Config, error) {
	dir, err := Home()
	if err != nil {
		return Config{}, err
	}
	path := filepath.Join(dir, FileName)

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Defaults()
		cfg.Home = dir
		if err := write(path, cfg, nil); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	cfg := Defaults()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.Home = dir

	// An empty db_file is a file that says nothing, not a request to open the
	// directory itself. Treat it as absent.
	if strings.TrimSpace(cfg.DBFile) == "" {
		cfg.DBFile = DefaultDBFile
	}

	var unknown map[string]json.RawMessage
	if err := json.Unmarshal(raw, &unknown); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	for k := range known() {
		delete(unknown, k)
	}

	// Rewrite only when the file is missing keys, so hms is self-healing after
	// an upgrade adds a setting without rewriting a file that is already fine.
	if complete, err := hasAllKeys(raw); err == nil && !complete {
		if err := write(path, cfg, unknown); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// DBPath is the database this configuration points at.
func (c Config) DBPath() string {
	file := Expand(c.DBFile)
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(c.Home, file)
}

// Resolution is a database path and what decided it, so `hms info` can name
// the thing to edit rather than a file it never read.
type Resolution struct {
	// Path is the database to open.
	Path string

	// Source is the flag, the environment variable, or the configuration file
	// that produced Path.
	Source string
}

// Resolve works out where the database lives, in precedence order: an explicit
// override (the --db-path flag), then $HMS_DB_PATH, then the configured file
// inside the hms home directory.
//
// The first two return without touching the configuration, so pointing hms at
// a scratch database does not create a home directory as a side effect.
func Resolve(override string) (Resolution, error) {
	if override != "" {
		return Resolution{Path: Expand(override), Source: "--db-path"}, nil
	}
	if path := os.Getenv(EnvDBPath); path != "" {
		return Resolution{Path: Expand(path), Source: EnvDBPath}, nil
	}
	cfg, err := Load()
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Path: cfg.DBPath(), Source: filepath.Join(cfg.Home, FileName)}, nil
}

// Expand resolves a leading ~ against the user's home directory.
func Expand(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	return path
}

func known() map[string]struct{} {
	blob, err := json.Marshal(Defaults())
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil
	}
	keys := make(map[string]struct{}, len(m))
	for k := range m {
		keys[k] = struct{}{}
	}
	return keys
}

func hasAllKeys(raw []byte) (bool, error) {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return false, err
	}
	for k := range known() {
		if _, ok := present[k]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func write(path string, cfg Config, extra map[string]json.RawMessage) error {
	blob, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(blob, &merged); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	for k, v := range extra {
		merged[k] = v
	}

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
