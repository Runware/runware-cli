package serverless

// GpuAvailability is the scheduler provisioning availability for a GPU type.
type GpuAvailability string

const (
	GpuAvailabilityAvailable GpuAvailability = "available"
	GpuAvailabilityReserved  GpuAvailability = "reserved"
	GpuAvailabilityCustom    GpuAvailability = "custom"
)

// GpuPricing is the price for a GPU type.
type GpuPricing struct {
	Currency  string `json:"currency"`
	PerSecond string `json:"perSecond"`
}

// GpuType is a supported GPU hardware type and its pricing.
type GpuType struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Memory       *string         `json:"memory,omitempty"`
	Availability GpuAvailability `json:"availability"`
	Pricing      GpuPricing      `json:"pricing"`
}
