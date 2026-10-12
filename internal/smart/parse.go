// Package smart wraps the smartctl and lsblk command-line tools and normalizes
// their JSON output into model types. It never parses human-readable output.
package smart

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/masieono/smart-serve/internal/model"
)

type rawRoot struct {
	Device          rawDevice `json:"device"`
	ModelName       string    `json:"model_name"`
	ModelFamily     string    `json:"model_family"`
	SerialNumber    string    `json:"serial_number"`
	FirmwareVersion string    `json:"firmware_version"`
	RotationRate    int       `json:"rotation_rate"`
	InterfaceSpeed  *struct {
		Max *struct {
			String string `json:"string"`
		} `json:"max"`
	} `json:"interface_speed"`
	UserCapacity *struct {
		Bytes uint64 `json:"bytes"`
	} `json:"user_capacity"`
	SmartStatus *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current int `json:"current"`
	} `json:"temperature"`
	PowerOnTime *struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	PowerCycleCount *int64 `json:"power_cycle_count"`
	ATAAttributes   *struct {
		Table []rawAttribute `json:"table"`
	} `json:"ata_smart_attributes"`
	NVMe     *rawNVMe `json:"nvme_smart_health_information_log"`
	WWN      *rawWWN  `json:"wwn"`
	Smartctl struct {
		ExitStatus int `json:"exit_status"`
		Messages   []struct {
			Severity string `json:"severity"`
			String   string `json:"string"`
		} `json:"messages"`
	} `json:"smartctl"`
}

type rawDevice struct {
	Name     string `json:"name"`
	InfoName string `json:"info_name"`
	Type     string `json:"type"`
	Protocol string `json:"protocol"`
}

type rawAttribute struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Value  int    `json:"value"`
	Worst  int    `json:"worst"`
	Thresh int    `json:"thresh"`
	Raw    struct {
		Value  uint64 `json:"value"`
		String string `json:"string"`
	} `json:"raw"`
}

type rawNVMe struct {
	CriticalWarning         int   `json:"critical_warning"`
	Temperature             int   `json:"temperature"` // Kelvin
	TemperatureSensors      []int `json:"temperature_sensors"`
	AvailableSpare          int   `json:"available_spare"`
	AvailableSpareThreshold int   `json:"available_spare_threshold"`
	PercentageUsed          int   `json:"percentage_used"`
	DataUnitsRead           int64 `json:"data_units_read"`
	DataUnitsWritten        int64 `json:"data_units_written"`
	HostReads               int64 `json:"host_reads"`
	HostWrites              int64 `json:"host_writes"`
	ControllerBusyTime      int64 `json:"controller_busy_time"`
	PowerCycles             int64 `json:"power_cycles"`
	PowerOnHours            int64 `json:"power_on_hours"`
	UnsafeShutdowns         int64 `json:"unsafe_shutdowns"`
	MediaErrors             int64 `json:"media_errors"`
	NumErrLogEntries        int64 `json:"num_err_log_entries"`
	WarningTempTime         int64 `json:"warning_temp_time"`
	CriticalCompTime        int64 `json:"critical_comp_time"`
}

type rawWWN struct {
	NAA int   `json:"naa"`
	OUI int   `json:"oui"`
	ID  int64 `json:"id"`
}

// ParseSMART normalizes a `smartctl --json` document into model.SmartData.
func ParseSMART(data []byte) (*model.SmartData, error) {
	var r rawRoot
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse smartctl json: %w", err)
	}

	sd := &model.SmartData{
		Model:        strings.TrimSpace(r.ModelName),
		ModelFamily:  strings.TrimSpace(r.ModelFamily),
		Serial:       strings.TrimSpace(r.SerialNumber),
		Firmware:     strings.TrimSpace(r.FirmwareVersion),
		RotationRate: r.RotationRate,
		Bus:          busFromType(r.Device.Type),
	}
	if r.InterfaceSpeed != nil && r.InterfaceSpeed.Max != nil {
		sd.InterfaceSpeed = strings.TrimSpace(r.InterfaceSpeed.Max.String)
	}
	if r.UserCapacity != nil {
		sd.CapacityBytes = r.UserCapacity.Bytes
	}
	if r.SmartStatus != nil {
		passed := r.SmartStatus.Passed
		sd.Passed = &passed
	}
	if r.Temperature != nil && r.Temperature.Current > 0 {
		t := r.Temperature.Current
		sd.TemperatureC = &t
	}
	if r.PowerOnTime != nil {
		h := r.PowerOnTime.Hours
		sd.PowerOnHours = &h
	}
	if r.PowerCycleCount != nil {
		c := *r.PowerCycleCount
		sd.PowerCycles = &c
	}
	if r.ATAAttributes != nil {
		for _, a := range r.ATAAttributes.Table {
			sd.Attributes = append(sd.Attributes, model.Attribute{
				ID:        a.ID,
				Name:      a.Name,
				Value:     a.Value,
				Worst:     a.Worst,
				Threshold: a.Thresh,
				RawValue:  a.Raw.Value,
				RawString: a.Raw.String,
			})
		}
	}
	if r.NVMe != nil {
		sd.NVMe = &model.NVMeHealth{
			CriticalWarning:      r.NVMe.CriticalWarning,
			TemperatureSensors:   r.NVMe.TemperatureSensors,
			AvailableSpare:       r.NVMe.AvailableSpare,
			AvailableSpareThresh: r.NVMe.AvailableSpareThreshold,
			PercentageUsed:       r.NVMe.PercentageUsed,
			DataUnitsRead:        r.NVMe.DataUnitsRead,
			DataUnitsWritten:     r.NVMe.DataUnitsWritten,
			HostReads:            r.NVMe.HostReads,
			HostWrites:           r.NVMe.HostWrites,
			ControllerBusyTime:   r.NVMe.ControllerBusyTime,
			PowerCycles:          r.NVMe.PowerCycles,
			PowerOnHours:         r.NVMe.PowerOnHours,
			UnsafeShutdowns:      r.NVMe.UnsafeShutdowns,
			MediaErrors:          r.NVMe.MediaErrors,
			ErrorLogEntries:      r.NVMe.NumErrLogEntries,
			WarningTempTime:      r.NVMe.WarningTempTime,
			CriticalCompTime:     r.NVMe.CriticalCompTime,
		}
		// The NVMe log temperature is in Kelvin; use it only as a fallback.
		if sd.TemperatureC == nil && r.NVMe.Temperature > 0 {
			t := r.NVMe.Temperature - 273
			sd.TemperatureC = &t
		}
		// NVMe drives often report hours/cycles only inside the health log, so
		// fall back to it when the top-level fields are absent.
		if sd.PowerOnHours == nil && r.NVMe.PowerOnHours > 0 {
			h := r.NVMe.PowerOnHours
			sd.PowerOnHours = &h
		}
		if sd.PowerCycles == nil && r.NVMe.PowerCycles > 0 {
			c := r.NVMe.PowerCycles
			sd.PowerCycles = &c
		}
	}
	if r.WWN != nil {
		sd.WWN = fmt.Sprintf("0x%01x%06x%09x", r.WWN.NAA, r.WWN.OUI, r.WWN.ID)
	}
	for _, m := range r.Smartctl.Messages {
		s := strings.TrimSpace(m.String)
		if s == "" {
			continue
		}
		if sev := strings.ToLower(strings.TrimSpace(m.Severity)); sev == "error" || sev == "warning" {
			s = sev + ": " + s
		}
		sd.Messages = append(sd.Messages, s)
	}
	return sd, nil
}

func busFromType(t string) model.Bus {
	switch strings.ToLower(t) {
	case "nvme":
		return model.BusNVMe
	case "scsi":
		return model.BusSCSI
	case "ata", "sat": // "sat" is a USB-SATA bridge speaking ATA
		return model.BusATA
	default:
		return ""
	}
}
