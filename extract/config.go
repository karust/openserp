package extract

import "time"

type Config struct {
	Enabled              bool          `json:"enabled" mapstructure:"enabled"`
	DefaultMode          string        `json:"default_mode" mapstructure:"default_mode"`
	Timeout              time.Duration `json:"timeout" mapstructure:"timeout"`
	MaxBytes             int           `json:"max_bytes" mapstructure:"max_bytes"`
	MaxConcurrent        int           `json:"max_concurrent" mapstructure:"max_concurrent"`
	AllowPrivateNetworks bool          `json:"allow_private_networks" mapstructure:"allow_private_networks"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		DefaultMode:   string(ModeAuto),
		Timeout:       20 * time.Second,
		MaxBytes:      2 * 1024 * 1024,
		MaxConcurrent: 2,
	}
}

// BatchTimeout derives the wall-clock ceiling for enriching one search response
// (extract=true) from the per-URL budget. Workers run in ceil(count/MaxConcurrent)
// waves; each worker's worst case is a raw fetch plus a rendered escalation, so a
// single Extract is bounded by 2*Timeout. This keeps the batch bound an explicit
// consequence of Timeout rather than a separate knob that can drift out of sync.
func (c Config) BatchTimeout(count int) time.Duration {
	c = c.Normalized()
	if count <= 0 {
		return c.Timeout
	}
	waves := (count + c.MaxConcurrent - 1) / c.MaxConcurrent
	return time.Duration(waves) * 2 * c.Timeout
}

func (c Config) Normalized() Config {
	def := DefaultConfig()
	if c.DefaultMode == "" {
		c.DefaultMode = def.DefaultMode
	}
	if c.Timeout <= 0 {
		c.Timeout = def.Timeout
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = def.MaxBytes
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = def.MaxConcurrent
	}
	return c
}
