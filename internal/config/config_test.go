package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"home-management-system/internal/config"
)

// home points HMS_HOME at a fresh directory and clears HMS_DB_PATH, so a test
// never reads or writes the real ~/hms.
func home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvHome, dir)
	t.Setenv(config.EnvDBPath, "")
	return dir
}

// First run has nothing to read, so it writes the defaults rather than failing
// or silently opening a database nobody configured.
func TestLoadCreatesTheFile(t *testing.T) {
	dir := home(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DBFile != config.DefaultDBFile {
		t.Errorf("db_file = %q, want %q", cfg.DBFile, config.DefaultDBFile)
	}
	if want := filepath.Join(dir, config.DefaultDBFile); cfg.DBPath() != want {
		t.Errorf("DBPath() = %q, want %q", cfg.DBPath(), want)
	}

	raw, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("what was written is not JSON: %v", err)
	}
	if written["db_file"] != config.DefaultDBFile {
		t.Errorf("written db_file = %v, want %q", written["db_file"], config.DefaultDBFile)
	}
	// Home is where the file was found. Recording it inside the file would go
	// stale the first time the directory moved.
	if _, ok := written["Home"]; ok {
		t.Error("Home was serialised into the configuration file")
	}
}

func TestLoadReadsAConfiguredFile(t *testing.T) {
	dir := home(t)
	writeConfig(t, dir, `{"db_file": "house.db"}`)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(dir, "house.db"); cfg.DBPath() != want {
		t.Errorf("DBPath() = %q, want %q", cfg.DBPath(), want)
	}
}

// An absolute db_file keeps a database where it already is, which is the whole
// reason it is a path and not just a name.
func TestAbsoluteDBFileIsTakenAsIs(t *testing.T) {
	dir := home(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.db")
	writeConfig(t, dir, `{"db_file": `+quote(elsewhere)+`}`)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DBPath() != elsewhere {
		t.Errorf("DBPath() = %q, want %q", cfg.DBPath(), elsewhere)
	}
}

// An empty value is a file that says nothing, not a request to open the
// directory itself -- which is what joining "" would produce.
func TestEmptyDBFileFallsBackToTheDefault(t *testing.T) {
	dir := home(t)
	writeConfig(t, dir, `{"db_file": "   "}`)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(dir, config.DefaultDBFile); cfg.DBPath() != want {
		t.Errorf("DBPath() = %q, want %q", cfg.DBPath(), want)
	}
}

// A setting this version does not know about belongs to a version that does.
// Rewriting the file must not drop it.
func TestUnknownKeysSurvive(t *testing.T) {
	dir := home(t)
	writeConfig(t, dir, `{"future_setting": "keep me"}`)

	if _, err := config.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	var after map[string]any
	raw, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if after["future_setting"] != "keep me" {
		t.Errorf("future_setting = %v, want %q", after["future_setting"], "keep me")
	}
	// The missing key is what triggered the rewrite, so it should be there now.
	if after["db_file"] != config.DefaultDBFile {
		t.Errorf("db_file = %v, want %q", after["db_file"], config.DefaultDBFile)
	}
}

// A file that is already complete is left alone, so hms does not rewrite a
// hand-formatted configuration on every run.
func TestACompleteFileIsNotRewritten(t *testing.T) {
	dir := home(t)
	path := filepath.Join(dir, config.FileName)
	original := `{"db_file":"house.db"}`
	writeConfig(t, dir, original)

	if _, err := config.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != original {
		t.Errorf("the file was rewritten:\n got %q\nwant %q", raw, original)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	dir := home(t)
	writeConfig(t, dir, `{"db_file": `)

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error for a malformed configuration file")
	}
}

// The precedence the flag, the environment, and the file are documented to
// have. An override must not create the home directory as a side effect, and
// it must not be reported as having come from a file nobody read.
func TestResolvePrecedence(t *testing.T) {
	t.Run("the override wins", func(t *testing.T) {
		dir := t.TempDir()
		unused := filepath.Join(dir, "never-created")
		t.Setenv(config.EnvHome, unused)
		t.Setenv(config.EnvDBPath, "/from/env.db")

		got, err := config.Resolve("/from/flag.db")
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "/from/flag.db" {
			t.Errorf("Path = %q, want the flag", got.Path)
		}
		if got.Source != "--db-path" {
			t.Errorf("Source = %q, want %q", got.Source, "--db-path")
		}
		if _, err := os.Stat(unused); !os.IsNotExist(err) {
			t.Error("an explicit --db-path created the hms home directory")
		}
	})

	t.Run("then the environment", func(t *testing.T) {
		dir := t.TempDir()
		unused := filepath.Join(dir, "never-created")
		t.Setenv(config.EnvHome, unused)
		t.Setenv(config.EnvDBPath, "/from/env.db")

		got, err := config.Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "/from/env.db" {
			t.Errorf("Path = %q, want the environment", got.Path)
		}
		if got.Source != config.EnvDBPath {
			t.Errorf("Source = %q, want %q", got.Source, config.EnvDBPath)
		}
		if _, err := os.Stat(unused); !os.IsNotExist(err) {
			t.Error("HMS_DB_PATH created the hms home directory")
		}
	})

	t.Run("then the configuration", func(t *testing.T) {
		dir := home(t)

		got, err := config.Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, config.DefaultDBFile); got.Path != want {
			t.Errorf("Path = %q, want %q", got.Path, want)
		}
		if want := filepath.Join(dir, config.FileName); got.Source != want {
			t.Errorf("Source = %q, want %q", got.Source, want)
		}
	})
}

// HMS_HOME unset means ~/hms, which is the arrangement the whole package is
// about: one directory, under the user's home, named hms.
func TestHomeDefaultsToTheUserHome(t *testing.T) {
	t.Setenv(config.EnvHome, "")

	got, err := config.Home()
	if err != nil {
		t.Fatal(err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	if want := filepath.Join(userHome, config.DirName); got != want {
		t.Errorf("Home() = %q, want %q", got, want)
	}
}

func TestExpandResolvesTilde(t *testing.T) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	for _, tc := range []struct{ in, want string }{
		{"~", userHome},
		{"~/hms/hms.db", filepath.Join(userHome, "hms", "hms.db")},
		{"/absolute/hms.db", "/absolute/hms.db"},
		{"relative.db", "relative.db"},
		// Not a tilde reference: ~other is another user's home, which this
		// does not pretend to resolve.
		{"~other/hms.db", "~other/hms.db"},
	} {
		if got := config.Expand(tc.in); got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
