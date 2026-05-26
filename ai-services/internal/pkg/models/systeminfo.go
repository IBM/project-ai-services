package models

// SystemInfo represents system resource information.
type SystemInfo struct {
	CPU          *CPUInfo                    `json:"cpu,omitempty"`
	Memory       *MemoryInfo                 `json:"memory,omitempty"`
	Accelerators map[string]*AcceleratorInfo `json:"accelerators,omitempty"`
}

// CPUInfo represents CPU utilization information.
type CPUInfo struct {
	TotalCores     int     `json:"total_cores"`
	AvailableCores float64 `json:"available_cores"`
}

// MemoryInfo represents memory usage information.
type MemoryInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
}

// AcceleratorInfo represents accelerator availability information.
type AcceleratorInfo struct {
	Total     int `json:"total"`
	Available int `json:"available"`
}

// Made with Bob
