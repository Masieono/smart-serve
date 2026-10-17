package notify

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// SecretSource names where a channel field's effective value came from.
//
// It is surfaced in the UI so an operator can tell a value they typed from one the
// deployment supplies — and, more to the point, can *see* when the environment has
// overridden what is on screen. A silent override is the failure mode this whole mechanism
// has to avoid: the field would look wrong, the send would use something else, and nothing
// would say so.
type SecretSource string

const (
	SourceNone SecretSource = ""
	SourceEnv  SecretSource = "environment"
	SourceFile SecretSource = "file"
	SourceDB   SecretSource = "database"
)

// EnvPrefix is the environment prefix for one channel's overrides.
// Keyed by the channel's **label** rather than its row id, because an id is not something a
// deployment can know: it is assigned on insert, differs between a fresh database and a
// restored one, and changes if a channel is deleted and recreated. The label is what the
// operator actually writes in the compose file, and it is the only identifier that is stable
// across all of that.
//
// The cost is that renaming a channel changes its variable names. That is why the UI displays
// the expected name rather than leaving it to be discovered — a rename is then visible
// immediately instead of silently dropping the override and falling back to the database.
func EnvPrefix(label string) string {
	return "SMARTSERVE_CHANNEL_" + slug(label)
}

// EnvVar is the variable that overrides one config key for one channel.
func EnvVar(label, key string) string {
	return EnvPrefix(label) + "_" + slug(key)
}

// slug uppercases and reduces any run of non-alphanumerics to a single underscore, so
// "Critical alerts", "critical-alerts" and "critical_alerts" all name the same variables.
// Two labels differing only in punctuation therefore collide, which is the intended reading:
// as far as a compose file is concerned they are the same name.
func slug(s string) string {
	var b strings.Builder
	sep := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			b.WriteRune(r - ('a' - 'A'))
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			b.WriteRune(r)
		default:
			sep = true
		}
	}
	return b.String()
}

// Resolve returns the effective value of one channel config key, and where it came from.
//
// Precedence is **environment > mounted file > database**, which is `ADR-013`'s decision. A
// `_FILE` variable names a file to read, so the secret never has to appear in the compose
// file, in the process environment, or in `docker inspect` output.
//
// An unreadable or empty `_FILE` is an **error rather than a fallback**. Silently dropping to
// the database value would send a stale token while the operator believed they had rotated
// it — the same silent-failure shape that made the original undocumented version of this
// feature so damaging.
func Resolve(label, key, stored string) (string, SecretSource, error) {
	name := EnvVar(label, key)

	if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), SourceEnv, nil
	}
	if path, ok := os.LookupEnv(name + "_FILE"); ok && strings.TrimSpace(path) != "" {
		path = strings.TrimSpace(path)
		b, err := os.ReadFile(path)
		if err != nil {
			return "", SourceNone, fmt.Errorf("%s_FILE: %w", name, err)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", SourceNone, fmt.Errorf("%s_FILE: %s is empty", name, path)
		}
		return v, SourceFile, nil
	}
	if stored != "" {
		return stored, SourceDB, nil
	}
	return "", SourceNone, nil
}

// ApplyOverrides replaces each string value in a decoded channel config with its effective
// one, in place.
//
// **Every** string key is eligible, not only the ones the UI flags as secret. That is
// deliberate: one mechanism then covers "keep my bot token in a Docker secret" and "point
// this channel at a different ntfy server per environment", and there is no second list of
// secret field names to keep in step with the adapters as they change.
//
// It is exported because a config must be *validated* with the same overrides it will be
// *sent* with. A token supplied by the environment is absent from the editor's form, so
// validating the form alone would reject the very save that makes the override usable.
func ApplyOverrides(label string, cfg map[string]any) error {
	for key, raw := range cfg {
		stored, ok := raw.(string)
		if !ok {
			continue
		}
		value, source, err := Resolve(label, key, stored)
		if err != nil {
			return err
		}
		if source == SourceEnv || source == SourceFile {
			cfg[key] = value
		}
	}
	return nil
}

// Sources reports, for every string key in a stored config, where its effective value comes
// from. Keys with no stored value and no override are omitted.
//
// It returns no values, only provenance, because the API must not hand resolved secrets to a
// browser: doing so would leak an environment secret into the page, and a subsequent Save
// would write it into the database, quietly promoting a secret that was meant to stay out of
// it.
func Sources(label string, cfg map[string]any) map[string]SecretSource {
	out := map[string]SecretSource{}
	for key, raw := range cfg {
		stored, ok := raw.(string)
		if !ok {
			continue
		}
		_, source, err := Resolve(label, key, stored)
		if err != nil || source == SourceNone {
			continue
		}
		out[key] = source
	}
	return out
}

// channelEnvVarPrefix is the namespace this mechanism owns. Nothing outside it is ever read.
const channelEnvVarPrefix = "SMARTSERVE_CHANNEL_"

// ChannelEnvVars lists the channel override variables set in this process, sorted.
//
// **Names only, never values.** The UI needs them to know which fields the deployment
// supplies, so it can stop demanding a value that will never be typed into the form, and so
// an overridden field reads as supplied rather than as blank. The names are derived from
// channel labels the operator chose, so someone reading the interface learns nothing they
// could not already see.
func ChannelEnvVars() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, channelEnvVarPrefix) {
			continue
		}
		// An empty variable means "not set" everywhere else in this file, so it must not be
		// advertised here either: the UI would drop a required field from its check and the
		// send would then fail for want of a value.
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
