package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// bundleVersion is the format marker in an exported bundle.
//
// Checked on import, and deliberately a *number in a named field* rather than a guess at the shape:
// handing this endpoint the wrong JSON file should say "this is not a smart-serve export", not
// silently apply nothing and report success.
const bundleVersion = 1

// bundleNote travels with the file, so a bundle found in a drawer a year later explains itself
// without anyone having to find this repository.
const bundleNote = "Device labels, settings and schedules. Channels are deliberately not included: " +
	"their config holds tokens and passwords, and a backup file is exactly the thing that gets " +
	"emailed around."

// configBundle is the exported shape (`FEATURES.md` C7).
type configBundle struct {
	SmartServe int    `json:"smartserve"`
	ExportedAt string `json:"exported_at"`
	Note       string `json:"note"`

	Devices   []bundleDevice    `json:"devices"`
	Settings  map[string]string `json:"settings"`
	Schedules []bundleSchedule  `json:"schedules,omitempty"`
}

// bundleDevice is one drive's labels. Keyed by serial, which is the only identifier that survives a
// host migration — a device path does not, and that is the whole problem being solved.
type bundleDevice struct {
	Serial string `json:"serial"`
	Name   string `json:"name,omitempty"`
	Bay    string `json:"bay,omitempty"`
	Tags   string `json:"tags,omitempty"`
	Notes  string `json:"notes,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// bundleSchedule is one schedule, without the bookkeeping fields: last-run and next-run are facts
// about the install being exported, not configuration.
type bundleSchedule struct {
	Kind         string `json:"kind"`
	EverySeconds *int64 `json:"every_seconds,omitempty"`
	Cron         string `json:"cron,omitempty"`
	Label        string `json:"label,omitempty"`
	Enabled      bool   `json:"enabled"`
}

// handleExportConfig writes the deployment's labels, settings and schedules as a JSON bundle.
//
// The point of it is a host migration: names, bays and tags live in the SQLite volume, which
// survives a restart but not a move, and re-labelling sixteen drives by hand is an evening's work.
//
// Channels are deliberately absent. Their config holds tokens and passwords, and an export file is
// exactly the kind of thing that gets emailed to yourself — the app already refuses to serve secret
// values over the API (`NOTIFICATIONS.md`), and a backup that quietly undid that would be a worse
// trade than the one it saves. The bundle says so in its own `note`.
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	notes, err := s.store.AllNotes(r.Context())
	if err != nil {
		s.fail(w, "list labels", err)
		return
	}
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, "read settings", err)
		return
	}
	schedules, err := s.store.ListSchedules(r.Context())
	if err != nil {
		s.fail(w, "list schedules", err)
		return
	}

	out := configBundle{
		SmartServe: bundleVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Note:       bundleNote,
		Devices:    make([]bundleDevice, 0, len(notes)),
		Settings:   settings,
		Schedules:  make([]bundleSchedule, 0, len(schedules)),
	}
	for _, n := range notes {
		out.Devices = append(out.Devices, bundleDevice{
			Serial: n.Serial, Name: n.Name, Bay: n.Bay, Tags: n.Tags,
			Notes: n.Notes, Hidden: n.Hidden,
		})
	}
	for _, sched := range schedules {
		out.Schedules = append(out.Schedules, bundleSchedule{
			Kind: sched.Kind, EverySeconds: sched.EverySeconds, Cron: sched.Cron,
			Label: sched.Label, Enabled: sched.Enabled,
		})
	}

	download(w, "application/json", "config", "json")
	// The header is already written, so an encode failure has nowhere to go but the log.
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Error("writing the config export", "err", err)
	}
}

// handleImportConfig applies a bundle.
//
// **Merge, with the file winning for what it contains.** A drive or a setting the file does not
// mention is left alone, because the usual reason to import is a fresh install that needs its
// labels back — and a restore that deleted anything it did not recognise would be one you could not
// safely run twice. Schedules are the one exception, and they say so.
func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	var bundle configBundle
	if !decodeJSON(w, r, &bundle) {
		return
	}
	if bundle.SmartServe != bundleVersion {
		writeError(w, http.StatusBadRequest,
			"this is not a smart-serve config export, or it is from a different version")
		return
	}

	applied := map[string]int{"devices": 0, "settings": 0, "schedules": 0}

	for _, d := range bundle.Devices {
		if d.Serial == "" {
			continue
		}
		if err := s.store.SetNotes(r.Context(), store.Notes{
			Serial: d.Serial, Name: d.Name, Bay: d.Bay, Tags: d.Tags,
			Notes: d.Notes, Hidden: d.Hidden,
		}); err != nil {
			s.fail(w, "import labels", err)
			return
		}
		applied["devices"]++
	}

	if len(bundle.Settings) > 0 {
		if err := s.store.SetSettings(r.Context(), bundle.Settings); err != nil {
			s.fail(w, "import settings", err)
			return
		}
		applied["settings"] = len(bundle.Settings)
	}

	// Only when the file carries them: a labels-only bundle must not wipe the schedule list.
	if bundle.Schedules != nil {
		schedules := make([]store.Schedule, 0, len(bundle.Schedules))
		for _, sched := range bundle.Schedules {
			schedules = append(schedules, store.Schedule{
				Kind: sched.Kind, EverySeconds: sched.EverySeconds, Cron: sched.Cron,
				Label: sched.Label, Enabled: sched.Enabled,
			})
		}
		if err := s.store.ReplaceSchedules(r.Context(), schedules); err != nil {
			s.fail(w, "import schedules", err)
			return
		}
		applied["schedules"] = len(schedules)
		// The scheduler arms a schedule that has no next run on its next pass, so it has to be told
		// there is work to do. Without this the imported schedules would sit dormant until a restart
		// — imported successfully, and never run.
		if s.scheduler != nil {
			s.scheduler.Wake()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"applied": applied,
		"note": "Anything the file did not mention was left alone. Schedules are replaced rather " +
			"than merged, when the file carries them.",
	})
}
