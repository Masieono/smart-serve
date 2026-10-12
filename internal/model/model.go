// Package model defines the domain types shared across Smart-Serve.
package model

// Verdict is the human-facing health state of a device.
type Verdict string

const (
	Good    Verdict = "good"
	Caution Verdict = "caution"
	Bad     Verdict = "bad"
	Unknown Verdict = "unknown"
)

// Bus identifies the S.M.A.R.T. transport family.
type Bus string

const (
	BusATA  Bus = "ata"
	BusNVMe Bus = "nvme"
	BusSCSI Bus = "scsi"
)

// Attribute is one normalized ATA S.M.A.R.T. attribute row.
type Attribute struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Value     int    `json:"value"`
	Worst     int    `json:"worst"`
	Threshold int    `json:"threshold"`
	RawValue  uint64 `json:"raw_value"`
	RawString string `json:"raw_string"`
}

// NVMeDataUnitBytes is one NVMe "data unit": 1,000 sectors of 512 bytes, per the
// NVMe specification's Data Units Read/Written fields.
const NVMeDataUnitBytes = 1000 * 512

// NVMeHealth mirrors the NVMe SMART/Health Information log.
type NVMeHealth struct {
	CriticalWarning      int   `json:"critical_warning"`
	TemperatureSensors   []int `json:"temperature_sensors,omitempty"`
	AvailableSpare       int   `json:"available_spare"`
	AvailableSpareThresh int   `json:"available_spare_threshold"`
	PercentageUsed       int   `json:"percentage_used"`
	DataUnitsRead        int64 `json:"data_units_read"`
	DataUnitsWritten     int64 `json:"data_units_written"`
	HostReads            int64 `json:"host_reads"`
	HostWrites           int64 `json:"host_writes"`
	ControllerBusyTime   int64 `json:"controller_busy_time"`
	PowerCycles          int64 `json:"power_cycles"`
	PowerOnHours         int64 `json:"power_on_hours"`
	UnsafeShutdowns      int64 `json:"unsafe_shutdowns"`
	MediaErrors          int64 `json:"media_errors"`
	ErrorLogEntries      int64 `json:"num_err_log_entries"`
	WarningTempTime      int64 `json:"warning_temp_time"`
	CriticalCompTime     int64 `json:"critical_comp_time"`
}

// SmartData is the normalized S.M.A.R.T. payload for a single device.
type SmartData struct {
	Model          string      `json:"model"`
	ModelFamily    string      `json:"model_family,omitempty"`
	Serial         string      `json:"serial"`
	Firmware       string      `json:"firmware"`
	CapacityBytes  uint64      `json:"capacity_bytes"`
	RotationRate   int         `json:"rotation_rate"`
	InterfaceSpeed string      `json:"interface_speed,omitempty"`
	WWN            string      `json:"wwn,omitempty"`
	Bus            Bus         `json:"bus"`
	Passed         *bool       `json:"passed,omitempty"`
	TemperatureC   *int        `json:"temperature_c,omitempty"`
	PowerOnHours   *int64      `json:"power_on_hours,omitempty"`
	PowerCycles    *int64      `json:"power_cycles,omitempty"`
	Attributes     []Attribute `json:"attributes,omitempty"`
	NVMe           *NVMeHealth `json:"nvme,omitempty"`
	// Messages holds smartctl's own messages (errors/warnings); used to explain
	// why a device could not be read.
	Messages []string `json:"messages,omitempty"`
}

// AttributeByID returns a pointer to the attribute with the given ID, or nil.
func (s *SmartData) AttributeByID(id int) *Attribute {
	for i := range s.Attributes {
		if s.Attributes[i].ID == id {
			return &s.Attributes[i]
		}
	}
	return nil
}
