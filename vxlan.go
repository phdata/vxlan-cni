package vxlan

// Vxlan represents the configuration for an overlay broadcast domain
type Vxlan struct {
	ID           int               `json:"id"`
	Name         string            `json:"name"`
	Cidr         string            `json:"cidr"`
	ExcludeFirst int               `json:"excludeFirst"`
	ExcludeLast  int               `json:"excludeLast"`
	Options      map[string]string `json:"options"`
	MTU          int               `json:"mtu"`
}
