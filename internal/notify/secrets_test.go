package notify

import (
	"os"
	"path/filepath"
	"testing"
)

// The variable name is the operator's whole interface to this feature, so it has to be
// predictable from the label they typed. Punctuation is normalised away rather than escaped,
// which means "Critical alerts", "critical-alerts" and "critical_alerts" name the same
// variables — that is the intended reading, not an accident.
func TestSlugMakesAStableVariableName(t *testing.T) {
	cases := map[string]string{
		"Critical alerts": "CRITICAL_ALERTS",
		"critical-alerts": "CRITICAL_ALERTS",
		"critical_alerts": "CRITICAL_ALERTS",
		"  spaced  out  ": "SPACED_OUT",
		"Pool #1 (media)": "POOL_1_MEDIA",
		"AlreadyCAPS":     "ALREADYCAPS",
		"":                "",
		"!!!":             "",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnvVarNamesOneChannelAndOneField(t *testing.T) {
	if got, want := EnvPrefix("Critical alerts"), "SMARTSERVE_CHANNEL_CRITICAL_ALERTS"; got != want {
		t.Errorf("EnvPrefix = %q, want %q", got, want)
	}
	if got, want := EnvVar("Critical alerts", "token"), "SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN"; got != want {
		t.Errorf("EnvVar = %q, want %q", got, want)
	}
	// The whole reason the key is the label rather than the channel type: two ntfy channels
	// must not fight over one variable.
	if EnvVar("Critical alerts", "token") == EnvVar("Weekly digest", "token") {
		t.Error("two channels resolve to the same variable name")
	}
}

func TestResolvePrefersEnvThenFileThenDatabase(t *testing.T) {
	const label, key = "Critical alerts", "token"
	t.Setenv(EnvVar(label, key), "")

	if v, src, err := Resolve(label, key, "from-db"); err != nil || v != "from-db" || src != SourceDB {
		t.Errorf("stored only: got (%q, %q, %v)", v, src, err)
	}

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar(label, key)+"_FILE", path)
	if v, src, err := Resolve(label, key, "from-db"); err != nil || v != "from-file" || src != SourceFile {
		t.Errorf("file should beat the database: got (%q, %q, %v)", v, src, err)
	}

	// Trimmed on the way out: a token pasted into a compose file routinely arrives with a
	// trailing newline, and sending it verbatim fails in a way that reads like a bad token.
	t.Setenv(EnvVar(label, key), " from-env\n")
	if v, src, err := Resolve(label, key, "from-db"); err != nil || v != "from-env" || src != SourceEnv {
		t.Errorf("environment should beat both: got (%q, %q, %v)", v, src, err)
	}
}

// A broken mount must be an error, never a fallback. Falling back would send a stale token
// while the operator believed they had rotated it — the silent-failure shape that made the
// original undocumented version of this feature so damaging.
func TestResolveRejectsAnUnusableSecretFile(t *testing.T) {
	const label, key = "Critical alerts", "token"
	t.Setenv(EnvVar(label, key), "")

	t.Run("missing", func(t *testing.T) {
		t.Setenv(EnvVar(label, key)+"_FILE", filepath.Join(t.TempDir(), "missing"))
		if _, _, err := Resolve(label, key, "from-db"); err == nil {
			t.Fatal("a missing secret file was accepted")
		}
	})

	t.Run("empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvVar(label, key)+"_FILE", path)
		if _, _, err := Resolve(label, key, "from-db"); err == nil {
			t.Fatal("an empty secret file was accepted — the mount happened but the secret did not")
		}
	})
}

func TestApplyOverridesCoversEveryStringField(t *testing.T) {
	const label = "Critical alerts"
	t.Setenv(EnvVar(label, "token"), "secret")
	t.Setenv(EnvVar(label, "topic"), "my-topic")

	cfg := map[string]any{"server": "https://ntfy.sh", "topic": "old", "token": "old", "priority": 3}
	if err := ApplyOverrides(label, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["topic"] != "my-topic" || cfg["token"] != "secret" {
		t.Errorf("overrides not applied: %v", cfg)
	}
	// Every string field is eligible, not only the ones the UI calls secret — that is what
	// makes one mechanism cover "keep the token in a Docker secret" and "point this channel
	// at a different server per environment".
	if cfg["server"] != "https://ntfy.sh" {
		t.Errorf("server = %v, want the stored value", cfg["server"])
	}
	// A non-string field is left alone rather than stringified into something the adapter
	// cannot read.
	if cfg["priority"] != 3 {
		t.Errorf("priority = %v, want the integer 3", cfg["priority"])
	}
}

// Sources exists so the UI can say where a value came from. It returns provenance only, and
// deliberately no values: an environment secret handed to the browser would be written back
// into the database by the next Save.
func TestSourcesReportsProvenanceWithoutValues(t *testing.T) {
	const label = "Critical alerts"
	t.Setenv(EnvVar(label, "token"), "super-secret")

	got := Sources(label, map[string]any{"token": "stored", "topic": "stored", "priority": 3})
	if got["token"] != SourceEnv {
		t.Errorf("token source = %q, want %q", got["token"], SourceEnv)
	}
	if got["topic"] != SourceDB {
		t.Errorf("topic source = %q, want %q", got["topic"], SourceDB)
	}
	if _, ok := got["priority"]; ok {
		t.Errorf("a non-string field was reported: %v", got)
	}
	for key, src := range got {
		if src != SourceEnv && src != SourceFile && src != SourceDB {
			t.Errorf("%s reported %q, which is not one of the known sources", key, src)
		}
	}
}
