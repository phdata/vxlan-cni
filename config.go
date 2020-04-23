package vxlan

import (
	"encoding/json"

	"gitlab.travishegner.com/travishegner/cni/cni"
)

// Config is the cni config extended with our required attributes
type Config struct {
	*cni.Config
	DefaultNetwork          string   `json:"defaultNetwork"`
	K8sNetworkFromNamespace bool     `json:"k8sNetworkFromNamespace"`
	K8sReadAnnotations      bool     `json:"k8sReadAnnotations"`
	K8sConfigPath           string   `json:"k8sConfigPath"`
	Vxlans                  []*Vxlan `json:"vxlans"`
}

// NewConfig returns a new vxlan config from the byte array
func NewConfig(confBytes []byte) (*Config, error) {
	conf := &Config{}
	err := json.Unmarshal(confBytes, conf)
	if err != nil {
		return nil, err
	}

	return conf, nil
}
